package watch

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kept/server/internal/events"
	"kept/server/internal/files"
	"kept/server/internal/ingest"
	"kept/server/internal/jobs"
	"kept/server/internal/parser"
	"kept/server/internal/pdftest"
	"kept/server/internal/store"
	"kept/server/internal/taxonomy"
	"kept/server/internal/tenant"
)

type env struct {
	t      *testing.T
	ctx    context.Context
	db     *sql.DB
	w      *Watcher
	ing    *ingest.Service
	tax    *taxonomy.Service
	queue  *jobs.Queue
	dir    string
	ws     int64
	now    time.Time
	merged [][2]string
	pages  map[string]int
}

func newEnv(t *testing.T) *env {
	t.Helper()
	data := t.TempDir()
	st, err := store.Open(context.Background(), filepath.Join(data, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	var ws int64
	if err := st.DB.QueryRow(`INSERT INTO workspaces (name, kind, created_at) VALUES ('王家', 'family', 1) RETURNING id`).Scan(&ws); err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, ctx: context.Background(), db: st.DB, dir: filepath.Join(t.TempDir(), "consume"), ws: ws,
		now: time.Date(2024, 3, 5, 10, 0, 0, 0, time.Local), pages: map[string]int{}}
	e.queue = jobs.NewQueue(st.DB)
	e.ing = ingest.NewService(st.DB, e.queue, files.NewStore(data), events.NewBus(), ingest.Options{Parser: parser.Config{OCRLanguages: "eng", OCRMode: "skip"}})
	e.tax = taxonomy.NewService(st.DB, taxonomy.Options{})
	e.w = New(st.DB, e.ing, e.tax, Options{Dir: e.dir, Now: func() time.Time { return e.now },
		Merge: func(_ context.Context, front, back, out string) error {
			e.merged = append(e.merged, [2]string{filepath.Base(front), filepath.Base(back)})
			return os.WriteFile(out, []byte("%PDF-1.4 merged "+filepath.Base(front)+"+"+filepath.Base(back)), 0o644)
		},
		Pages: func(_ context.Context, p string) (int, error) { return e.pages[filepath.Base(p)], nil },
	})
	return e
}

// put writes a file in the workspace folder, modified at the given offset
// from now (negative: in the past).
func (e *env) put(rel, body string, age time.Duration) string {
	e.t.Helper()
	p := filepath.Join(e.dir, "1-王家", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		e.t.Fatal(err)
	}
	at := e.now.Add(-age)
	if err := os.Chtimes(p, at, at); err != nil {
		e.t.Fatal(err)
	}
	return p
}

func (e *env) scan() {
	e.t.Helper()
	if err := e.w.Scan(e.ctx); err != nil {
		e.t.Fatal(err)
	}
}

type queued struct {
	Filename  string `json:"filename"`
	Source    string `json:"source"`
	Origin    string `json:"origin"`
	Overrides struct {
		TagIDs []int64 `json:"tagIds"`
	} `json:"overrides"`
}

func (e *env) jobs() []queued {
	e.t.Helper()
	rows, err := e.db.Query(`SELECT payload FROM jobs WHERE kind = ? ORDER BY id`, ingest.KindDocument)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []queued
	for rows.Next() {
		var p string
		rows.Scan(&p)
		var q queued
		json.Unmarshal([]byte(p), &q)
		out = append(out, q)
	}
	return out
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestWorkspaceFoldersAndSettling(t *testing.T) {
	e := newEnv(t)
	e.scan()
	if !exists(filepath.Join(e.dir, "1-王家")) {
		t.Fatal("the workspace folder was not created")
	}
	p := e.put("电费.txt", "国家电网 电费", 10*time.Second)
	e.put(".DS_Store", "x", time.Minute)
	e.put("下载中.pdf.part", "x", time.Minute)
	os.WriteFile(filepath.Join(e.dir, "根目录.txt"), []byte("x"), 0o644)

	e.scan() // first sight: not yet known to be finished
	if len(e.jobs()) != 0 || e.w.Status().Waiting != 1 {
		t.Fatalf("after one scan: jobs %v, status %+v", e.jobs(), e.w.Status())
	}
	e.scan()
	got := e.jobs()
	if len(got) != 1 || got[0].Filename != "电费.txt" || got[0].Source != "watch" || got[0].Origin != "1-王家/电费.txt" {
		t.Fatalf("jobs = %+v", got)
	}
	if exists(p) {
		t.Error("the source file should be removed once accepted")
	}
	if !exists(filepath.Join(e.dir, "根目录.txt")) || !exists(filepath.Join(e.dir, "1-王家", ".DS_Store")) {
		t.Error("files outside workspace folders and hidden files are left alone")
	}
	if st := e.w.Status(); len(st.Folders) != 1 || st.Folders[0].Name != "1-王家" || st.Waiting != 0 {
		t.Errorf("status = %+v", st)
	}

	// A file still being written (changed between scans) waits.
	e.put("扫描.txt", "第一部分", 10*time.Second)
	e.scan()
	e.put("扫描.txt", "第一部分 第二部分", 10*time.Second)
	e.scan()
	if len(e.jobs()) != 1 {
		t.Error("a growing file was ingested")
	}
	e.scan()
	if len(e.jobs()) != 2 {
		t.Error("the finished file was not ingested")
	}
}

func TestSubfoldersBecomeTags(t *testing.T) {
	e := newEnv(t)
	scope := tenant.New(e.ws, tenant.RoleOwner)
	old, err := e.tax.CreateTag(e.ctx, scope, taxonomy.TagInput{Name: "发票"})
	if err != nil {
		t.Fatal(err)
	}
	e.put("发票/专票/a.txt", "增值税专用发票", 10*time.Second)
	e.scan()
	e.scan()
	got := e.jobs()
	if len(got) != 1 || len(got[0].Overrides.TagIDs) != 2 || got[0].Overrides.TagIDs[0] != old.ID {
		t.Fatalf("jobs = %+v", got)
	}
	tags, _ := e.tax.ListTags(e.ctx, scope)
	if len(tags) != 2 {
		t.Errorf("tags = %+v, want 发票 reused and 专票 created", tags)
	}
}

func TestRefusedFilesMoveAside(t *testing.T) {
	e := newEnv(t)
	e.put("空的.txt", "", 10*time.Second)
	e.put("a/甲.txt", "同一份内容", 10*time.Second)
	e.scan()
	e.scan()
	failed := filepath.Join(e.dir, FailedDir, "1-王家", "空的.txt")
	if !exists(failed) || !exists(failed+".原因.txt") {
		t.Fatal("the empty file was not moved to " + FailedDir)
	}
	if b, _ := os.ReadFile(failed + ".原因.txt"); !strings.Contains(string(b), "空") {
		t.Errorf("reason = %q", b)
	}
	if !exists(filepath.Join(e.dir, "1-王家", "a")) || exists(filepath.Join(e.dir, "1-王家", "a", "甲.txt")) {
		t.Error("the good file should be accepted and removed")
	}
	if st := e.w.Status(); st.Failed != 1 {
		t.Errorf("failed = %d, want 1", st.Failed)
	}
}

func TestDuplex(t *testing.T) {
	e := newEnv(t)
	e.pages["正面.pdf"], e.pages["反面.pdf"] = 3, 3
	front := e.put("双面/正面.pdf", "%PDF-1.4 fronts", 60*time.Second)
	back := e.put("双面/反面.pdf", "%PDF-1.4 backs", 30*time.Second)
	e.scan()
	e.scan()
	if len(e.merged) != 1 || e.merged[0] != [2]string{"正面.pdf", "反面.pdf"} {
		t.Fatalf("merged = %v (fronts first, by time)", e.merged)
	}
	if got := e.jobs(); len(got) != 1 || got[0].Filename != "正面.pdf" || len(got[0].Overrides.TagIDs) != 0 {
		t.Errorf("jobs = %+v", got)
	}
	if exists(front) || exists(back) {
		t.Error("both halves should be removed after the merge")
	}

	// A lone half waits, then goes in alone.
	e.put("双面/单张.pdf", "%PDF-1.4 lone", 10*time.Second)
	e.scan()
	e.scan()
	if len(e.jobs()) != 1 || e.w.Status().Waiting != 1 {
		t.Fatalf("a lone half was not kept waiting: %+v", e.w.Status())
	}
	e.now = e.now.Add(31 * time.Minute)
	e.scan()
	if got := e.jobs(); len(got) != 2 || got[1].Filename != "单张.pdf" {
		t.Errorf("after the timeout: %+v", got)
	}

	// Different page counts: no merge, both go in.
	e.pages["x.pdf"], e.pages["y.pdf"] = 2, 3
	e.put("双面/x.pdf", "%PDF-1.4 x", 60*time.Second)
	e.put("双面/y.pdf", "%PDF-1.4 y", 30*time.Second)
	e.scan()
	e.scan()
	if len(e.merged) != 1 || len(e.jobs()) != 4 {
		t.Errorf("mismatched pages: merged %v, jobs %d", e.merged, len(e.jobs()))
	}
}

// A file whose processing fails later is put back into _未能入库.
func TestFailedProcessingKeepsTheFile(t *testing.T) {
	if len(parser.MissingTools()) > 0 {
		t.Skip("needs poppler")
	}
	e := newEnv(t)
	pool := jobs.NewPool(e.queue, jobs.PoolConfig{Workers: 1, PollInterval: 10 * time.Millisecond}, slog.New(slog.DiscardHandler))
	e.ing.Register(pool)
	ctx, cancel := context.WithCancel(e.ctx)
	done := make(chan struct{})
	go func() { _ = pool.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	e.put("坏/broken.pdf", "%PDF-1.4\n this is not really a pdf", 10*time.Second)
	e.scan()
	e.scan()
	kept := filepath.Join(e.dir, FailedDir, "1-王家", "坏", "broken.pdf")
	deadline := time.Now().Add(20 * time.Second)
	for !exists(kept+".原因.txt") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if b, err := os.ReadFile(kept); err != nil || !strings.Contains(string(b), "not really") {
		t.Fatalf("kept file = %q, %v", b, err)
	}
}

func TestQpdfMerge(t *testing.T) {
	if _, err := exec.LookPath("qpdf"); err != nil {
		t.Skip("needs qpdf")
	}
	dir := t.TempDir()
	front, back, out := filepath.Join(dir, "f.pdf"), filepath.Join(dir, "b.pdf"), filepath.Join(dir, "out.pdf")
	os.WriteFile(front, pdftest.Text("P1", "P3"), 0o644)
	os.WriteFile(back, pdftest.Text("P4", "P2"), 0o644)
	if err := qpdfMerge(context.Background(), front, back, out); err != nil {
		t.Fatal(err)
	}
	if n, err := qpdfPages(context.Background(), out); err != nil || n != 4 {
		t.Fatalf("pages = %d, %v", n, err)
	}
	if _, err := exec.LookPath("pdftotext"); err == nil {
		b, _ := exec.Command("pdftotext", out, "-").Output()
		if got := strings.Join(strings.Fields(string(b)), " "); got != "P1 P2 P3 P4" {
			t.Errorf("merged order = %q", got)
		}
	}
}

// v1.7.13: with the instance setting on, a file put again under the same
// path becomes a new version of the document it made.
func TestSamePathBecomesVersion(t *testing.T) {
	e := newEnv(t)
	pool := jobs.NewPool(e.queue, jobs.PoolConfig{Workers: 1, PollInterval: 10 * time.Millisecond}, slog.New(slog.DiscardHandler))
	e.ing.Register(pool)
	ctx, cancel := context.WithCancel(e.ctx)
	done := make(chan struct{})
	go func() { _ = pool.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	count := func(q string, args ...any) int {
		var n int
		if err := e.db.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	idle := func() {
		deadline := time.Now().Add(20 * time.Second)
		for count(`SELECT count(*) FROM jobs WHERE status IN ('queued', 'running')`) > 0 && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
	}
	drop := func(body string) {
		e.now = e.now.Add(time.Minute)
		e.put("合同/租房.txt", body, 10*time.Second)
		e.scan()
		e.scan()
		idle()
	}
	drop("第一稿：房租 3000 元")
	var doc int64
	if err := e.db.QueryRow(`SELECT id FROM documents WHERE origin_path = '合同/租房.txt'`).Scan(&doc); err != nil {
		t.Fatalf("origin path not kept: %v", err)
	}
	// Off by default: another document.
	drop("另一份：房租 3100 元")
	if n := count(`SELECT count(*) FROM documents`); n != 2 {
		t.Fatalf("documents = %d", n)
	}
	if err := e.ing.SetWatchVersions(e.ctx, true); err != nil {
		t.Fatal(err)
	}
	drop("第二稿：房租 3200 元")
	if n := count(`SELECT count(*) FROM documents`); n != 2 {
		t.Fatalf("documents after a version = %d", n)
	}
	latest := doc
	e.db.QueryRow(`SELECT max(id) FROM documents`).Scan(&latest)
	if n := count(`SELECT count(*) FROM document_versions WHERE document_id = ?`, latest); n != 2 {
		t.Fatalf("versions of the latest = %d", n)
	}
	var content string
	e.db.QueryRow(`SELECT content FROM documents WHERE id = ?`, latest).Scan(&content)
	if !strings.Contains(content, "3200") {
		t.Fatalf("content = %q", content)
	}
	if exists(filepath.Join(e.dir, "1-王家", "合同", "租房.txt")) {
		t.Fatal("source not removed")
	}
}
