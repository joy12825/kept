package storage

import (
	"context"
	"database/sql"
	"errors"

	"kept/server/internal/files"
	"kept/server/internal/jobs"
	"kept/server/internal/store/sqlc"
)

// Hot and cold (v1.7.8): a workspace may name a cold storage and an age.
// Documents dated earlier than that many months ago belong in the cold
// storage, the rest in the workspace's storage. The move job puts every
// document where it belongs, so changing either, or time passing, is
// handled the same way; a daily check queues the job.

// KindTier is the daily check.
const KindTier = "storage.tier"

// plan is where a workspace's documents belong.
type plan struct {
	hot, cold string
	months    int
	cutoff    string // YYYY-MM-DD: documents dated before go cold; "" no cold storage
}

// want is where a document belongs: a family member's private document
// stays on this server (v1.8.1, see seal.go).
func (p plan) want(date string, private bool) string {
	if private {
		return files.Local
	}
	if p.cutoff != "" && date < p.cutoff {
		return p.cold
	}
	return p.hot
}

func (p plan) same(o plan) bool { return p.hot == o.hot && p.cold == o.cold && p.months == o.months }

func (s *Service) planOf(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, ws int64) (plan, error) {
	var p plan
	err := q.QueryRowContext(ctx, `SELECT storage, cold_storage, cold_after_months FROM workspaces WHERE id = ? AND deleted_at IS NULL`, ws).
		Scan(&p.hot, &p.cold, &p.months)
	if err != nil {
		return p, err
	}
	if p.cold != "" && p.months > 0 {
		p.cutoff = s.opt.Now().AddDate(0, -p.months, 0).Format("2006-01-02")
	}
	return p, nil
}

// pendingOf counts the documents not where they belong.
func (s *Service) pendingOf(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, ws int64, p plan) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM documents WHERE workspace_id = ?
		AND storage <> CASE WHEN private = 1 THEN 'local' WHEN ? <> '' AND created_date < ? THEN ? ELSE ? END`, ws, p.cutoff, p.cutoff, p.cold, p.hot).Scan(&n)
	if err != nil {
		return 0, err
	}
	sealed, err := s.pendingSealedOf(ctx, q, ws, p)
	return n + sealed, err
}

// queueMove queues the move job unless one is waiting or running; it
// returns the job, 0 when nothing needs moving.
func (s *Service) queueMove(ctx context.Context, tx *sql.Tx, q *sqlc.Queries, ws int64, p plan) (int64, error) {
	pending, err := s.pendingOf(ctx, tx, ws, p)
	if err != nil {
		return 0, err
	}
	var active int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM jobs WHERE kind = ? AND workspace_id = ? AND status IN ('queued', 'running') ORDER BY id LIMIT 1`, KindMigrate, ws).Scan(&active)
	switch {
	case err == nil:
		return active, nil // it reads the plan when it runs
	case !errors.Is(err, sql.ErrNoRows):
		return 0, err
	case pending == 0:
		return 0, nil
	}
	return s.opt.Queue.EnqueueWith(ctx, q, KindMigrate, migratePayload{Target: p.hot}, jobs.EnqueueOptions{WorkspaceID: &ws, MaxAttempts: 10})
}

// Tier is a workspace's cold storage.
type Tier struct {
	Storage string // "" none
	Months  int
}

// AnyCold: some workspace has a cold storage (the daily check has work).
func (s *Service) AnyCold(ctx context.Context) bool {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM workspaces WHERE deleted_at IS NULL AND cold_storage <> '' AND cold_after_months > 0`).Scan(&n)
	return err == nil && n > 0
}

// Requeue queues the move job when a workspace's documents are not where
// they belong (v1.8.1: a document became private, or stopped being so).
func (s *Service) Requeue(ctx context.Context, ws int64) error {
	if s.opt.Queue == nil {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := s.planOf(ctx, tx, ws)
	if err != nil {
		return err
	}
	id, err := s.queueMove(ctx, tx, s.q.WithTx(tx), ws, p)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if id != 0 {
		s.opt.Queue.Wake()
	}
	return nil
}
