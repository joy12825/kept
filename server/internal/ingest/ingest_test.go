package ingest

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/png"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-pdf/fpdf"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/barcode/barcodetest"
	"kept/server/internal/classify"
	"kept/server/internal/document"
	"kept/server/internal/drawer"
	"kept/server/internal/epubtest"
	"kept/server/internal/events"
	"kept/server/internal/files"
	"kept/server/internal/jobs"
	"kept/server/internal/parser"
	"kept/server/internal/store"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

type env struct {
	t     *testing.T
	s     *Service
	st    *store.Store
	bus   *events.Bus
	files *files.Store
	docs  *document.Service
	scope tenant.Scope
	ctx   context.Context
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(context.Background(), filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, q := range []string{
		`INSERT INTO users (id, username, display_name, password_hash, password_changed_at, created_at, updated_at) VALUES (1, 'wang', '小王', 'x', 1, 1, 1)`,
		`INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'home', 'family', 1), (2, 'other', 'family', 1)`,
		`INSERT INTO document_types (id, workspace_id, name, sort_key, created_at, updated_at) VALUES (5, 1, 'bill', 'bill', 1, 1), (6, 2, 'other', 'other', 1, 1)`,
	} {
		if _, err := st.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	queue := jobs.NewQueue(st.DB)
	pool := jobs.NewPool(queue, jobs.PoolConfig{Workers: 1, PollInterval: 20 * time.Millisecond}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	bus := events.NewBus()
	fs := files.NewStore(dir)
	s := NewService(st.DB, queue, fs, bus, Options{Parser: parser.Config{OCRLanguages: "eng", OCRMode: "skip"}, MaxUploadSize: 1 << 20})
	s.Register(pool)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go pool.Run(ctx)
	actorCtx := audit.WithActor(context.Background(), audit.Actor{UserID: 1, Name: "小王", Via: audit.ViaSession})
	return &env{t: t, s: s, st: st, bus: bus, files: fs, docs: document.NewService(st.DB, document.Options{Bus: bus, Files: fs}), scope: tenant.New(1, tenant.RoleOwner), ctx: actorCtx}
}

// wait returns the final job.updated event of job id.
func (e *env) wait(events <-chan events.Event, id int64) JobEvent {
	e.t.Helper()
	timeout := time.After(60 * time.Second)
	for {
		select {
		case ev := <-events:
			if je, ok := ev.Data.(JobEvent); ok && je.ID == id && (je.Status == "succeeded" || je.Status == "failed") {
				return je
			}
		case <-timeout:
			e.t.Fatalf("job %d did not finish", id)
		}
	}
}

func pdfBytes(t *testing.T, lines ...string) []byte {
	t.Helper()
	p := fpdf.New("P", "mm", "A4", "")
	p.AddPage()
	p.SetFont("Helvetica", "", 18)
	for _, l := range lines {
		p.Cell(0, 12, l)
		p.Ln(14)
	}
	var b bytes.Buffer
	if err := p.Output(&b); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestTextFileIsIngested(t *testing.T) {
	e := newEnv(t)
	sub, cancel := e.bus.Subscribe(1)
	defer cancel()
	id, err := e.s.Submit(e.ctx, e.scope, "../../2024年3月电费账单.txt", strings.NewReader("国家电网\n开票日期：2024年3月5日\n金额 128.50 元"), Overrides{DocumentTypeID: 5}, Upload{})
	if err != nil {
		t.Fatal(err)
	}
	je := e.wait(sub, id)
	if je.Status != "succeeded" || je.DocumentID == 0 || je.Progress != 100 {
		t.Fatalf("job event = %+v", je)
	}
	d, err := e.docs.Get(context.Background(), e.scope, je.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "2024年3月电费账单" || d.CreatedDate != "2024-03-05" || d.MimeType != "text/plain" || !d.Inbox ||
		d.DocumentTypeID != 5 || !strings.Contains(d.Content, "国家电网") || d.OriginalFilename != "2024年3月电费账单.txt" {
		t.Errorf("document = %+v", d)
	}
	if _, err := os.Stat(e.files.Path(files.Original(1, d.ID, "txt"))); err != nil {
		t.Errorf("original missing: %v", err)
	}
	inbox, _ := e.docs.ListInbox(context.Background(), e.scope)
	if len(inbox) != 1 {
		t.Errorf("inbox = %+v", inbox)
	}
	var summary string
	if err := e.st.DB.QueryRow(`SELECT summary FROM audit_events WHERE action = 'document.create'`).Scan(&summary); err != nil || summary != "小王 上传了“2024年3月电费账单.txt”" {
		t.Errorf("audit = %q, %v", summary, err)
	}
	// The working directory is gone once the job is done.
	if entries, _ := os.ReadDir(e.files.TmpRoot()); len(entries) != 0 {
		t.Errorf("tmp left behind: %v", entries)
	}
	// A type of another workspace is ignored.
	id2, _ := e.s.Submit(e.ctx, e.scope, "b.txt", strings.NewReader("另一份"), Overrides{DocumentTypeID: 6}, Upload{})
	if d2, _ := e.docs.Get(context.Background(), e.scope, e.wait(sub, id2).DocumentID); d2.DocumentTypeID != 0 {
		t.Errorf("foreign type applied: %+v", d2)
	}
}

func TestDuplicatesAndLimits(t *testing.T) {
	e := newEnv(t)
	sub, cancel := e.bus.Subscribe(1)
	defer cancel()
	id, err := e.s.Submit(e.ctx, e.scope, "a.txt", strings.NewReader("same content"), Overrides{}, Upload{})
	if err != nil {
		t.Fatal(err)
	}
	docID := e.wait(sub, id).DocumentID
	_, err = e.s.Submit(e.ctx, e.scope, "copy.txt", strings.NewReader("same content"), Overrides{}, Upload{})
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Kind != apperr.KindConflict || ae.Details["documentId"] != docID {
		t.Errorf("duplicate err = %v (%+v)", err, ae)
	}
	// The same file in another workspace is not a duplicate there.
	if _, err := e.s.Submit(e.ctx, tenant.New(2, tenant.RoleOwner), "a.txt", strings.NewReader("same content"), Overrides{}, Upload{}); err != nil {
		t.Errorf("same file in another workspace: %v", err)
	}
	if _, err := e.s.Submit(e.ctx, e.scope, "big.txt", bytes.NewReader(make([]byte, 2<<20)), Overrides{}, Upload{}); !errors.As(err, &ae) || ae.Kind != apperr.KindInvalid {
		t.Errorf("too large err = %v", err)
	}
	if _, err := e.s.Submit(e.ctx, e.scope, "empty.txt", strings.NewReader(""), Overrides{}, Upload{}); !errors.As(err, &ae) || ae.Kind != apperr.KindInvalid {
		t.Errorf("empty err = %v", err)
	}
}

func TestPDFAndBrokenFiles(t *testing.T) {
	if m := parser.MissingTools(); len(m) > 0 {
		t.Skipf("missing tools: %v", m)
	}
	e := newEnv(t)
	sub, cancel := e.bus.Subscribe(1)
	defer cancel()
	id, err := e.s.Submit(e.ctx, e.scope, "bill.pdf", bytes.NewReader(pdfBytes(t, "ACME POWER COMPANY", "Electricity bill", "Issued 2024-03-05", "Total due 128.50 for the account")), Overrides{}, Upload{})
	if err != nil {
		t.Fatal(err)
	}
	je := e.wait(sub, id)
	d, err := e.docs.Get(context.Background(), e.scope, je.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	if d.PageCount != 1 || !d.HasThumbnail || d.HasArchive || d.CreatedDate != "2024-03-05" {
		t.Errorf("pdf document = %+v", d)
	}
	if _, err := os.Stat(e.files.ThumbnailPath(1, d.ID)); err != nil {
		t.Errorf("thumbnail: %v", err)
	}

	// Reprocess keeps the document and its metadata.
	rid, err := e.s.Reprocess(e.ctx, e.scope, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if re := e.wait(sub, rid); re.Status != "succeeded" || re.DocumentID != d.ID {
		t.Errorf("reprocess = %+v", re)
	}

	bad, err := e.s.Submit(e.ctx, e.scope, "broken.pdf", strings.NewReader("%PDF-1.4\n this is not really a pdf"), Overrides{}, Upload{})
	if err != nil {
		t.Fatal(err)
	}
	if je := e.wait(sub, bad); je.Status != "failed" || je.Error != parser.ErrCorrupt.Message {
		t.Errorf("broken pdf = %+v", je)
	}
	if _, err := e.s.Submit(e.ctx, e.scope, "x.docx", bytes.NewReader([]byte("PK\x03\x04\x14\x00\x06\x00 fake office file")), Overrides{}, Upload{}); err != nil {
		t.Fatal(err)
	}
}

func TestCleanTmpKeepsUnfinishedJobs(t *testing.T) {
	e := newEnv(t)
	stale, _ := e.files.NewTmpDir("upload-")
	n, err := e.s.CleanTmp(context.Background())
	if err != nil || n != 1 {
		t.Errorf("CleanTmp = %d, %v", n, err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale directory kept")
	}
}

func TestHelpers(t *testing.T) {
	for in, want := range map[string]string{"../../etc/passwd": "passwd", "C:\\scans\\a.pdf": "a.pdf", "": "未命名文件", "a\x00b.pdf": "ab.pdf"} {
		if got := cleanFilename(in); got != want {
			t.Errorf("cleanFilename(%q) = %q, want %q", in, got, want)
		}
	}
	if titleFrom(".pdf") != "未命名文档" || titleFrom("账单.PDF") != "账单" {
		t.Error("titleFrom")
	}
}

type fakeClassifier struct{ sug classify.Suggestion }

func (f fakeClassifier) Suggest(context.Context, int64, string, string) (classify.Suggestion, error) {
	return f.sug, nil
}

func TestClassificationIsAppliedAndRecorded(t *testing.T) {
	e := newEnv(t)
	for _, q := range []string{
		`INSERT INTO correspondents (id, workspace_id, name, sort_key, created_at, updated_at) VALUES (10, 1, '国家电网', 'g', 1, 1)`,
		`INSERT INTO tags (id, workspace_id, name, sort_key, color, created_at, updated_at) VALUES (100, 1, '水电', 's', 'blue', 1, 1)`,
	} {
		if _, err := e.st.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rule := document.Source{Source: document.SourceRule, Basis: "关键词“国家电网”"}
	e.s.opt.Classifier = fakeClassifier{classify.Suggestion{
		TypeID: 5, CorrespondentID: 10, TagIDs: []int64{100, 999}, // 999 was deleted meanwhile
		Sources: document.FieldSources{
			Type:          &document.Source{Source: document.SourceModel, Confidence: 0.83},
			Correspondent: &rule,
			Tags:          map[int64]document.Source{100: rule, 999: rule},
		},
	}}
	sub, cancel := e.bus.Subscribe(1)
	defer cancel()
	id, err := e.s.Submit(e.ctx, e.scope, "a.txt", strings.NewReader("国家电网 电费\n2024年3月5日"), Overrides{}, Upload{})
	if err != nil {
		t.Fatal(err)
	}
	d, _ := e.docs.Get(context.Background(), e.scope, e.wait(sub, id).DocumentID)
	fs := d.FieldSources
	if d.DocumentTypeID != 5 || d.CorrespondentID != 10 || fs.Type.Source != "model" || fs.Type.Confidence != 0.83 || fs.Correspondent.Basis != "关键词“国家电网”" {
		t.Errorf("document = %+v, sources %+v", d, fs)
	}
	if fs.Date == nil || fs.Date.Source != "text" || fs.Date.Basis != "2024年3月5日" {
		t.Errorf("date source = %+v", fs.Date)
	}
	tags, _ := sqlc.New(e.st.DB).ListDocumentTagIDs(context.Background(), d.ID)
	if len(tags) != 1 || tags[0] != 100 || len(fs.Tags) != 1 {
		t.Errorf("tags = %v, sources %+v", tags, fs.Tags)
	}

	// What the uploader chose wins.
	id, _ = e.s.Submit(e.ctx, e.scope, "b.txt", strings.NewReader("另一份 国家电网"), Overrides{CorrespondentID: 10, DocumentTypeID: 5}, Upload{})
	d, _ = e.docs.Get(context.Background(), e.scope, e.wait(sub, id).DocumentID)
	if d.FieldSources.Type.Source != "preset" || d.FieldSources.Correspondent.Source != "preset" || d.FieldSources.Date != nil {
		t.Errorf("preset sources = %+v", d.FieldSources)
	}
}

// A password-protected PDF needs its password at upload; the original stays
// encrypted, search and preview use the decrypted copy, reprocessing works.
func TestEncryptedPDFWithPassword(t *testing.T) {
	if m := parser.MissingTools(); len(m) > 0 {
		t.Skipf("missing tools: %v", m)
	}
	if _, err := exec.LookPath("qpdf"); err != nil {
		t.Skip("needs qpdf")
	}
	e := newEnv(t)
	sub, cancel := e.bus.Subscribe(1)
	defer cancel()
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.pdf")
	os.WriteFile(plain, pdfBytes(t, "HOUSE LEASE AGREEMENT", "Signed 2023-09-01", "Monthly rent 6000 for the flat"), 0o644)
	enc := filepath.Join(dir, "enc.pdf")
	if out, err := exec.Command("qpdf", "--encrypt", "secret", "owner", "256", "--", plain, enc).CombinedOutput(); err != nil {
		t.Fatalf("qpdf --encrypt: %v %s", err, out)
	}
	encBytes, _ := os.ReadFile(enc)

	var ae *apperr.Error
	if _, err := e.s.Submit(e.ctx, e.scope, "lease.pdf", bytes.NewReader(encBytes), Overrides{}, Upload{}); !errors.As(err, &ae) || ae.Code != "pdf_encrypted" {
		t.Fatalf("no password: %v", err)
	}
	if _, err := e.s.Submit(e.ctx, e.scope, "lease.pdf", bytes.NewReader(encBytes), Overrides{}, Upload{Password: "wrong"}); !errors.As(err, &ae) || ae.Code != "wrong_password" {
		t.Fatalf("wrong password: %v", err)
	}
	id, err := e.s.Submit(e.ctx, e.scope, "lease.pdf", bytes.NewReader(encBytes), Overrides{}, Upload{Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	je := e.wait(sub, id)
	if je.Status != "succeeded" {
		t.Fatalf("job = %+v", je)
	}
	var payload string
	e.st.DB.QueryRow(`SELECT payload FROM jobs WHERE id = ?`, id).Scan(&payload)
	if strings.Contains(payload, "secret") {
		t.Error("the password was stored with the job")
	}
	d, _ := e.docs.Get(e.ctx, e.scope, je.DocumentID)
	if !d.HasArchive || !strings.Contains(d.Content, "LEASE") || d.CreatedDate != "2023-09-01" {
		t.Errorf("document = %+v", d)
	}
	if orig, _ := os.ReadFile(e.files.Path(files.Original(1, d.ID, d.OriginalExt))); !bytes.Equal(orig, encBytes) {
		t.Error("the original should stay the encrypted upload")
	}
	rid, err := e.s.Reprocess(e.ctx, e.scope, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if re := e.wait(sub, rid); re.Status != "succeeded" {
		t.Errorf("reprocessing an encrypted original: %+v", re)
	}
	if d2, _ := e.docs.Get(e.ctx, e.scope, d.ID); !d2.HasArchive || !strings.Contains(d2.Content, "LEASE") {
		t.Errorf("after reprocessing = %+v", d2)
	}
}

// With barcodes on, a separator sheet splits a scan into two documents
// (the sheet itself is dropped) and an ASN label fills in the archive
// serial number; a number already taken is reported, not forced.
func TestBarcodeSeparatorsAndASN(t *testing.T) {
	if m := parser.MissingTools(); len(m) > 0 {
		t.Skipf("missing tools: %v", m)
	}
	if _, err := exec.LookPath("qpdf"); err != nil {
		t.Skip("needs qpdf")
	}
	e := newEnv(t)
	e.s.opt.Barcodes = true
	sub, cancel := e.bus.Subscribe(1)
	defer cancel()

	scan := barcodetest.PDF(barcodetest.Blank(), barcodetest.QR("PATCHT"), barcodetest.Code128("ASN00123"), barcodetest.Blank())
	id, err := e.s.Submit(e.ctx, e.scope, "扫描.pdf", bytes.NewReader(scan), Overrides{}, Upload{})
	if err != nil {
		t.Fatal(err)
	}
	je := e.wait(sub, id)
	if je.Status != "succeeded" {
		t.Fatalf("job = %+v", je)
	}
	rows, err := e.st.DB.Query(`SELECT title, page_count, asn FROM documents WHERE workspace_id = 1 ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	type doc struct {
		title string
		pages int
		asn   sql.NullInt64
	}
	var got []doc
	for rows.Next() {
		var d doc
		rows.Scan(&d.title, &d.pages, &d.asn)
		got = append(got, d)
	}
	rows.Close()
	if len(got) != 2 || got[0].title != "扫描（1/2）" || got[0].pages != 1 || got[0].asn.Valid ||
		got[1].title != "扫描（2/2）" || got[1].pages != 2 || got[1].asn.Int64 != 123 {
		t.Fatalf("documents = %+v", got)
	}

	// The same number again: not filled in, and the task says why.
	again := barcodetest.PDF(barcodetest.Code128("ASN123"), barcodetest.QR("again"))
	id, err = e.s.Submit(e.ctx, e.scope, "再扫.pdf", bytes.NewReader(again), Overrides{}, Upload{})
	if err != nil {
		t.Fatal(err)
	}
	if je := e.wait(sub, id); je.Status != "succeeded" {
		t.Fatalf("second job = %+v", je)
	}
	var result string
	e.st.DB.QueryRow(`SELECT result FROM jobs WHERE id = ?`, id).Scan(&result)
	if !strings.Contains(result, "#000123 已被其他文档使用") {
		t.Errorf("result = %s", result)
	}
}

func TestEPUBBecomesABook(t *testing.T) {
	e := newEnv(t)
	drawers := drawer.NewService(e.st.DB, drawer.Options{})
	lib, _, err := drawers.EnableLibrary(e.ctx, e.scope)
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(e.st.DB, jobs.NewQueue(e.st.DB), e.files, e.bus, Options{Placer: drawers, MaxUploadSize: 1 << 20})
	var cover bytes.Buffer
	_ = png.Encode(&cover, image.NewRGBA(image.Rect(0, 0, 300, 450)))
	book := epubtest.Book{Title: "唐诗选", Authors: []string{"李白"}, Publisher: "人民文学出版社", Date: "2012", Language: "zh", ISBN: "9787020002207",
		Chapters: []epubtest.Chapter{{Title: "静夜思", Paras: []string{"床前明月光"}}}, Cover: cover.Bytes()}
	id, err := s.Import(e.ctx, e.scope, "tangshi.epub", bytes.NewReader(epubtest.Build(book)), Overrides{}, Upload{})
	if err != nil {
		t.Fatal(err)
	}
	d, err := e.docs.Get(e.ctx, e.scope, id)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "唐诗选" || d.MimeType != "application/epub+zip" || !strings.Contains(d.Content, "床前明月光") || !d.HasThumbnail || d.HasArchive || d.DrawerID != lib.ID {
		t.Errorf("document = %+v", d)
	}
	q := sqlc.New(e.st.DB)
	b, err := q.GetBook(e.ctx, sqlc.GetBookParams{WorkspaceID: 1, DocumentID: id})
	if err != nil || b.Isbn != "9787020002207" || b.Authors != `["李白"]` || b.Publisher != "人民文学出版社" || b.Language != "中文" || b.Cover != 1 {
		t.Fatalf("book = %+v, %v", b, err)
	}
	for _, p := range []string{e.files.CoverPath(1, id), e.files.ThumbnailPath(1, id), e.files.Path(files.Original(1, id, "epub"))} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing %s", p)
		}
	}

	// The same ISBN again, and an uploader's title: the ISBN is left out.
	book.Chapters[0].Paras = []string{"another edition"}
	book.Cover = nil
	id2, err := s.Import(e.ctx, e.scope, "tangshi2.epub", bytes.NewReader(epubtest.Build(book)), Overrides{Title: "唐诗选（第二版）"}, Upload{})
	if err != nil {
		t.Fatal(err)
	}
	d2, _ := e.docs.Get(e.ctx, e.scope, id2)
	b2, _ := q.GetBook(e.ctx, sqlc.GetBookParams{WorkspaceID: 1, DocumentID: id2})
	if d2.Title != "唐诗选（第二版）" || b2.Isbn != "" || b2.Cover != 0 || d2.HasThumbnail {
		t.Errorf("second = %+v / %+v", d2, b2)
	}
}
