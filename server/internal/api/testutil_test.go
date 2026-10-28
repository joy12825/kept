package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"kept/server/internal/secret"
	"kept/server/internal/storage"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"

	"kept/server/internal/audit"
	"kept/server/internal/auth"
	"kept/server/internal/backup"
	"kept/server/internal/booklists"
	"kept/server/internal/books"
	"kept/server/internal/branding"
	"kept/server/internal/checklist"
	"kept/server/internal/classify"
	"kept/server/internal/config"
	"kept/server/internal/dedupe"
	"kept/server/internal/document"
	"kept/server/internal/drawer"
	"kept/server/internal/events"
	"kept/server/internal/export"
	"kept/server/internal/files"
	"kept/server/internal/ingest"
	"kept/server/internal/jobs"
	"kept/server/internal/notes"
	"kept/server/internal/notify"
	"kept/server/internal/notify/smtptest"
	"kept/server/internal/pages"
	"kept/server/internal/parser"
	"kept/server/internal/privacy"
	"kept/server/internal/quota"
	"kept/server/internal/reading"
	"kept/server/internal/search"
	"kept/server/internal/share"
	"kept/server/internal/store"
	"kept/server/internal/system"
	"kept/server/internal/taxonomy"
	"kept/server/internal/transfer"
	"kept/server/internal/views"
	"kept/server/internal/workspace"
)

// clock is an injectable time source for tests that need to fast-forward
// past session idle timeouts.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

// newEnvWithClock is like newEnv but its auth service uses a fake clock the
// test can advance, to exercise idle-session expiry without sleeping.
func newEnvWithClock(t *testing.T, secureCookie string) (*testEnv, *clock) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	c := &clock{t: time.Date(2024, 3, 5, 10, 0, 0, 0, time.UTC)}
	svc := auth.NewService(st.DB, auth.Options{SessionTTL: 24 * time.Hour, Now: c.Now})
	d, mailbox := testDeps(t, st, svc)
	d.SecureCookie = secureCookie
	srv, err := NewServer(d)
	if err != nil {
		t.Fatal(err)
	}
	return &testEnv{t: t, h: srv.Handler(), auth: svc, db: st.DB, smtp: mailbox, ee: d.EE, exp: d.Export, fs: d.Files, pv: d.Privacy, ing: d.Ingest}, c
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type testEnv struct {
	t    *testing.T
	h    http.Handler
	auth *auth.Service
	db   *sql.DB
	smtp *smtptest.Server // receives what the notify service sends
	ee   EEDeps           // the paid modules (v1.13): emergency access, whose clock a test may move …
	exp  *export.Service  // exports, for an export into a folder
	fs   *files.Store     // the files, in every storage
	pv   *privacy.Service // private documents, whose clock a test may move
	ing  *ingest.Service  // ingest, for its housekeeping
}

// newEnv builds the real handler over a fresh database. db overrides the
// health-check dependency when non-nil.
func newEnv(t *testing.T, secureCookie string, db Pinger) *testEnv {
	return newEnvFull(t, secureCookie, db, nil)
}

func newEnvWithWeb(t *testing.T, web http.Handler) *testEnv {
	return newEnvFull(t, "auto", nil, web)
}

func newEnvFull(t *testing.T, secureCookie string, db Pinger, web http.Handler) *testEnv {
	t.Helper()
	return newEnvDeps(t, secureCookie, db, web, nil)
}

// newEnvDeps is newEnvFull with a last change to the server's dependencies.
func newEnvDeps(t *testing.T, secureCookie string, db Pinger, web http.Handler, change func(*Deps)) *testEnv {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := auth.NewService(st.DB, auth.Options{SessionTTL: 24 * time.Hour})
	if db == nil {
		db = st
	}
	d, mailbox := testDeps(t, st, svc)
	d.DB, d.SecureCookie, d.Web = db, secureCookie, web
	if change != nil {
		change(&d)
	}
	srv, err := NewServer(d)
	if err != nil {
		t.Fatal(err)
	}
	return &testEnv{t: t, h: srv.Handler(), auth: svc, db: st.DB, smtp: mailbox, ee: d.EE, exp: d.Export, fs: d.Files, pv: d.Privacy, ing: d.Ingest}
}

// client is a tiny browser: it keeps the session cookie and CSRF token,
// or sends a bearer token instead.
type client struct {
	env     *testEnv
	cookie  *http.Cookie
	csrf    string
	bearer  string
	headers map[string]string
}

func (e *testEnv) anon() *client { return &client{env: e, headers: map[string]string{}} }

type resp struct {
	code   int
	header http.Header
	body   map[string]any
	list   []any
	raw    string
}

func (c *client) do(method, path string, body any) resp {
	c.env.t.Helper()
	if body == nil {
		return c.doRaw(method, path, "", nil)
	}
	b, _ := json.Marshal(body)
	return c.doRaw(method, path, "application/json", b)
}

