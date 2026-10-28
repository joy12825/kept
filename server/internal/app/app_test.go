package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kept/server/internal/config"
	"kept/server/internal/store"
)

func TestServeStartsAndShutsDown(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(func(k string) string {
		return map[string]string{
			"KEPT_DATA_DIR":         dir,
			"KEPT_HTTP_ADDR":        "127.0.0.1:0",
			"KEPT_SHUTDOWN_TIMEOUT": "6s",
		}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, cfg, log, ready) }()

	var addr string
	select {
	case addr = <-ready:
	case err := <-done:
		t.Fatalf("Serve exited early: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("server did not start")
	}

	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	var body struct{ Status string }
	_ = json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || body.Status != "ok" {
		t.Fatalf("healthz: %d %+v", resp.StatusCode, body)
	}

	// First-run setup over real HTTP, then an authenticated call with the cookie.
	jar, _ := cookiejar.New(nil)
	cl := &http.Client{Jar: jar}
	resp, err = cl.Post("http://"+addr+"/api/v1/setup", "application/json",
		strings.NewReader(`{"username":"admin","email":"admin@example.com","password":"password-123"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup: %d", resp.StatusCode)
	}
	resp, err = cl.Get("http://" + addr + "/api/v1/me")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me after setup: %d", resp.StatusCode)
	}

	for _, d := range cfg.Dirs() {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("data dir not created: %v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "app.db")); err != nil && !store.TestingPostgres() {
		t.Errorf("database not created: %v", err)
	}

	// An open event stream does not hold the shutdown up: it ends as soon
	// as shutting down starts, well inside KEPT_SHUTDOWN_TIMEOUT.
	stream, err := cl.Get("http://" + addr + "/api/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	first := make([]byte, 64)
	if n, _ := stream.Body.Read(first); !strings.Contains(string(first[:n]), "connected") {
		t.Fatalf("events: %d %q", stream.StatusCode, first[:n])
	}
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v", err)
		}
		if d := time.Since(start); d > 3*time.Second {
			t.Errorf("shutdown took %v with an event stream open", d)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not shut down")
	}
}
