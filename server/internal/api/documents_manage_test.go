package api

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
)

func q(filter string) string { return url.QueryEscape(filter) }

func titles(r resp) []string {
	var out []string
	for _, it := range r.body["items"].([]any) {
		out = append(out, it.(map[string]any)["title"].(string))
	}
	return out
}

func TestDocumentListEditAndBulk(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, bob, carol, _ := home(t, e)
	bill := alice.mustDo(http.MethodPost, "/api/v1/document-types", map[string]any{"name": "账单"}, http.StatusCreated).body["id"].(float64)
	tax := alice.mustDo(http.MethodPost, "/api/v1/tags", map[string]any{"name": "税务"}, http.StatusCreated).body["id"].(float64)
	a := uploadedDocument(alice, "电费 2024-03-05.txt")
	b := uploadedDocument(alice, "燃气 2024-04-03.txt")
	c := uploadedDocument(bob, "合同 2023-09-01.txt")

	// Edit: the list and the details agree; a stale modifiedAt is refused.
	d := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(a), nil, http.StatusOK).body
	if d["fields"] == nil || d["checksum"] == "" || d["originalSize"] == nil {
		t.Errorf("details = %v", d)
	}
	patch := map[string]any{"title": "3月电费", "documentTypeId": bill, "tagIds": []any{tax}, "asn": 12, "inbox": false,
		"customFields": []any{map[string]any{"fieldId": builtInField(alice, "amount"), "value": "326.40"}}, "modifiedAt": d["modifiedAt"]}
	up := alice.mustDo(http.MethodPatch, "/api/v1/documents/"+itoa(a), patch, http.StatusOK).body
	if up["title"] != "3月电费" || up["amount"] != "326.40" || up["asn"] != float64(12) || up["inbox"] != false {
		t.Errorf("updated = %v", up)
	}
	if r := alice.do(http.MethodPatch, "/api/v1/documents/"+itoa(a), map[string]any{"title": "x", "modifiedAt": d["modifiedAt"]}); r.code != http.StatusConflict {
		t.Errorf("stale modifiedAt: %d %v", r.code, r.body)
	}
	if r := carol.do(http.MethodPatch, "/api/v1/documents/"+itoa(a), map[string]any{"title": "x"}); r.code != http.StatusForbidden {
		t.Errorf("viewer edit: %d", r.code)
	}

	// List: filters, sort, cursor paging, highlight.
	byType := alice.mustDo(http.MethodGet, "/api/v1/documents?filter="+q(fmt.Sprintf(`{"field":"type","op":"in","value":[%v]}`, bill)), nil, http.StatusOK)
	if fmt.Sprint(titles(byType)) != "[3月电费]" || byType.body["total"] != float64(1) {
		t.Errorf("by type = %v", byType.body)
	}
	page1 := alice.mustDo(http.MethodGet, "/api/v1/documents?sort=title&dir=asc&limit=2", nil, http.StatusOK)
	page2 := alice.mustDo(http.MethodGet, "/api/v1/documents?sort=title&dir=asc&limit=2&cursor="+url.QueryEscape(page1.body["nextCursor"].(string)), nil, http.StatusOK)
	if all := append(titles(page1), titles(page2)...); fmt.Sprint(all) != "[3月电费 合同 2023-09-01 燃气 2024-04-03]" || page2.body["nextCursor"] != nil {
		t.Errorf("paged by title = %v", all)
	}
	e.waitIndexed()
	hit := alice.mustDo(http.MethodGet, "/api/v1/documents?sort=relevance&filter="+q(`{"text":"燃气"}`), nil, http.StatusOK).body["items"].([]any)
	if len(hit) != 1 || fmt.Sprint(hit[0].(map[string]any)["highlight"]) != "[map[hit:false text:content of ] map[hit:true text:燃气] map[hit:false text: 2024-04-03.txt]]" {
		t.Errorf("text search = %v", hit)
	}
	facets := alice.mustDo(http.MethodGet, "/api/v1/documents/facets?groups=type,year&filter="+q(fmt.Sprintf(`{"field":"type","op":"in","value":[%v]}`, bill)), nil, http.StatusOK).body
	if fmt.Sprint(facets["type"]) != fmt.Sprintf("[map[count:2 key:none] map[count:1 key:%v]]", bill) {
		t.Errorf("facets = %v", facets)
	}
	if r := alice.do(http.MethodGet, "/api/v1/documents/facets?groups=folder", nil); r.code != http.StatusBadRequest {
		t.Errorf("unknown facet: %d", r.code)
	}

	// Bulk: tag everything still in the inbox, then complete it.
	inbox := `{"field":"inbox","op":"eq","value":true}`
	r := bob.mustDo(http.MethodPost, "/api/v1/documents/bulk", map[string]any{"filter": map[string]any{"field": "inbox", "op": "eq", "value": true}, "edit": map[string]any{"addTagIds": []any{tax}, "inbox": false}}, http.StatusOK)
	if r.body["count"] != float64(2) {
		t.Errorf("bulk = %v", r.body)
	}
	if n := alice.mustDo(http.MethodGet, "/api/v1/documents?filter="+q(inbox), nil, http.StatusOK).body["total"]; n != float64(0) {
		t.Errorf("inbox after bulk complete = %v", n)
	}
	// History, newest first, readable by viewers.
	h := carol.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(b)+"/history", nil, http.StatusOK)
	if acts := actionsOf(h); len(acts) != 2 || acts[0] != "document.update" || acts[1] != "document.create" {
		t.Errorf("history = %v", acts)
	}

	rp := alice.mustDo(http.MethodPost, "/api/v1/documents/bulk", map[string]any{"ids": []any{b, c}, "reprocess": true}, http.StatusOK).body
	if rp["count"] != float64(2) || len(rp["jobIds"].([]any)) != 2 {
		t.Errorf("bulk reprocess = %v", rp)
	}
	for _, body := range []map[string]any{
		{"ids": []any{a}}, // no operation
		{"ids": []any{a}, "delete": true, "reprocess": true}, // two
		{"delete": true}, // no selection
	} {
		if r := alice.do(http.MethodPost, "/api/v1/documents/bulk", body); r.code != http.StatusBadRequest {
			t.Errorf("bulk %v: %d", body, r.code)
		}
	}

	// Directory counts.
	tags := alice.mustDo(http.MethodGet, "/api/v1/tags", nil, http.StatusOK).list
	if tags[0].(map[string]any)["documentCount"] != float64(3) {
		t.Errorf("tag count = %v", tags[0])
	}
}

