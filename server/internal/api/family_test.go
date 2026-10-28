package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// v1.6.3: family members (家人) and where originals are kept (存放位置).
func TestPersonsAndLocations(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, _, carol, _ := home(t, e)
	carol.mustDo(http.MethodPost, "/api/v1/persons", map[string]any{"name": "乐乐"}, http.StatusForbidden)
	kid := alice.mustDo(http.MethodPost, "/api/v1/persons", map[string]any{"name": "乐乐"}, http.StatusCreated).body["id"].(float64)
	dad := alice.mustDo(http.MethodPost, "/api/v1/persons", map[string]any{"name": "爸爸"}, http.StatusCreated).body["id"].(float64)
	alice.mustDo(http.MethodPost, "/api/v1/persons", map[string]any{"name": "乐乐"}, http.StatusConflict)

	// an upload may name the person; editing and bulk editing set it too
	r := alice.upload("护照.txt", "中华人民共和国护照 有效期至 2026-10-28", map[string]string{"personId": itoa(kid)})
	passport := alice.waitJob(r.body["jobId"].(float64))["documentId"].(float64)
	check := alice.waitJob(alice.upload("体检报告.txt", "年度体检报告 血压正常", nil).body["jobId"].(float64))["documentId"].(float64)
	d := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(passport), nil, http.StatusOK).body
	if d["personId"] != kid || d["fieldSources"].(map[string]any)["person"].(map[string]any)["source"] != "preset" {
		t.Fatalf("uploaded: %v %v", d["personId"], d["fieldSources"])
	}
	box := alice.mustDo(http.MethodPost, "/api/v1/locations", map[string]any{"name": "文件盒 A", "note": "书房柜子第 2 层"}, http.StatusCreated).body
	if box["documentCount"] != float64(0) || box["initial"] != "W" {
		t.Errorf("location: %v", box)
	}
	alice.mustDo(http.MethodPatch, "/api/v1/documents/"+itoa(check), map[string]any{"personId": dad, "asn": 7}, http.StatusOK)
	alice.mustDo(http.MethodPatch, "/api/v1/documents/"+itoa(check), map[string]any{"personId": 99999}, http.StatusBadRequest)
	alice.mustDo(http.MethodPatch, "/api/v1/documents/"+itoa(passport), map[string]any{"locationId": box["id"]}, http.StatusOK)

	list := func(f string) []any {
		return alice.mustDo(http.MethodGet, "/api/v1/documents?filter="+url.QueryEscape(f), nil, http.StatusOK).body["items"].([]any)
	}
	if got := list(`{"field":"person","op":"in","value":[` + itoa(kid) + `]}`); len(got) != 1 || got[0].(map[string]any)["locationId"] != box["id"] {
		t.Errorf("by person: %v", got)
	}
	// the person's name is searchable
	e.waitIndexed()
	if got := list(`{"text":"爸爸"}`); len(got) != 1 || got[0].(map[string]any)["id"] != check {
		t.Errorf("search by person: %v", got)
	}

	// originals 1..10 into the box at once
	n := alice.mustDo(http.MethodPost, "/api/v1/locations/assign-asn", map[string]any{"locationId": box["id"], "from": 1, "to": 10}, http.StatusOK).body["updated"]
	if n != float64(1) {
		t.Errorf("assigned %v", n)
	}
	alice.mustDo(http.MethodPost, "/api/v1/locations/assign-asn", map[string]any{"locationId": box["id"], "from": 10, "to": 1}, http.StatusBadRequest)
	if got := list(`{"field":"location","op":"in","value":[` + itoa(box["id"].(float64)) + `]}`); len(got) != 2 {
		t.Errorf("in the box: %v", got)
	}
	locs := alice.mustDo(http.MethodGet, "/api/v1/locations", nil, http.StatusOK).list
	if len(locs) != 1 || locs[0].(map[string]any)["documentCount"] != float64(2) {
		t.Errorf("locations: %v", locs)
	}
	people := carol.mustDo(http.MethodGet, "/api/v1/persons", nil, http.StatusOK).list
	if len(people) != 2 || people[0].(map[string]any)["name"] != "爸爸" || people[0].(map[string]any)["documentCount"] != float64(1) {
		t.Errorf("persons: %v", people)
	}

	// bulk: everyone's papers are 乐乐's; then clear the location
	alice.mustDo(http.MethodPost, "/api/v1/documents/bulk", map[string]any{"ids": []any{passport, check}, "edit": map[string]any{"personId": kid, "locationId": 0}}, http.StatusOK)
	if got := list(`{"field":"person","op":"in","value":[` + itoa(kid) + `]}`); len(got) != 2 {
		t.Errorf("after bulk: %v", got)
	}
	if got := list(`{"field":"location","op":"not_null"}`); len(got) != 0 {
		t.Errorf("locations after bulk: %v", got)
	}
	facets := alice.mustDo(http.MethodGet, "/api/v1/documents/facets?groups=person,location", nil, http.StatusOK).raw
	if !strings.Contains(facets, `"person"`) || !strings.Contains(facets, `"location"`) {
		t.Errorf("facets: %s", facets)
	}

	// merging and deleting a person leaves the documents
	alice.mustDo(http.MethodPost, "/api/v1/directory/persons/merge", map[string]any{"keep": kid, "merge": []any{dad}}, http.StatusOK)
	alice.mustDo(http.MethodDelete, "/api/v1/persons/"+itoa(kid), nil, http.StatusNoContent)
	alice.mustDo(http.MethodDelete, "/api/v1/locations/"+itoa(box["id"].(float64)), nil, http.StatusNoContent)
	if got := list(`{"field":"person","op":"is_null"}`); len(got) != 2 {
		t.Errorf("after deleting the person: %v", got)
	}
	hist := alice.mustDo(http.MethodGet, "/api/v1/audit", nil, http.StatusOK).raw
	for _, want := range []string{"新建了家人“乐乐”", "新建了存放位置“文件盒 A”", "放到了“文件盒 A”", "删除了家人“乐乐”"} {
		if !strings.Contains(hist, want) {
			t.Errorf("audit lacks %q", want)
		}
	}
}
