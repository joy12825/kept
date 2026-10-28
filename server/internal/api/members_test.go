package api

import (
	"net/http"
	"testing"
)

func userID(c *client) float64 {
	return c.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK).body["user"].(map[string]any)["id"].(float64)
}

func TestMembers(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, bob, carol := e.user("alice"), e.user("bob"), e.user("carol")
	homeID := alice.mustDo(http.MethodPost, "/api/v1/workspaces", map[string]any{"name": "王家", "kind": "family"}, http.StatusCreated).body["id"].(float64)
	members := "/api/v1/workspaces/" + itoa(homeID) + "/members"

	r := alice.mustDo(http.MethodPost, members, map[string]any{"login": "bob", "role": "viewer"}, http.StatusCreated)
	if r.body["username"] != "bob" || r.body["role"] != "viewer" || r.body["email"] != "bob@example.com" || r.body["joinedAt"] == nil {
		t.Errorf("added = %v", r.body)
	}
	alice.mustDo(http.MethodPost, members, map[string]any{"login": "CAROL@example.com", "role": "editor"}, http.StatusCreated)

	// bob now sees the workspace, with his role.
	list := bob.mustDo(http.MethodGet, "/api/v1/workspaces", nil, http.StatusOK).list
	if len(list) != 2 || list[1].(map[string]any)["id"] != homeID || list[1].(map[string]any)["role"] != "viewer" || list[1].(map[string]any)["memberCount"] != float64(3) {
		t.Errorf("bob's workspaces = %v", list)
	}
	if got := bob.mustDo(http.MethodGet, members, nil, http.StatusOK).list; len(got) != 3 {
		t.Errorf("a viewer should see all 3 members, got %v", got)
	}

	// An unknown login and a disabled account get the same answer.
	unknown := alice.do(http.MethodPost, members, map[string]any{"login": "nobody", "role": "viewer"})
	if unknown.code != http.StatusNotFound {
		t.Errorf("unknown login: %d %v", unknown.code, unknown.body)
	}
	e.user("root")
	dave := e.user("dave")
	if err := e.auth.SetDisabled(t.Context(), int64(userID(dave)), true); err != nil {
		t.Fatal(err)
	}
	if r := alice.do(http.MethodPost, members, map[string]any{"login": "dave", "role": "viewer"}); r.code != unknown.code || r.raw != unknown.raw {
		t.Errorf("disabled account: %d %s, want the same as an unknown login %s", r.code, r.raw, unknown.raw)
	}
	if r := alice.do(http.MethodPost, members, map[string]any{"login": "bob", "role": "viewer"}); r.code != http.StatusConflict {
		t.Errorf("adding twice: %d %v", r.code, r.body)
	}

	// A viewer cannot manage members, but can leave.
	bobID, carolID := userID(bob), userID(carol)
	if r := bob.do(http.MethodPost, members, map[string]any{"login": "root", "role": "viewer"}); r.code != http.StatusForbidden {
		t.Errorf("viewer adding: %d %v", r.code, r.body)
	}
	if r := bob.do(http.MethodPatch, members+"/"+itoa(carolID), map[string]any{"role": "viewer"}); r.code != http.StatusForbidden {
		t.Errorf("viewer changing a role: %d %v", r.code, r.body)
	}
	if r := bob.do(http.MethodDelete, members+"/"+itoa(carolID), nil); r.code != http.StatusForbidden {
		t.Errorf("viewer removing someone: %d %v", r.code, r.body)
	}

	r = alice.mustDo(http.MethodPatch, members+"/"+itoa(bobID), map[string]any{"role": "editor"}, http.StatusOK)
	if r.body["role"] != "editor" {
		t.Errorf("role change = %v", r.body)
	}
	if r := alice.do(http.MethodPatch, members+"/"+itoa(userID(alice)), map[string]any{"role": "editor"}); r.code != http.StatusConflict {
		t.Errorf("last owner stepping down: %d %v", r.code, r.body)
	}

	// Removal takes effect on the next request.
	bobInHome := bob.inSpace(homeID)
	bobInHome.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK)
	alice.mustDo(http.MethodDelete, members+"/"+itoa(bobID), nil, http.StatusNoContent)
	if r := bobInHome.do(http.MethodGet, "/api/v1/me", nil); r.code != http.StatusNotFound || code(r) != "workspace_not_found" {
		t.Errorf("after removal: %d %v", r.code, r.body)
	}
	if r := bob.do(http.MethodGet, members, nil); r.code != http.StatusNotFound {
		t.Errorf("removed member listing members: %d %v", r.code, r.body)
	}
	carol.mustDo(http.MethodDelete, members+"/"+itoa(carolID), nil, http.StatusNoContent) // leaves
	if got := alice.mustDo(http.MethodGet, members, nil, http.StatusOK).list; len(got) != 1 {
		t.Errorf("members left = %v", got)
	}
}