func builtInField(c *client, key string) float64 {
	c.env.t.Helper()
	for _, f := range c.mustDo(http.MethodGet, "/api/v1/custom-fields", nil, http.StatusOK).list {
		f := f.(map[string]any)
		if f["builtIn"] == true && ((key == "amount" && f["dataType"] == "monetary") || (key == "due_date" && f["dataType"] == "date")) {
			return f["id"].(float64)
		}
	}
	c.env.t.Fatalf("no built-in field %s", key)
	return 0
}

func TestTrashNotesViewsAndDownload(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, bob, carol, _ := home(t, e)
	a := uploadedDocument(alice, "a.txt")
	up := alice.upload("a.txt", "content of another a", nil) // same name, other content: two entries in the zip
	b := alice.waitJob(up.body["jobId"].(float64))["documentId"].(float64)
	path := "/api/v1/documents/" + itoa(a)

	// Notes.
	n := bob.mustDo(http.MethodPost, path+"/notes", map[string]any{"body": "押金已退"}, http.StatusCreated).body
	if n["authorName"] == "" {
		t.Errorf("note = %v", n)
	}
	if r := carol.do(http.MethodPost, path+"/notes", map[string]any{"body": "x"}); r.code != http.StatusForbidden {
		t.Errorf("viewer note: %d", r.code)
	}
	if list := carol.mustDo(http.MethodGet, path+"/notes", nil, http.StatusOK).list; len(list) != 1 {
		t.Errorf("notes = %v", list)
	}
	alice.mustDo(http.MethodDelete, path+"/notes/"+itoa(n["id"].(float64)), nil, http.StatusNoContent) // an owner deletes any note

	// Download: a zip of both, with distinct names.
	dl := alice.do(http.MethodGet, "/api/v1/documents/download?ids="+itoa(a)+","+itoa(b), nil)
	if dl.code != http.StatusOK || dl.header.Get("Content-Type") != "application/zip" {
		t.Fatalf("download: %d %s", dl.code, dl.raw)
	}
	zr, err := zip.NewReader(bytes.NewReader([]byte(dl.raw)), int64(len(dl.raw)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
		rc, _ := f.Open()
		body, _ := io.ReadAll(rc)
		rc.Close()
		if !strings.HasPrefix(string(body), "content of") {
			t.Errorf("%s = %q", f.Name, body)
		}
	}
	sort.Strings(names)
	if fmt.Sprint(names) != "[a (2).txt a.txt]" {
		t.Errorf("zip entries = %v", names)
	}
	if r := alice.do(http.MethodGet, "/api/v1/documents/download?ids=999", nil); r.code != http.StatusNotFound {
		t.Errorf("download nothing: %d", r.code)
	}

	// Trash: editors delete and restore, only owners purge.
	bob.mustDo(http.MethodDelete, path, nil, http.StatusNoContent)
	alice.mustDo(http.MethodGet, path, nil, http.StatusNotFound)
	if r := alice.upload("a.txt", "content of a.txt", nil); r.code != http.StatusConflict || r.body["details"].(map[string]any)["inTrash"] != true {
		t.Errorf("uploading a trashed file again: %d %v", r.code, r.body)
	}
	trash := carol.mustDo(http.MethodGet, "/api/v1/trash", nil, http.StatusOK).list
	if len(trash) != 1 || trash[0].(map[string]any)["daysLeft"] != float64(30) {
		t.Errorf("trash = %v", trash)
	}
	bob.mustDo(http.MethodPost, path+"/restore", nil, http.StatusNoContent)
	alice.mustDo(http.MethodGet, path, nil, http.StatusOK)
	bob.mustDo(http.MethodDelete, path, nil, http.StatusNoContent)
	if r := bob.do(http.MethodDelete, "/api/v1/trash/"+itoa(a), nil); r.code != http.StatusForbidden {
		t.Errorf("editor purge: %d", r.code)
	}
	alice.mustDo(http.MethodDelete, "/api/v1/trash/"+itoa(a), nil, http.StatusNoContent)
	if r := alice.do(http.MethodPost, path+"/restore", nil); r.code != http.StatusNotFound {
		t.Errorf("restore after purge: %d", r.code)
	}
	if r := alice.mustDo(http.MethodDelete, "/api/v1/trash", nil, http.StatusOK); r.body["count"] != float64(0) {
		t.Errorf("empty trash = %v", r.body)
	}

	// Saved views.
	v := bob.mustDo(http.MethodPost, "/api/v1/saved-views", map[string]any{"name": "待整理", "filter": map[string]any{"field": "inbox", "op": "eq", "value": true}, "sort": "added:asc"}, http.StatusCreated).body
	if v["creatorName"] == "" || v["filter"].(map[string]any)["field"] != "inbox" {
		t.Errorf("view = %v", v)
	}
	vpath := "/api/v1/saved-views/" + itoa(v["id"].(float64))
	if r := carol.do(http.MethodPatch, vpath, map[string]any{"name": "x"}); r.code != http.StatusForbidden {
		t.Errorf("viewer edits a view: %d", r.code)
	}
	bob.mustDo(http.MethodPatch, vpath, map[string]any{"name": "新的"}, http.StatusOK)
	if list := carol.mustDo(http.MethodGet, "/api/v1/saved-views", nil, http.StatusOK).list; len(list) != 1 || list[0].(map[string]any)["name"] != "新的" {
		t.Errorf("views = %v", list)
	}
	alice.mustDo(http.MethodDelete, vpath, nil, http.StatusNoContent)
}

// Another account's documents are invisible through every new endpoint.
func TestDocumentEndpointsAreIsolated(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, _, _, _ := home(t, e)
	docID := uploadedDocument(alice, "secret.txt")
	doc := itoa(docID)
	note := alice.mustDo(http.MethodPost, "/api/v1/documents/"+doc+"/notes", map[string]any{"body": "x"}, http.StatusCreated).body["id"].(float64)
	view := alice.mustDo(http.MethodPost, "/api/v1/saved-views", map[string]any{"name": "v", "filter": map[string]any{}}, http.StatusCreated).body["id"].(float64)
	mallory := e.user("mallory") // her own personal workspace
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/documents/" + doc},
		{http.MethodPatch, "/api/v1/documents/" + doc},
		{http.MethodDelete, "/api/v1/documents/" + doc},
		{http.MethodPost, "/api/v1/documents/" + doc + "/restore"},
		{http.MethodGet, "/api/v1/documents/" + doc + "/history"},
		{http.MethodGet, "/api/v1/documents/" + doc + "/notes"},
		{http.MethodPost, "/api/v1/documents/" + doc + "/notes"},
		{http.MethodDelete, "/api/v1/documents/" + doc + "/notes/" + itoa(note)},
		{http.MethodGet, "/api/v1/documents/download?ids=" + doc},
		{http.MethodDelete, "/api/v1/trash/" + doc},
		{http.MethodPatch, "/api/v1/saved-views/" + itoa(view)},
		{http.MethodDelete, "/api/v1/saved-views/" + itoa(view)},
	} {
		body := map[string]any{"title": "x", "body": "x", "name": "x"}
		if r := mallory.do(c.method, c.path, body); r.code != http.StatusNotFound {
			t.Errorf("%s %s: %d %v", c.method, c.path, r.code, r.body)
		}
	}
	if r := mallory.mustDo(http.MethodPost, "/api/v1/documents/bulk", map[string]any{"ids": []any{docID}, "delete": true}, http.StatusOK); r.body["count"] != float64(0) {
		t.Errorf("bulk on another workspace's document = %v", r.body)
	}
	if list := mallory.mustDo(http.MethodGet, "/api/v1/documents", nil, http.StatusOK); list.body["total"] != float64(0) {
		t.Errorf("mallory sees %v", list.body)
	}
	e.waitIndexed()
	if r := mallory.mustDo(http.MethodGet, "/api/v1/documents?filter="+q(`{"text":"secret"}`), nil, http.StatusOK); r.body["total"] != float64(0) {
		t.Errorf("mallory finds alice's document by keyword: %v", r.body)
	}
	if r := alice.mustDo(http.MethodGet, "/api/v1/documents?filter="+q(`{"text":"secret"}`), nil, http.StatusOK); r.body["total"] != float64(1) {
		t.Errorf("alice's own keyword search: %v", r.body)
	}
	alice.mustDo(http.MethodGet, "/api/v1/documents/"+doc, nil, http.StatusOK)
}

func TestSecurityHeadersAndBodyLimit(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	r := alice.do(http.MethodGet, "/api/v1/me", nil)
	if r.header.Get("X-Content-Type-Options") != "nosniff" || r.header.Get("Referrer-Policy") != "same-origin" {
		t.Errorf("headers = %v", r.header)
	}
	big := map[string]any{"body": strings.Repeat("x", 2<<20)}
	if r := alice.do(http.MethodPost, "/api/v1/saved-views", big); r.code != http.StatusBadRequest {
		t.Errorf("2 MiB of JSON: %d", r.code)
	}
}
