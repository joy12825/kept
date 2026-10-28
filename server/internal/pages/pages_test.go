package pages

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"kept/server/internal/apperr"
	"kept/server/internal/files"
	"kept/server/internal/parser"
	"kept/server/internal/pdftest"
	"kept/server/internal/store"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

type env struct {
	t     *testing.T
	ctx   context.Context
	s     *Service
	q     *sqlc.Queries
	fs    *files.Store
	ws    int64
	scope tenant.Scope
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if _, err := exec.LookPath("qpdf"); err != nil || len(parser.MissingTools()) > 0 {
		t.Skip("needs qpdf and poppler")
	}
	data := t.TempDir()
	st, err := store.Open(context.Background(), filepath.Join(data, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	var ws int64
	_ = st.DB.QueryRow(`INSERT INTO workspaces (name, kind, created_at) VALUES ('家', 'family', 1) RETURNING id`).Scan(&ws)
	fs := files.NewStore(data)
	return &env{t: t, ctx: context.Background(), s: NewService(st.DB, fs, Options{}), q: sqlc.New(st.DB), fs: fs, ws: ws, scope: tenant.New(ws, tenant.RoleOwner)}
}

// doc stores a PDF document; with archive, the same PDF is also its archive.
func (e *env) doc(labels []string, archive bool) int64 {
	e.t.Helper()
	pdf := pdftest.Text(labels...)
	sum := sha256.Sum256(pdf)
	id, err := e.q.InsertDocument(e.ctx, sqlc.InsertDocumentParams{
		WorkspaceID: e.ws, Title: "合同", TitleSort: "合同", Content: strings.Join(labels, " "), CreatedDate: "2024-03-05", AddedAt: 1, ModifiedAt: 1,
		MimeType: "application/pdf", OriginalFilename: "合同.pdf", OriginalExt: "pdf", PageCount: int64(len(labels)), Checksum: hex.EncodeToString(sum[:]),
		HasArchive: boolInt(archive), FieldSources: "{}", Source: "web", Storage: files.Local,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	write := func(p string) {
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, pdf, 0o644); err != nil {
			e.t.Fatal(err)
		}
	}
	write(e.fs.Path(files.Original(e.ws, id, "pdf")))
	if archive {
		write(e.fs.Path(files.Archive(e.ws, id)))
	}
	return id
}

func (e *env) row(id int64) sqlc.Document {
	e.t.Helper()
	d, err := e.q.GetDocument(e.ctx, sqlc.GetDocumentParams{WorkspaceID: e.ws, ID: id})
	if err != nil {
		e.t.Fatal(err)
	}
	return d
}

var rotRE = regexp.MustCompile(`Page\s+(\d+)\s+rot:\s+(\d+)`)

// rotations reads each page's rotation with pdfinfo.
func rotations(t *testing.T, pdf string) []string {
	t.Helper()
	out, err := exec.Command("pdfinfo", "-f", "1", "-l", "99", pdf).Output()
	if err != nil {
		t.Fatal(err)
	}
	var r []string
	for _, m := range rotRE.FindAllStringSubmatch(string(out), -1) {
		r = append(r, m[2])
	}
	return r
}

func TestEditPagesOfAnOriginalPDF(t *testing.T) {
	e := newEnv(t)
	id := e.doc([]string{"P1", "P2", "P3"}, false)
	orig, _ := os.ReadFile(e.fs.Path(files.Original(e.ws, id, "pdf")))

	if err := e.s.Set(e.ctx, e.scope, id, Edits{Rotate: map[int]int{1: 90, 3: 180}, Delete: []int{2}}); err != nil {
		t.Fatal(err)
	}
	d := e.row(id)
	if d.HasArchive != 1 || d.PageCount != 2 || strings.Contains(d.Content, "P2") || !strings.Contains(d.Content, "P3") || d.HasThumbnail != 1 {
		t.Fatalf("after editing: archive=%d pages=%d content=%q thumb=%d", d.HasArchive, d.PageCount, d.Content, d.HasThumbnail)
	}
	if got := rotations(t, e.fs.Path(files.Archive(e.ws, id))); strings.Join(got, ",") != "90,180" {
		t.Errorf("rotations = %v", got)
	}
	if ed := ParseEdits(d.PageEdits); ed.Base != "original" || ed.Rotate[1] != 90 || len(ed.Delete) != 1 {
		t.Errorf("saved edits = %+v", ed)
	}
	if now, _ := os.ReadFile(e.fs.Path(files.Original(e.ws, id, "pdf"))); string(now) != string(orig) {
		t.Error("the original changed")
	}
	// The editor still sees the unedited pages.
	if p, err := e.s.Unedited(e.ctx, e.scope, id); err != nil || p.Ref != files.Original(e.ws, id, "pdf") {
		t.Errorf("unedited = %v, %v", p, err)
	}
	// Edits are a state, not a sequence: now only page 3 is deleted.
	if err := e.s.Set(e.ctx, e.scope, id, Edits{Delete: []int{3}}); err != nil {
		t.Fatal(err)
	}
	if d := e.row(id); d.PageCount != 2 || !strings.Contains(d.Content, "P2") || strings.Contains(d.Content, "P3") {
		t.Errorf("second edit: pages=%d content=%q", d.PageCount, d.Content)
	}
	// Back to the original: no archive.
	if err := e.s.Set(e.ctx, e.scope, id, Edits{}); err != nil {
		t.Fatal(err)
	}
	if d := e.row(id); d.HasArchive != 0 || d.PageCount != 3 || d.PageEdits != "" {
		t.Errorf("reset: archive=%d pages=%d edits=%q", d.HasArchive, d.PageCount, d.PageEdits)
	}
	if _, err := os.Stat(e.fs.Path(files.Archive(e.ws, id))); err == nil {
		t.Error("the edited archive should be gone")
	}
}

func TestEditPagesKeepsTheArchiveAside(t *testing.T) {
	e := newEnv(t)
	id := e.doc([]string{"A1", "A2"}, true)
	if err := e.s.Set(e.ctx, e.scope, id, Edits{Rotate: map[int]int{2: 270}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(e.fs.Path(files.Base(e.ws, id))); err != nil {
		t.Fatal("the unedited archive was not kept")
	}
	if ParseEdits(e.row(id).PageEdits).Base != "archive" {
		t.Error("base should be the archive")
	}
	// Reprocessing rebuilds the archive; the edits are applied again on top.
	os.WriteFile(e.fs.Path(files.Archive(e.ws, id)), pdftest.Text("B1", "B2"), 0o644)
	if err := e.s.Reapply(e.ctx, e.ws, id); err != nil {
		t.Fatal(err)
	}
	if d := e.row(id); !strings.Contains(d.Content, "B1") {
		t.Errorf("reapplied content = %q", d.Content)
	}
	if got := rotations(t, e.fs.Path(files.Archive(e.ws, id))); strings.Join(got, ",") != "0,270" {
		t.Errorf("rotations after reprocessing = %v", got)
	}
	if b, _ := os.ReadFile(e.fs.Path(files.Base(e.ws, id))); !strings.Contains(string(b), "B1") {
		t.Error("the new archive should now be the unedited base")
	}
	// Reset puts the unedited archive back.
	if err := e.s.Set(e.ctx, e.scope, id, Edits{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(e.fs.Path(files.Base(e.ws, id))); err == nil {
		t.Error("the base file should be moved back")
	}
	if got := rotations(t, e.fs.Path(files.Archive(e.ws, id))); strings.Join(got, ",") != "0,0" {
		t.Errorf("rotations after reset = %v", got)
	}
}

func TestEditPagesValidates(t *testing.T) {
	e := newEnv(t)
	id := e.doc([]string{"P1", "P2"}, false)
	for _, bad := range []Edits{{Delete: []int{1, 2}}, {Delete: []int{3}}, {Rotate: map[int]int{1: 45}}, {Rotate: map[int]int{0: 90}},
		{Order: []int{2, 2}}, {Order: []int{2}}, {Order: []int{3, 1}}} {
		var ae *apperr.Error
		if err := e.s.Set(e.ctx, e.scope, id, bad); !errors.As(err, &ae) || ae.Kind != apperr.KindInvalid {
			t.Errorf("%+v: %v", bad, err)
		}
	}
	if err := e.s.Set(e.ctx, tenant.New(e.ws+1, tenant.RoleOwner), id, Edits{Delete: []int{1}}); !errors.Is(err, ErrNotFound) {
		t.Errorf("another workspace: %v", err)
	}
}

// v1.3.9: pages can be put in another order; deleted pages may stay in the
// order, and the unedited order is no edit at all.
func TestReorderPages(t *testing.T) {
	e := newEnv(t)
	id := e.doc([]string{"P1", "P2", "P3"}, false)
	if err := e.s.Set(e.ctx, e.scope, id, Edits{Order: []int{3, 1, 2}, Delete: []int{1}}); err != nil {
		t.Fatal(err)
	}
	d := e.row(id)
	i3, i2 := strings.Index(d.Content, "P3"), strings.Index(d.Content, "P2")
	if d.PageCount != 2 || strings.Contains(d.Content, "P1") || i3 < 0 || i2 < 0 || i3 > i2 {
		t.Fatalf("after reordering: pages=%d content=%q", d.PageCount, d.Content)
	}
	if ed := ParseEdits(d.PageEdits); fmt.Sprint(ed.Order) != "[3 1 2]" {
		t.Errorf("saved edits = %+v", ed)
	}
	if h := describe("", "", ParseEdits(d.PageEdits)); !strings.Contains(h, "调整了页序") {
		t.Errorf("history = %s", h)
	}
	// the unedited order is dropped: back to no edits
	if err := e.s.Set(e.ctx, e.scope, id, Edits{Order: []int{1, 2, 3}}); err != nil {
		t.Fatal(err)
	}
	if d := e.row(id); d.PageEdits != "" || d.PageCount != 3 {
		t.Errorf("natural order: edits=%q pages=%d", d.PageEdits, d.PageCount)
	}
}