// doRaw sends body as is with the given content type (none when "").
func (c *client) doRaw(method, path, contentType string, body []byte) resp {
	c.env.t.Helper()
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	req.RemoteAddr = "192.0.2.10:5000"
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	c.env.h.ServeHTTP(rec, req)

	out := resp{code: rec.Code, header: rec.Header(), raw: rec.Body.String()}
	if rec.Body.Len() > 0 {
		if rec.Body.Bytes()[0] == '[' {
			_ = json.Unmarshal(rec.Body.Bytes(), &out.list)
		} else {
			_ = json.Unmarshal(rec.Body.Bytes(), &out.body)
		}
	}
	for _, ck := range (&http.Response{Header: rec.Header()}).Cookies() {
		if ck.Name == "kept_session" {
			if ck.MaxAge < 0 || ck.Value == "" {
				c.cookie = nil
			} else {
				c.cookie = ck
			}
		}
	}
	if me, ok := out.body["csrfToken"].(string); ok {
		c.csrf = me
	}
	return out
}

func (c *client) mustDo(method, path string, body any, want int) resp {
	c.env.t.Helper()
	r := c.do(method, path, body)
	if r.code != want {
		c.env.t.Fatalf("%s %s: status %d, want %d; body %s", method, path, r.code, want, r.raw)
	}
	return r
}

// setupAdmin completes first-run setup and returns the logged-in admin.
func (e *testEnv) setupAdmin() *client {
	c := e.anon()
	c.mustDo(http.MethodPost, "/api/v1/setup", map[string]any{"username": "admin", "email": "admin@example.com", "password": "password-123"}, http.StatusOK)
	return c
}

// user creates an account (through the service) and logs it in.
func (e *testEnv) user(name string) *client {
	e.t.Helper()
	if _, err := e.auth.CreateUser(context.Background(), auth.NewUser{Username: name, Password: "password-123", Email: name + "@example.com"}); err != nil {
		e.t.Fatal(err)
	}
	c := e.anon()
	c.mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": name, "password": "password-123"}, http.StatusOK)
	return c
}

func code(r resp) string { s, _ := r.body["code"].(string); return s }

// testDeps wires every service over st, with a running job pool so uploads
// are processed as in production, and mail going to an in-memory server.
func testDeps(t *testing.T, st *store.Store, authSvc *auth.Service) (Deps, *smtptest.Server) {
	t.Helper()
	dir := t.TempDir()
	queue := jobs.NewQueue(st.DB)
	pool := jobs.NewPool(queue, jobs.PoolConfig{Workers: 2, PollInterval: 20 * time.Millisecond}, quietLog())
	bus := events.NewBus()
	fs := files.NewStore(dir)
	fs.AddStorage("nas", files.DirBackend{Root: filepath.Join(t.TempDir(), "nas")}) // a second storage (v1.7.0)
	fs.AddStorage("s3", fakeS3(t))                                                  // and an S3 one (v1.7.1)
	index, _, err := search.Open(filepath.Join(dir, "index"))
	if err != nil {
		t.Fatal(err)
	}
	drw := drawer.NewService(st.DB, drawer.Options{Queue: queue, Bus: bus, Files: fs})
	drw.Register(pool)
	drw.RegisterAlerts(pool)
	var userUpdated func(ws int64, ids []int64) // workflows', set below (v1.13)
	wf := &ingest.LateWorkflows{}
	var stg *storage.Service // made below; documents tell it about privacy changes
	docs := document.NewService(st.DB, document.Options{Bus: bus, Files: fs, Search: index, Placer: drw, OnPurge: func(ids []int64) { _ = index.Delete(ids) },
		OnUserUpdate: func(ws int64, ids []int64) {
			if userUpdated != nil {
				userUpdated(ws, ids)
			}
		},
		OnPrivacyChange: func(ws int64) { _ = stg.Requeue(context.Background(), ws) }})
	cls := classify.NewService(st.DB, classify.Options{Queue: queue, Documents: docs})
	cls.Register(pool)
	pg := pages.NewService(st.DB, fs, pages.Options{Bus: bus})
	mailbox := smtptest.Start(t, smtptest.Config{})
	nt := notify.NewService(st.DB, notify.Options{SMTP: config.SMTP{Host: mailbox.Host, Port: mailbox.Port, Security: "none",
		From: "收好 <kept@example.com>", MaxAttachments: 1 << 20}, Queue: queue, Files: fs, Documents: docs, Drawers: drw})
	nt.Register(pool)
	dd := dedupe.NewService(st.DB, dedupe.Options{Queue: queue, Documents: docs})
	dd.Register(pool)
	qs := quota.NewService(st.DB, quota.Options{Files: fs, Room: func(ctx context.Context, ws, need int64) error { return stg.Room(ctx, ws, need) }})
	box, err := secret.Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	stg = storage.NewService(st.DB, fs, storage.Options{Queue: queue, Log: quietLog(), Env: []config.Storage{{Name: "nas"}, {Name: "s3"}}, Box: box, DataDir: dir})
	stg.Register(pool)
	exp := export.NewService(export.Deps{DB: st.DB, Documents: docs, Taxonomy: taxonomy.NewService(st.DB, taxonomy.Options{}), Files: fs})
	var added func(ctx context.Context, ws, id int64) // the paid modules', set below
	ing := ingest.NewService(st.DB, queue, fs, bus, ingest.Options{Quota: qs, Parser: parser.Config{OCRLanguages: "eng", OCRMode: "skip"}, MaxUploadSize: 5 << 20, Classifier: cls, Placer: drw, AfterReprocess: pg.Reapply, Workflows: wf, SetFields: ingest.SetFieldsWith(docs),
		OnAdded: func(ctx context.Context, ws, id int64) {
			if added != nil {
				added(ctx, ws, id)
			}
		}})
	ing.Register(pool)
	pg.Register(pool, pages.Deps{Queue: queue, Ingest: ing, Documents: docs})
	bks := books.NewService(st.DB, bus, drw, fs)
	eeDeps := testEE(eeTestWiring{db: st.DB, ingest: ing, bus: bus, queue: queue, pool: pool, notify: nt, storage: stg, documents: docs, files: fs, drawers: drw, books: bks, pages: pg, export: exp, dataDir: dir})
	indexer := search.NewIndexer(st.DB, index, bus, quietLog())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{}, 2)
	go func() { _ = pool.Run(ctx); done <- struct{}{} }()
	go func() { indexer.Run(ctx); done <- struct{}{} }()
	t.Cleanup(func() { cancel(); <-done; <-done; index.Close() })
	wf.W, userUpdated = eeDeps.workflows()
	added = eeDeps.added()
	// the daily backup folder, chosen on the page in these tests (v1.13.9)
	bdir := backup.NewDir(st.DB, "", dir)
	backup.Register(pool, st.DB, bdir, dir, fs, quietLog())
	return Deps{
		Notify: nt, Shares: share.NewService(st.DB, share.Options{Documents: docs, Files: fs}), Dedupe: dd,
		DB: st, Auth: authSvc, Workspaces: workspace.NewService(st.DB, workspace.Options{}), Audit: audit.NewLog(st.DB),
		Taxonomy: taxonomy.NewService(st.DB, taxonomy.Options{}), Branding: branding.NewService(st.DB, filepath.Join(dir, "branding"), branding.Options{}),
		Documents: docs, Classifier: cls, Drawers: drw, Pages: pg, System: system.NewService(st.DB, system.Options{BackupDir: bdir, DataDir: dir, DBPath: filepath.Join(dir, "app.db"), IndexDir: filepath.Join(dir, "index"), Index: index, StartedAt: time.Now()}), Notes: notes.NewService(st.DB, bus), Books: bks, BookLookup: fakeBookLookup(t), Reading: reading.NewService(st.DB), BookLists: booklists.NewService(st.DB), Views: views.NewService(st.DB), Search: index, Ingest: ing, Quota: qs, Storage: stg, Export: exp, BackupDir: bdir, Transfer: transfer.NewService(st.DB, transfer.Options{Documents: docs, Taxonomy: taxonomy.NewService(st.DB, taxonomy.Options{}), Files: fs, Placer: drw, Quota: qs, Bus: bus}),
		Privacy:    privacy.New(st.DB),
		Checklists: checklist.NewService(st.DB, checklist.Options{Documents: docs}),
		EE:         eeDeps, Files: fs, Events: bus, Jobs: queue, Log: quietLog(),
	}, mailbox
}

