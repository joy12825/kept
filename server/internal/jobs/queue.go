// Package jobs is the background job queue: jobs live in the SQLite jobs
// table and are executed by a pool of in-process workers (no Redis).
package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"kept/server/internal/audit"
	"kept/server/internal/logging"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// Job is what a Handler receives.
type Job struct {
	ID          int64
	WorkspaceID *int64
	Kind        string
	Payload     json.RawMessage
	Attempt     int // 1 for the first run
	MaxAttempts int
	RequestID   string // the HTTP request that enqueued it; "" when none
}

// Handler executes one job. A returned error makes the job retry with
// backoff, unless it is wrapped with Permanent or attempts are exhausted.
type Handler func(ctx context.Context, job Job) (result any, err error)

type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent marks err as not worth retrying (bad input, unsupported format...).
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err}
}

func IsPermanent(err error) bool {
	var p permanentError
	return errors.As(err, &p)
}

type EnqueueOptions struct {
	WorkspaceID *int64
	Priority    int       // higher runs first
	MaxAttempts int       // default 3
	RunAfter    time.Time // zero means now
	// ClientUploadID is unique per workspace (a phone's upload, design doc 9.4).
	ClientUploadID string
}

// Queue wraps the jobs table.
type Queue struct {
	q       *sqlc.Queries
	now     func() time.Time
	backoff func(attempt int) time.Duration
	wake    chan struct{}
}

type QueueOption func(*Queue)

// WithClock replaces time.Now (tests).
func WithClock(now func() time.Time) QueueOption { return func(q *Queue) { q.now = now } }

// WithBackoff replaces the retry delay policy.
func WithBackoff(f func(attempt int) time.Duration) QueueOption {
	return func(q *Queue) { q.backoff = f }
}

func NewQueue(db sqlc.DBTX, opts ...QueueOption) *Queue {
	q := &Queue{
		q:       sqlc.New(db),
		now:     time.Now,
		backoff: DefaultBackoff,
		wake:    make(chan struct{}, 1),
	}
	for _, o := range opts {
		o(q)
	}
	return q
}

// DefaultBackoff waits 10s, 20s, 40s ... capped at 10 minutes.
func DefaultBackoff(attempt int) time.Duration {
	d := 10 * time.Second
	for i := 1; i < attempt && d < 10*time.Minute; i++ {
		d *= 2
	}
	return min(d, 10*time.Minute)
}

// Enqueue stores a new job and wakes an idle worker.
func (q *Queue) Enqueue(ctx context.Context, kind string, payload any, opt EnqueueOptions) (int64, error) {
	id, err := q.EnqueueWith(ctx, q.q, kind, payload, opt)
	if err != nil {
		return 0, err
	}
	q.Wake()
	return id, nil
}

// Active returns an unfinished (queued or running) job of kind, if any.
func (q *Queue) Active(ctx context.Context, kind string) (int64, bool, error) {
	id, err := q.q.FindActiveJob(ctx, kind)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return id, err == nil, err
}

// EnqueueWith stores a new job through qs, e.g. inside a transaction. The
// caller calls Wake after committing.
func (q *Queue) EnqueueWith(ctx context.Context, qs *sqlc.Queries, kind string, payload any, opt EnqueueOptions) (int64, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("encode payload for %s: %w", kind, err)
	}
	if payload == nil {
		body = []byte("{}")
	}
	now := q.now()
	runAfter := opt.RunAfter
	if runAfter.IsZero() {
		runAfter = now
	}
	maxAttempts := opt.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	id, err := qs.InsertJob(ctx, sqlc.InsertJobParams{
		WorkspaceID:    nullInt(opt.WorkspaceID),
		Kind:           kind,
		Payload:        string(body),
		Priority:       int64(opt.Priority),
		MaxAttempts:    int64(maxAttempts),
		RunAfter:       runAfter.UnixMilli(),
		CreatedAt:      now.UnixMilli(),
		ClientUploadID: sql.NullString{String: opt.ClientUploadID, Valid: opt.ClientUploadID != ""},
		RequestID:      logging.RequestID(ctx),
		// who started it, for the task list's “mine” (v1.7.14)
		CreatedBy: nullInt64(audit.ActorFrom(ctx).UserID),
	})
	if err != nil {
		return 0, fmt.Errorf("enqueue %s: %w", kind, err)
	}
	return id, nil
}

// Wake tells an idle worker to look for work now.
func (q *Queue) Wake() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// Get returns the stored row, mainly for tests and the task list API.
func (q *Queue) Get(ctx context.Context, id int64) (sqlc.Job, error) {
	return q.q.GetJob(ctx, id)
}

