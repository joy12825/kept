package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"kept/server/internal/system"
)

type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

func TestHealthOK(t *testing.T) {
	c := newEnv(t, "auto", fakeDB{}).anon()
	r := c.mustDo(http.MethodGet, "/healthz", nil, http.StatusOK)
	if r.body["status"] != "ok" {
		t.Fatalf("body %v", r.body)
	}
	if r.header.Get("X-Request-ID") == "" {
		t.Error("missing X-Request-ID")
	}
}

func TestHealthDegraded(t *testing.T) {
	c := newEnv(t, "auto", fakeDB{err: errors.New("disk I/O error")}).anon()
	r := c.mustDo(http.MethodGet, "/healthz", nil, http.StatusServiceUnavailable)
	if r.body["status"] != "degraded" {
		t.Fatalf("body %v", r.body)
	}
}

func TestHealthReportsLowDisk(t *testing.T) {
	e := newEnvDeps(t, "auto", fakeDB{}, nil, func(d *Deps) {
		d.System = system.NewService(nil, system.Options{DataDir: t.TempDir(), MinFree: 1 << 62}) // more than any disk has
	})
	r := e.anon().mustDo(http.MethodGet, "/healthz", nil, http.StatusServiceUnavailable)
	if r.body["status"] != "degraded" || !strings.HasPrefix(r.body["checks"].(map[string]any)["disk"].(string), "剩余空间不足") {
		t.Fatalf("body %v", r.body)
	}
}

func TestVersion(t *testing.T) {
	r := newEnv(t, "auto", nil).anon().mustDo(http.MethodGet, "/api/v1/system/version", nil, http.StatusOK)
	if r.body["version"] == nil {
		t.Fatalf("body %v", r.body)
	}
}

func TestUnknownPathReturnsJSONError(t *testing.T) {
	r := newEnv(t, "auto", nil).anon().mustDo(http.MethodGet, "/api/v1/nope", nil, http.StatusNotFound)
	if code(r) != "not_found" {
		t.Fatalf("body %v", r.body)
	}
}

func TestRequestIDIsEchoed(t *testing.T) {
	c := newEnv(t, "auto", nil).anon()
	c.headers["X-Request-ID"] = "abc123"
	if got := c.do(http.MethodGet, "/healthz", nil).header.Get("X-Request-ID"); got != "abc123" {
		t.Errorf("X-Request-ID = %q, want abc123", got)
	}
}

func TestPanicBecomesJSON500(t *testing.T) {
	h := chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }),
		recoverer(quietLog()), requestID)
	e := &testEnv{t: t, h: h}
	r := e.anon().mustDo(http.MethodGet, "/x", nil, http.StatusInternalServerError)
	if code(r) != "internal" {
		t.Fatalf("body %v", r.body)
	}
}

func TestNonAPIPathsGoToTheWebApp(t *testing.T) {
	e := newEnvWithWeb(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("web app"))
	}))
	c := e.anon()
	if r := c.do(http.MethodGet, "/files", nil); r.raw != "web app" {
		t.Errorf("/files: %d %q", r.code, r.raw)
	}
	if r := c.do(http.MethodGet, "/api/v1/nope", nil); r.code != http.StatusNotFound || code(r) != "not_found" {
		t.Errorf("/api/v1/nope should stay a JSON 404: %d %q", r.code, r.raw)
	}
}
