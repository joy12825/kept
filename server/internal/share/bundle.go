package share

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"kept/server/internal/i18n"
	"strconv"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/files"
	"kept/server/internal/filter"
	"kept/server/internal/privacy"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
	"kept/server/internal/watermark"
)

// Bundles (v1.4.2): one link for several documents, e.g. a batch of
// receipts for the accountant. The visitor sees the list, previews and
// downloads each one, or all of them as a zip.

const (
	MaxBundleDocs    = 200
	MaxActiveBundles = 100 // per workspace
	maxBundleTitle   = 100
)

var errBundleDocs = apperr.New(apperr.KindInvalid, fmt.Sprintf("一个分享包可以放 1 到 %d 份文档", MaxBundleDocs))

// Bundle is a share bundle as the workspace's members see it.
type Bundle struct {
	ID           int64
	Token        string
	Title        string
	File         string
	Documents    int
	ExpiresAt    time.Time
	CreatedAt    time.Time
	CreatorName  string
	AccessCount  int
	LastAccessAt *time.Time
	Protect
}

// RevokeBundle ends a bundle at once.
func (s *Service) RevokeBundle(ctx context.Context, scope tenant.Scope, id int64) error {
	ws, now := scope.WorkspaceID(), s.opt.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var title string
	err = tx.QueryRowContext(ctx, `SELECT title FROM share_bundles WHERE workspace_id = ? AND id = ? AND revoked_at IS NULL`, ws, id).Scan(&title)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE share_bundles SET revoked_at = ? WHERE id = ?`, now.UnixMilli(), id); err != nil {
		return err
	}
	if err := audit.Record(ctx, sqlc.New(tx), audit.Event{WorkspaceID: ws, Action: "share.unbundle", TargetType: "share_bundle", TargetID: strconv.FormatInt(id, 10),
		Summary: fmt.Sprintf(i18n.N("%s 作废了分享包“%s”"), audit.ActorFrom(ctx).Name, title), At: now}); err != nil {
		return err
	}
	return tx.Commit()
}

// Active is every link of the workspace still working, newest first: the
// settings page that shows and ends them in one place.
type Active struct {
	Kind         string // document or bundle
	ID           int64
	Title        string
	DocumentID   int64 // a document's link
	Documents    int   // a bundle's documents still there
	Token        string
	File         string
	ExpiresAt    time.Time
	CreatedAt    time.Time
	CreatorName  string
	AccessCount  int
	LastAccessAt *time.Time
	Protect
}

func (s *Service) ListActive(ctx context.Context, scope tenant.Scope) ([]Active, error) {
	ws, now := scope.WorkspaceID(), s.opt.Now().UnixMilli()
	vis, vargs := privacy.Visible("d", scope)
	rows, err := s.db.QueryContext(ctx, `
		SELECT 'document', l.id, d.title, l.document_id, 1, l.token, l.file, l.expires_at, l.created_at,
			coalesce(nullif(u.display_name, ''), u.username, ''), l.access_count, l.last_access_at, l.watermark, l.allow_download
		FROM share_links l JOIN documents d ON d.id = l.document_id LEFT JOIN users u ON u.id = l.created_by
		WHERE l.workspace_id = ? AND l.revoked_at IS NULL AND l.expires_at > ? AND d.deleted_at IS NULL AND d.workspace_id = l.workspace_id AND `+vis+`
		UNION ALL
		SELECT 'bundle', b.id, b.title, 0,
			(SELECT count(*) FROM share_bundle_documents x JOIN documents d ON d.id = x.document_id
			 WHERE x.bundle_id = b.id AND d.deleted_at IS NULL AND d.workspace_id = b.workspace_id),
			b.token, b.file, b.expires_at, b.created_at, coalesce(nullif(u.display_name, ''), u.username, ''), b.access_count, b.last_access_at, b.watermark, b.allow_download
		FROM share_bundles b LEFT JOIN users u ON u.id = b.created_by
		WHERE b.workspace_id = ? AND b.revoked_at IS NULL AND b.expires_at > ?
		ORDER BY 9 DESC`, append(append([]any{ws, now}, vargs...), ws, now)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Active{}
	for rows.Next() {
		var a Active
		var expires, created int64
		var last sql.NullInt64
		var download int64
		if err := rows.Scan(&a.Kind, &a.ID, &a.Title, &a.DocumentID, &a.Documents, &a.Token, &a.File, &expires, &created, &a.CreatorName, &a.AccessCount, &last, &a.Watermark, &download); err != nil {
			return nil, err
		}
		a.AllowDownload = download == 1
		a.ExpiresAt, a.CreatedAt = time.UnixMilli(expires), time.UnixMilli(created)
		if last.Valid {
			t := time.UnixMilli(last.Int64)
			a.LastAccessAt = &t
		}
		if a.Kind == "bundle" && a.Documents == 0 {
			continue // nothing left to see
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SharedBundle is what a visitor of a bundle may see.
type SharedBundle struct {
	ID        int64
	Title     string
	ExpiresAt time.Time
	Documents []SharedDoc
	Protect
	created time.Time
}

// SharedDoc is one document of a bundle.
type SharedDoc struct {
	ID         int64
	Title      string
	Date       string
	Filename   string
	MimeType   string
	PageCount  int
	Size       int64
	File       files.Loc
	ModifiedAt time.Time
	key        string // names the stamped copy
	source     string // the type of File; MimeType is what is served
}

// OpenBundle resolves a bundle's token; any bundle that does not work, or
// has no document left, is ErrExpired.
func (s *Service) OpenBundle(ctx context.Context, token string) (SharedBundle, error) {
	if len(token) < 20 || len(token) > 64 {
		return SharedBundle{}, ErrExpired
	}
	var b SharedBundle
	var ws, expires, created, download int64
	var revoked sql.NullInt64
	var file string
	err := s.db.QueryRowContext(ctx, `SELECT id, workspace_id, title, file, expires_at, revoked_at, created_at, watermark, allow_download FROM share_bundles WHERE token = ?`, token).
		Scan(&b.ID, &ws, &b.Title, &file, &expires, &revoked, &created, &b.Watermark, &download)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrExpired
	}
	if err != nil {
		return b, err
	}
	if revoked.Valid || s.opt.Now().UnixMilli() >= expires {
		return SharedBundle{}, ErrExpired
	}
	b.ExpiresAt, b.created, b.AllowDownload = time.UnixMilli(expires), time.UnixMilli(created), download == 1
	rows, err := s.db.QueryContext(ctx, `SELECT d.id, d.title, d.created_date, d.mime_type, d.original_ext, d.has_archive, d.page_count, d.modified_at, d.storage,
			d.checksum, coalesce(d.archive_checksum, ''), d.original_encrypted
		FROM share_bundle_documents x JOIN documents d ON d.id = x.document_id
		WHERE x.bundle_id = ? AND d.deleted_at IS NULL AND d.workspace_id = ? AND NOT `+filter.SensitiveSQL+` ORDER BY x.position`, b.ID, ws)
	if err != nil {
		return b, err
	}
	defer rows.Close()
	used := map[string]int{}
	for rows.Next() {
		var d SharedDoc
		var ext, storage, sum, archiveSum string
		var archive, modified, encrypted int64
		if err := rows.Scan(&d.ID, &d.Title, &d.Date, &d.MimeType, &ext, &archive, &d.PageCount, &modified, &storage, &sum, &archiveSum, &encrypted); err != nil {
			return b, err
		}
		d.ModifiedAt = time.UnixMilli(modified)
		f := file
		if b.Watermark != "" {
			f, _ = stampFile(file, d.MimeType, archive == 1, encrypted == 1)
		}
		if f == FileArchive && archive == 1 {
			d.File, d.MimeType, d.Filename = files.At(storage, files.Archive(ws, d.ID)), "application/pdf", files.FileName(d.Title, "pdf", used)
			d.key = cacheKey("bundle", b.ID, d.ID, archiveSum)
		} else {
			d.File, d.Filename = files.At(storage, files.Original(ws, d.ID, ext)), files.FileName(d.Title, ext, used)
			d.key = cacheKey("bundle", b.ID, d.ID, sum)
		}
		d.source = d.MimeType
		if b.Watermark != "" && d.MimeType != "application/pdf" && watermark.ImageType(d.MimeType) != "" {
			// a picture with the watermark drawn on (v1.11.2)
			out := watermark.ImageType(d.MimeType)
			d.MimeType = out
			d.Filename = files.FileName(d.Title, map[string]string{"image/jpeg": "jpg", "image/png": "png"}[out], used)
		}
		if st, err := s.opt.Files.Stat(ctx, d.File.Storage, d.File.Ref); err == nil {
			d.Size = st.Size
		}
		b.Documents = append(b.Documents, d)
	}
	if err := rows.Err(); err != nil {
		return b, err
	}
	if len(b.Documents) == 0 {
		return SharedBundle{}, ErrExpired
	}
	return b, nil
}

// TouchBundle counts a visit.
func (s *Service) TouchBundle(ctx context.Context, id int64) {
	_, _ = s.db.ExecContext(ctx, `UPDATE share_bundles SET access_count = access_count + 1, last_access_at = ? WHERE id = ?`, s.opt.Now().UnixMilli(), id)
}

// SetSummary keeps the summary a doctor sees first with a bundle (v1.8.3):
// a snapshot made when the link was, of what its maker could see.
func (s *Service) SetSummary(ctx context.Context, scope tenant.Scope, bundleID int64, summary []byte) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM share_bundles WHERE id = ? AND workspace_id = ?`, bundleID, scope.WorkspaceID()).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO share_bundle_summaries (bundle_id, summary) VALUES (?, ?)
		ON CONFLICT (bundle_id) DO UPDATE SET summary = excluded.summary`, bundleID, string(summary))
	return err
}

// Summary is a bundle's summary, or nil.
func (s *Service) Summary(ctx context.Context, bundleID int64) ([]byte, error) {
	var out string
	err := s.db.QueryRowContext(ctx, `SELECT summary FROM share_bundle_summaries WHERE bundle_id = ?`, bundleID).Scan(&out)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return []byte(out), err
}

// ServedDoc is the file a visitor of a bundle gets for d: stamped when the
// bundle has a watermark and d is a PDF.
func (s *Service) ServedDoc(ctx context.Context, b SharedBundle, d SharedDoc) (files.Loc, error) {
	if b.Watermark == "" || (d.source != "application/pdf" && watermark.ImageType(d.source) == "") {
		return d.File, nil
	}
	l, _, err := s.Stamp(ctx, d.File, d.source, d.key, b.Watermark, footer(b.Watermark, b.created))
	return l, err
}
