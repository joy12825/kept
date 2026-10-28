// Package share makes share links (v0.3): a URL that lets anyone holding
// it read one document, until it expires or is revoked. The link's token
// is the credential.
package share

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"kept/server/internal/i18n"
	"strconv"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/document"
	"kept/server/internal/files"
	"kept/server/internal/filter"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
	"kept/server/internal/watermark"
)

const (
	FileArchive  = "archive"
	FileOriginal = "original"

	MaxDays         = 365
	MaxActivePerDoc = 20
	tokenBytes      = 24 // 32 characters of base64url
)

var (
	ErrNotFound = apperr.New(apperr.KindNotFound, "找不到这个分享链接")
	// ErrExpired is all a visitor learns about a link that does not work.
	ErrExpired = apperr.WithCode(apperr.KindNotFound, "share_expired", "这个链接已失效")
)

// Link is a share link as its document's members see it.
type Link struct {
	ID           int64
	DocumentID   int64
	Token        string
	File         string
	ExpiresAt    time.Time
	CreatedAt    time.Time
	CreatorName  string
	AccessCount  int
	LastAccessAt *time.Time
	Protect
}

type Options struct {
	Documents *document.Service
	Files     *files.Store
	Now       func() time.Time
}

type Service struct {
	db  *sql.DB
	q   *sqlc.Queries
	opt Options
}

func NewService(db *sql.DB, opt Options) *Service {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	return &Service{db: db, q: sqlc.New(db), opt: opt}
}

func newToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *Service) List(ctx context.Context, scope tenant.Scope, docID int64) ([]Link, error) {
	if _, err := s.opt.Documents.Get(ctx, scope, docID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListActiveShareLinks(ctx, sqlc.ListActiveShareLinksParams{WorkspaceID: scope.WorkspaceID(), DocumentID: docID, Now: s.opt.Now().UnixMilli()})
	if err != nil {
		return nil, err
	}
	out := make([]Link, len(rows))
	for i, r := range rows {
		out[i] = Link{ID: r.ID, DocumentID: r.DocumentID, Token: r.Token, File: r.File, ExpiresAt: time.UnixMilli(r.ExpiresAt),
			CreatedAt: time.UnixMilli(r.CreatedAt), CreatorName: r.CreatorName, AccessCount: int(r.AccessCount),
			Protect: Protect{Watermark: r.Watermark, AllowDownload: r.AllowDownload == 1}}
		if r.LastAccessAt.Valid {
			t := time.UnixMilli(r.LastAccessAt.Int64)
			out[i].LastAccessAt = &t
		}
	}
	return out, nil
}

// Create makes a link valid for days days to the searchable PDF (the
// original when there is none) or the original. With a watermark (v1.9.2)
// it is always a PDF.
func (s *Service) Create(ctx context.Context, scope tenant.Scope, docID int64, set Settings) (Link, error) {
	days, file := set.Days, set.File
	wm, err := cleanWatermark(set.Watermark)
	if err != nil {
		return Link{}, err
	}
	if days < 1 || days > MaxDays {
		return Link{}, apperr.New(apperr.KindInvalid, fmt.Sprintf("有效期应在 1 到 %d 天之间", MaxDays))
	}
	if file != FileArchive && file != FileOriginal {
		return Link{}, apperr.New(apperr.KindInvalid, "请选择分享可搜索版还是原件")
	}
	d, err := s.opt.Documents.Get(ctx, scope, docID)
	if err != nil {
		return Link{}, err
	}
	if file == FileArchive && !d.HasArchive {
		file = FileOriginal
	}
	if wm != "" {
		f, ok := stampFile(file, d.MimeType, d.HasArchive, d.OriginalEncrypted)
		if !ok {
			return Link{}, apperr.New(apperr.KindInvalid, "这份文档没有 PDF 或图片，不能加水印")
		}
		file = f
		served := d.MimeType
		if f == FileArchive {
			served = "application/pdf"
		}
		if err := checkStamp(served); err != nil {
			return Link{}, err
		}
	}
	ws, now := scope.WorkspaceID(), s.opt.Now()
	token, err := newToken()
	if err != nil {
		return Link{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Link{}, err
	}
	defer tx.Rollback()
	q := s.q.WithTx(tx)
	n, err := q.CountActiveShareLinks(ctx, sqlc.CountActiveShareLinksParams{WorkspaceID: ws, DocumentID: docID, Now: now.UnixMilli()})
	if err != nil {
		return Link{}, err
	}
	if n >= MaxActivePerDoc {
		return Link{}, apperr.New(apperr.KindConflict, fmt.Sprintf("这份文档已经有 %d 个有效的分享链接，请先作废一些", MaxActivePerDoc))
	}
	actor := audit.ActorFrom(ctx)
	expires := now.Add(time.Duration(days) * 24 * time.Hour)
	id, err := q.InsertShareLink(ctx, sqlc.InsertShareLinkParams{WorkspaceID: ws, DocumentID: docID, Token: token, File: file,
		ExpiresAt: expires.UnixMilli(), CreatedBy: sql.NullInt64{Int64: actor.UserID, Valid: actor.UserID != 0}, CreatedAt: now.UnixMilli(),
		Watermark: wm, AllowDownload: boolInt(set.AllowDownload)})
	if err != nil {
		return Link{}, err
	}
	format, extra := protected(shareSummaries, wm, set.AllowDownload)
	if err := audit.Record(ctx, q, audit.Event{WorkspaceID: ws, Action: "document.share", TargetType: "document", TargetID: strconv.FormatInt(docID, 10),
		Summary: fmt.Sprintf(format, append([]any{actor.Name, d.Title, fileWord(file), days}, extra...)...), At: now}); err != nil {
		return Link{}, err
	}
	if err := tx.Commit(); err != nil {
		return Link{}, err
	}
	return Link{ID: id, DocumentID: docID, Token: token, File: file, ExpiresAt: expires, CreatedAt: now, CreatorName: actor.Name,
		Protect: Protect{Watermark: wm, AllowDownload: set.AllowDownload}}, nil
}

// Revoke ends a link at once.
func (s *Service) Revoke(ctx context.Context, scope tenant.Scope, id int64) error {
	ws, now := scope.WorkspaceID(), s.opt.Now()
	l, err := s.q.GetShareLink(ctx, sqlc.GetShareLinkParams{WorkspaceID: ws, ID: id})
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	d, err := s.opt.Documents.Get(ctx, scope, l.DocumentID)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := s.q.WithTx(tx)
	n, err := q.RevokeShareLink(ctx, sqlc.RevokeShareLinkParams{Now: sql.NullInt64{Int64: now.UnixMilli(), Valid: true}, WorkspaceID: ws, ID: id})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	if err := audit.Record(ctx, q, audit.Event{WorkspaceID: ws, Action: "document.unshare", TargetType: "document", TargetID: strconv.FormatInt(l.DocumentID, 10),
		Summary: fmt.Sprintf(i18n.N("%s 作废了“%s”的一个分享链接"), audit.ActorFrom(ctx).Name, d.Title), At: now}); err != nil {
		return err
	}
	return tx.Commit()
}

// Shared is what a visitor of a link may see.
type Shared struct {
	ID         int64
	Title      string
	Filename   string // for downloading
	MimeType   string // of the shared file
	PageCount  int
	Size       int64
	ExpiresAt  time.Time
	File       files.Loc
	ModifiedAt time.Time
	Protect
	created time.Time
	key     string // names the stamped copy
	source  string // the type of File; MimeType is what is served
}

// Open resolves a token; any link that does not work is ErrExpired.
func (s *Service) Open(ctx context.Context, token string) (Shared, error) {
	if len(token) < 20 || len(token) > 64 {
		return Shared{}, ErrExpired
	}
	r, err := s.q.FindShareByToken(ctx, token)
	if errors.Is(err, sql.ErrNoRows) {
		return Shared{}, ErrExpired
	}
	if err != nil {
		return Shared{}, err
	}
	now := s.opt.Now()
	if r.RevokedAt.Valid || r.DocumentDeletedAt.Valid || now.UnixMilli() >= r.ExpiresAt {
		return Shared{}, ErrExpired
	}
	// a document marked sensitive since the link was made (v1.6.4)
	var sensitive bool
	if err := s.db.QueryRowContext(ctx, `SELECT `+filter.SensitiveSQL+` FROM documents d WHERE d.id = ?`, r.DocumentID).Scan(&sensitive); err != nil {
		return Shared{}, err
	}
	if sensitive {
		return Shared{}, ErrExpired
	}
	out := Shared{ID: r.ID, Title: r.Title, PageCount: int(r.PageCount), ExpiresAt: time.UnixMilli(r.ExpiresAt),
		Protect: Protect{Watermark: r.Watermark, AllowDownload: r.AllowDownload == 1}, created: time.UnixMilli(r.CreatedAt)}
	file := r.File
	if out.Watermark != "" {
		file, _ = stampFile(file, r.MimeType, r.HasArchive == 1, false)
	}
	if file == FileArchive && r.HasArchive == 1 {
		out.File, out.MimeType, out.Filename = files.At(r.Storage, files.Archive(r.WorkspaceID, r.DocumentID)), "application/pdf", files.FileName(r.Title, "pdf", map[string]int{})
		out.key = cacheKey("link", r.ID, r.DocumentID, r.ArchiveChecksum)
	} else {
		out.File, out.MimeType, out.Filename = files.At(r.Storage, files.Original(r.WorkspaceID, r.DocumentID, r.OriginalExt)), r.MimeType, files.FileName(r.Title, r.OriginalExt, map[string]int{})
		out.key = cacheKey("link", r.ID, r.DocumentID, r.DocumentChecksum)
	}
	out.source = out.MimeType
	if out.Watermark != "" && out.MimeType != "application/pdf" {
		// a picture with the watermark drawn on (v1.11.2)
		out.MimeType, out.Filename = watermark.ImageType(r.MimeType), servedName(r.Title, r.OriginalExt, r.MimeType)
	}
	return out, nil
}

// Served is the file a visitor gets: with the watermark stamped on when
// the link has one.
func (s *Service) Served(ctx context.Context, sh Shared) (files.Loc, error) {
	if sh.Watermark == "" || (sh.source != "application/pdf" && watermark.ImageType(sh.source) == "") {
		return sh.File, nil
	}
	l, _, err := s.Stamp(ctx, sh.File, sh.source, sh.key, sh.Watermark, footer(sh.Watermark, sh.created))
	return l, err
}

// Touch counts a visit.
func (s *Service) Touch(ctx context.Context, id int64) {
	_ = s.q.TouchShareLink(ctx, sqlc.TouchShareLinkParams{Now: sql.NullInt64{Int64: s.opt.Now().UnixMilli(), Valid: true}, ID: id})
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// fileWord names the shared file in the audit log; the word is a catalog
// entry, so the stored summary reads in English too (v1.10.1).
func fileWord(file string) string {
	if file == FileOriginal {
		return i18n.N("原件")
	}
	return i18n.N("可搜索版")
}

// The audit summaries of a share and a bundle, by how they are protected:
// none, a watermark, view only, both (whole formats, v1.10.1).
var (
	shareSummaries = [4]string{
		i18n.N("%s 分享了“%s”的%s，%d 天有效"),
		i18n.N("%s 分享了“%s”的%s，%d 天有效，加了水印“%s”"),
		i18n.N("%s 分享了“%s”的%s，%d 天有效，只能在线查看"),
		i18n.N("%s 分享了“%s”的%s，%d 天有效，加了水印“%s”、只能在线查看"),
	}
	bundleSummaries = [4]string{
		i18n.N("%s 把 %d 份文档打成分享包“%s”（%s），%d 天有效"),
		i18n.N("%s 把 %d 份文档打成分享包“%s”（%s），%d 天有效，加了水印“%s”"),
		i18n.N("%s 把 %d 份文档打成分享包“%s”（%s），%d 天有效，只能在线查看"),
		i18n.N("%s 把 %d 份文档打成分享包“%s”（%s），%d 天有效，加了水印“%s”、只能在线查看"),
	}
)

// protected picks the summary of how a share is protected, and the
// watermark to fill in.
func protected(formats [4]string, wm string, download bool) (string, []any) {
	i := 0
	var extra []any
	if wm != "" {
		i, extra = 1, []any{wm}
	}
	if !download {
		i += 2
	}
	return formats[i], extra
}
