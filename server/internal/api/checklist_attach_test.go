package api

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// uploadTo posts a file to a checklist item; title, when given, goes first.
func (c *client) uploadTo(path, filename, content string, title ...string) resp {
	c.env.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if len(title) > 0 {
		_ = mw.WriteField("title", title[0])
	}
	fw, _ := mw.CreateFormFile("file", filename)
	_, _ = io.WriteString(fw, content)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return c.send(req)
}

// v1.6.7: a missing paper is uploaded, or an existing document chosen,
// straight into a checklist item; it counts whatever the filter says.
func TestChecklistAttachAndUpload(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, _, carol, _ := home(t, e)
	photo := alice.waitJob(alice.upload("全家福.txt", "合影", nil).body["jobId"].(float64))["documentId"].(float64)
	c := alice.mustDo(http.MethodPost, "/api/v1/checklists", map[string]any{"name": "落户", "items": []any{
		map[string]any{"label": "户口本", "filter": map[string]any{"text": "户口本"}},
		map[string]any{"label": "照片", "filter": map[string]any{"text": "证件照"}, "count": 2},
	}}, http.StatusCreated).body
	id := itoa(c["id"].(float64))
	hukou := c["items"].([]any)[0].(map[string]any)["id"].(string)
	pics := c["items"].([]any)[1].(map[string]any)["id"].(string)
	item := func(label string) map[string]any {
		for _, it := range alice.mustDo(http.MethodGet, "/api/v1/checklists/"+id+"/check", nil, http.StatusOK).body["items"].([]any) {
			if it := it.(map[string]any); it["label"] == label {
				return it
			}
		}
		t.Fatalf("no item %s", label)
		return nil
	}

	// upload into 户口本: the scan does not say “户口本”, yet it counts
	if r := carol.uploadTo("/api/v1/checklists/"+id+"/items/"+hukou+"/upload", "扫描.txt", "常住人口登记卡 王小明"); r.code != http.StatusForbidden {
		t.Errorf("a viewer uploaded: %d", r.code)
	}
	if r := alice.uploadTo("/api/v1/checklists/"+id+"/items/nope/upload", "x.txt", "x"); r.code != http.StatusNotFound {
		t.Errorf("unknown item: %d", r.code)
	}
	r := alice.uploadTo("/api/v1/checklists/"+id+"/items/"+hukou+"/upload", "扫描.txt", "常住人口登记卡 王小明")
	if r.code != http.StatusAccepted {
		t.Fatalf("upload: %d %s", r.code, r.raw)
	}
	scan := alice.waitJob(r.body["jobId"].(float64))["documentId"].(float64)
	it := item("户口本")
	if it["status"] != "ok" || len(it["attachedIds"].([]any)) != 1 || it["attachedIds"].([]any)[0] != scan {
		t.Fatalf("after the upload: %v", it)
	}
	if d := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(scan), nil, http.StatusOK).body; d["inbox"] != true {
		t.Errorf("an upload from a checklist is still filed like any other: %v", d["inbox"])
	}
	// the same file again: nothing new, the existing one is put in
	r = alice.uploadTo("/api/v1/checklists/"+id+"/items/"+pics+"/upload", "扫描副本.txt", "常住人口登记卡 王小明")
	if r.code != http.StatusOK || r.body["documentId"] != scan {
		t.Fatalf("duplicate: %d %s", r.code, r.raw)
	}

	// choose an existing document; then take it out again
	carol.mustDo(http.MethodPost, "/api/v1/checklists/"+id+"/items/"+pics+"/documents", map[string]any{"documentId": photo}, http.StatusForbidden)
	alice.mustDo(http.MethodPost, "/api/v1/checklists/"+id+"/items/"+pics+"/documents", map[string]any{"documentId": photo}, http.StatusNoContent)
	alice.mustDo(http.MethodPost, "/api/v1/checklists/"+id+"/items/"+pics+"/documents", map[string]any{"documentId": photo}, http.StatusNoContent) // twice is once
	alice.mustDo(http.MethodPost, "/api/v1/checklists/"+id+"/items/"+pics+"/documents", map[string]any{"documentId": 99999}, http.StatusNotFound)
	if it := item("照片"); it["status"] != "ok" || len(it["documents"].([]any)) != 2 {
		t.Errorf("two put in: %v", it)
	}
	alice.mustDo(http.MethodDelete, "/api/v1/checklists/"+id+"/items/"+pics+"/documents/"+itoa(photo), nil, http.StatusNoContent)
	alice.mustDo(http.MethodDelete, "/api/v1/checklists/"+id+"/items/"+pics+"/documents/"+itoa(photo), nil, http.StatusNotFound)
	if it := item("照片"); it["status"] != "partial" || len(it["attachedIds"].([]any)) != 1 {
		t.Errorf("after taking one out: %v", it)
	}
	// a document in the trash no longer counts
	alice.mustDo(http.MethodDelete, "/api/v1/documents/"+itoa(scan), nil, http.StatusNoContent)
	if it := item("户口本"); it["status"] != "missing" {
		t.Errorf("trashed: %v", it)
	}
	// v1.6.8: a title given with the upload (a photo called IMG_1234)
	r = alice.uploadTo("/api/v1/checklists/"+id+"/items/"+pics+"/upload", "IMG_1234.txt", "证件照 两寸 蓝底", "  照片  ")
	if r.code != http.StatusAccepted {
		t.Fatalf("upload with a title: %d %s", r.code, r.raw)
	}
	titled := alice.waitJob(r.body["jobId"].(float64))["documentId"].(float64)
	if d := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(titled), nil, http.StatusOK).body; d["title"] != "照片" {
		t.Errorf("title: %v", d["title"])
	}

	// an item taken out of the checklist takes its documents with it
	alice.mustDo(http.MethodPut, "/api/v1/checklists/"+id, map[string]any{"name": "落户", "items": []any{
		map[string]any{"id": hukou, "label": "户口本", "filter": map[string]any{"text": "户口本"}},
	}}, http.StatusOK)
	var left int
	if err := e.db.QueryRow(`SELECT count(*) FROM checklist_documents WHERE item_id = ?`, pics).Scan(&left); err != nil || left != 0 {
		t.Errorf("documents of a removed item: %d %v", left, err)
	}
	hist := alice.mustDo(http.MethodGet, "/api/v1/audit", nil, http.StatusOK).raw
	if !strings.Contains(hist, "放进了清单“落户”的“照片”") || !strings.Contains(hist, "移出了清单“落户”的“照片”") {
		t.Errorf("audit: %s", hist)
	}
}
