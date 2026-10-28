// Package audit records who changed what. Business services call Record
// inside the same database transaction as the change itself, so a change
// and its audit entry are committed or rolled back together.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"time"

	"kept/server/internal/i18n"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// How the caller was authenticated (audit_events.actor_via).
const (
	ViaSession = "session"
	ViaToken   = "token"
	ViaDevice  = "device" // a paired phone (design doc 9.6)
	ViaSystem  = "system" // background jobs, the command line
)

// Actor is the caller an event is attributed to.
type Actor struct {
	UserID    int64  // 0 when there is no account behind the action
	Name      string // display name, for building summaries
	Via       string
	RequestID string
}

type ctxKey struct{}

// WithActor is called by the HTTP auth middleware once the caller is known.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, ctxKey{}, a)
}

// ActorFrom returns the request's caller. Without one (background jobs, the
// command line) the action is attributed to the system.
func ActorFrom(ctx context.Context) Actor {
	if a, ok := ctx.Value(ctxKey{}).(Actor); ok {
		return a
	}
	return Actor{Via: ViaSystem, Name: i18n.N("系统")}
}

// Event is one audited change. Never put passwords, tokens or document
// content in Summary or Changes.
type Event struct {
	WorkspaceID int64  // 0 for an instance-level event (account management)
	Action      string // "resource.verb", e.g. member.add
	TargetType  string
	TargetID    string
	Summary     string            // ready-to-display Chinese, e.g. 小王 停用了账号 xiaoli
	Changes     map[string][2]any // field -> [before, after]; only fields that changed
	Actor       *Actor            // overrides the context's caller (first-run setup has none yet)
	At          time.Time         // defaults to now
}

// Record writes e using q, which must be the Queries of the transaction
// making the change. An error must fail that transaction.
func Record(ctx context.Context, q *sqlc.Queries, e Event) error {
	if e.Action == "" || e.Summary == "" {
		return errors.New("audit: an event needs an action and a summary")
	}
	actor := ActorFrom(ctx)
	if e.Actor != nil {
		actor = *e.Actor
	}
	changes := "{}"
	if len(e.Changes) > 0 {
		b, err := json.Marshal(e.Changes)
		if err != nil {
			return err
		}
		changes = string(b)
	}
	at := e.At
	if at.IsZero() {
		at = time.Now()
	}
	return q.InsertAuditEvent(ctx, sqlc.InsertAuditEventParams{
		WorkspaceID: sql.NullInt64{Int64: e.WorkspaceID, Valid: e.WorkspaceID != 0},
		ActorUserID: sql.NullInt64{Int64: actor.UserID, Valid: actor.UserID != 0},
		ActorVia:    actor.Via,
		Action:      e.Action,
		TargetType:  e.TargetType,
		TargetID:    e.TargetID,
		Summary:     e.Summary,
		Changes:     changes,
		RequestID:   actor.RequestID,
		CreatedAt:   at.UnixMilli(),
	})
}

// Log reads the audit trail.
type Log struct {
	q *sqlc.Queries
}

func NewLog(db *sql.DB) *Log { return &Log{q: sqlc.New(db)} }

// Add records an event that goes with no change of its own (v1.9.7: a
// sensitive document downloaded or printed).
func (l *Log) Add(ctx context.Context, e Event) error { return Record(ctx, l.q, e) }

// Entry is one recorded event as shown in the activity pages.
type Entry struct {
	ID         int64
	Action     string
	ActorID    int64  // 0 when no account was behind the action
	ActorName  string // the account's current display name; "" once it is gone
	ActorVia   string
	TargetType string
	TargetID   string
	Summary    string
	Changes    map[string]any
	CreatedAt  time.Time
}

// Filter narrows a listing; zero values leave a criterion open.
type Filter struct {
	ActionPrefix string // literal prefix, e.g. "member."
	ActorID      int64
	From, To     time.Time // [From, To)
	BeforeID     int64     // keyset cursor: only entries older than this ID
	Limit        int       // clamped to [1, MaxPageSize]; 0 means DefaultPageSize
}

const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

