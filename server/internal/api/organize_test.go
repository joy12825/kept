package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// v1.5.8: presets at upload, a document folded in as another's version,
// the directory tidied in bulk, and multi-line text fields.

func TestUploadPresetsDateASNAndFields(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	note := alice.mustDo(http.MethodPost, "/api/v1/custom-fields", map[string]any{"name": "说明", "dataType": "text", "extra": map[string]any{"multiline": true}}, http.StatusCreated).body
	if note["extra"].(map[string]any)["multiline"] != true {
		t.Fatalf("multiline field: %v", note)
	}
	fields := fmt.Sprintf(`[{"fieldId":%v,"value":"326.40"},{"fieldId":%v,"value":"第一行\n第二行"}]`, builtInField(alice, "amount"), note["id"])
	r := alice.upload("电费.txt", "国家电网 电费 开票日期 2024年3月5日", map[string]string{"createdDate": "2023-12-31", "asn": "42", "customFields": fields})
	doc := alice.waitJob(r.body["jobId"].(float64))["documentId"].(float64)
	d := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(doc), nil, http.StatusOK).body
	if d["createdDate"] != "2023-12-31" || d["asn"] != float64(42) || d["amount"] != "326.40" {
		t.Fatalf("presets: %v", d)
	}
	if src := d["fieldSources"].(map[string]any)["date"].(map[string]any); src["source"] != "preset" {
		t.Errorf("date source: %v", src)
	}
	found := false
	for _, f := range d["fields"].([]any) {
		if f.(map[string]any)["fieldId"] == note["id"] && f.(map[string]any)["value"] == "第一行\n第二行" {
			found = true
		}
	}
	if !found {
		t.Errorf("multi-line value not kept: %v", d["fields"])
	}
	for name, v := range map[string]string{"createdDate": "2024/03/05", "asn": "0", "customFields": "{oops"} {
		if r := alice.upload("x.txt", "x", map[string]string{name: v}); r.code != http.StatusBadRequest {
			t.Errorf("%s=%q: %d %s", name, v, r.code, r.raw)
		}
	}
}

func TestDocumentFoldedInAsVersion(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	contract := alice.waitJob(alice.upload("租房合同.txt", "租房合同 2023 年 房租 3000 元", nil).body["jobId"].(float64))["documentId"].(float64)
	renewal := alice.waitJob(alice.upload("续签合同.txt", "租房合同 2024 年续签 房租 3200 元", nil).body["jobId"].(float64))["documentId"].(float64)
	invoice := alice.waitJob(alice.upload("房租发票.txt", "房租发票", nil).body["jobId"].(float64))["documentId"].(float64)
	tag := alice.mustDo(http.MethodPost, "/api/v1/tags", map[string]any{"name": "续签"}, http.StatusCreated).body["id"]
	link := alice.mustDo(http.MethodPost, "/api/v1/custom-fields", map[string]any{"name": "合同", "dataType": "document_link"}, http.StatusCreated).body["id"]
	alice.mustDo(http.MethodPatch, "/api/v1/documents/"+itoa(renewal), map[string]any{"tagIds": []any{tag}}, http.StatusOK)
	alice.mustDo(http.MethodPost, "/api/v1/documents/"+itoa(renewal)+"/notes", map[string]any{"body": "押金不变"}, http.StatusCreated)
	alice.mustDo(http.MethodPatch, "/api/v1/documents/"+itoa(invoice), map[string]any{"customFields": []any{map[string]any{"fieldId": link, "value": renewal}}}, http.StatusOK)

	base := "/api/v1/documents/" + itoa(contract)
	alice.mustDo(http.MethodPost, base+"/versions/from-document", map[string]any{"documentId": contract}, http.StatusBadRequest)
	r := alice.mustDo(http.MethodPost, base+"/versions/from-document", map[string]any{"documentId": renewal}, http.StatusAccepted)
	alice.waitJob(r.body["jobId"].(float64))

	d := alice.mustDo(http.MethodGet, base, nil, http.StatusOK).body
	if !strings.Contains(d["content"].(string), "3200") || d["originalFilename"] != "续签合同.txt" || d["title"] != "租房合同" || len(d["tagIds"].([]any)) != 1 || d["noteCount"] != float64(1) {
		t.Fatalf("after folding in: %v", d)
	}
	vs := alice.mustDo(http.MethodGet, base+"/versions", nil, http.StatusOK)
	if len(vs.list) != 2 || vs.list[0].(map[string]any)["label"] != "取自《续签合同》" {
		t.Fatalf("versions: %s", vs.raw)
	}
	// the renewal is gone, not in the trash; the invoice now links to the contract
	alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(renewal), nil, http.StatusNotFound)
	if trash := alice.mustDo(http.MethodGet, "/api/v1/trash", nil, http.StatusOK).raw; strings.Contains(trash, "续签合同") {
		t.Errorf("in the trash: %s", trash)
	}
	inv := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(invoice), nil, http.StatusOK).body
	if v := inv["fields"].([]any)[0].(map[string]any)["value"]; v != contract {
		t.Errorf("link = %v, want %v", v, contract)
	}
	if hist := alice.mustDo(http.MethodGet, base+"/history", nil, http.StatusOK).raw; !strings.Contains(hist, "把《续签合同》并为“租房合同”的第 2 版") {
		t.Errorf("history: %s", hist)
	}
	// a document with versions of its own cannot be folded in; nor another workspace's
	other := alice.waitJob(alice.upload("另一份.txt", "另一份", nil).body["jobId"].(float64))["documentId"].(float64)
	alice.waitJob(alice.mustDo(http.MethodPost, base+"/versions/from-document", map[string]any{"documentId": other}, http.StatusAccepted).body["jobId"].(float64))
	third := alice.waitJob(alice.upload("第三份.txt", "第三份", nil).body["jobId"].(float64))["documentId"].(float64)
	if r := alice.do(http.MethodPost, "/api/v1/documents/"+itoa(third)+"/versions/from-document", map[string]any{"documentId": contract}); r.code != http.StatusBadRequest || !strings.Contains(r.raw, "多个版本") {
		t.Errorf("with versions: %d %s", r.code, r.raw)
	}
	bob := e.user("bob")
	bobDoc := bob.waitJob(bob.upload("b.txt", "bob 的", nil).body["jobId"].(float64))["documentId"].(float64)
	bob.mustDo(http.MethodPost, "/api/v1/documents/"+itoa(bobDoc)+"/versions/from-document", map[string]any{"documentId": third}, http.StatusNotFound)
}

