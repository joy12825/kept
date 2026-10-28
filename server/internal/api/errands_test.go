package api

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// v1.6.2: checklists of an errand's papers, and upload links.

// waitIdle waits until the workspace has no queued or running jobs.
func (c *client) waitIdle() {
	c.env.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		// the jobs table: the tray hides others' uploads until they are documents
		var busy int
		if err := c.env.db.QueryRow(`SELECT count(*) FROM jobs WHERE status IN ('queued', 'running')`).Scan(&busy); err != nil {
			c.env.t.Fatal(err)
		}
		if busy == 0 {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	c.env.t.Fatal("jobs did not finish")
}

func TestChecklists(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, _, carol, _ := home(t, e)
	passport := alice.waitJob(alice.upload("护照.txt", "中华人民共和国护照 王小明", nil).body["jobId"].(float64))["documentId"].(float64)
	alice.waitJob(alice.upload("工资条 1月.txt", "工资条 2024年1月", nil).body["jobId"].(float64))
	slip := alice.waitJob(alice.upload("工资条 2月.txt", "工资条 2024年2月", nil).body["jobId"].(float64))["documentId"].(float64)
	alice.mustDo(http.MethodPatch, "/api/v1/documents/"+itoa(passport), map[string]any{"customFields": []any{map[string]any{"fieldId": builtInField(alice, "due_date"), "value": "2020-01-01"}}}, http.StatusOK)
	e.waitIndexed()

	body := map[string]any{"name": "办签证", "note": "大使馆要原件和复印件", "items": []any{
		map[string]any{"label": "护照", "filter": map[string]any{"text": "护照"}},
		map[string]any{"label": "近 3 个月工资条", "filter": map[string]any{"text": "工资条"}, "count": 3},
		map[string]any{"label": "户口本", "filter": map[string]any{"text": "户口本"}, "note": "整本复印"},
	}}
	carol.mustDo(http.MethodPost, "/api/v1/checklists", body, http.StatusForbidden) // a viewer only reads
	c := alice.mustDo(http.MethodPost, "/api/v1/checklists", body, http.StatusCreated).body
	id := itoa(c["id"].(float64))
	items := c["items"].([]any)
	if len(items) != 3 || items[0].(map[string]any)["id"] == "" || items[0].(map[string]any)["count"] != float64(1) {
		t.Fatalf("created: %v", c)
	}

	r := carol.mustDo(http.MethodGet, "/api/v1/checklists/"+id+"/check", nil, http.StatusOK).body
	got := map[string]string{}
	for _, it := range r["items"].([]any) {
		it := it.(map[string]any)
		got[it["label"].(string)] = fmt.Sprintf("%s %d %d", it["status"], len(it["documents"].([]any)), len(it["expiredIds"].([]any)))
	}
	want := map[string]string{"护照": "ok 1 1", "近 3 个月工资条": "partial 2 0", "户口本": "missing 0 0"}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: %q, want %q", k, got[k], w)
		}
	}
	slips := r["items"].([]any)[1].(map[string]any)["documents"].([]any)
	if slips[0].(map[string]any)["id"] != slip {
		t.Errorf("newest first: %v", slips)
	}

	// a filter naming a tag that does not exist is refused
	bad := map[string]any{"name": "x", "items": []any{map[string]any{"label": "x", "filter": map[string]any{"field": "tag", "op": "has_any", "value": []any{9999}}}}}
	alice.mustDo(http.MethodPut, "/api/v1/checklists/"+id, bad, http.StatusBadRequest)
	for _, b := range []map[string]any{{"name": "", "items": []any{}}, {"name": "x", "items": []any{map[string]any{"label": "x", "filter": map[string]any{}}}}, {"name": "x", "items": []any{map[string]any{"label": "x", "filter": map[string]any{"text": "a"}, "count": 30}}}} {
		alice.mustDo(http.MethodPost, "/api/v1/checklists", b, http.StatusBadRequest)
	}
	// edit keeps item IDs; delete
	items[0].(map[string]any)["label"] = "护照（有效期 6 个月以上）"
	upd := alice.mustDo(http.MethodPut, "/api/v1/checklists/"+id, map[string]any{"name": "办签证", "items": items}, http.StatusOK).body
	if upd["items"].([]any)[0].(map[string]any)["id"] != items[0].(map[string]any)["id"] {
		t.Errorf("item id changed: %v", upd)
	}
	if list := carol.mustDo(http.MethodGet, "/api/v1/checklists", nil, http.StatusOK); len(list.list) != 1 {
		t.Errorf("list: %s", list.raw)
	}
	e.user("bob2").mustDo(http.MethodGet, "/api/v1/checklists/"+id, nil, http.StatusNotFound) // another workspace
	alice.mustDo(http.MethodDelete, "/api/v1/checklists/"+id, nil, http.StatusNoContent)
	alice.mustDo(http.MethodGet, "/api/v1/checklists/"+id, nil, http.StatusNotFound)
}

func (c *client) uploadThrough(token, filename, content string) resp {
	c.env.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", filename)
	_, _ = io.WriteString(fw, content)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/public/uploads/"+token, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return c.send(req)
}
