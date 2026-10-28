package workspace

import (
	"context"
	"errors"
	"sync"
	"testing"

	"kept/server/internal/apperr"
	"kept/server/internal/tenant"
)

// home creates alice's family workspace with bob as editor and carol as viewer.
func (e *env) home() (ws Workspace, alice, bob, carol int64, actx, bctx, cctx context.Context) {
	e.t.Helper()
	a, actx := e.user("alice")
	b, bctx := e.user("bob")
	c, cctx := e.user("carol")
	ws, err := e.s.Create(actx, a.ID, "王家", "family", "green")
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.s.AddMember(actx, a.ID, ws.ID, "bob", tenant.RoleEditor); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.s.AddMember(actx, a.ID, ws.ID, "carol", tenant.RoleViewer); err != nil {
		e.t.Fatal(err)
	}
	return ws, a.ID, b.ID, c.ID, actx, bctx, cctx
}

func TestAddAndListMembers(t *testing.T) {
	e := newEnv(t)
	ws, alice, bob, carol, actx, _, cctx := e.home()

	// Every member may see who else is in the workspace.
	list, err := e.s.ListMembers(cctx, carol, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].UserID != alice || list[0].Role != tenant.RoleOwner ||
		list[1].UserID != bob || list[1].Role != tenant.RoleEditor || list[1].Username != "bob" || list[1].Email != "bob@example.com" ||
		list[2].UserID != carol || list[2].Role != tenant.RoleViewer {
		t.Errorf("members = %+v", list)
	}
	if got, _ := e.s.List(cctx, carol); len(got) != 2 || got[1].ID != ws.ID || got[1].Role != tenant.RoleViewer || got[1].MemberCount != 3 {
		t.Errorf("carol's workspaces = %+v", got)
	}

	dave, dctx := e.user("dave")
	if _, err := e.s.ListMembers(dctx, dave.ID, ws.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("non-member listing err = %v, want ErrNotFound", err)
	}
	// By email, in any letter case.
	m, err := e.s.AddMember(actx, alice, ws.ID, "  Dave@Example.COM ", tenant.RoleEditor)
	if err != nil || m.UserID != dave.ID || m.Role != tenant.RoleEditor {
		t.Errorf("add by email = %+v, %v", m, err)
	}
	if _, err := e.s.AddMember(actx, alice, ws.ID, "DAVE", tenant.RoleViewer); !errors.Is(err, ErrAlreadyMember) {
		t.Errorf("adding twice err = %v, want ErrAlreadyMember", err)
	}
	want := []string{"workspace.create", "member.add", "member.add", "member.add"}
	if got := e.actions(ws.ID); len(got) != len(want) {
		t.Errorf("audit = %v, want %v", got, want)
	}
}

func TestAddMemberRules(t *testing.T) {
	e := newEnv(t)
	ws, alice, bob, _, actx, bctx, _ := e.home()
	erin, _ := e.user("erin")
	before := len(e.actions(ws.ID))

	if _, err := e.s.AddMember(bctx, bob, ws.ID, "erin", tenant.RoleViewer); !errors.Is(err, ErrNotOwner) {
		t.Errorf("editor adding err = %v, want ErrNotOwner", err)
	}
	if _, err := e.s.AddMember(actx, alice, ws.ID, "erin", tenant.Role("boss")); !isKind(err, apperr.KindInvalid) {
		t.Errorf("unknown role err = %v, want invalid", err)
	}
	// An unknown login and a disabled account are reported identically.
	_, errUnknown := e.s.AddMember(actx, alice, ws.ID, "nobody", tenant.RoleViewer)
	if !errors.Is(errUnknown, ErrUserNotFound) {
		t.Errorf("unknown login err = %v, want ErrUserNotFound", errUnknown)
	}
	if err := e.auth.SetDisabled(context.Background(), erin.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.AddMember(actx, alice, ws.ID, "erin", tenant.RoleViewer); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("disabled account err = %v, want ErrUserNotFound", err)
	}
	// Nobody else can join a personal workspace.
	mine, _ := e.s.List(actx, alice)
	if _, err := e.s.AddMember(actx, alice, mine[0].ID, "bob", tenant.RoleViewer); !errors.Is(err, ErrPersonalMembers) {
		t.Errorf("adding to a personal workspace err = %v", err)
	}
	if got := len(e.actions(ws.ID)); got != before {
		t.Errorf("refused requests were audited: %d new events", got-before)
	}
}

