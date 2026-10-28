package api

import (
	"net/http"
	"testing"
)

// v1.6.5: saved views keep the order they were dragged into; until then
// they are in pinyin order, and once ordered a new one goes last.
func TestSavedViewOrder(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, _, carol, _ := home(t, e)
	add := func(name string) float64 {
		return alice.mustDo(http.MethodPost, "/api/v1/saved-views", map[string]any{"name": name, "filter": map[string]any{"text": name}}, http.StatusCreated).body["id"].(float64)
	}
	names := func() []string {
		var out []string
		for _, v := range alice.mustDo(http.MethodGet, "/api/v1/saved-views", nil, http.StatusOK).list {
			out = append(out, v.(map[string]any)["name"].(string))
		}
		return out
	}
	bill, policy, power := add("账单"), add("保单"), add("电费")
	if got := names(); len(got) != 3 || got[0] != "保单" || got[1] != "电费" || got[2] != "账单" {
		t.Fatalf("pinyin order: %v", got)
	}
	if got := add("合同"); got == 0 || names()[2] != "合同" {
		t.Errorf("before ordering a new view keeps pinyin order: %v", names())
	}
	contract := float64(0)
	for _, v := range alice.mustDo(http.MethodGet, "/api/v1/saved-views", nil, http.StatusOK).list {
		if v.(map[string]any)["name"] == "合同" {
			contract = v.(map[string]any)["id"].(float64)
		}
	}
	carol.mustDo(http.MethodPut, "/api/v1/saved-views/order", map[string]any{"ids": []any{bill, policy, power, contract}}, http.StatusForbidden)
	alice.mustDo(http.MethodPut, "/api/v1/saved-views/order", map[string]any{"ids": []any{bill, policy}}, http.StatusBadRequest)
	alice.mustDo(http.MethodPut, "/api/v1/saved-views/order", map[string]any{"ids": []any{bill, bill, policy, power}}, http.StatusBadRequest)
	alice.mustDo(http.MethodPut, "/api/v1/saved-views/order", map[string]any{"ids": []any{bill, policy, power, contract}}, http.StatusNoContent)
	add("房贷")
	if got := names(); len(got) != 5 || got[0] != "账单" || got[1] != "保单" || got[2] != "电费" || got[3] != "合同" || got[4] != "房贷" {
		t.Errorf("after ordering: %v", got)
	}
}
