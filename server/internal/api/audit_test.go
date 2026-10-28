package api

import (
	"net/http"
	"testing"
)

type auditEntry struct {
	action, via, requestID, summary string
	actor                           int64
}

func (e *testEnv) auditLog() []auditEntry {
	e.t.Helper()
	rows, err := e.db.Query(`SELECT action, actor_via, request_id, summary, coalesce(actor_user_id, 0) FROM audit_events ORDER BY id`)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []auditEntry
	for rows.Next() {
		var a auditEntry
		if err := rows.Scan(&a.action, &a.via, &a.requestID, &a.summary, &a.actor); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, a)
	}
	return out
}

// The audit log attributes each change to the authenticated caller, how it
// authenticated, and the request that made it.
func TestAuditRecordsTheCallerOfARequest(t *testing.T) {
	e := newEnv(t, "auto", nil)
	admin := e.setupAdmin()
	adminID := int64(admin.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK).body["user"].(map[string]any)["id"].(float64))

	admin.headers["X-Request-ID"] = "req-session"
	admin.mustDo(http.MethodPost, "/api/v1/admin/users", map[string]any{"username": "bob", "email": "bob@example.com", "password": "password-123"}, http.StatusCreated)

	tok := admin.mustDo(http.MethodPost, "/api/v1/me/tokens", map[string]any{"name": "script"}, http.StatusCreated).body["token"].(string)
	script := e.anon()
	script.bearer = tok
	script.headers["X-Request-ID"] = "req-token"
	script.mustDo(http.MethodPost, "/api/v1/admin/users", map[string]any{"username": "carol", "email": "carol@example.com", "password": "password-123"}, http.StatusCreated)

	log := e.auditLog()
	if len(log) != 3 {
		t.Fatalf("audit log = %+v, want setup and two account creations", log)
	}
	if log[0].action != "user.setup" || log[0].actor != adminID || log[0].requestID == "" {
		t.Errorf("setup = %+v", log[0])
	}
	if log[1].action != "user.create" || log[1].actor != adminID || log[1].via != "session" || log[1].requestID != "req-session" {
		t.Errorf("creation through the session = %+v", log[1])
	}
	if log[2].action != "user.create" || log[2].actor != adminID || log[2].via != "token" || log[2].requestID != "req-token" {
		t.Errorf("creation through the API token = %+v", log[2])
	}
}

// home builds alice's family workspace with bob (editor) and carol (viewer)
// and returns clients acting inside it.
func home(t *testing.T, e *testEnv) (alice, bob, carol *client, homeID float64) {
	t.Helper()
	alice, bob, carol = e.user("alice"), e.user("bob"), e.user("carol")
	homeID = alice.mustDo(http.MethodPost, "/api/v1/workspaces", map[string]any{"name": "王家", "kind": "family"}, http.StatusCreated).body["id"].(float64)
	members := "/api/v1/workspaces/" + itoa(homeID) + "/members"
	alice.mustDo(http.MethodPost, members, map[string]any{"login": "bob", "role": "editor"}, http.StatusCreated)
	alice.mustDo(http.MethodPost, members, map[string]any{"login": "carol", "role": "viewer"}, http.StatusCreated)
	return alice.inSpace(homeID), bob.inSpace(homeID), carol.inSpace(homeID), homeID
}

func actionsOf(r resp) []string {
	var out []string
	for _, it := range r.body["items"].([]any) {
		out = append(out, it.(map[string]any)["action"].(string))
	}
	return out
}