// claim returns the next runnable job, or nil when there is none.
func (q *Queue) claim(ctx context.Context) (*sqlc.Job, error) {
	row, err := q.q.ClaimNextJob(ctx, sql.NullInt64{Int64: q.now().UnixMilli(), Valid: true})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (q *Queue) heartbeat(ctx context.Context, id int64) error {
	return q.q.HeartbeatJob(ctx, sqlc.HeartbeatJobParams{
		Now: sql.NullInt64{Int64: q.now().UnixMilli(), Valid: true},
		ID:  id,
	})
}

func (q *Queue) complete(ctx context.Context, id int64, result any) error {
	var res sql.NullString
	if result != nil {
		b, err := json.Marshal(result)
		if err != nil {
			return q.finishWithError(ctx, id, 0, 0, Permanent(fmt.Errorf("encode result: %w", err)))
		}
		res = sql.NullString{String: string(b), Valid: true}
	}
	return q.q.CompleteJob(ctx, sqlc.CompleteJobParams{
		Result: res,
		Now:    sql.NullInt64{Int64: q.now().UnixMilli(), Valid: true},
		ID:     id,
	})
}

// finishWithError either schedules a retry or marks the job failed.
func (q *Queue) finishWithError(ctx context.Context, id int64, attempt, maxAttempts int, jobErr error) error {
	msg := sql.NullString{String: jobErr.Error(), Valid: true}
	if !IsPermanent(jobErr) && attempt < maxAttempts {
		return q.q.RetryJob(ctx, sqlc.RetryJobParams{
			Error:    msg,
			RunAfter: q.now().Add(q.backoff(attempt)).UnixMilli(),
			ID:       id,
		})
	}
	return q.q.FailJob(ctx, sqlc.FailJobParams{
		Error: msg,
		Now:   sql.NullInt64{Int64: q.now().UnixMilli(), Valid: true},
		ID:    id,
	})
}

// RecoverStale handles running jobs whose heartbeat is older than staleAfter,
// typically left behind by a crash: requeued if attempts remain, else failed.
func (q *Queue) RecoverStale(ctx context.Context, staleAfter time.Duration) (requeued, failed int64, err error) {
	now := q.now()
	cutoff := sql.NullInt64{Int64: now.Add(-staleAfter).UnixMilli(), Valid: true}
	requeued, err = q.q.RequeueStaleJobs(ctx, sqlc.RequeueStaleJobsParams{
		Now:    now.UnixMilli(),
		Error:  sql.NullString{String: "执行任务的进程已中断，已重新排队", Valid: true},
		Cutoff: cutoff,
	})
	if err != nil {
		return 0, 0, err
	}
	failed, err = q.q.FailStaleJobs(ctx, sqlc.FailStaleJobsParams{
		Now:    sql.NullInt64{Int64: now.UnixMilli(), Valid: true},
		Error:  sql.NullString{String: "执行任务的进程已中断，且已达到最大重试次数", Valid: true},
		Cutoff: cutoff,
	})
	return requeued, failed, err
}

func nullInt(p *int64) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *p, Valid: true}
}

func toJob(r *sqlc.Job) Job {
	j := Job{
		ID:          r.ID,
		Kind:        r.Kind,
		Payload:     json.RawMessage(r.Payload),
		Attempt:     int(r.Attempts),
		MaxAttempts: int(r.MaxAttempts),
		RequestID:   r.RequestID,
	}
	if r.WorkspaceID.Valid {
		ws := r.WorkspaceID.Int64
		j.WorkspaceID = &ws
	}
	return j
}

// SetProgress stores the progress of a running job (its result column until
// it completes), e.g. {"stage": "ocr", "progress": 40}.
func (q *Queue) SetProgress(ctx context.Context, id int64, progress any) error {
	b, err := json.Marshal(progress)
	if err != nil {
		return err
	}
	return q.q.SetJobProgress(ctx, sqlc.SetJobProgressParams{Result: sql.NullString{String: string(b), Valid: true}, ID: id})
}

// SetDocument links a job to the document it produced (or, for a duplicate
// upload, the existing one).
func (q *Queue) SetDocument(ctx context.Context, id, documentID int64) error {
	return q.q.SetJobDocument(ctx, sqlc.SetJobDocumentParams{DocumentID: sql.NullInt64{Int64: documentID, Valid: true}, ID: id})
}

// ListRecent returns a workspace's jobs whose kind starts with kindPrefix
// (e.g. "ingest."), created since, newest first; not those about a private
// document the scope's viewer may not see (v1.8.0).
func (q *Queue) ListRecent(ctx context.Context, scope tenant.Scope, kindPrefix string, since time.Time, limit int) ([]sqlc.Job, error) {
	return q.q.ListRecentJobs(ctx, sqlc.ListRecentJobsParams{
		WorkspaceID: sql.NullInt64{Int64: scope.WorkspaceID(), Valid: true}, KindPrefix: kindPrefix, Since: since.UnixMilli(), MaxRows: int64(limit),
		Viewer: scope.Viewer(),
	})
}

// ListWorkspace returns a workspace's jobs of every kind created since,
// optionally with one status, newest first, with the title of the
// document a job made or worked on.
// PurgeFinished deletes the jobs that finished before before; queued and
// running ones stay (v1.7.19: the table grew without end).
func (q *Queue) PurgeFinished(ctx context.Context, before time.Time) (int64, error) {
	return q.q.PurgeFinishedJobs(ctx, sql.NullInt64{Int64: before.UnixMilli(), Valid: true})
}

// ListFilter narrows a workspace's job list (v1.7.14).
type ListFilter struct {
	Status    string   // "" any; "active" queued or running
	Kinds     []string // none: any
	CreatedBy int64    // 0: anyone
	People    bool     // only jobs a person started, not scheduled ones (v1.7.16)
}

func (q *Queue) ListWorkspace(ctx context.Context, scope tenant.Scope, since time.Time, f ListFilter, limit int) ([]sqlc.ListWorkspaceJobsRow, error) {
	kinds := ""
	if len(f.Kinds) > 0 {
		kinds = "," + strings.Join(f.Kinds, ",") + ","
	}
	return q.q.ListWorkspaceJobs(ctx, sqlc.ListWorkspaceJobsParams{
		WorkspaceID: sql.NullInt64{Int64: scope.WorkspaceID(), Valid: true}, Since: since.UnixMilli(), Status: f.Status, Kinds: kinds, CreatedBy: f.CreatedBy, People: boolInt64(f.People),
		MaxRows: int64(limit), Viewer: scope.Viewer(),
	})
}

func boolInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func nullInt64(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: v != 0} }

// UnfinishedPayloads returns the payloads of queued and running jobs.
func (q *Queue) UnfinishedPayloads(ctx context.Context) ([]string, error) {
	return q.q.ListUnfinishedJobPayloads(ctx)
}
