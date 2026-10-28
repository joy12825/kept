package workspace

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/auth"
	"kept/server/internal/store"
	"kept/server/internal/tenant"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

type env struct {
	t     *testing.T
	s     *Service
	auth  *auth.Service
	st    *store.Store
	clock *clock
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	c := &clock{t: time.Date(2024, 3, 5, 10, 0, 0, 0, time.UTC)}
	return &env{t: t, s: NewService(st.DB, Options{Now: c.Now}), auth: auth.NewService(st.DB, auth.Options{Now: c.Now}), st: st, clock: c}
}

// user creates an account and returns it with a context acting as it.
func (e *env) user(name string) (auth.User, context.Context) {
	e.t.Helper()
	u, err := e.auth.CreateUser(context.Background(), auth.NewUser{Username: name, Password: "password-123", Email: name + "@example.com", DisplayName: name})
	if err != nil {
		e.t.Fatal(err)
	}
	return u, audit.WithActor(context.Background(), audit.Actor{UserID: u.ID, Name: u.DisplayName, Via: audit.ViaSession})
}

// actions lists the audit actions recorded for a workspace, oldest first.
func (e *env) actions(workspaceID int64) []string {
	e.t.Helper()
	rows, err := e.st.DB.Query(`SELECT action FROM audit_events WHERE workspace_id = ? ORDER BY id`, workspaceID)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, a)
	}
	return out
}

func isKind(err error, k apperr.Kind) bool {
	var e *apperr.Error
	return errors.As(err, &e) && e.Kind == k
}

func TestCreateAndList(t *testing.T) {
	e := newEnv(t)
	alice, ctx := e.user("alice")

	e.clock.Add(time.Minute)
	home, err := e.s.Create(ctx, alice.ID, "  王家  ", "family", "green")
	if err != nil {
		t.Fatal(err)
	}
	if home.Name != "王家" || home.Kind != "family" || home.Color != "green" || home.Role != tenant.RoleOwner || home.MemberCount != 1 {
		t.Errorf("created = %+v", home)
	}
	e.clock.Add(time.Minute)
	studio, err := e.s.Create(ctx, alice.ID, "工作室", "team", "")
	if err != nil {
		t.Fatal(err)
	}
	if studio.Color != "blue" {
		t.Errorf("default color = %q, want blue", studio.Color)
	}

	list, err := e.s.List(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].Kind != "personal" || list[1].ID != home.ID || list[2].ID != studio.ID {
		t.Errorf("list = %+v, want the personal workspace first, then by creation time", list)
	}
	var builtIns int
	if err := e.st.DB.QueryRow(`SELECT count(*) FROM custom_fields WHERE workspace_id = ? AND key IN ('amount', 'due_date')`, home.ID).Scan(&builtIns); err != nil {
		t.Fatal(err)
	}
	if builtIns != 2 {
		t.Errorf("a new workspace has %d built-in fields, want 2", builtIns)
	}
	if got := e.actions(home.ID); len(got) != 1 || got[0] != "workspace.create" {
		t.Errorf("audit = %v", got)
	}

	bob, bctx := e.user("bob")
	if list, _ := e.s.List(bctx, bob.ID); len(list) != 1 {
		t.Errorf("bob sees %d workspaces, want only his own", len(list))
	}
}

func TestCreateValidation(t *testing.T) {
	e := newEnv(t)
	alice, ctx := e.user("alice")
	cases := []struct{ name, kind, color string }{
		{"", "family", "blue"},
		{"   ", "family", "blue"},
		{strings.Repeat("字", 33), "family", "blue"},
		{"ok", "personal", "blue"}, // a second personal workspace is never allowed
		{"ok", "club", "blue"},
		{"ok", "family", "magenta"},
	}
	for _, c := range cases {
		if _, err := e.s.Create(ctx, alice.ID, c.name, c.kind, c.color); !isKind(err, apperr.KindInvalid) {
			t.Errorf("Create(%q, %q, %q) err = %v, want invalid", c.name, c.kind, c.color, err)
		}
	}
	if _, err := e.s.Create(ctx, alice.ID, strings.Repeat("字", 32), "team", "gray"); err != nil {
		t.Errorf("a 32-character name should be accepted: %v", err)
	}
	if list, _ := e.s.List(ctx, alice.ID); len(list) != 2 {
		t.Errorf("rejected requests created workspaces: %+v", list)
	}
}

