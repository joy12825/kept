package audit

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"kept/server/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.DB.Exec(`INSERT INTO users (id, username, display_name, password_hash, password_changed_at, created_at, updated_at)
		VALUES (7, 'wang', 'Wang', 'x', 1, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`INSERT INTO workspaces (id, name, kind, created_by, created_at) VALUES (3, 'home', 'family', 7, 1)`); err != nil {
		t.Fatal(err)
	}
	return st
}

type row struct {
	workspaceID, actorUserID                                       sql.NullInt64
	via, action, targetType, targetID, summary, changes, requestID string
	createdAt                                                      int64
}

func lastRow(t *testing.T, db *sql.DB) row {
	t.Helper()
	var r row
	if err := db.QueryRow(`SELECT workspace_id, actor_user_id, actor_via, action, target_type, target_id, summary, changes, request_id, created_at
		FROM audit_events ORDER BY id DESC LIMIT 1`).Scan(&r.workspaceID, &r.actorUserID, &r.via, &r.action, &r.targetType, &r.targetID,
		&r.summary, &r.changes, &r.requestID, &r.createdAt); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRecordWritesTheCallerAndTheEvent(t *testing.T) {
	st := newStore(t)
	at := time.Date(2024, 3, 5, 10, 0, 0, 0, time.UTC)
	ctx := WithActor(context.Background(), Actor{UserID: 7, Name: "Wang", Via: ViaToken, RequestID: "req-1"})
	err := Record(ctx, st.Queries, Event{
		WorkspaceID: 3, Action: "member.role_change", TargetType: "user", TargetID: "9", Summary: "Wang changed a role",
		Changes: map[string][2]any{"role": {"viewer", "editor"}}, At: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := lastRow(t, st.DB)
	if r.workspaceID.Int64 != 3 || r.actorUserID.Int64 != 7 || r.via != "token" || r.action != "member.role_change" ||
		r.targetType != "user" || r.targetID != "9" || r.summary != "Wang changed a role" || r.requestID != "req-1" || r.createdAt != at.UnixMilli() {
		t.Errorf("row = %+v", r)
	}
	if r.changes != `{"role":["viewer","editor"]}` {
		t.Errorf("changes = %s", r.changes)
	}
}

func TestRecordWithoutACallerIsTheSystem(t *testing.T) {
	st := newStore(t)
	if got := ActorFrom(context.Background()); got.Via != ViaSystem || got.UserID != 0 || got.Name == "" {
		t.Errorf("default actor = %+v", got)
	}
	if err := Record(context.Background(), st.Queries, Event{Action: "user.create", Summary: "s"}); err != nil {
		t.Fatal(err)
	}
	r := lastRow(t, st.DB)
	if r.workspaceID.Valid || r.actorUserID.Valid || r.via != "system" {
		t.Errorf("instance-level system event = %+v", r)
	}
	if r.changes != "{}" {
		t.Errorf("empty changes = %q, want {}", r.changes)
	}
	if r.createdAt == 0 {
		t.Error("created_at should default to now")
	}
}

func TestRecordPrefersTheEventsOwnActor(t *testing.T) {
	st := newStore(t)
	ctx := WithActor(context.Background(), Actor{UserID: 99, Name: "someone else", Via: ViaSession})
	if err := Record(ctx, st.Queries, Event{Action: "user.setup", Summary: "s", Actor: &Actor{UserID: 7, Name: "Wang", Via: ViaSession}}); err != nil {
		t.Fatal(err)
	}
	if r := lastRow(t, st.DB); r.actorUserID.Int64 != 7 {
		t.Errorf("actor = %v, want the event's own actor", r.actorUserID)
	}
}

// A failed audit write must surface, so the caller's transaction rolls back.
func TestRecordFailureIsReturned(t *testing.T) {
	st := newStore(t)
	if err := Record(context.Background(), st.Queries, Event{WorkspaceID: 999, Action: "x", Summary: "s"}); err == nil {
		t.Error("recording against a missing workspace should fail")
	}
	if err := Record(context.Background(), st.Queries, Event{Action: "", Summary: "s"}); err == nil {
		t.Error("an event without an action should be rejected")
	}
}

func TestPurgeDeletesOnlyEventsOlderThanTheCutoff(t *testing.T) {
	st := newStore(t)
	cutoff := time.Date(2024, 3, 5, 10, 0, 0, 0, time.UTC)
	for _, at := range []time.Time{cutoff.Add(-time.Hour), cutoff.Add(-time.Millisecond), cutoff, cutoff.Add(time.Hour)} {
		if err := Record(context.Background(), st.Queries, Event{Action: "user.create", Summary: "s", At: at}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := NewLog(st.DB).Purge(context.Background(), cutoff)
	if err != nil || n != 2 {
		t.Fatalf("Purge = %d, %v; want 2 deleted", n, err)
	}
	var oldest int64
	if err := st.DB.QueryRow(`SELECT min(created_at) FROM audit_events`).Scan(&oldest); err != nil {
		t.Fatal(err)
	}
	if oldest != cutoff.UnixMilli() {
		t.Errorf("oldest kept event is at %d, want exactly the cutoff %d", oldest, cutoff.UnixMilli())
	}
}