// eeTestWiring is what the paid modules are made from in tests (v1.13);
// testEE is in ee_testutil_test.go, or empty in the open-source build.
type eeTestWiring struct {
	db        *sql.DB
	ingest    *ingest.Service
	bus       *events.Bus
	queue     *jobs.Queue
	pool      *jobs.Pool
	notify    *notify.Service
	storage   *storage.Service
	documents *document.Service
	files     *files.Store
	drawers   *drawer.Service
	books     *books.Service
	pages     *pages.Service
	export    *export.Service
	dataDir   string
}

// waitIndexed waits until the background indexer has caught up, before a
// test searches by keyword.
func (e *testEnv) waitIndexed() {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var behind int
		if err := e.db.QueryRow(`SELECT count(*) FROM documents WHERE indexed_rev < index_rev`).Scan(&behind); err != nil {
			e.t.Fatal(err)
		}
		if behind == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.t.Fatal("the search index did not catch up")
}

// fakeS3 is an in-memory S3 service with bucket "kept".
func fakeS3(t *testing.T) *files.S3Backend {
	t.Helper()
	mem := s3mem.New()
	if err := mem.CreateBucket("kept"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(gofakes3.New(mem).Server())
	t.Cleanup(srv.Close)
	b, err := files.NewS3(files.S3Config{Endpoint: strings.TrimPrefix(srv.URL, "http://"), Bucket: "kept", Prefix: "kept", AccessKey: "ak", SecretKey: "sk", PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// paidPath: in the open-source build, the paths of the paid modules (v1.13)
// answer edition_required, so a test of something else skips them.
func paidPath(path string) bool {
	if eeBuild {
		return false
	}
	for _, p := range []string{"/api/v1/mail-accounts", "/api/v1/upload-links", "/api/v1/public/uploads", "/api/v1/collections", "/api/v1/shared-collections",
		"/api/v1/emergency-", "/api/v1/remote-inbox", "/api/v1/admin/branding", "/api/v1/workflows"} {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}
