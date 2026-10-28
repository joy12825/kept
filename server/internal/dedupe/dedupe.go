package dedupe

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/document"
	"kept/server/internal/jobs"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// Kind is the scan of one workspace (weekly, or at once).
const Kind = "dedupe.scan"

// maxListed caps the pairs a page shows.
const maxListed = 100

var (
	ErrNotFound  = apperr.New(apperr.KindNotFound, "这一对已经处理过了")
	errNotInPair = apperr.New(apperr.KindInvalid, "要保留的文档不在这一对里")
)

type Options struct {
	Queue     *jobs.Queue
	Documents *document.Service
	Now       func() time.Time
}

type Service struct {
	db  *sql.DB
	q   *sqlc.Queries
	opt Options
}

func NewService(db *sql.DB, opt Options) *Service {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	return &Service{db: db, q: sqlc.New(db), opt: opt}
}

// Result is a scan's result.
type Result struct {
	Fingerprinted int `json:"fingerprinted"`
	Compared      int `json:"compared"` // documents with a fingerprint
	Open          int `json:"open"`     // open pairs after the scan
}

// Scan brings fingerprints up to date and records the near pairs. Pairs a
// person dismissed stay dismissed; open pairs that are no longer near go.
func (s *Service) Scan(ctx context.Context, ws int64) (Result, error) {
	var res Result
	for {
		rows, err := s.q.ListStaleFingerprints(ctx, ws)
		if err != nil {
			return res, err
		}
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			fp, ok := Fingerprint(r.Content)
			if err := s.q.SetFingerprint(ctx, sqlc.SetFingerprintParams{Simhash: sql.NullInt64{Int64: int64(fp), Valid: ok}, Rev: r.IndexRev, WorkspaceID: ws, ID: r.ID}); err != nil {
				return res, err
			}
			res.Fingerprinted++
		}
	}
	fps, err := s.q.ListFingerprints(ctx, ws)
	if err != nil {
		return res, err
	}
	res.Compared = len(fps)
	type key struct{ a, b int64 }
	near := map[key]int{}
	for i := range fps {
		for j := i + 1; j < len(fps); j++ {
			if d := Distance(uint64(fps[i].Simhash), uint64(fps[j].Simhash)); d <= MaxDistance {
				ok, err := s.confirm(ctx, ws, fps[i].ID, fps[j].ID, d)
				if err != nil {
					return res, err
				}
				if ok {
					near[key{fps[i].ID, fps[j].ID}] = d // ordered by id, so a < b
				}
			}
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	q := s.q.WithTx(tx)
	have, err := q.ListDuplicatePairs(ctx, ws)
	if err != nil {
		return res, err
	}
	for _, p := range have {
		if _, ok := near[key{p.DocA, p.DocB}]; !ok && p.Status == "open" {
			if err := q.DeleteDuplicatePair(ctx, sqlc.DeleteDuplicatePairParams{WorkspaceID: ws, ID: p.ID}); err != nil {
				return res, err
			}
		}
	}
	now := s.opt.Now().UnixMilli()
	for k, d := range near {
		if err := q.UpsertDuplicatePair(ctx, sqlc.UpsertDuplicatePairParams{WorkspaceID: ws, DocA: k.a, DocB: k.b, Distance: int64(d), Now: now}); err != nil {
			return res, err
		}
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	open, err := s.q.ListOpenDuplicatePairs(ctx, ws)
	res.Open = len(open)
	return res, err
}

// confirm checks a candidate pair's numbers.
func (s *Service) confirm(ctx context.Context, ws, a, b int64, distance int) (bool, error) {
	ta, err := s.q.GetDocumentContent(ctx, sqlc.GetDocumentContentParams{WorkspaceID: ws, ID: a})
	if err != nil {
		return false, err
	}
	tb, err := s.q.GetDocumentContent(ctx, sqlc.GetDocumentContentParams{WorkspaceID: ws, ID: b})
	if err != nil {
		return false, err
	}
	return Confirm(ta, tb, distance), nil
}

// QueueScan queues a scan of the workspace.
func (s *Service) QueueScan(ctx context.Context, scope tenant.Scope) (int64, error) {
	ws := scope.WorkspaceID()
	return s.opt.Queue.Enqueue(ctx, Kind, nil, jobs.EnqueueOptions{WorkspaceID: &ws, MaxAttempts: 1, Priority: 1})
}

// Pair is an open pair with both documents.
type Pair struct {
	ID         int64
	Distance   int
	A, B       document.Detail
	FoundAt    time.Time
	Similarity int // percent of matching bits
}

// List returns the open pairs, most similar first.
func (s *Service) List(ctx context.Context, scope tenant.Scope) ([]Pair, error) {
	return s.ListFor(ctx, scope, 0)
}

// ListFor is List limited to the pairs with doc (v1.3.7, the details
// page's hint); 0 lists all.
func (s *Service) ListFor(ctx context.Context, scope tenant.Scope, doc int64) ([]Pair, error) {
	rows, err := s.q.ListOpenDuplicatePairs(ctx, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	out := make([]Pair, 0, min(len(rows), maxListed))
	for _, r := range rows {
		if len(out) == maxListed {
			break
		}
		if doc != 0 && r.DocA != doc && r.DocB != doc {
			continue
		}
		a, err := s.opt.Documents.GetDetail(ctx, scope, r.DocA)
		if errors.Is(err, document.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		b, err := s.opt.Documents.GetDetail(ctx, scope, r.DocB)
		if errors.Is(err, document.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, Pair{ID: r.ID, Distance: int(r.Distance), A: a, B: b, FoundAt: time.UnixMilli(r.FoundAt),
			Similarity: 100 - int(r.Distance)*100/64})
	}
	return out, nil
}

func (s *Service) pair(ctx context.Context, scope tenant.Scope, id int64) (sqlc.DuplicatePair, error) {
	p, err := s.q.GetDuplicatePair(ctx, sqlc.GetDuplicatePairParams{WorkspaceID: scope.WorkspaceID(), ID: id})
	if errors.Is(err, sql.ErrNoRows) || (err == nil && p.Status != "open") {
		return p, ErrNotFound
	}
	return p, err
}

// Dismiss records that the pair is not a duplicate.
func (s *Service) Dismiss(ctx context.Context, scope tenant.Scope, id int64) error {
	n, err := s.q.DismissDuplicatePair(ctx, sqlc.DismissDuplicatePairParams{WorkspaceID: scope.WorkspaceID(), ID: id})
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

// Keep keeps one document of the pair: the other's tags are added to it,
// and the other goes to the trash (it can be restored from there).
func (s *Service) Keep(ctx context.Context, scope tenant.Scope, id, keep int64) error {
	p, err := s.pair(ctx, scope, id)
	if err != nil {
		return err
	}
	var other int64
	switch keep {
	case p.DocA:
		other = p.DocB
	case p.DocB:
		other = p.DocA
	default:
		return errNotInPair
	}
	kept, err := s.opt.Documents.GetDetail(ctx, scope, keep)
	if err != nil {
		return err
	}
	gone, err := s.opt.Documents.GetDetail(ctx, scope, other)
	if err != nil {
		return err
	}
	tags := slices.Clone(kept.TagIDs)
	for _, t := range gone.TagIDs {
		if !slices.Contains(tags, t) {
			tags = append(tags, t)
		}
	}
	if len(tags) != len(kept.TagIDs) {
		if _, err := s.opt.Documents.Update(ctx, scope, keep, document.Patch{TagIDs: &tags}); err != nil {
			return err
		}
	}
	if _, err := s.opt.Documents.Delete(ctx, scope, document.Selection{IDs: []int64{other}}); err != nil {
		return err
	}
	return s.q.DeleteDuplicatePair(ctx, sqlc.DeleteDuplicatePairParams{WorkspaceID: scope.WorkspaceID(), ID: p.ID})
}

// Register adds the scan job's handler.
func (s *Service) Register(p *jobs.Pool) {
	p.Handle(Kind, func(ctx context.Context, job jobs.Job) (any, error) {
		if job.WorkspaceID == nil {
			return nil, jobs.Permanent(errors.New("dedupe.scan needs a workspace"))
		}
		return s.Scan(ctx, *job.WorkspaceID)
	})
}
