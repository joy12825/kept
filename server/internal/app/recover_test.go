package app

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"kept/server/internal/config"
	"kept/server/internal/events"
	"kept/server/internal/files"
	"kept/server/internal/ingest"
	"kept/server/internal/jobs"
	"kept/server/internal/store"
	"kept/server/internal/tenant"
)

// Acceptance item 11: a container restarted while an upload is being
// processed picks the job up again and the file is not lost. The crash is
// simulated by leaving a staged upload whose job is still marked running
// with a heartbeat from long ago, then starting the server.
func TestServeRecoversJobsInterruptedByARestart(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(func(k string) string {
		return map[string]string{"KEPT_DATA_DIR": dir, "KEPT_HTTP_ADDR": "127.0.0.1:0", "KEPT_SHUTDOWN_TIMEOUT": "6s"}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareDirs(cfg); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Before the "crash": a workspace and an upload whose processing began.
	st, err := store.Open(ctx, cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	var ws int64
	if err := st.DB.QueryRowContext(ctx, `INSERT INTO workspaces (name, kind, created_at) VALUES ('家', 'family', ?) RETURNING id`, now).Scan(&ws); err != nil {
		t.Fatal(err)
	}
	svc := ingest.NewService(st.DB, jobs.NewQueue(st.DB), files.NewStore(cfg.DataDir), events.NewBus(), ingest.Options{})
	jobID, err := svc.Submit(ctx, tenant.New(ws, tenant.RoleOwner), "电费.txt", strings.NewReader("国家电网 电费账单 2024-03-05 金额 128.50"), ingest.Overrides{}, ingest.Upload{})
	if err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-time.Hour).UnixMilli()
	if _, err := st.DB.ExecContext(ctx, `UPDATE jobs SET status = 'running', attempts = 1, heartbeat_at = ? WHERE id = ?`, stale, jobID); err != nil {
		t.Fatal(err)
	}
	st.Close()

	// After the restart.
	runCtx, cancel := context.WithCancel(ctx)
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- Serve(runCtx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), ready) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("server did not shut down")
		}
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("Serve exited early: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("server did not start")
	}

	st2, err := store.Open(ctx, cfg.DBPath()) // the same database, also on Postgres
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	db := st2.DB
	deadline := time.Now().Add(20 * time.Second)
	for {
		var status string
		var docs int
		if err := db.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, jobID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM documents WHERE workspace_id = ? AND title = '电费'`, ws).Scan(&docs); err != nil {
			t.Fatal(err)
		}
		if status == "succeeded" && docs == 1 {
			return
		}
		if status == "failed" || time.Now().After(deadline) {
			t.Fatalf("job %d is %s with %d documents; want succeeded with 1", jobID, status, docs)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