func TestUpdate(t *testing.T) {
	e := newEnv(t)
	alice, ctx := e.user("alice")
	bob, bctx := e.user("bob")
	home, _ := e.s.Create(ctx, alice.ID, "王家", "family", "green")

	name, color := "王家大院", "red"
	got, err := e.s.Update(ctx, alice.ID, home.ID, &name, &color)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != name || got.Color != color {
		t.Errorf("updated = %+v", got)
	}
	// Unchanged values are not an audited change.
	if _, err := e.s.Update(ctx, alice.ID, home.ID, &name, nil); err != nil {
		t.Fatal(err)
	}
	if got := e.actions(home.ID); len(got) != 2 || got[1] != "workspace.update" {
		t.Errorf("audit = %v, want create and one update", got)
	}
	var changes string
	if err := e.st.DB.QueryRow(`SELECT changes FROM audit_events WHERE workspace_id = ? AND action = 'workspace.update'`, home.ID).Scan(&changes); err != nil {
		t.Fatal(err)
	}
	if changes != `{"color":["green","red"],"name":["王家","王家大院"]}` {
		t.Errorf("changes = %s", changes)
	}

	bad := "magenta"
	if _, err := e.s.Update(ctx, alice.ID, home.ID, nil, &bad); !isKind(err, apperr.KindInvalid) {
		t.Errorf("bad color err = %v", err)
	}
	// A non-member cannot tell the workspace exists.
	if _, err := e.s.Update(bctx, bob.ID, home.ID, &name, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("non-member err = %v, want ErrNotFound", err)
	}
	// An editor may not change workspace settings.
	if _, err := e.st.DB.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role, created_at) VALUES (?, ?, 'editor', 1)`, home.ID, bob.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Update(bctx, bob.ID, home.ID, &name, nil); !isKind(err, apperr.KindForbidden) {
		t.Errorf("editor err = %v, want forbidden", err)
	}

	// The personal workspace can be renamed by its owner.
	list, _ := e.s.List(ctx, alice.ID)
	mine := "我的文件"
	if got, err := e.s.Update(ctx, alice.ID, list[0].ID, &mine, nil); err != nil || got.Name != mine {
		t.Errorf("rename personal workspace = %+v, %v", got, err)
	}
}

func TestDelete(t *testing.T) {
	e := newEnv(t)
	alice, ctx := e.user("alice")
	bob, bctx := e.user("bob")
	home, _ := e.s.Create(ctx, alice.ID, "王家", "family", "green")
	list, _ := e.s.List(ctx, alice.ID)
	personal := list[0]

	if err := e.s.Delete(ctx, alice.ID, personal.ID, personal.Name); !isKind(err, apperr.KindConflict) {
		t.Errorf("deleting the personal workspace err = %v, want conflict", err)
	}
	if err := e.s.Delete(ctx, alice.ID, home.ID, "王 家"); !isKind(err, apperr.KindInvalid) {
		t.Errorf("wrong confirmation err = %v, want invalid", err)
	}
	if err := e.s.Delete(bctx, bob.ID, home.ID, "王家"); !errors.Is(err, ErrNotFound) {
		t.Errorf("non-member err = %v, want ErrNotFound", err)
	}
	if _, err := e.st.DB.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role, created_at) VALUES (?, ?, 'editor', 1)`, home.ID, bob.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.s.Delete(bctx, bob.ID, home.ID, "王家"); !isKind(err, apperr.KindForbidden) {
		t.Errorf("editor err = %v, want forbidden", err)
	}

	if err := e.s.Delete(ctx, alice.ID, home.ID, "王家"); err != nil {
		t.Fatal(err)
	}
	if list, _ := e.s.List(ctx, alice.ID); len(list) != 1 {
		t.Errorf("deleted workspace still listed: %+v", list)
	}
	if _, _, err := e.s.Resolve(ctx, alice.ID, home.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Resolve after delete err = %v, want ErrNotFound", err)
	}
	if list, _ := e.s.List(bctx, bob.ID); len(list) != 1 {
		t.Errorf("a member still sees the deleted workspace: %+v", list)
	}
	if err := e.s.Delete(ctx, alice.ID, home.ID, "王家"); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting twice err = %v, want ErrNotFound", err)
	}
	// Data stays for 30 days: the row and its memberships are kept.
	var members int
	if err := e.st.DB.QueryRow(`SELECT count(*) FROM workspace_members WHERE workspace_id = ?`, home.ID).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if members != 2 {
		t.Errorf("memberships after soft delete = %d, want 2", members)
	}
	if got := e.actions(home.ID); len(got) != 2 || got[1] != "workspace.delete" {
		t.Errorf("audit = %v", got)
	}
}

