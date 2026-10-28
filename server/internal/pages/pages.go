// Package pages edits a document's PDF pages (rotate, delete, reorder) without
// touching the original, and splits and merges documents (v0.2).
package pages

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/events"
	"kept/server/internal/files"
	"kept/server/internal/i18n"
	"kept/server/internal/parser"
	"kept/server/internal/privacy"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// Edits is the requested state of a document's pages, numbered as in the
// unedited PDF. Base says what the unedited PDF is: "archive" (kept as
// <id>.base.pdf meanwhile) or "original".
type Edits struct {
	Rotate map[int]int `json:"rotate,omitempty"` // page -> 90, 180 or 270
	Delete []int       `json:"delete,omitempty"`
	// Order lists every unedited page in the order shown (v1.3.9); empty
	// keeps the unedited order. Deleted pages may stay in it.
	Order []int  `json:"order,omitempty"`
	Base  string `json:"base,omitempty"`
}

// Empty reports whether the edits change nothing.
func (e Edits) Empty() bool { return len(e.Rotate) == 0 && len(e.Delete) == 0 && len(e.Order) == 0 }

// normalized drops an order that is the unedited one.
func (e Edits) normalized() Edits {
	natural := true
	for i, p := range e.Order {
		natural = natural && p == i+1
	}
	if natural {
		e.Order = nil
	}
	return e
}

// ParseEdits reads documents.page_edits ("" is no edits).
func ParseEdits(s string) Edits {
	var e Edits
	if s != "" {
		_ = json.Unmarshal([]byte(s), &e)
	}
	return e
}

func (e Edits) String() string {
	if e.Empty() {
		return ""
	}
	b, _ := json.Marshal(e)
	return string(b)
}

var (
	ErrNotPDF   = apperr.New(apperr.KindInvalid, "只有 PDF 文档可以编辑页面")
	ErrNotFound = apperr.New(apperr.KindNotFound, "找不到这份文档，它可能已被删除")
)

type Options struct {
	Bus *events.Bus
	Now func() time.Time
}

type Service struct {
	db    *sql.DB
	q     *sqlc.Queries
	files *files.Store
	opt   Options
	deps  Deps // split and merge (Register)
}

func NewService(db *sql.DB, fs *files.Store, opt Options) *Service {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	return &Service{db: db, q: sqlc.New(db), files: fs, opt: opt}
}

// unedited returns the PDF the edits apply to, given the current state.
func (s *Service) unedited(d sqlc.Document, cur Edits) (ref files.Ref, base string, err error) {
	ws, id := d.WorkspaceID, d.ID
	if !cur.Empty() {
		if cur.Base == "archive" {
			return files.Base(ws, id), "archive", nil
		}
		return files.Original(ws, id, d.OriginalExt), "original", nil
	}
	if d.HasArchive == 1 {
		return files.Archive(ws, id), "archive", nil
	}
	if d.MimeType == "application/pdf" && d.OriginalEncrypted == 0 {
		return files.Original(ws, id, d.OriginalExt), "original", nil
	}
	return ref, "", ErrNotPDF
}

// Unedited is the PDF the page editor shows (pages numbered as in the
// edits), and whether the document has one.
func (s *Service) Unedited(ctx context.Context, scope tenant.Scope, id int64) (files.Loc, error) {
	d, err := s.get(ctx, scope, id)
	if err != nil {
		return files.Loc{}, err
	}
	r, _, err := s.unedited(d, ParseEdits(d.PageEdits))
	return files.At(d.Storage, r), err
}

// Current is the PDF as it looks now (edited, archive or original PDF),
// what split and merge read.
func (s *Service) Current(d sqlc.Document) (files.Ref, error) {
	if d.HasArchive == 1 {
		return files.Archive(d.WorkspaceID, d.ID), nil
	}
	if d.MimeType == "application/pdf" && d.OriginalEncrypted == 0 {
		return files.Original(d.WorkspaceID, d.ID, d.OriginalExt), nil
	}
	return files.Ref{}, ErrNotPDF
}

// editable is get for a change: a private document only for its person,
// a guardian or a manager (v1.8.0).
func (s *Service) editable(ctx context.Context, scope tenant.Scope, id int64) (sqlc.Document, error) {
	d, err := s.get(ctx, scope, id)
	if err != nil {
		return d, err
	}
	return d, privacy.New(s.db).CheckEdit(ctx, nil, scope, d.Private == 1, d.PersonID)
}

func (s *Service) get(ctx context.Context, scope tenant.Scope, id int64) (sqlc.Document, error) {
	d, err := s.q.GetDocument(ctx, sqlc.GetDocumentParams{WorkspaceID: scope.WorkspaceID(), ID: id, Viewer: scope.Viewer()})
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	return d, err
}