func TestChangeRole(t *testing.T) {
	e := newEnv(t)
	ws, alice, bob, carol, actx, bctx, _ := e.home()

	m, err := e.s.UpdateMemberRole(actx, alice, ws.ID, carol, tenant.RoleEditor)
	if err != nil || m.Role != tenant.RoleEditor {
		t.Fatalf("promote = %+v, %v", m, err)
	}
	var changes, summary string
	if err := e.st.DB.QueryRow(`SELECT changes, summary FROM audit_events WHERE workspace_id = ? AND action = 'member.role_change'`, ws.ID).Scan(&changes, &summary); err != nil {
		t.Fatal(err)
	}
	if changes != `{"role":["viewer","editor"]}` || summary != "alice 将 carol 的角色从 只看 改为 成员" {
		t.Errorf("audit = %s / %s", changes, summary)
	}
	before := len(e.actions(ws.ID))
	// Same role again: nothing changes, nothing is audited.
	if _, err := e.s.UpdateMemberRole(actx, alice, ws.ID, carol, tenant.RoleEditor); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.UpdateMemberRole(bctx, bob, ws.ID, carol, tenant.RoleViewer); !errors.Is(err, ErrNotOwner) {
		t.Errorf("editor changing a role err = %v", err)
	}
	if _, err := e.s.UpdateMemberRole(actx, alice, ws.ID, 99999, tenant.RoleViewer); !errors.Is(err, ErrMemberNotFound) {
		t.Errorf("unknown member err = %v", err)
	}
	// The only administrator cannot step down.
	if _, err := e.s.UpdateMemberRole(actx, alice, ws.ID, alice, tenant.RoleEditor); !errors.Is(err, ErrLastOwner) {
		t.Errorf("last owner demoting themselves err = %v, want ErrLastOwner", err)
	}
	if got := len(e.actions(ws.ID)); got != before {
		t.Errorf("%d events recorded for requests that changed nothing", got-before)
	}
	// With a second administrator, the first may.
	if _, err := e.s.UpdateMemberRole(actx, alice, ws.ID, bob, tenant.RoleOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.UpdateMemberRole(actx, alice, ws.ID, alice, tenant.RoleEditor); err != nil {
		t.Errorf("demoting with another owner present: %v", err)
	}
}

func TestRemoveAndLeave(t *testing.T) {
	e := newEnv(t)
	ws, alice, bob, carol, actx, bctx, cctx := e.home()

	if err := e.s.RemoveMember(bctx, bob, ws.ID, carol); !errors.Is(err, ErrNotOwner) {
		t.Errorf("editor removing someone err = %v, want ErrNotOwner", err)
	}
	if err := e.s.RemoveMember(actx, alice, ws.ID, alice); !errors.Is(err, ErrLastOwner) {
		t.Errorf("last owner leaving err = %v, want ErrLastOwner", err)
	}
	mine, _ := e.s.List(actx, alice)
	if err := e.s.RemoveMember(actx, alice, mine[0].ID, alice); !errors.Is(err, ErrPersonalLeave) {
		t.Errorf("leaving the personal workspace err = %v", err)
	}

	// Anyone may leave.
	if err := e.s.RemoveMember(cctx, carol, ws.ID, carol); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.s.Resolve(cctx, carol, ws.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after leaving, Resolve err = %v, want ErrNotFound", err)
	}
	// The administrator removes bob, who loses access at once.
	if err := e.s.RemoveMember(actx, alice, ws.ID, bob); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.ListMembers(bctx, bob, ws.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after removal, listing err = %v, want ErrNotFound", err)
	}
	if err := e.s.RemoveMember(actx, alice, ws.ID, bob); !errors.Is(err, ErrMemberNotFound) {
		t.Errorf("removing twice err = %v", err)
	}
	got := e.actions(ws.ID)
	if n := len(got); n < 2 || got[n-2] != "member.leave" || got[n-1] != "member.remove" {
		t.Errorf("audit = %v", got)
	}
}

// Two administrators demoting each other at the same moment must not leave
// the workspace without one.
func TestLastOwnerHoldsUnderConcurrency(t *testing.T) {
	e := newEnv(t)
	ws, alice, bob, _, actx, bctx, _ := e.home()
	if _, err := e.s.UpdateMemberRole(actx, alice, ws.ID, bob, tenant.RoleOwner); err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 5; round++ {
		var wg sync.WaitGroup
		errs := make([]error, 2)
		wg.Add(2)
		go func() { defer wg.Done(); _, errs[0] = e.s.UpdateMemberRole(actx, alice, ws.ID, bob, tenant.RoleEditor) }()
		go func() { defer wg.Done(); _, errs[1] = e.s.UpdateMemberRole(bctx, bob, ws.ID, alice, tenant.RoleEditor) }()
		wg.Wait()

		var owners int
		if err := e.st.DB.QueryRow(`SELECT count(*) FROM workspace_members WHERE workspace_id = ? AND role = 'owner'`, ws.ID).Scan(&owners); err != nil {
			t.Fatal(err)
		}
		if owners != 1 {
			t.Fatalf("round %d: %d owners left (errors: %v), want exactly 1", round, owners, errs)
		}
		// Restore two owners for the next round, as whoever is still one.
		if _, err := e.s.UpdateMemberRole(actx, alice, ws.ID, bob, tenant.RoleOwner); err != nil {
			if _, err := e.s.UpdateMemberRole(bctx, bob, ws.ID, alice, tenant.RoleOwner); err != nil {
				t.Fatal(err)
			}
		}
	}
}
