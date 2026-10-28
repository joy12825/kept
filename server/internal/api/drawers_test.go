package api

import (
	"net/http"
	"testing"
	"time"
)

func TestDrawersFlow(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	tpl := alice.mustDo(http.MethodGet, "/api/v1/drawer-templates", nil, http.StatusOK).body
	if len(tpl["scenes"].([]any)) != 3 || len(tpl["templates"].([]any)) < 10 {
		t.Fatalf("templates = %v", tpl)
	}
	created := alice.mustDo(http.MethodPost, "/api/v1/drawers/from-templates", map[string]any{"keys": []string{"house", "car"}}, http.StatusCreated).body
	ds := created["drawers"].([]any)
	if len(ds) != 2 {
		t.Fatalf("created = %v", created)
	}
	house := ds[0].(map[string]any)["id"].(float64)
	car := ds[1].(map[string]any)["id"].(float64)

	// A matching upload goes into the drawer, with the basis recorded.
	r := alice.upload("电费.txt", "国家电网 电费通知单 2024年3月5日", nil)
	doc := alice.waitJob(r.body["jobId"].(float64))["documentId"].(float64)
	d := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(doc), nil, http.StatusOK).body
	if d["drawerId"] != house || d["drawerManual"] != false || d["fieldSources"].(map[string]any)["drawer"].(map[string]any)["basis"] != "收纳规则“房子”" {
		t.Fatalf("document = %v", d)
	}
	// Put by hand into 车: the details say where the rules would put it.
	d = alice.mustDo(http.MethodPatch, "/api/v1/documents/"+itoa(doc), map[string]any{"drawerId": car}, http.StatusOK).body
	if d["drawerId"] != car || d["drawerManual"] != true || d["ruleDrawerId"] != house {
		t.Fatalf("by hand = %v", d)
	}
	// Previewing 房子 without its rule: the manual document is not affected.
	p := alice.mustDo(http.MethodPost, "/api/v1/drawers/preview", map[string]any{"drawerId": house, "clearRule": true}, http.StatusOK).body
	if p["total"] != float64(0) || p["moves"] != float64(0) {
		t.Errorf("preview = %v", p)
	}
	list := alice.mustDo(http.MethodGet, "/api/v1/drawers", nil, http.StatusOK).body
	items := list["items"].([]any)
	if items[1].(map[string]any)["documentCount"] != float64(1) || list["unfiled"] != float64(0) {
		t.Errorf("list = %v", list)
	}
	if items[0].(map[string]any)["primary"].(map[string]any)["fact"].(map[string]any)["label"] != "合同到期" {
		t.Errorf("primary = %v", items[0])
	}
	// Reorder: car first; a job is started.
	if r := alice.mustDo(http.MethodPut, "/api/v1/drawers/order", map[string]any{"ids": []float64{car, house}}, http.StatusOK); r.body["jobId"] == nil {
		t.Errorf("reorder = %v", r.body)
	}
	// Drawer page: facts computed.
	dd := alice.mustDo(http.MethodGet, "/api/v1/drawers/"+itoa(car), nil, http.StatusOK).body
	if dd["manualCount"] != float64(1) || len(dd["facts"].([]any)) != 3 || dd["firstYear"] != float64(2024) {
		t.Errorf("drawer = %v", dd)
	}
	// Fact with a bad config.
	if r := alice.do(http.MethodPost, "/api/v1/drawers/"+itoa(car)+"/facts", map[string]any{"kind": "sum", "label": "x", "config": map[string]any{}}); r.code != http.StatusBadRequest {
		t.Errorf("sum without a field: %d", r.code)
	}
	// Facets and filter by drawer.
	f := alice.mustDo(http.MethodGet, "/api/v1/documents/facets?groups=drawer", nil, http.StatusOK).body
	if len(f["drawer"].([]any)) != 1 {
		t.Errorf("facets = %v", f)
	}
	// Yearbook and alerts.
	y := alice.mustDo(http.MethodGet, "/api/v1/yearbook/2024", nil, http.StatusOK).body
	if y["documents"] != float64(1) || y["months"].([]any)[2] != float64(1) {
		t.Errorf("yearbook = %v", y)
	}
	if ys := alice.mustDo(http.MethodGet, "/api/v1/yearbook", nil, http.StatusOK).list; ys[0] != float64(time.Now().Year()) {
		t.Errorf("years = %v", ys)
	}
	alice.mustDo(http.MethodGet, "/api/v1/alerts", nil, http.StatusOK)
	// Another account cannot put its document into alice's drawer.
	carol := e.user("carol")
	mine := uploadedDocument(carol, "mine.txt")
	if r := carol.do(http.MethodPatch, "/api/v1/documents/"+itoa(mine), map[string]any{"drawerId": house}); r.code != http.StatusNotFound {
		t.Errorf("carol into alice's drawer: %d %s", r.code, r.raw)
	}
	// Deleting the drawer frees the document.
	alice.mustDo(http.MethodDelete, "/api/v1/drawers/"+itoa(car), nil, http.StatusOK)
	d = alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(doc), nil, http.StatusOK).body
	if d["drawerManual"] != false {
		t.Errorf("after deleting the drawer = %v", d)
	}
}
