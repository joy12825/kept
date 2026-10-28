package tenant

import (
	"context"
	"testing"
)

func TestScopeRoundTripsThroughContext(t *testing.T) {
	ctx := With(context.Background(), New(42, RoleOwner))
	s, ok := From(ctx)
	if !ok {
		t.Fatal("scope not found in context")
	}
	if s.WorkspaceID() != 42 || s.Role() != RoleOwner {
		t.Errorf("got workspace %d role %s", s.WorkspaceID(), s.Role())
	}
}

func TestFromEmptyContext(t *testing.T) {
	if _, ok := From(context.Background()); ok {
		t.Error("empty context should carry no scope")
	}
}

func TestMustFromPanicsWithoutScope(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustFrom should panic when the request has no workspace")
		}
	}()
	MustFrom(context.Background())
}

func TestZeroScopeIsInvalid(t *testing.T) {
	ctx := With(context.Background(), Scope{})
	if _, ok := From(ctx); ok {
		t.Error("a zero Scope must not be treated as a workspace")
	}
}

func TestRoleCanWrite(t *testing.T) {
	cases := map[Role]bool{RoleOwner: true, RoleEditor: true, RoleViewer: false}
	for role, want := range cases {
		if got := New(1, role).CanWrite(); got != want {
			t.Errorf("%s CanWrite = %v, want %v", role, got, want)
		}
	}
}

func TestRoleAtLeast(t *testing.T) {
	order := []Role{RoleViewer, RoleEditor, RoleOwner}
	for i, have := range order {
		for j, min := range order {
			if got := have.AtLeast(min); got != (i >= j) {
				t.Errorf("%s.AtLeast(%s) = %v", have, min, got)
			}
		}
	}
	if Role("").AtLeast(RoleViewer) || Role("admin").AtLeast(RoleViewer) {
		t.Error("an unknown role must not satisfy any requirement")
	}
	if RoleOwner.AtLeast(Role("superuser")) {
		t.Error("an unknown requirement must not be satisfiable")
	}
}
