package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// v1.6.5: a document type's retention, and the documents past it.
func TestRetention(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, _, carol, _ := home(t, e)
	bill := alice.mustDo(http.MethodPost, "/api/v1/document-types", map[string]any{"name": "快递单"}, http.StatusCreated).body["id"].(float64)
	types := "/api/v1/document-types/" + itoa(bill)
	carol.mustDo(http.MethodPatch, types, map[string]any{"retentionMonths": 6}, http.StatusForbidden)
	alice.mustDo(http.MethodPatch, types, map[string]any{"retentionMonths": 601}, http.StatusBadRequest)
	if it := alice.mustDo(http.MethodPatch, types, map[string]any{"retentionMonths": 6}, http.StatusOK).body; it["retentionMonths"] != float64(6) {
		t.Errorf("patched: %v", it)
	}
	if l := alice.mustDo(http.MethodGet, "/api/v1/document-types", nil, http.StatusOK).list; l[0].(map[string]any)["retentionMonths"] != float64(6) {
		t.Errorf("list: %v", l)
	}
	old := alice.waitJob(alice.upload("快递单.txt", "顺丰 快递", map[string]string{"documentTypeId": itoa(bill), "createdDate": "2020-01-05"}).body["jobId"].(float64))["documentId"].(float64)
	fresh := alice.waitJob(alice.upload("新快递单.txt", "京东 快递", map[string]string{"documentTypeId": itoa(bill)}).body["jobId"].(float64))["documentId"].(float64)
	expired := "/api/v1/documents?filter=" + url.QueryEscape(`{"field":"retention","op":"expired","value":true}`)
	if n := alice.mustDo(http.MethodGet, expired, nil, http.StatusOK).body["total"]; n != float64(0) {
		t.Errorf("documents still in the inbox are not suggested: %v", n)
	}
	alice.mustDo(http.MethodPost, "/api/v1/documents/bulk", map[string]any{"ids": []any{old, fresh}, "edit": map[string]any{"inbox": false}}, http.StatusOK)
	items := alice.mustDo(http.MethodGet, expired, nil, http.StatusOK).body["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != old {
		t.Fatalf("expired: %v", items)
	}
	// moving them to the trash is the ordinary bulk delete
	alice.mustDo(http.MethodPost, "/api/v1/documents/bulk", map[string]any{"filter": map[string]any{"field": "retention", "op": "expired", "value": true}, "delete": true}, http.StatusOK)
	if n := alice.mustDo(http.MethodGet, expired, nil, http.StatusOK).body["total"]; n != float64(0) {
		t.Errorf("after the clean-up: %v", n)
	}
	alice.mustDo(http.MethodPatch, types, map[string]any{"retentionMonths": 0}, http.StatusOK)
	if l := alice.mustDo(http.MethodGet, "/api/v1/document-types", nil, http.StatusOK).raw; strings.Contains(l, "retentionMonths") {
		t.Errorf("cleared: %s", l)
	}
	if hist := alice.mustDo(http.MethodGet, "/api/v1/audit", nil, http.StatusOK).raw; !strings.Contains(hist, "保留期限") {
		t.Error("audit lacks the retention change")
	}
}
