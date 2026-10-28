package api

import (
	"net/http"
	"slices"
	"testing"
)

// v1.3.8: a document moves from a shared space into the caller's personal
// space, with its classification, fields, notes and file; the shared
// space lets go of it.
func TestMoveDocumentToPersonalSpace(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, bob, carol, _ := home(t, e)
	mine := personalOf(alice) // alice acting in her personal space
	personal := mine.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK).body["workspace"].(map[string]any)["id"].(float64)

	tag := alice.mustDo(http.MethodPost, "/api/v1/tags", map[string]any{"name": "医疗"}, http.StatusCreated).body["id"].(float64)
	typ := alice.mustDo(http.MethodPost, "/api/v1/document-types", map[string]any{"name": "体检报告"}, http.StatusCreated).body["id"].(float64)
	doc := alice.waitJob(alice.upload("体检.txt", "2024 年度体检报告 血压正常", nil).body["jobId"].(float64))["documentId"].(float64)
	path := "/api/v1/documents/" + itoa(doc)
	d := alice.mustDo(http.MethodGet, path, nil, http.StatusOK).body
	alice.mustDo(http.MethodPatch, path, map[string]any{"tagIds": []any{tag}, "documentTypeId": typ, "asn": 5, "modifiedAt": d["modifiedAt"],
		"customFields": []any{map[string]any{"fieldId": builtInField(alice, "amount"), "value": "680.00"}}}, http.StatusOK)
	bob.mustDo(http.MethodPost, path+"/notes", map[string]any{"body": "复查在 3 月"}, http.StatusCreated)
	link := alice.mustDo(http.MethodPost, path+"/share-links", map[string]any{"days": 7, "file": "original"}, http.StatusCreated).body["token"].(string)
	// the personal space already uses #000005
	other := mine.waitJob(mine.upload("便条.txt", "买牛奶", nil).body["jobId"].(float64))["documentId"].(float64)
	o := mine.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(other), nil, http.StatusOK).body
	mine.mustDo(http.MethodPatch, "/api/v1/documents/"+itoa(other), map[string]any{"asn": 5, "modifiedAt": o["modifiedAt"]}, http.StatusOK)

	move := func(c *client, target float64, want int) resp {
		return c.mustDo(http.MethodPost, "/api/v1/documents/move", map[string]any{"ids": []any{doc}, "workspaceId": target}, want)
	}
	move(carol, personal, http.StatusForbidden) // only looks
	move(bob, personal, http.StatusForbidden)   // not bob's space
	move(alice, personalOf(bob).mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK).body["workspace"].(map[string]any)["id"].(float64), http.StatusForbidden)

	r := move(alice, personal, http.StatusOK).body
	if r["moved"] != float64(1) || r["workspaceName"] == "" || len(r["asnCleared"].([]any)) != 1 {
		t.Fatalf("result: %v", r)
	}
	created := r["created"].([]any)
	if !slices.Contains(created, any("标签“医疗”")) || !slices.Contains(created, any("类型“体检报告”")) {
		t.Errorf("created: %v", created)
	}

	// gone from the shared space, for everyone
	alice.mustDo(http.MethodGet, path, nil, http.StatusNotFound)
	bob.mustDo(http.MethodGet, path, nil, http.StatusNotFound)
	e.anon().mustDo(http.MethodGet, "/api/v1/public/shares/"+link, nil, http.StatusNotFound)

	// in the personal space, with what it had
	got := mine.mustDo(http.MethodGet, path, nil, http.StatusOK).body
	if got["asn"] != nil || got["amount"] != "680.00" || got["noteCount"] != float64(1) || len(got["tagIds"].([]any)) != 1 || got["tagIds"].([]any)[0] == tag || got["documentTypeId"] == typ {
		t.Fatalf("moved: %v", got)
	}
	tags := mine.mustDo(http.MethodGet, "/api/v1/tags", nil, http.StatusOK).list
	if !slices.ContainsFunc(tags, func(x any) bool {
		m := x.(map[string]any)
		return m["name"] == "医疗" && m["id"] == got["tagIds"].([]any)[0]
	}) {
		t.Errorf("tags: %v", tags)
	}
	if f := mine.do(http.MethodGet, path+"/original", nil); f.code != http.StatusOK || f.raw != "2024 年度体检报告 血压正常" {
		t.Errorf("original: %d %q", f.code, f.raw)
	}
	if n := mine.mustDo(http.MethodGet, path+"/notes", nil, http.StatusOK).list; len(n) != 1 {
		t.Errorf("notes: %v", n)
	}

	// recorded in both spaces
	if !slices.Contains(actionsOf(alice.mustDo(http.MethodGet, "/api/v1/audit", nil, http.StatusOK)), "document.move") {
		t.Error("the shared space should record the move")
	}

	// a space keeps one document per file
	mine.waitJob(mine.upload("收据.txt", "停车费收据 20 元", nil).body["jobId"].(float64))
	same := alice.waitJob(alice.upload("收据.txt", "停车费收据 20 元", nil).body["jobId"].(float64))["documentId"].(float64)
	if c := alice.do(http.MethodPost, "/api/v1/documents/move", map[string]any{"ids": []any{same}, "workspaceId": personal}); c.code != http.StatusConflict {
		t.Errorf("duplicate: %d %s", c.code, c.raw)
	}
	alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(same), nil, http.StatusOK) // still there
	move(mine, personal, http.StatusBadRequest)                                       // already there
}

