package api

import (
	"net/http"
	"testing"
)

func TestMilestoneRulesAPI(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	r := alice.mustDo(http.MethodGet, "/api/v1/yearbook/rules", nil, http.StatusOK).body
	if r["default"] != true {
		t.Fatalf("defaults: %v", r)
	}
	tag := alice.mustDo(http.MethodPost, "/api/v1/tags", map[string]any{"name": "大件"}, http.StatusCreated).body["id"].(float64)
	alice.mustDo(http.MethodPut, "/api/v1/yearbook/rules", map[string]any{"rules": []any{map[string]any{"verb": "", "filter": map[string]any{}}}}, http.StatusBadRequest)
	r = alice.mustDo(http.MethodPut, "/api/v1/yearbook/rules", map[string]any{"rules": []any{
		map[string]any{"verb": "购入", "filter": map[string]any{"field": "tag", "op": "has_any", "value": []float64{tag}}},
	}}, http.StatusOK).body
	if r["default"] != false || len(r["rules"].([]any)) != 1 {
		t.Fatalf("custom: %v", r)
	}
	r = alice.mustDo(http.MethodPut, "/api/v1/yearbook/rules", map[string]any{"reset": true}, http.StatusOK).body
	if r["default"] != true {
		t.Fatalf("reset: %v", r)
	}
	y := alice.mustDo(http.MethodGet, "/api/v1/yearbook/2024", nil, http.StatusOK).body
	if y["previous"].(map[string]any)["year"] != float64(2023) || len(y["history"].([]any)) != 5 || y["storage"] == nil {
		t.Fatalf("yearbook: %v", y)
	}
}
