package api

import (
	"net/http"
	"testing"
)

func names(list []any) []string {
	var out []string
	for _, it := range list {
		out = append(out, it.(map[string]any)["name"].(string))
	}
	return out
}

func TestTaxonomyEndpoints(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")

	for _, path := range []string{"/api/v1/correspondents", "/api/v1/document-types"} {
		r := alice.mustDo(http.MethodPost, path, map[string]any{"name": "国家电网", "rule": map[string]any{"algorithm": "any", "match": "国家电网 国网", "caseInsensitive": true}}, http.StatusCreated)
		if r.body["initial"] != "G" || r.body["sortKey"] != "guojiadianwang" || r.body["rule"].(map[string]any)["algorithm"] != "any" {
			t.Errorf("%s created = %v", path, r.body)
		}
		id := itoa(r.body["id"].(float64))
		alice.mustDo(http.MethodPost, path, map[string]any{"name": "Apple"}, http.StatusCreated)
		if r := alice.do(http.MethodPost, path, map[string]any{"name": "apple"}); r.code != http.StatusConflict {
			t.Errorf("%s duplicate: %d %v", path, r.code, r.body)
		}
		if r := alice.do(http.MethodPost, path, map[string]any{"name": "x", "rule": map[string]any{"algorithm": "regex", "match": "a(", "caseInsensitive": false}}); r.code != http.StatusBadRequest {
			t.Errorf("%s bad regex: %d %v", path, r.code, r.body)
		}
		if got := names(alice.mustDo(http.MethodGet, path, nil, http.StatusOK).list); len(got) != 2 || got[0] != "Apple" {
			t.Errorf("%s list = %v", path, got)
		}
		r = alice.mustDo(http.MethodPatch, path+"/"+id, map[string]any{"name": "国网北京"}, http.StatusOK)
		if r.body["name"] != "国网北京" || r.body["rule"].(map[string]any)["match"] != "国家电网 国网" {
			t.Errorf("%s renamed = %v", path, r.body)
		}
		alice.mustDo(http.MethodDelete, path+"/"+id, nil, http.StatusNoContent)
		alice.mustDo(http.MethodDelete, path+"/"+id, nil, http.StatusNotFound)
	}

	// Tags: hierarchy and moving children up on delete.
	root := alice.mustDo(http.MethodPost, "/api/v1/tags", map[string]any{"name": "财务", "color": "#3A6EA5"}, http.StatusCreated).body
	rootID := root["id"].(float64)
	if _, has := root["parentId"]; has || root["color"] != "#3A6EA5" {
		t.Errorf("root tag = %v", root)
	}
	mid := alice.mustDo(http.MethodPost, "/api/v1/tags", map[string]any{"name": "发票", "parentId": rootID}, http.StatusCreated).body
	leaf := alice.mustDo(http.MethodPost, "/api/v1/tags", map[string]any{"name": "专票", "parentId": mid["id"]}, http.StatusCreated).body
	if r := alice.do(http.MethodPatch, "/api/v1/tags/"+itoa(rootID), map[string]any{"parentId": leaf["id"]}); r.code != http.StatusBadRequest {
		t.Errorf("moving a tag under its descendant: %d %v", r.code, r.body)
	}
	alice.mustDo(http.MethodDelete, "/api/v1/tags/"+itoa(mid["id"].(float64)), nil, http.StatusNoContent)
	for _, tg := range alice.mustDo(http.MethodGet, "/api/v1/tags", nil, http.StatusOK).list {
		if tg := tg.(map[string]any); tg["name"] == "专票" && tg["parentId"] != rootID {
			t.Errorf("专票 parent = %v, want 财务 after its parent was deleted", tg["parentId"])
		}
	}
	r := alice.mustDo(http.MethodPatch, "/api/v1/tags/"+itoa(leaf["id"].(float64)), map[string]any{"parentId": 0}, http.StatusOK)
	if _, has := r.body["parentId"]; has {
		t.Errorf("moved to top level, still has parentId: %v", r.body)
	}

	// Custom fields: the personal workspace already has the two built-ins.
	fields := alice.mustDo(http.MethodGet, "/api/v1/custom-fields", nil, http.StatusOK).list
	if len(fields) != 2 {
		t.Fatalf("built-in fields = %v", fields)
	}
	var amountID float64
	for _, f := range fields {
		if f := f.(map[string]any); f["key"] == "amount" {
			amountID = f["id"].(float64)
			if f["builtIn"] != true || f["dataType"] != "monetary" || f["extra"].(map[string]any)["currency"] != "CNY" {
				t.Errorf("amount = %v", f)
			}
		}
	}
	if r := alice.do(http.MethodDelete, "/api/v1/custom-fields/"+itoa(amountID), nil); r.code != http.StatusConflict {
		t.Errorf("deleting a built-in field: %d %v", r.code, r.body)
	}
	sel := alice.mustDo(http.MethodPost, "/api/v1/custom-fields", map[string]any{
		"name": "报销状态", "dataType": "select", "extra": map[string]any{"options": []any{map[string]any{"label": "未报销"}, map[string]any{"label": "已报销"}}},
	}, http.StatusCreated).body
	opts := sel["extra"].(map[string]any)["options"].([]any)
	if len(opts) != 2 || opts[0].(map[string]any)["id"] == "" {
		t.Errorf("select options = %v", opts)
	}
}

func TestTaxonomyFollowsTheCurrentWorkspace(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	homeID := alice.mustDo(http.MethodPost, "/api/v1/workspaces", map[string]any{"name": "王家", "kind": "family"}, http.StatusCreated).body["id"].(float64)
	alice.inSpace(homeID).mustDo(http.MethodPost, "/api/v1/correspondents", map[string]any{"name": "链家"}, http.StatusCreated)
	if got := alice.mustDo(http.MethodGet, "/api/v1/correspondents", nil, http.StatusOK).list; len(got) != 0 {
		t.Errorf("the personal workspace sees the family's correspondents: %v", got)
	}
	if got := names(alice.inSpace(homeID).mustDo(http.MethodGet, "/api/v1/correspondents", nil, http.StatusOK).list); len(got) != 1 || got[0] != "链家" {
		t.Errorf("family correspondents = %v", got)
	}
	// The family workspace got its own built-in fields.
	if got := alice.inSpace(homeID).mustDo(http.MethodGet, "/api/v1/custom-fields", nil, http.StatusOK).list; len(got) != 2 {
		t.Errorf("family custom fields = %v", got)
	}
}
