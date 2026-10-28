package api

import (
	"net/http"
	"testing"
)

// v1.10.1: the documents group answers in the request's language — a name
// conflict (marked as the name field's, whatever the language), the
// activity summaries, the drawer templates — and names what it creates
// (the built-in fields of a new account's Space) in it too.
func TestDocsI18n(t *testing.T) {
	e := newEnv(t, "auto", nil)
	admin := e.setupAdmin() // no header: Chinese
	admin.headers["Accept-Language"] = "en"

	admin.mustDo(http.MethodPost, "/api/v1/correspondents", map[string]any{"name": "Acme"}, http.StatusCreated)
	r := admin.mustDo(http.MethodPost, "/api/v1/correspondents", map[string]any{"name": "Acme"}, http.StatusConflict)
	if r.body["message"] != "A correspondent with this name already exists" {
		t.Errorf("English conflict = %v", r.body["message"])
	}
	if d, _ := r.body["details"].(map[string]any); d["field"] != "name" {
		t.Errorf("conflict details = %v", r.body["details"])
	}
	delete(admin.headers, "Accept-Language")
	r = admin.mustDo(http.MethodPost, "/api/v1/correspondents", map[string]any{"name": "Acme"}, http.StatusConflict)
	if r.body["message"] != "已经有同名的往来方" {
		t.Errorf("Chinese conflict = %v", r.body["message"])
	}
	if d, _ := r.body["details"].(map[string]any); d["field"] != "name" {
		t.Errorf("conflict details = %v", r.body["details"])
	}

	// the activity summary, stored in Chinese, read in both languages
	summaries := func() []any {
		var out []any
		for _, it := range admin.mustDo(http.MethodGet, "/api/v1/audit", nil, http.StatusOK).body["items"].([]any) {
			out = append(out, it.(map[string]any)["summary"])
		}
		return out
	}
	if got := summaries(); !hasValue(got, "admin 新建了往来方“Acme”") {
		t.Errorf("Chinese activity = %v", got)
	}
	admin.headers["Accept-Language"] = "en"
	if got := summaries(); !hasValue(got, "admin created the correspondent “Acme”") {
		t.Errorf("English activity = %v", got)
	}

	// the drawer templates
	tpl := admin.mustDo(http.MethodGet, "/api/v1/drawer-templates", nil, http.StatusOK).body
	if name := tpl["templates"].([]any)[0].(map[string]any)["name"]; name != "House" {
		t.Errorf("English template = %v", name)
	}

	// a new account's Space: the built-in fields in the creating request's language
	admin.mustDo(http.MethodPost, "/api/v1/admin/users", map[string]any{"username": "bob", "email": "bob@example.com", "password": "password-123"}, http.StatusCreated)
	var names []string
	rows, err := e.db.Query(`SELECT name FROM custom_fields WHERE key IS NOT NULL ORDER BY workspace_id, key`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		names = append(names, n)
	}
	// admin's Space (made without a header), then bob's
	if len(names) != 4 || names[0] != "金额" || names[1] != "到期日" || names[2] != "Amount" || names[3] != "Due date" {
		t.Errorf("built-in fields = %v", names)
	}
}
