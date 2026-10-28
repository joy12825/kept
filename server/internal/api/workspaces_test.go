package api

import (
	"net/http"
	"testing"
)

func TestWorkspaceLifecycle(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")

	personal := alice.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK).body["workspace"].(map[string]any)
	if personal["kind"] != "personal" || personal["color"] != "blue" || personal["memberCount"] != float64(1) || personal["role"] != "owner" {
		t.Errorf("me.workspace = %v", personal)
	}

	r := alice.mustDo(http.MethodPost, "/api/v1/workspaces", map[string]any{"name": "王家", "kind": "family", "color": "green"}, http.StatusCreated)
	if r.body["name"] != "王家" || r.body["kind"] != "family" || r.body["color"] != "green" || r.body["role"] != "owner" || r.body["memberCount"] != float64(1) {
		t.Errorf("created = %v", r.body)
	}
	path := "/api/v1/workspaces/" + itoa(r.body["id"].(float64))

	list := alice.mustDo(http.MethodGet, "/api/v1/workspaces", nil, http.StatusOK).list
	if len(list) != 2 || list[0].(map[string]any)["kind"] != "personal" || list[1].(map[string]any)["name"] != "王家" {
		t.Errorf("list = %v", list)
	}

	r = alice.mustDo(http.MethodPatch, path, map[string]any{"name": "王家大院"}, http.StatusOK)
	if r.body["name"] != "王家大院" || r.body["color"] != "green" {
		t.Errorf("renamed = %v", r.body)
	}
	if r := alice.do(http.MethodPatch, path, map[string]any{"color": "magenta"}); r.code != http.StatusBadRequest {
		t.Errorf("bad color: %d %v", r.code, r.body)
	}
	if r := alice.do(http.MethodPost, "/api/v1/workspaces", map[string]any{"name": "x", "kind": "personal"}); r.code != http.StatusBadRequest {
		t.Errorf("creating a personal workspace: %d %v", r.code, r.body)
	}

	if r := alice.do(http.MethodDelete, path, map[string]any{"name": "王家"}); r.code != http.StatusBadRequest {
		t.Errorf("delete with the wrong name: %d %v", r.code, r.body)
	}
	alice.mustDo(http.MethodDelete, path, map[string]any{"name": "王家大院"}, http.StatusNoContent)
	if list := alice.mustDo(http.MethodGet, "/api/v1/workspaces", nil, http.StatusOK).list; len(list) != 1 {
		t.Errorf("list after delete = %v", list)
	}
	if r := alice.do(http.MethodPatch, path, map[string]any{"name": "again"}); r.code != http.StatusNotFound {
		t.Errorf("patching a deleted workspace: %d %v", r.code, r.body)
	}

	personalPath := "/api/v1/workspaces/" + itoa(personal["id"].(float64))
	if r := alice.do(http.MethodDelete, personalPath, map[string]any{"name": personal["name"]}); r.code != http.StatusConflict {
		t.Errorf("deleting the personal workspace: %d %v", r.code, r.body)
	}
}

func TestWorkspacesNeedLogin(t *testing.T) {
	e := newEnv(t, "auto", nil)
	e.setupAdmin()
	if r := e.anon().do(http.MethodGet, "/api/v1/workspaces", nil); r.code != http.StatusUnauthorized {
		t.Errorf("anonymous list: %d", r.code)
	}
}

// inSpace returns a copy of c that acts in the given workspace.
func (c *client) inSpace(id any) *client {
	cp := *c
	cp.headers = map[string]string{}
	for k, v := range c.headers {
		cp.headers[k] = v
	}
	switch v := id.(type) {
	case float64:
		cp.headers["X-Workspace-ID"] = itoa(v)
	case string:
		cp.headers["X-Workspace-ID"] = v
	}
	return &cp
}

func TestWorkspaceHeaderSelectsTheCurrentWorkspace(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, bob := e.user("alice"), e.user("bob")
	homeID := alice.mustDo(http.MethodPost, "/api/v1/workspaces", map[string]any{"name": "王家", "kind": "family"}, http.StatusCreated).body["id"].(float64)

	ws := alice.inSpace(homeID).mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK).body["workspace"].(map[string]any)
	if ws["id"] != homeID || ws["role"] != "owner" || ws["kind"] != "family" {
		t.Errorf("with the header, me.workspace = %v", ws)
	}
	if ws := alice.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK).body["workspace"].(map[string]any); ws["kind"] != "personal" {
		t.Errorf("without the header, me.workspace = %v, want the personal workspace", ws)
	}

	// A workspace the caller is not a member of, one that does not exist and
	// a malformed ID must be indistinguishable.
	notMember := bob.inSpace(homeID).do(http.MethodGet, "/api/v1/me", nil)
	if notMember.code != http.StatusNotFound || code(notMember) != "workspace_not_found" {
		t.Fatalf("non-member: %d %v", notMember.code, notMember.body)
	}
	for _, id := range []string{"999999", "abc", "0", "-3", "1.5", " "} {
		r := bob.inSpace(id).do(http.MethodGet, "/api/v1/me", nil)
		if r.code != notMember.code || r.raw != notMember.raw {
			t.Errorf("X-Workspace-ID %q: %d %s, want exactly the non-member response %s", id, r.code, r.raw, notMember.raw)
		}
	}
	// Writes are refused the same way, before they take effect.
	if r := bob.inSpace(homeID).do(http.MethodPatch, "/api/v1/me", map[string]any{"displayName": "x", "email": "bob@example.com"}); r.code != http.StatusNotFound {
		t.Errorf("write with a foreign workspace: %d %v", r.code, r.body)
	}
	// A deleted workspace behaves like a missing one.
	alice.mustDo(http.MethodDelete, "/api/v1/workspaces/"+itoa(homeID), map[string]any{"name": "王家"}, http.StatusNoContent)
	if r := alice.inSpace(homeID).do(http.MethodGet, "/api/v1/me", nil); r.raw != notMember.raw {
		t.Errorf("deleted workspace: %d %s", r.code, r.raw)
	}
}

func TestStaleWorkspaceHeaderStillAllowsLogoutAndPublicRequests(t *testing.T) {
	e := newEnv(t, "auto", nil)
	bob := e.user("bob")
	stale := bob.inSpace("999999")
	stale.mustDo(http.MethodGet, "/api/v1/setup", nil, http.StatusOK)
	stale.mustDo(http.MethodPost, "/api/v1/auth/logout", nil, http.StatusNoContent)
	bob.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusUnauthorized)

	// Logging in does not depend on the header either.
	stale.mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "bob", "password": "password-123"}, http.StatusOK)
}

func TestWorkspaceHeaderWorksWithAPITokens(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	homeID := alice.mustDo(http.MethodPost, "/api/v1/workspaces", map[string]any{"name": "王家", "kind": "family"}, http.StatusCreated).body["id"].(float64)
	script := e.anon()
	script.bearer = alice.mustDo(http.MethodPost, "/api/v1/me/tokens", map[string]any{"name": "script"}, http.StatusCreated).body["token"].(string)

	if ws := script.inSpace(homeID).mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK).body["workspace"].(map[string]any); ws["id"] != homeID {
		t.Errorf("token + header: %v", ws)
	}
	if r := script.inSpace("999999").do(http.MethodGet, "/api/v1/me", nil); r.code != http.StatusNotFound || code(r) != "workspace_not_found" {
		t.Errorf("token + unknown workspace: %d %v", r.code, r.body)
	}
}