// List returns a workspace's events, newest first, and the ID to pass as
// BeforeID for the next page (0 when there is none). The caller must have
// checked that the scope's role may read them.
func (l *Log) List(ctx context.Context, scope tenant.Scope, f Filter) ([]Entry, int64, error) {
	f = f.normalized()
	rows, err := l.q.ListWorkspaceAuditEvents(ctx, sqlc.ListWorkspaceAuditEventsParams{
		WorkspaceID:  sql.NullInt64{Int64: scope.WorkspaceID(), Valid: true},
		ActionPrefix: f.ActionPrefix, ActorID: f.ActorID,
		FromMs: f.From.UnixMilli(), ToMs: f.To.UnixMilli(), BeforeID: f.BeforeID, MaxRows: int64(f.Limit + 1),
		Viewer: scope.Viewer(),
	})
	if err != nil {
		return nil, 0, err
	}
	entries := make([]Entry, len(rows))
	for i, r := range rows {
		if entries[i], err = toEntry(sqlc.ListInstanceAuditEventsRow(r)); err != nil {
			return nil, 0, err
		}
	}
	page, next := paginate(entries, f.Limit)
	return page, next, nil
}

// ListTarget returns the events about one target in a workspace (a
// document's history), newest first, paginated like List.
func (l *Log) ListTarget(ctx context.Context, scope tenant.Scope, targetType, targetID string, f Filter) ([]Entry, int64, error) {
	f = f.normalized()
	rows, err := l.q.ListTargetAuditEvents(ctx, sqlc.ListTargetAuditEventsParams{
		WorkspaceID: sql.NullInt64{Int64: scope.WorkspaceID(), Valid: true},
		TargetType:  targetType, TargetID: targetID, BeforeID: f.BeforeID, MaxRows: int64(f.Limit + 1),
	})
	if err != nil {
		return nil, 0, err
	}
	entries := make([]Entry, len(rows))
	for i, r := range rows {
		if entries[i], err = toEntry(sqlc.ListInstanceAuditEventsRow(r)); err != nil {
			return nil, 0, err
		}
	}
	page, next := paginate(entries, f.Limit)
	return page, next, nil
}

// ListInstance returns instance-level events (account management): those
// that belong to no workspace.
func (l *Log) ListInstance(ctx context.Context, f Filter) ([]Entry, int64, error) {
	f = f.normalized()
	rows, err := l.q.ListInstanceAuditEvents(ctx, sqlc.ListInstanceAuditEventsParams{
		ActionPrefix: f.ActionPrefix, ActorID: f.ActorID,
		FromMs: f.From.UnixMilli(), ToMs: f.To.UnixMilli(), BeforeID: f.BeforeID, MaxRows: int64(f.Limit + 1),
	})
	if err != nil {
		return nil, 0, err
	}
	entries := make([]Entry, len(rows))
	for i, r := range rows {
		if entries[i], err = toEntry(r); err != nil {
			return nil, 0, err
		}
	}
	page, next := paginate(entries, f.Limit)
	return page, next, nil
}

func (f Filter) normalized() Filter {
	if f.Limit <= 0 {
		f.Limit = DefaultPageSize
	}
	f.Limit = min(f.Limit, MaxPageSize)
	if f.From.IsZero() {
		f.From = time.UnixMilli(0)
	}
	if f.To.IsZero() {
		f.To = time.UnixMilli(math.MaxInt64)
	}
	if f.BeforeID <= 0 {
		f.BeforeID = math.MaxInt64
	}
	return f
}

// paginate trims the one extra row fetched to learn whether more exist.
func paginate(entries []Entry, limit int) ([]Entry, int64) {
	if len(entries) <= limit {
		return entries, 0
	}
	return entries[:limit], entries[limit-1].ID
}

func toEntry(r sqlc.ListInstanceAuditEventsRow) (Entry, error) {
	e := Entry{
		ID: r.ID, Action: r.Action, ActorID: r.ActorUserID.Int64, ActorName: r.ActorName, ActorVia: r.ActorVia,
		TargetType: r.TargetType, TargetID: r.TargetID, Summary: r.Summary, CreatedAt: time.UnixMilli(r.CreatedAt),
	}
	if err := json.Unmarshal([]byte(r.Changes), &e.Changes); err != nil {
		return Entry{}, err
	}
	return e, nil
}

// Purge deletes events recorded before the cutoff (the retention period).
func (l *Log) Purge(ctx context.Context, before time.Time) (int64, error) {
	return l.q.DeleteAuditEventsBefore(ctx, before.UnixMilli())
}