func TestDirectoryBulkDeleteAndMerge(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, _, carol, _ := home(t, e)
	mk := func(path, name string, extra map[string]any) float64 {
		body := map[string]any{"name": name}
		for k, v := range extra {
			body[k] = v
		}
		return alice.mustDo(http.MethodPost, path, body, http.StatusCreated).body["id"].(float64)
	}
	a := mk("/api/v1/correspondents", "国家电网", nil)
	b := mk("/api/v1/correspondents", "国网北京", nil)
	c := mk("/api/v1/correspondents", "国网北京分公司", nil)
	junk1, junk2 := mk("/api/v1/correspondents", "noreply", nil), mk("/api/v1/correspondents", "广告", nil)
	doc := alice.waitJob(alice.upload("电费.txt", "电费", nil).body["jobId"].(float64))["documentId"].(float64)
	alice.mustDo(http.MethodPatch, "/api/v1/documents/"+itoa(doc), map[string]any{"correspondentId": b}, http.StatusOK)

	r := alice.mustDo(http.MethodPost, "/api/v1/directory/correspondents/merge", map[string]any{"keep": a, "merge": []any{b, c}}, http.StatusOK)
	if r.body["documents"] != float64(1) {
		t.Errorf("merge: %s", r.raw)
	}
	if d := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(doc), nil, http.StatusOK).body; d["correspondentId"] != a {
		t.Errorf("document moved to %v", d["correspondentId"])
	}
	r = alice.mustDo(http.MethodPost, "/api/v1/directory/correspondents/delete", map[string]any{"ids": []any{junk1, junk2, b, 999999}}, http.StatusOK)
	if r.body["count"] != float64(2) {
		t.Errorf("delete: %s", r.raw)
	}
	if list := alice.mustDo(http.MethodGet, "/api/v1/correspondents", nil, http.StatusOK); len(list.list) != 1 {
		t.Errorf("left: %s", list.raw)
	}
	// tags: not into a child of a merged tag
	parent := mk("/api/v1/tags", "水电", nil)
	child := mk("/api/v1/tags", "电", map[string]any{"parentId": parent})
	if r := alice.do(http.MethodPost, "/api/v1/directory/tags/merge", map[string]any{"keep": child, "merge": []any{parent}}); r.code != http.StatusBadRequest {
		t.Errorf("into a child: %d %s", r.code, r.raw)
	}
	alice.mustDo(http.MethodPost, "/api/v1/directory/tags/delete", map[string]any{"ids": []any{}}, http.StatusBadRequest)
	// a viewer cannot
	carol.mustDo(http.MethodPost, "/api/v1/directory/tags/delete", map[string]any{"ids": []any{parent}}, http.StatusForbidden)
}
