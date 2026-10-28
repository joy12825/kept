package api

import (
	"net/http"
	"testing"
)

// The server answers in the request's language (v1.10.1): errors, task
// names and activity summaries are English with Accept-Language: en, and
// Chinese without the header.
func TestI18nAnswersInTheRequestLanguage(t *testing.T) {
	e := newEnv(t, "auto", nil)
	admin := e.setupAdmin() // no header: Chinese

	// an error
	r := admin.mustDo(http.MethodGet, "/api/v1/tasks?days=0", nil, http.StatusBadRequest)
	if r.body["message"] != "days 应在 1 到 90 之间" {
		t.Errorf("Chinese error = %v", r.body["message"])
	}
	admin.headers["Accept-Language"] = "en-US,en;q=0.9"
	r = admin.mustDo(http.MethodGet, "/api/v1/tasks?days=0", nil, http.StatusBadRequest)
	if r.body["message"] != "days must be between 1 and 90" {
		t.Errorf("English error = %v", r.body["message"])
	}
	admin.headers["Accept-Language"] = "zh-TW"
	if r := admin.mustDo(http.MethodGet, "/api/v1/tasks?days=0", nil, http.StatusBadRequest); r.body["message"] != "days 应在 1 到 90 之间" {
		t.Errorf("any Chinese is Chinese: %v", r.body["message"])
	}
	delete(admin.headers, "Accept-Language")

	// a task's name
	up := admin.upload("note.txt", "hello", nil)
	if up.code != http.StatusAccepted {
		t.Fatalf("upload: %d %s", up.code, up.raw)
	}
	job := up.body["jobId"].(float64)
	admin.waitJob(job)
	label := func() any {
		for _, it := range admin.mustDo(http.MethodGet, "/api/v1/tasks", nil, http.StatusOK).list {
			if it := it.(map[string]any); it["id"] == job {
				return it["label"]
			}
		}
		return nil
	}
	if got := label(); got != "上传" {
		t.Errorf("Chinese task label = %v", got)
	}
	admin.headers["Accept-Language"] = "en"
	if got := label(); got != "Upload" {
		t.Errorf("English task label = %v", got)
	}

	// an activity summary, and the new account's personal Space named in
	// the language of the request creating it
	admin.mustDo(http.MethodPost, "/api/v1/admin/users", map[string]any{"username": "bob", "email": "bob@example.com", "password": "password-123"}, http.StatusCreated)
	summaries := func() []any {
		var out []any
		for _, it := range admin.mustDo(http.MethodGet, "/api/v1/admin/audit", nil, http.StatusOK).body["items"].([]any) {
			out = append(out, it.(map[string]any)["summary"])
		}
		return out
	}
	if got := summaries(); !hasValue(got, "admin created the account bob") {
		t.Errorf("English activity = %v", got)
	}
	delete(admin.headers, "Accept-Language")
	if got := summaries(); !hasValue(got, "admin 创建了账号 bob") {
		t.Errorf("Chinese activity = %v", got)
	}
	var names []any
	rows, err := e.db.Query(`SELECT name FROM workspaces WHERE kind = 'personal' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		names = append(names, n)
	}
	if len(names) != 2 || names[0] != "admin 的空间" || names[1] != "bob's Space" {
		t.Errorf("personal Spaces = %v", names)
	}
}

func hasValue(list []any, v any) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// v1.10.2: opening the web or the app records the language in use, for the
// emails to someone whose account follows the browser; a request that names
// no language (a script) leaves it.
func TestMeRecordsTheLanguageInUse(t *testing.T) {
	e := newEnv(t, "auto", nil)
	c := e.user("wang")
	seen := func() string {
		var l string
		if err := e.db.QueryRow(`SELECT seen_lang FROM users WHERE username = 'wang'`).Scan(&l); err != nil {
			t.Fatal(err)
		}
		return l
	}
	c.headers["Accept-Language"] = "en-US,en;q=0.9"
	c.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK)
	if l := seen(); l != "en" {
		t.Fatalf("after an English request: %q", l)
	}
	delete(c.headers, "Accept-Language")
	c.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK)
	if l := seen(); l != "en" {
		t.Errorf("a request naming no language changed it: %q", l)
	}
	c.headers["Accept-Language"] = "zh-CN"
	c.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK)
	if l := seen(); l != "zh" {
		t.Errorf("after a Chinese request: %q", l)
	}
}
