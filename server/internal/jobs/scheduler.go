package jobs

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"kept/server/internal/store/sqlc"
)

// Entry is one scheduler entry (design doc 11.7): every Every, or daily at
// At ("15:04", server local time), it enqueues a job of Kind. PerWorkspace
// entries enqueue one job for each workspace that is not deleted.
type Entry struct {
	Name         string
	Kind         string
	Every        time.Duration
	At           string
	PerWorkspace bool
	MaxAttempts  int
	// When, if set, is asked at each due time: false skips this run
	// without a job (a feature configured on the page and turned off).
	When func(ctx context.Context) bool
	// WhenWorkspace, for a per-workspace entry, is asked for each
	// workspace: false skips it (v1.7.16: no mail fetch for a workspace
	// without a mailbox, so the task list is not full of empty runs).
	WhenWorkspace func(ctx context.Context, ws int64) bool
}

// next is when e is due after now.
func (e Entry) next(now time.Time) (time.Time, error) {
	if e.At == "" {
		if e.Every <= 0 {
			return time.Time{}, fmt.Errorf("schedule %s: neither Every nor At", e.Name)
		}
		return now.Add(e.Every), nil
	}
	at, err := time.ParseInLocation("15:04", e.At, now.Location())
	if err != nil {
		return time.Time{}, fmt.Errorf("schedule %s: %w", e.Name, err)
	}
	t := time.Date(now.Year(), now.Month(), now.Day(), at.Hour(), at.Minute(), 0, 0, now.Location())
	if !t.After(now) {
		t = t.AddDate(0, 0, 1)
	}
	return t, nil
}

// Scheduler turns entries into jobs. Each check advances the due entries
// and enqueues their jobs in one transaction, so an entry is enqueued once
// per period; runs missed while the server was down are made up once. A
// new entry runs at the first check. A job is not enqueued again while the
// previous one for the same entry (and workspace) is still queued or running.
type Scheduler struct {
	db      *sql.DB
	queue   *Queue
	entries []Entry
	log     *slog.Logger
	now     func() time.Time
	every   time.Duration
}

type SchedulerOption func(*Scheduler)

// WithSchedulerClock replaces time.Now (tests).
func WithSchedulerClock(now func() time.Time) SchedulerOption {
	return func(s *Scheduler) { s.now = now }
}

func NewScheduler(db *sql.DB, q *Queue, entries []Entry, log *slog.Logger, opts ...SchedulerOption) *Scheduler {
	s := &Scheduler{db: db, queue: q, entries: entries, log: log, now: time.Now, every: 30 * time.Second}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Run checks every 30 seconds until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(s.every)
	defer t.Stop()
	for {
		if n, err := s.Check(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("scheduler check", "err", err)
		} else if n > 0 {
			s.log.Debug("scheduler enqueued jobs", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Check enqueues the jobs of every due entry and returns how many.
func (s *Scheduler) Check(ctx context.Context) (int, error) {
	now := s.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	qs := sqlc.New(tx)
	for _, e := range s.entries {
		if err := qs.InsertSchedule(ctx, sqlc.InsertScheduleParams{Name: e.Name, NextRunAt: now.UnixMilli()}); err != nil {
			return 0, err
		}
	}
	rows, err := qs.ListSchedules(ctx)
	if err != nil {
		return 0, err
	}
	due := map[string]int64{}
	for _, r := range rows {
		if r.NextRunAt <= now.UnixMilli() {
			due[r.Name] = r.NextRunAt
		}
	}
	var workspaces []int64
	n := 0
	for _, e := range s.entries {
		at, ok := due[e.Name]
		if !ok {
			continue
		}
		next, err := e.next(now)
		if err != nil {
			return 0, err
		}
		if c, err := qs.AdvanceSchedule(ctx, sqlc.AdvanceScheduleParams{
			NextRunAt: next.UnixMilli(), Now: sql.NullInt64{Int64: now.UnixMilli(), Valid: true}, Name: e.Name, DueAt: at,
		}); err != nil || c == 0 {
			if err != nil {
				return 0, err
			}
			continue
		}
		if e.When != nil && !e.When(ctx) {
			continue
		}
		targets := []*int64{nil}
		if e.PerWorkspace {
			if workspaces == nil {
				if workspaces, err = qs.ListActiveWorkspaceIDs(ctx); err != nil {
					return 0, err
				}
			}
			targets = targets[:0]
			for _, id := range workspaces {
				targets = append(targets, &id)
			}
		}
		for _, ws := range targets {
			if ws != nil && e.WhenWorkspace != nil && !e.WhenWorkspace(ctx, *ws) {
				continue
			}
			busy, err := qs.CountUnfinishedJobs(ctx, sqlc.CountUnfinishedJobsParams{Kind: e.Kind, WorkspaceID: nullInt(ws)})
			if err != nil {
				return 0, err
			}
			if busy > 0 {
				continue
			}
			if _, err := s.queue.EnqueueWith(ctx, qs, e.Kind, nil, EnqueueOptions{WorkspaceID: ws, MaxAttempts: e.MaxAttempts}); err != nil {
				return 0, err
			}
			n++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	if n > 0 {
		s.queue.Wake()
	}
	return n, nil
}
