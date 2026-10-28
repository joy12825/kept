// Package reading keeps each person's reading data (v0.4): where they
// stopped in a document, their status, rating and dates, and their
// highlights and notes. It belongs to the person, not the workspace, so a
// viewer may write their own, and none of it goes into the activity log.
package reading

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"kept/server/internal/apperr"
	"kept/server/internal/document"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

var (
	errPosition = apperr.New(apperr.KindInvalid, "阅读位置不正确")
	errStatus   = apperr.New(apperr.KindInvalid, "阅读状态只能是想读、在读或读完")
	errRating   = apperr.New(apperr.KindInvalid, "评分为 1 到 5 星，0 表示不评")
	errDate     = apperr.New(apperr.KindInvalid, "日期格式应为 YYYY-MM-DD")
)

// Statuses a person can set; "" means none.
var Statuses = []string{"want", "reading", "finished"}

// Progress is a person's reading of one document.
type Progress struct {
	Position   string // "page:12" or an EPUB CFI; "" when never opened
	Percent    float64
	Status     string // "", want, reading, finished
	Rating     int    // 0: not rated
	StartedOn  string // YYYY-MM-DD or ""
	FinishedOn string
	Highlights int
	UpdatedAt  *time.Time
}

// StatePatch changes the status, rating or dates; nil fields stay.
type StatePatch struct {
	Status     *string
	Rating     *int
	StartedOn  *string
	FinishedOn *string
}

type Service struct {
	db  *sql.DB
	q   *sqlc.Queries
	now func() time.Time
}

func NewService(db *sql.DB) *Service { return &Service{db: db, q: sqlc.New(db), now: time.Now} }

// readable checks the document exists in the workspace, outside the trash.
func (s *Service) readable(ctx context.Context, scope tenant.Scope, docID int64) error {
	d, err := s.q.GetDocument(ctx, sqlc.GetDocumentParams{WorkspaceID: scope.WorkspaceID(), ID: docID, Viewer: scope.Viewer()})
	if errors.Is(err, sql.ErrNoRows) || (err == nil && d.DeletedAt.Valid) {
		return document.ErrNotFound
	}
	return err
}

// Get returns the user's progress in a document (empty when none).
func (s *Service) Get(ctx context.Context, scope tenant.Scope, userID, docID int64) (Progress, error) {
	if err := s.readable(ctx, scope, docID); err != nil {
		return Progress{}, err
	}
	return s.get(ctx, scope, userID, docID)
}

func (s *Service) get(ctx context.Context, scope tenant.Scope, userID, docID int64) (Progress, error) {
	n, err := s.q.CountHighlights(ctx, sqlc.CountHighlightsParams{UserID: userID, DocumentID: docID, WorkspaceID: scope.WorkspaceID()})
	if err != nil {
		return Progress{}, err
	}
	r, err := s.q.GetReadingProgress(ctx, sqlc.GetReadingProgressParams{UserID: userID, DocumentID: docID, WorkspaceID: scope.WorkspaceID()})
	if errors.Is(err, sql.ErrNoRows) {
		return Progress{Highlights: int(n)}, nil
	}
	if err != nil {
		return Progress{}, err
	}
	at := time.UnixMilli(r.UpdatedAt)
	return Progress{Position: r.Position, Percent: r.Percent, Status: r.Status, Rating: int(r.Rating), StartedOn: r.StartedOn,
		FinishedOn: r.FinishedOn, Highlights: int(n), UpdatedAt: &at}, nil
}

// Save records where the user is now.
func (s *Service) Save(ctx context.Context, scope tenant.Scope, userID, docID int64, position string, percent float64) (Progress, error) {
	position = strings.TrimSpace(position)
	if utf8.RuneCountInString(position) > 2000 || math.IsNaN(percent) || percent < 0 || percent > 1 {
		return Progress{}, errPosition
	}
	if err := s.readable(ctx, scope, docID); err != nil {
		return Progress{}, err
	}
	now := s.now()
	if err := s.q.UpsertReadingProgress(ctx, sqlc.UpsertReadingProgressParams{
		UserID: userID, DocumentID: docID, WorkspaceID: scope.WorkspaceID(), Position: position, Percent: percent, Now: now.UnixMilli(),
	}); err != nil {
		return Progress{}, err
	}
	// Opening a book starts reading it.
	if _, err := s.db.ExecContext(ctx, `UPDATE reading_progress SET status = 'reading', started_on = CASE WHEN started_on = '' THEN ? ELSE started_on END
		WHERE user_id = ? AND document_id = ? AND status IN ('', 'want')`, now.Format("2006-01-02"), userID, docID); err != nil {
		return Progress{}, err
	}
	return s.get(ctx, scope, userID, docID)
}

// SetState changes the status, rating or dates. Starting to read fills in
// the start date, finishing the finish date, when they are empty.
func (s *Service) SetState(ctx context.Context, scope tenant.Scope, userID, docID int64, p StatePatch) (Progress, error) {
	if err := s.readable(ctx, scope, docID); err != nil {
		return Progress{}, err
	}
	cur, err := s.get(ctx, scope, userID, docID)
	if err != nil {
		return Progress{}, err
	}
	today := s.now().Format("2006-01-02")
	if p.Status != nil {
		if *p.Status != "" && !slices.Contains(Statuses, *p.Status) {
			return Progress{}, errStatus
		}
		cur.Status = *p.Status
		if (cur.Status == "reading" || cur.Status == "finished") && cur.StartedOn == "" && p.StartedOn == nil {
			cur.StartedOn = today
		}
		if cur.Status == "finished" && cur.FinishedOn == "" && p.FinishedOn == nil {
			cur.FinishedOn = today
		}
	}
	if p.Rating != nil {
		if *p.Rating < 0 || *p.Rating > 5 {
			return Progress{}, errRating
		}
		cur.Rating = *p.Rating
	}
	for _, d := range []struct {
		in  *string
		out *string
	}{{p.StartedOn, &cur.StartedOn}, {p.FinishedOn, &cur.FinishedOn}} {
		if d.in == nil {
			continue
		}
		v := strings.TrimSpace(*d.in)
		if v != "" {
			if _, err := time.Parse("2006-01-02", v); err != nil {
				return Progress{}, errDate
			}
		}
		*d.out = v
	}
	if err := s.q.UpsertReadingState(ctx, sqlc.UpsertReadingStateParams{
		UserID: userID, DocumentID: docID, WorkspaceID: scope.WorkspaceID(), Status: cur.Status, Rating: int64(cur.Rating),
		StartedOn: cur.StartedOn, FinishedOn: cur.FinishedOn, Now: s.now().UnixMilli(),
	}); err != nil {
		return Progress{}, err
	}
	return s.get(ctx, scope, userID, docID)
}