func TestResolve(t *testing.T) {
	e := newEnv(t)
	alice, ctx := e.user("alice")
	bob, bctx := e.user("bob")
	home, _ := e.s.Create(ctx, alice.ID, "王家", "family", "green")
	if _, err := e.st.DB.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role, created_at) VALUES (?, ?, 'viewer', 1)`, home.ID, bob.ID); err != nil {
		t.Fatal(err)
	}

	scope, ws, err := e.s.Resolve(ctx, alice.ID, home.ID)
	if err != nil || scope.WorkspaceID() != home.ID || scope.Role() != tenant.RoleOwner || ws.Name != "王家" || ws.MemberCount != 2 {
		t.Errorf("owner: scope %+v ws %+v err %v", scope, ws, err)
	}
	if scope, _, err := e.s.Resolve(bctx, bob.ID, home.ID); err != nil || scope.Role() != tenant.RoleViewer {
		t.Errorf("viewer: scope %+v err %v", scope, err)
	}
	carol, cctx := e.user("carol")
	for _, id := range []int64{home.ID, 99999, 0, -1} {
		if _, _, err := e.s.Resolve(cctx, carol.ID, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Resolve(%d) as a non-member err = %v, want ErrNotFound", id, err)
		}
	}
}

func TestPurgeDeletedRemovesWorkspacesDeletedBeforeTheCutoff(t *testing.T) {
	e := newEnv(t)
	alice, ctx := e.user("alice")
	old, _ := e.s.Create(ctx, alice.ID, "旧家", "family", "green")
	recent, _ := e.s.Create(ctx, alice.ID, "新家", "family", "green")
	kept, _ := e.s.Create(ctx, alice.ID, "在用", "family", "green")
	if _, err := e.st.DB.Exec(`INSERT INTO jobs (workspace_id, kind, run_after, created_at) VALUES (?, 'x', 0, 0)`, old.ID); err != nil {
		t.Fatal(err)
	}

	if err := e.s.Delete(ctx, alice.ID, old.ID, "旧家"); err != nil {
		t.Fatal(err)
	}
	deletedAt := e.clock.Now()
	e.clock.Add(time.Hour)
	if err := e.s.Delete(ctx, alice.ID, recent.ID, "新家"); err != nil {
		t.Fatal(err)
	}

	// Exactly at the cutoff the workspace is kept; one millisecond past it, purged.
	if n, err := e.s.PurgeDeleted(context.Background(), deletedAt); err != nil || n != 0 {
		t.Fatalf("PurgeDeleted at the deletion time = %d, %v; want nothing purged", n, err)
	}
	if ids, err := e.s.DeletedBefore(context.Background(), deletedAt.Add(time.Millisecond)); err != nil || len(ids) != 1 || ids[0] != old.ID {
		t.Fatalf("DeletedBefore = %v, %v; want [%d]", ids, err, old.ID)
	}
	n, err := e.s.PurgeDeleted(context.Background(), deletedAt.Add(time.Millisecond))
	if err != nil || n != 1 {
		t.Fatalf("PurgeDeleted = %d, %v; want 1", n, err)
	}
	for table, col := range map[string]string{"workspaces": "id", "workspace_members": "workspace_id", "audit_events": "workspace_id", "jobs": "workspace_id"} {
		var left int
		if err := e.st.DB.QueryRow("SELECT count(*) FROM "+table+" WHERE "+col+" = ?", old.ID).Scan(&left); err != nil {
			t.Fatal(err)
		}
		if left != 0 {
			t.Errorf("%d rows of the purged workspace left in %s", left, table)
		}
	}
	var others int
	if err := e.st.DB.QueryRow(`SELECT count(*) FROM workspaces WHERE id IN (?, ?)`, recent.ID, kept.ID).Scan(&others); err != nil {
		t.Fatal(err)
	}
	if others != 2 {
		t.Errorf("a recently deleted or live workspace was purged")
	}
	if list, _ := e.s.List(ctx, alice.ID); len(list) != 2 {
		t.Errorf("alice's workspaces = %+v, want the personal one and the live one", list)
	}
}