// personalOf is c without a workspace header: in the personal space.
func personalOf(c *client) *client {
	cp := c.inSpace(nil)
	delete(cp.headers, "X-Workspace-ID")
	return cp
}

// A select field's value follows its label: an option the target's field
// lacks is added to it.
func TestMoveMapsSelectOptionsByLabel(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, _, _, _ := home(t, e)
	mine := personalOf(alice)
	personal := mine.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK).body["workspace"].(map[string]any)["id"].(float64)
	field := func(c *client, labels ...string) map[string]any {
		var opts []any
		for _, l := range labels {
			opts = append(opts, map[string]any{"label": l})
		}
		return c.mustDo(http.MethodPost, "/api/v1/custom-fields", map[string]any{"name": "报销状态", "dataType": "select", "extra": map[string]any{"options": opts}}, http.StatusCreated).body
	}
	homeField := field(alice, "未报销", "已报销")
	field(mine, "已报销")
	level := alice.mustDo(http.MethodPost, "/api/v1/custom-fields", map[string]any{"name": "级别", "dataType": "select",
		"extra": map[string]any{"options": []any{map[string]any{"label": "高"}, map[string]any{"label": "低"}}}}, http.StatusCreated).body
	high := level["extra"].(map[string]any)["options"].([]any)[0].(map[string]any)["id"]
	pending := homeField["extra"].(map[string]any)["options"].([]any)[0].(map[string]any)["id"]

	doc := alice.waitJob(alice.upload("出差.txt", "出差住宿发票", nil).body["jobId"].(float64))["documentId"].(float64)
	d := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(doc), nil, http.StatusOK).body
	alice.mustDo(http.MethodPatch, "/api/v1/documents/"+itoa(doc), map[string]any{"modifiedAt": d["modifiedAt"],
		"customFields": []any{map[string]any{"fieldId": homeField["id"], "value": pending}, map[string]any{"fieldId": level["id"], "value": high}}}, http.StatusOK)
	alice.mustDo(http.MethodPost, "/api/v1/documents/move", map[string]any{"ids": []any{doc}, "workspaceId": personal}, http.StatusOK)

	var target map[string]any
	for _, f := range mine.mustDo(http.MethodGet, "/api/v1/custom-fields", nil, http.StatusOK).list {
		if f.(map[string]any)["name"] == "报销状态" {
			target = f.(map[string]any)
		}
	}
	label := map[any]any{}
	for _, o := range target["extra"].(map[string]any)["options"].([]any) {
		label[o.(map[string]any)["id"]] = o.(map[string]any)["label"]
	}
	got := mine.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(doc), nil, http.StatusOK).body
	var value any
	for _, f := range got["fields"].([]any) {
		if f.(map[string]any)["fieldId"] == target["id"] {
			value = f.(map[string]any)["value"]
		}
	}
	if len(label) != 2 || label[value] != "未报销" {
		t.Fatalf("options %v, value %v", label, value)
	}
	// a field the target lacks is created with its options
	for _, f := range mine.mustDo(http.MethodGet, "/api/v1/custom-fields", nil, http.StatusOK).list {
		if f := f.(map[string]any); f["name"] == "级别" {
			for _, v := range got["fields"].([]any) {
				if v := v.(map[string]any); v["fieldId"] == f["id"] {
					for _, o := range f["extra"].(map[string]any)["options"].([]any) {
						if o := o.(map[string]any); o["id"] == v["value"] && o["label"] == "高" {
							return
						}
					}
				}
			}
		}
	}
	t.Error("the created select field should keep the value 高")
}
