package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"kept/server/internal/logging"
	"kept/server/internal/store"
)

// fakeClock is a manually advanced clock shared by queue and tests.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func newQueue(t *testing.T) (*Queue, *fakeClock) {
	t.Helper()
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	clk := &fakeClock{t: time.Date(2024, 3, 5, 10, 0, 0, 0, time.UTC)}
	q := NewQueue(s.DB, WithClock(clk.Now), WithBackoff(func(int) time.Duration { return time.Minute }))
	return q, clk
}

func mustEnqueue(t *testing.T, q *Queue, kind string, payload any, opt EnqueueOptions) int64 {
	t.Helper()
	id, err := q.Enqueue(context.Background(), kind, payload, opt)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func status(t *testing.T, q *Queue, id int64) string {
	t.Helper()
	row, err := q.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return row.Status
}

func TestClaimOrderAndDelay(t *testing.T) {
	ctx := context.Background()
	q, clk := newQueue(t)
	low := mustEnqueue(t, q, "a", nil, EnqueueOptions{})
	high := mustEnqueue(t, q, "a", nil, EnqueueOptions{Priority: 5})
	later := mustEnqueue(t, q, "a", nil, EnqueueOptions{Priority: 9, RunAfter: clk.Now().Add(time.Hour)})

	for _, want := range []int64{high, low} {
		row, err := q.claim(ctx)
		if err != nil || row == nil {
			t.Fatalf("claim: %v %v", row, err)
		}
		if row.ID != want {
			t.Errorf("claimed %d, want %d", row.ID, want)
		}
		if row.Status != "running" || row.Attempts != 1 {
			t.Errorf("claimed row not marked running: %+v", row)
		}
	}
	if row, _ := q.claim(ctx); row != nil {
		t.Fatalf("claimed delayed job %d before its time", row.ID)
	}
	clk.Advance(time.Hour)
	if row, _ := q.claim(ctx); row == nil || row.ID != later {
		t.Fatalf("delayed job not claimable after its time: %v", row)
	}
}

func TestRetryThenFail(t *testing.T) {
	ctx := context.Background()
	q, clk := newQueue(t)
	id := mustEnqueue(t, q, "a", nil, EnqueueOptions{MaxAttempts: 2})

	row, _ := q.claim(ctx)
	if err := q.finishWithError(ctx, id, int(row.Attempts), int(row.MaxAttempts), errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	if got := status(t, q, id); got != "queued" {
		t.Fatalf("after first failure status = %s, want queued", got)
	}
	if row, _ := q.claim(ctx); row != nil {
		t.Fatal("retried before backoff elapsed")
	}
	clk.Advance(time.Minute)
	row, _ = q.claim(ctx)
	if row == nil || row.Attempts != 2 {
		t.Fatalf("retry not claimable: %+v", row)
	}
	if err := q.finishWithError(ctx, id, int(row.Attempts), int(row.MaxAttempts), errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	got, _ := q.Get(ctx, id)
	if got.Status != "failed" || got.Error.String != "boom" {
		t.Fatalf("after last attempt: status=%s error=%q", got.Status, got.Error.String)
	}
}

func TestPermanentErrorFailsImmediately(t *testing.T) {
	ctx := context.Background()
	q, _ := newQueue(t)
	id := mustEnqueue(t, q, "a", nil, EnqueueOptions{MaxAttempts: 5})
	row, _ := q.claim(ctx)
	if err := q.finishWithError(ctx, id, int(row.Attempts), int(row.MaxAttempts), Permanent(errors.New("文件已加密"))); err != nil {
		t.Fatal(err)
	}
	if got := status(t, q, id); got != "failed" {
		t.Fatalf("status = %s, want failed", got)
	}
}

func TestRecoverStale(t *testing.T) {
	ctx := context.Background()
	q, clk := newQueue(t)
	retryable := mustEnqueue(t, q, "a", nil, EnqueueOptions{MaxAttempts: 3})
	exhausted := mustEnqueue(t, q, "a", nil, EnqueueOptions{MaxAttempts: 1})
	q.claim(ctx)
	q.claim(ctx)

	clk.Advance(30 * time.Second)
	if n, f, _ := q.RecoverStale(ctx, time.Minute); n+f != 0 {
		t.Fatalf("recovered fresh jobs: requeued=%d failed=%d", n, f)
	}
	clk.Advance(2 * time.Minute)
	n, f, err := q.RecoverStale(ctx, time.Minute)
	if err != nil || n != 1 || f != 1 {
		t.Fatalf("RecoverStale = %d, %d, %v; want 1, 1", n, f, err)
	}
	if got := status(t, q, retryable); got != "queued" {
		t.Errorf("retryable status = %s, want queued", got)
	}
	if got := status(t, q, exhausted); got != "failed" {
		t.Errorf("exhausted status = %s, want failed", got)
	}
}

func TestPoolRunsHandlersAndStops(t *testing.T) {
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	q := NewQueue(s.DB)
	pool := NewPool(q, PoolConfig{Workers: 2, PollInterval: 20 * time.Millisecond}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	var ran atomic.Int32
	pool.Handle("echo", func(ctx context.Context, job Job) (any, error) {
		ran.Add(1)
		var p map[string]string
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return nil, Permanent(err)
		}
		return map[string]string{"echo": p["msg"]}, nil
	})
	pool.Handle("panics", func(context.Context, Job) (any, error) { panic("oops") })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- pool.Run(ctx) }()

	echo := mustEnqueue(t, q, "echo", map[string]string{"msg": "你好"}, EnqueueOptions{})
	unknown := mustEnqueue(t, q, "nobody-handles-this", nil, EnqueueOptions{})
	panics := mustEnqueue(t, q, "panics", nil, EnqueueOptions{MaxAttempts: 1})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if status(t, q, echo) == "succeeded" && status(t, q, unknown) == "failed" && status(t, q, panics) == "failed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	row, _ := q.Get(context.Background(), echo)
	if row.Status != "succeeded" || row.Result.String != `{"echo":"你好"}` {
		t.Errorf("echo job: status=%s result=%q", row.Status, row.Result.String)
	}
	if got := status(t, q, unknown); got != "failed" {
		t.Errorf("unknown kind status = %s, want failed", got)
	}
	if got := status(t, q, panics); got != "failed" {
		t.Errorf("panicking job status = %s, want failed", got)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pool did not stop")
	}
	if ran.Load() != 1 {
		t.Errorf("echo handler ran %d times, want 1", ran.Load())
	}
}

func TestDefaultBackoff(t *testing.T) {
	cases := map[int]time.Duration{1: 10 * time.Second, 2: 20 * time.Second, 3: 40 * time.Second, 20: 10 * time.Minute}
	for attempt, want := range cases {
		if got := DefaultBackoff(attempt); got != want {
			t.Errorf("DefaultBackoff(%d) = %s, want %s", attempt, got, want)
		}
	}
}

func TestOnFinishReportsTheOutcome(t *testing.T) {
	q, _ := newQueue(t)
	pool := NewPool(q, PoolConfig{Workers: 1, PollInterval: 20 * time.Millisecond}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	got := make(chan Outcome, 4)
	pool.OnFinish(func(_ Job, o Outcome) { got <- o })
	pool.Handle("ok", func(context.Context, Job) (any, error) { return map[string]int{"n": 1}, nil })
	pool.Handle("bad", func(context.Context, Job) (any, error) { return nil, Permanent(errors.New("no")) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go pool.Run(ctx)
	if _, err := q.Enqueue(ctx, "ok", nil, EnqueueOptions{}); err != nil {
		t.Fatal(err)
	}
	if o := <-got; o.Status != "succeeded" {
		t.Errorf("ok outcome = %+v", o)
	}
	if _, err := q.Enqueue(ctx, "bad", nil, EnqueueOptions{}); err != nil {
		t.Fatal(err)
	}
	if o := <-got; o.Status != "failed" || o.Err == nil {
		t.Errorf("bad outcome = %+v", o)
	}
}

// A job remembers the request that enqueued it; jobs it enqueues in turn
// keep the same request ID.
func TestJobsKeepTheRequestID(t *testing.T) {
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	q := NewQueue(s.DB)
	pool := NewPool(q, PoolConfig{Workers: 1, PollInterval: 20 * time.Millisecond}, slog.New(slog.DiscardHandler))
	seen := make(chan string, 2)
	pool.Handle("first", func(ctx context.Context, job Job) (any, error) {
		seen <- job.RequestID
		_, err := q.Enqueue(ctx, "second", nil, EnqueueOptions{})
		return nil, err
	})
	pool.Handle("second", func(ctx context.Context, job Job) (any, error) {
		seen <- logging.RequestID(ctx)
		return nil, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go pool.Run(ctx)

	if _, err := q.Enqueue(logging.WithRequestID(context.Background(), "req-42"), "first", nil, EnqueueOptions{}); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"req-42", "req-42"} {
		select {
		case got := <-seen:
			if got != want {
				t.Errorf("job %d request ID = %q, want %q", i+1, got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("job %d did not run", i+1)
		}
	}
}