func TestWorkspaceActivityIsForItsAdministrators(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, bob, carol, homeID := home(t, e)
	alice.mustDo(http.MethodPatch, "/api/v1/workspaces/"+itoa(homeID)+"/members/"+itoa(userID(carol)), map[string]any{"role": "editor"}, http.StatusOK)

	r := alice.mustDo(http.MethodGet, "/api/v1/audit", nil, http.StatusOK)
	got := actionsOf(r)
	want := []string{"member.role_change", "member.add", "member.add", "workspace.create"} // newest first
	if len(got) != len(want) {
		t.Fatalf("actions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("actions = %v, want %v", got, want)
			break
		}
	}
	first := r.body["items"].([]any)[0].(map[string]any)
	if first["actorName"] != "alice" || first["actorVia"] != "session" || first["targetType"] != "user" ||
		first["summary"] != "alice 将 carol 的角色从 只看 改为 成员" || first["createdAt"] == nil {
		t.Errorf("first item = %v", first)
	}
	if ch := first["changes"].(map[string]any)["role"].([]any); ch[0] != "viewer" || ch[1] != "editor" {
		t.Errorf("changes = %v", first["changes"])
	}
	if _, more := r.body["nextCursor"]; more {
		t.Errorf("nextCursor present on the only page: %v", r.body["nextCursor"])
	}

	// Filters.
	if got := actionsOf(alice.mustDo(http.MethodGet, "/api/v1/audit?action=member.", nil, http.StatusOK)); len(got) != 3 {
		t.Errorf("action=member. -> %v", got)
	}
	if got := actionsOf(alice.mustDo(http.MethodGet, "/api/v1/audit?action=member.add", nil, http.StatusOK)); len(got) != 2 {
		t.Errorf("action=member.add -> %v", got)
	}
	// The prefix is literal: SQL wildcards match nothing.
	if got := actionsOf(alice.mustDo(http.MethodGet, "/api/v1/audit?action=%25", nil, http.StatusOK)); len(got) != 0 {
		t.Errorf("action=%% -> %v, want nothing", got)
	}
	if got := actionsOf(alice.mustDo(http.MethodGet, "/api/v1/audit?actorId="+itoa(userID(bob)), nil, http.StatusOK)); len(got) != 0 {
		t.Errorf("bob did nothing, got %v", got)
	}
	if got := actionsOf(alice.mustDo(http.MethodGet, "/api/v1/audit?from=2999-01-01T00:00:00Z", nil, http.StatusOK)); len(got) != 0 {
		t.Errorf("from the future -> %v", got)
	}
	if got := actionsOf(alice.mustDo(http.MethodGet, "/api/v1/audit?to=2999-01-01T00:00:00Z&from=2000-01-01T00:00:00Z", nil, http.StatusOK)); len(got) != 4 {
		t.Errorf("wide time range -> %v", got)
	}

	// Paging: two at a time adds up to the full list, then stops.
	var paged []string
	path := "/api/v1/audit?limit=2"
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("paging does not terminate")
		}
		r := alice.mustDo(http.MethodGet, path, nil, http.StatusOK)
		paged = append(paged, actionsOf(r)...)
		next, ok := r.body["nextCursor"].(string)
		if !ok {
			break
		}
		path = "/api/v1/audit?limit=2&cursor=" + next
	}
	if len(paged) != 4 || paged[0] != want[0] || paged[3] != want[3] {
		t.Errorf("paged = %v, want %v", paged, want)
	}
	if r := alice.do(http.MethodGet, "/api/v1/audit?cursor=abc", nil); r.code != http.StatusBadRequest {
		t.Errorf("bad cursor: %d %v", r.code, r.body)
	}

	// Members and viewers may not read the activity log; outsiders cannot
	// tell the workspace exists.
	for name, c := range map[string]*client{"editor": bob, "viewer": carol} {
		if r := c.do(http.MethodGet, "/api/v1/audit", nil); r.code != http.StatusForbidden || code(r) != "forbidden" {
			t.Errorf("%s reading the activity log: %d %v", name, r.code, r.body)
		}
	}
	dave := e.user("dave")
	if r := dave.inSpace(homeID).do(http.MethodGet, "/api/v1/audit", nil); r.code != http.StatusNotFound || code(r) != "workspace_not_found" {
		t.Errorf("outsider: %d %v", r.code, r.body)
	}
	// In her personal workspace alice sees none of the family's activity.
	personal := e.user("erin")
	if got := actionsOf(personal.mustDo(http.MethodGet, "/api/v1/audit", nil, http.StatusOK)); len(got) != 0 {
		t.Errorf("a fresh personal workspace has activity: %v", got)
	}
}

func TestInstanceActivityIsForInstanceAdministrators(t *testing.T) {
	e := newEnv(t, "auto", nil)
	admin := e.setupAdmin()
	admin.mustDo(http.MethodPost, "/api/v1/admin/users", map[string]any{"username": "bob", "email": "bob@example.com", "password": "password-123"}, http.StatusCreated)
	admin.mustDo(http.MethodPost, "/api/v1/workspaces", map[string]any{"name": "王家", "kind": "family"}, http.StatusCreated)

	got := actionsOf(admin.mustDo(http.MethodGet, "/api/v1/admin/audit", nil, http.StatusOK))
	if len(got) != 2 || got[0] != "user.create" || got[1] != "user.setup" {
		t.Errorf("instance activity = %v, want account events only", got)
	}
	// The personal workspace's own log has no account events.
	if got := actionsOf(admin.mustDo(http.MethodGet, "/api/v1/audit", nil, http.StatusOK)); len(got) != 0 {
		t.Errorf("personal workspace activity = %v", got)
	}
	bob := e.anon()
	bob.mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "bob", "password": "password-123"}, http.StatusOK)
	if r := bob.do(http.MethodGet, "/api/v1/admin/audit", nil); r.code != http.StatusForbidden {
		t.Errorf("non-admin reading instance activity: %d %v", r.code, r.body)
	}
}
