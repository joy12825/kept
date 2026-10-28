package api

import (
	"net/http"
	"strings"
	"testing"
)

// v1.7.13: a file uploaded under the name of a document in the same drawer
// can become its new version.
func TestSameNameUploads(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	drawer := alice.mustDo(http.MethodPost, "/api/v1/drawers", map[string]any{"name": "合同"}, http.StatusCreated).body["drawer"].(map[string]any)["id"].(float64)
	in := map[string]string{"drawerId": itoa(drawer)}
	doc := alice.waitJob(alice.upload("租房合同.txt", "第一稿", in).body["jobId"].(float64))["documentId"].(float64)
	versions := func(doc float64) int {
		return len(alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(doc)+"/versions", nil, http.StatusOK).list)
	}
	check := func(body map[string]any) map[string]any {
		return alice.mustDo(http.MethodPost, "/api/v1/uploads/same-name", body, http.StatusOK).body
	}

	r := check(map[string]any{"drawerId": drawer, "names": []string{"租房合同.txt", "别的.txt", "租房合同.txt"}})
	if m := r["matches"].([]any); r["auto"] != false || len(m) != 1 || m[0].(map[string]any)["documentId"] != doc || m[0].(map[string]any)["title"] != "租房合同" {
		t.Fatalf("check: %v", r)
	}
	// Outside the drawer the name is not taken.
	if r := check(map[string]any{"names": []string{"租房合同.txt"}}); len(r["matches"].([]any)) != 0 {
		t.Fatalf("no drawer: %v", r)
	}
	alice.mustDo(http.MethodPost, "/api/v1/uploads/same-name", map[string]any{"drawerId": 99999, "names": []string{"a"}}, http.StatusNotFound)

	// Asked: as a new version.
	up := alice.upload("租房合同.txt", "第二稿", map[string]string{"drawerId": itoa(drawer), "onConflict": "version"})
	if up.code != http.StatusAccepted || up.body["versionOf"] != doc {
		t.Fatalf("as version: %d %s", up.code, up.raw)
	}
	alice.waitJob(up.body["jobId"].(float64))
	if n := versions(doc); n != 2 {
		t.Fatalf("versions: %d", n)
	}
	// The same content again is refused as before.
	if r := alice.upload("租房合同.txt", "第二稿", map[string]string{"drawerId": itoa(drawer), "onConflict": "version"}); r.code != http.StatusConflict {
		t.Fatalf("same content: %d %s", r.code, r.raw)
	}
	// Not asked and the drawer not set: a document of its own.
	up = alice.upload("租房合同.txt", "另一份", in)
	if up.code != http.StatusAccepted || up.body["versionOf"] != nil {
		t.Fatalf("default: %d %s", up.code, up.raw)
	}
	other := alice.waitJob(up.body["jobId"].(float64))["documentId"].(float64)
	if other == doc {
		t.Fatal("default made a version")
	}
	if r := alice.upload("租房合同.txt", "x", map[string]string{"onConflict": "replace"}); r.code != http.StatusBadRequest {
		t.Fatalf("bad onConflict: %d %s", r.code, r.raw)
	}

	// A drawer set to do it without asking; the latest same-name document wins.
	d := alice.mustDo(http.MethodPatch, "/api/v1/drawers/"+itoa(drawer), map[string]any{"sameNameVersions": true}, http.StatusOK).body["drawer"].(map[string]any)
	if d["sameNameVersions"] != true {
		t.Fatalf("drawer: %v", d)
	}
	if r := check(map[string]any{"drawerId": drawer, "names": []string{"租房合同.txt"}}); r["auto"] != true || r["matches"].([]any)[0].(map[string]any)["documentId"] != other {
		t.Fatalf("auto check: %v", r)
	}
	up = alice.upload("租房合同.txt", "另一份第二稿", in)
	if up.code != http.StatusAccepted || up.body["versionOf"] != other {
		t.Fatalf("auto: %d %s", up.code, up.raw)
	}
	alice.waitJob(up.body["jobId"].(float64))
	// …unless the upload says new.
	up = alice.upload("租房合同.txt", "第三份", map[string]string{"drawerId": itoa(drawer), "onConflict": "new"})
	if up.code != http.StatusAccepted || up.body["versionOf"] != nil {
		t.Fatalf("new: %d %s", up.code, up.raw)
	}
	third := alice.waitJob(up.body["jobId"].(float64))["documentId"].(float64)

	// Documents in no drawer match each other.
	loose := alice.waitJob(alice.upload("收据.txt", "收据一", nil).body["jobId"].(float64))["documentId"].(float64)
	if up := alice.upload("收据.txt", "收据二", map[string]string{"onConflict": "version"}); up.code != http.StatusAccepted || up.body["versionOf"] != loose {
		t.Fatalf("no drawer: %d %s", up.code, up.raw)
	} else {
		alice.waitJob(up.body["jobId"].(float64))
	}

	// At most one earlier version: the oldest go when a new one comes.
	if u := alice.mustDo(http.MethodGet, "/api/v1/storage", nil, http.StatusOK).body; u["keepVersions"] != float64(0) {
		t.Fatalf("keep default: %v", u)
	}
	alice.mustDo(http.MethodPut, "/api/v1/storage/versions", map[string]any{"keep": 1001}, http.StatusBadRequest)
	alice.mustDo(http.MethodPut, "/api/v1/storage/versions", map[string]any{"keep": 1}, http.StatusNoContent)
	if u := alice.mustDo(http.MethodGet, "/api/v1/storage", nil, http.StatusOK).body; u["keepVersions"] != float64(1) {
		t.Fatalf("keep: %v", u)
	}
	for _, c := range []string{"三稿", "四稿"} {
		up := alice.upload("租房合同.txt", c, map[string]string{"drawerId": itoa(drawer), "onConflict": "version"})
		if up.code != http.StatusAccepted || up.body["versionOf"] != third {
			t.Fatalf("version %s: %d %s", c, up.code, up.raw)
		}
		alice.waitJob(up.body["jobId"].(float64))
	}
	vs := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(third)+"/versions", nil, http.StatusOK).list
	if len(vs) != 2 || vs[0].(map[string]any)["number"] != float64(3) || vs[1].(map[string]any)["number"] != float64(2) {
		t.Fatalf("pruned: %v", vs)
	}
	if f := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(third)+"/versions/"+itoa(vs[1].(map[string]any)["id"].(float64))+"/file", nil, http.StatusOK); f.raw != "三稿" {
		t.Fatalf("kept version: %q", f.raw)
	}
	// Other documents keep theirs until they get a new version.
	if n := versions(doc); n != 2 {
		t.Fatalf("untouched: %d", n)
	}
	acts := alice.mustDo(http.MethodGet, "/api/v1/audit?limit=50", nil, http.StatusOK).raw
	if !strings.Contains(acts, "最多保留 1 个旧版本") {
		t.Fatalf("audit: %s", acts)
	}
}

func TestWatchVersionsSetting(t *testing.T) {
	e := newEnv(t, "auto", nil)
	admin := e.setupAdmin()
	alice := e.user("alice")
	if w := admin.mustDo(http.MethodGet, "/api/v1/admin/system", nil, http.StatusOK).body["watch"].(map[string]any); w["versions"] != nil {
		t.Fatalf("default: %v", w)
	}
	alice.mustDo(http.MethodPut, "/api/v1/admin/watch", map[string]any{"versions": true}, http.StatusForbidden)
	admin.mustDo(http.MethodPut, "/api/v1/admin/watch", map[string]any{"versions": true}, http.StatusNoContent)
	if w := admin.mustDo(http.MethodGet, "/api/v1/admin/system", nil, http.StatusOK).body["watch"].(map[string]any); w["versions"] != true {
		t.Fatalf("on: %v", w)
	}
	admin.mustDo(http.MethodPut, "/api/v1/admin/watch", map[string]any{"versions": false}, http.StatusNoContent)
	if w := admin.mustDo(http.MethodGet, "/api/v1/admin/system", nil, http.StatusOK).body["watch"].(map[string]any); w["versions"] != nil {
		t.Fatalf("off: %v", w)
	}
}