// Set makes a document's pages look like e (empty: as uploaded).
func (s *Service) Set(ctx context.Context, scope tenant.Scope, id int64, e Edits) error {
	d, err := s.editable(ctx, scope, id)
	if err != nil {
		return err
	}
	return s.apply(ctx, d, ParseEdits(d.PageEdits), e, true)
}

// Reapply puts a document's saved edits back on top of a freshly made
// archive (after reprocessing, which rebuilt the files from the original).
func (s *Service) Reapply(ctx context.Context, ws, id int64) error {
	d, err := s.get(ctx, tenant.New(ws, tenant.RoleOwner), id)
	if err != nil {
		return err
	}
	e := ParseEdits(d.PageEdits)
	if e.Empty() {
		return nil
	}
	// Stale: the new archive is the unedited PDF now.
	if err := s.files.Remove(ctx, d.Storage, files.Base(ws, id)); err != nil {
		return err
	}
	return s.apply(ctx, d, Edits{}, e, false)
}

// apply turns the document from state cur into want. The new PDF is made
// and read on this machine first; the stored files change last.
func (s *Service) apply(ctx context.Context, d sqlc.Document, cur, want Edits, record bool) error {
	ws, id := d.WorkspaceID, d.ID
	srcRef, base, err := s.unedited(d, cur)
	if err != nil {
		return err
	}
	tmp, err := s.files.NewTmpDir("pages-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	src, release, err := s.files.Local(ctx, d.Storage, srcRef, tmp)
	if err != nil {
		return err
	}
	defer release()
	pages, err := parser.PDFPages(ctx, src)
	if err != nil {
		return err
	}
	want.Base = base
	if err := validate(want, pages); err != nil {
		return err
	}
	want = want.normalized()
	want.Delete = sorted(want.Delete)
	if record && cur.String() == want.String() {
		return nil // nothing changes
	}

	view := src // what the document shows afterwards
	hasArchive := d.HasArchive == 1
	edited := ""
	switch {
	case want.Empty() && base == "archive": // the unedited archive
		hasArchive = true
	case want.Empty(): // base original: no archive any more
		hasArchive = false
	default:
		edited = filepath.Join(tmp, "edited.pdf")
		if err := transform(ctx, src, edited, want, pages); err != nil {
			return err
		}
		view, hasArchive = edited, true
	}

	n, err := parser.PDFPages(ctx, view)
	if err != nil {
		return err
	}
	text, err := parser.PDFText(ctx, view, n, base == "archive")
	if err != nil {
		return err
	}
	var sum sql.NullString
	if hasArchive {
		if h, err := fileSHA256(view); err == nil {
			sum = sql.NullString{String: h, Valid: true}
		}
	}
	hasThumb := false
	if thumb, err := parser.Thumbnail(ctx, view, tmp); err == nil {
		if err := files.Move(thumb, s.files.ThumbnailPath(ws, id)); err == nil {
			hasThumb = true
		}
	}

	archive := files.Archive(ws, id)
	switch {
	case want.Empty() && base == "archive":
		if !cur.Empty() { // put the unedited archive back
			release()
			if err := s.files.Rename(ctx, d.Storage, files.Base(ws, id), archive); err != nil {
				return err
			}
		}
	case want.Empty():
		if err := s.files.Remove(ctx, d.Storage, archive); err != nil {
			return err
		}
	default:
		if cur.Empty() && base == "archive" { // keep the unedited archive aside
			release()
			if err := s.files.Rename(ctx, d.Storage, archive, files.Base(ws, id)); err != nil {
				return err
			}
		}
		if err := s.files.Put(ctx, d.Storage, archive, edited, true); err != nil {
			return err
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := s.q.WithTx(tx)
	now := s.opt.Now()
	if err := q.SetDocumentPages(ctx, sqlc.SetDocumentPagesParams{
		Content: text, PageCount: int64(n), HasArchive: boolInt(hasArchive), ArchiveChecksum: sum, HasThumbnail: boolInt(hasThumb),
		PageEdits: want.String(), ModifiedAt: now.UnixMilli(), WorkspaceID: ws, ID: id,
	}); err != nil {
		return err
	}
	if record {
		if err := audit.Record(ctx, q, audit.Event{
			WorkspaceID: ws, Action: "document.pages", TargetType: "document", TargetID: strconv.FormatInt(id, 10),
			Summary: describe(audit.ActorFrom(ctx).Name, d.Title, want), At: now,
		}); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if s.opt.Bus != nil {
		s.opt.Bus.Publish(events.Event{Type: events.DocumentUpdated, WorkspaceID: ws, Data: map[string]any{"documentId": id}})
	}
	return nil
}

func validate(e Edits, pages int) error {
	for p, deg := range e.Rotate {
		if p < 1 || p > pages {
			return apperr.New(apperr.KindInvalid, fmt.Sprintf("没有第 %d 页", p))
		}
		if deg != 90 && deg != 180 && deg != 270 {
			return apperr.New(apperr.KindInvalid, "只能旋转 90°、180° 或 270°")
		}
	}
	del := map[int]bool{}
	for _, p := range e.Delete {
		if p < 1 || p > pages {
			return apperr.New(apperr.KindInvalid, fmt.Sprintf("没有第 %d 页", p))
		}
		del[p] = true
	}
	if len(del) >= pages {
		return apperr.New(apperr.KindInvalid, "至少要保留一页")
	}
	if len(e.Order) > 0 {
		seen := map[int]bool{}
		for _, p := range e.Order {
			if p < 1 || p > pages || seen[p] {
				return apperr.New(apperr.KindInvalid, "页序不完整，请刷新后重试")
			}
			seen[p] = true
		}
		if len(seen) != pages {
			return apperr.New(apperr.KindInvalid, "页序不完整，请刷新后重试")
		}
	}
	return nil
}

// transform writes src with e applied to out: rotations first (pages as in
// src), then the pages not deleted, in e's order.
func transform(ctx context.Context, src, out string, e Edits, pages int) error {
	byDeg := map[int][]string{}
	for p, deg := range e.Rotate {
		byDeg[deg] = append(byDeg[deg], strconv.Itoa(p))
	}
	rotated := src
	if len(byDeg) > 0 {
		rotated = out + ".rotated.pdf"
		args := []string{src, rotated}
		for _, deg := range []int{90, 180, 270} {
			if ps := byDeg[deg]; len(ps) > 0 {
				sort.Slice(ps, func(i, j int) bool { a, _ := strconv.Atoi(ps[i]); b, _ := strconv.Atoi(ps[j]); return a < b })
				args = append(args, fmt.Sprintf("--rotate=+%d:%s", deg, strings.Join(ps, ",")))
			}
		}
		if err := qpdf(ctx, args...); err != nil {
			return err
		}
		defer os.Remove(rotated)
	}
	if len(e.Delete) == 0 && len(e.Order) == 0 {
		return files.Copy(rotated, out)
	}
	del := map[int]bool{}
	for _, p := range e.Delete {
		del[p] = true
	}
	order := e.Order
	if len(order) == 0 {
		for p := 1; p <= pages; p++ {
			order = append(order, p)
		}
	}
	var keep []string
	for _, p := range order {
		if !del[p] {
			keep = append(keep, strconv.Itoa(p))
		}
	}
	return qpdf(ctx, "--empty", "--pages", rotated, strings.Join(keep, ","), "--", out)
}

// qpdf runs qpdf; exit status 3 means it worked with warnings.
func qpdf(ctx context.Context, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	b, err := exec.CommandContext(ctx, "qpdf", args...).CombinedOutput()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 3 {
		return nil
	}
	if err != nil {
		return fmt.Errorf("qpdf: %v: %s", err, strings.TrimSpace(string(b)))
	}
	return nil
}

// describe says what the edits do, for the history: “张三 旋转了第 1、3 页，
// 删除了第 2 页（“合同”）”. Each combination is one format, so the history
// can be translated (v1.10.1).
func describe(who, title string, e Edits) string {
	r, d, o := len(e.Rotate) > 0, len(e.Delete) > 0, len(e.Order) > 0
	rp, dp := joinPages(sortedKeys(e.Rotate)), joinPages(sorted(e.Delete))
	switch {
	case r && d && o:
		return fmt.Sprintf(i18n.N("%s 旋转了第 %s 页，删除了第 %s 页，调整了页序（“%s”）"), who, rp, dp, title)
	case r && d:
		return fmt.Sprintf(i18n.N("%s 旋转了第 %s 页，删除了第 %s 页（“%s”）"), who, rp, dp, title)
	case r && o:
		return fmt.Sprintf(i18n.N("%s 旋转了第 %s 页，调整了页序（“%s”）"), who, rp, title)
	case d && o:
		return fmt.Sprintf(i18n.N("%s 删除了第 %s 页，调整了页序（“%s”）"), who, dp, title)
	case r:
		return fmt.Sprintf(i18n.N("%s 旋转了第 %s 页（“%s”）"), who, rp, title)
	case d:
		return fmt.Sprintf(i18n.N("%s 删除了第 %s 页（“%s”）"), who, dp, title)
	case o:
		return fmt.Sprintf(i18n.N("%s 调整了页序（“%s”）"), who, title)
	}
	return fmt.Sprintf(i18n.N("%s 把页面恢复成了原样（“%s”）"), who, title)
}

func joinPages(ps []int) string {
	s := make([]string, len(ps))
	for i, p := range ps {
		s[i] = strconv.Itoa(p)
	}
	return strings.Join(s, "、")
}

func sortedKeys(m map[int]int) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

func sorted(xs []int) []int {
	out := slices.Clone(xs)
	sort.Ints(out)
	return slices.Compact(out)
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
