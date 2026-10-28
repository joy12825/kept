package jobs

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"kept/server/internal/store"
)

func schedEnv(t *testing.T) (*sql.DB, *Queue, *fakeClock) {
	t.Helper()
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	clk := &fakeClock{t: time.Date(2024, 3, 5, 10, 0, 0, 0, time.Local)}
	s.DB.Exec(`INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'a', 'family', 1), (2, 'b', 'family', 1), (3, 'gone', 'family', 1)`)
	s.DB.Exec(`UPDATE workspaces SET deleted_at = 1 WHERE id = 3`)
	return s.DB, NewQueue(s.DB, WithClock(clk.Now)), clk
}

func countJobs(t *testing.T, db *sql.DB, kind string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM jobs WHERE kind = ?`, kind).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSchedulerEnqueuesDueEntriesOnce(t *testing.T) {
	db, q, clk := schedEnv(t)
	ctx := context.Background()
	s := NewScheduler(db, q, []Entry{
		{Name: "sessions", Kind: "system.sessions", Every: 6 * time.Hour},
		{Name: "trash", Kind: "system.trash", At: "04:00"},
		{Name: "train", Kind: "classify.train", Every: time.Hour, PerWorkspace: true},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), WithSchedulerClock(clk.Now))

	// A new entry runs at the first check; per-workspace entries skip deleted workspaces.
	if n, err := s.Check(ctx); err != nil || n != 4 {
		t.Fatalf("first check = %d, %v; want 4", n, err)
	}
	if n, _ := s.Check(ctx); n != 0 {
		t.Errorf("nothing is due right after, got %d", n)
	}

	// The previous training job is still queued: no second one.
	clk.Advance(time.Hour)
	if n, _ := s.Check(ctx); n != 0 {
		t.Errorf("a job still queued must not be enqueued again, got %d", n)
	}
	db.Exec(`UPDATE jobs SET status = 'succeeded'`)
	clk.Advance(time.Hour) // 12:00
	if n, _ := s.Check(ctx); n != 2 {
		t.Errorf("training due again for both workspaces, got %d", n)
	}

	// The daily entry is next due tomorrow at 04:00; a long outage runs it once.
	db.Exec(`UPDATE jobs SET status = 'succeeded'`)
	var next int64
	db.QueryRow(`SELECT next_run_at FROM schedules WHERE name = 'trash'`).Scan(&next)
	if want := time.Date(2024, 3, 6, 4, 0, 0, 0, time.Local); time.UnixMilli(next) != want {
		t.Errorf("trash next run %v, want %v", time.UnixMilli(next), want)
	}
	clk.Advance(72 * time.Hour)
	s.Check(ctx)
	if got := countJobs(t, db, "system.trash"); got != 2 {
		t.Errorf("missed runs are made up once, trash jobs = %d, want 2", got)
	}
	db.QueryRow(`SELECT next_run_at FROM schedules WHERE name = 'trash'`).Scan(&next)
	if want := time.Date(2024, 3, 9, 4, 0, 0, 0, time.Local); time.UnixMilli(next) != want {
		t.Errorf("after catching up, next run %v, want %v", time.UnixMilli(next), want)
	}
}

// TestSchedulerWhen: an entry whose When is false skips its run, without a
// job, and runs again once it is true at a later due time.
func TestSchedulerWhen(t *testing.T) {
	db, q, clk := schedEnv(t)
	ctx := context.Background()
	on := false
	s := NewScheduler(db, q, []Entry{
		{Name: "backup-storage", Kind: "system.backup-storage", Every: time.Hour, When: func(context.Context) bool { return on }},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), WithSchedulerClock(clk.Now))
	if n, err := s.Check(ctx); err != nil || n != 0 {
		t.Fatalf("off = %d, %v", n, err)
	}
	var jobs int
	db.QueryRow(`SELECT count(*) FROM jobs`).Scan(&jobs)
	if jobs != 0 {
		t.Fatalf("a job was made while off: %d", jobs)
	}
	on = true
	if n, _ := s.Check(ctx); n != 0 {
		t.Errorf("not due yet, got %d", n)
	}
	clk.Advance(time.Hour)
	if n, err := s.Check(ctx); err != nil || n != 1 {
		t.Errorf("on = %d, %v", n, err)
	}
}

// TestSchedulerWhenWorkspace (v1.7.16): a per-workspace entry skips the
// workspaces its WhenWorkspace says no for.
func TestSchedulerWhenWorkspace(t *testing.T) {
	db, q, _ := schedEnv(t)
	s := NewScheduler(db, q, []Entry{
		{Name: "mail-fetch", Kind: "mail.fetch", Every: time.Hour, PerWorkspace: true,
			WhenWorkspace: func(_ context.Context, ws int64) bool { return ws == 2 }},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), WithSchedulerClock((&fakeClock{t: time.Date(2024, 3, 5, 10, 0, 0, 0, time.Local)}).Now))
	if n, err := s.Check(context.Background()); err != nil || n != 1 {
		t.Fatalf("enqueued %d, %v", n, err)
	}
	var ws int64
	if err := db.QueryRow(`SELECT workspace_id FROM jobs WHERE kind = 'mail.fetch'`).Scan(&ws); err != nil || ws != 2 {
		t.Fatalf("job for workspace %d, %v", ws, err)
	}
}

// TestPurgeFinished (v1.7.19): finished jobs older than the cutoff go;
// newer ones and unfinished ones stay.
func TestPurgeFinished(t *testing.T) {
	db, q, _ := schedEnv(t)
	ctx := context.Background()
	for _, r := range []struct {
		status   string
		finished any
	}{{"succeeded", 100}, {"failed", 200}, {"succeeded", 5000}, {"queued", nil}, {"running", nil}} {
		if _, err := db.Exec(`INSERT INTO jobs (kind, payload, status, run_after, created_at, finished_at) VALUES ('k', '{}', ?, 1, 1, ?)`, r.status, r.finished); err != nil {
			t.Fatal(err)
		}
	}
	n, err := q.PurgeFinished(ctx, time.UnixMilli(1000))
	if err != nil || n != 2 {
		t.Fatalf("purged %d, %v", n, err)
	}
	if left := countJobs(t, db, "k"); left != 3 {
		t.Fatalf("left %d", left)
	}
}
