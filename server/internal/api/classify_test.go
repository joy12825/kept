package api

import (
	"net/http"
	"testing"
	"time"
)

func TestClassificationOnUploadAndReapply(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	rule := func(alg, match string) map[string]any {
		return map[string]any{"algorithm": alg, "match": match, "caseInsensitive": true}
	}
	corrID := alice.mustDo(http.MethodPost, "/api/v1/correspondents", map[string]any{"name": "国家电网", "rule": rule("any", "国家电网 国网")}, http.StatusCreated).body["id"].(float64)
	typeID := alice.mustDo(http.MethodPost, "/api/v1/document-types", map[string]any{"name": "账单", "rule": rule("any", "电费")}, http.StatusCreated).body["id"].(float64)

	r := alice.upload("三月.txt", "国家电网\n开票日期：2024年3月5日\n本期电费 128.50 元", nil)
	docID := alice.waitJob(r.body["jobId"].(float64))["documentId"].(float64)
	d := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(docID), nil, http.StatusOK).body
	fs := d["fieldSources"].(map[string]any)
	src := func(field string) map[string]any { m, _ := fs[field].(map[string]any); return m }
	if d["correspondentId"] != corrID || d["documentTypeId"] != typeID {
		t.Fatalf("not classified: %v", d)
	}
	if src("correspondent")["source"] != "rule" || src("correspondent")["basis"] != "关键词“国家电网”" || src("type")["basis"] != "关键词“电费”" ||
		src("date")["source"] != "text" || src("date")["basis"] != "开票日期：2024年3月5日" {
		t.Errorf("fieldSources = %v", fs)
	}

	// A person clears the correspondent; re-applying (even overwriting) leaves it.
	d = alice.mustDo(http.MethodPatch, "/api/v1/documents/"+itoa(docID), map[string]any{"correspondentId": 0}, http.StatusOK).body
	if d["fieldSources"].(map[string]any)["correspondent"].(map[string]any)["source"] != "user" {
		t.Errorf("after the edit: %v", d["fieldSources"])
	}
	b := alice.mustDo(http.MethodPost, "/api/v1/documents/bulk", map[string]any{"ids": []float64{docID}, "classify": map[string]any{"mode": "overwrite"}}, http.StatusOK).body
	if b["count"] != float64(1) || len(b["jobIds"].([]any)) != 1 {
		t.Fatalf("bulk classify = %v", b)
	}
	waitApplied(t, alice)
	d = alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(docID), nil, http.StatusOK).body
	if _, has := d["correspondentId"]; has {
		t.Errorf("re-applying overwrote a user's choice: %v", d["correspondentId"])
	}
	// Another account's IDs select nothing.
	if b := e.user("mallory").mustDo(http.MethodPost, "/api/v1/documents/bulk", map[string]any{"ids": []float64{docID}, "classify": map[string]any{"mode": "fill"}}, http.StatusOK).body; b["count"] != float64(0) {
		t.Errorf("mallory classified %v documents", b["count"])
	}

	st := alice.mustDo(http.MethodGet, "/api/v1/classifier", nil, http.StatusOK).body
	if st["minDocuments"] != float64(10) || st["applying"].(map[string]any)["changed"] != float64(0) {
		t.Errorf("status = %v", st)
	}
	if r := alice.do(http.MethodPost, "/api/v1/classifier/train", nil); r.code != http.StatusAccepted || r.body["jobId"] == nil {
		t.Errorf("train: %d %v", r.code, r.body)
	}
}

func waitApplied(t *testing.T, c *client) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		st := c.mustDo(http.MethodGet, "/api/v1/classifier", nil, http.StatusOK).body
		if a, ok := st["applying"].(map[string]any); ok && (a["status"] == "succeeded" || a["status"] == "failed") {
			if a["status"] == "failed" {
				t.Fatalf("classify job failed: %v", a)
			}
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("the classify job did not finish")
}

func TestMatchRuleTryOut(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	try := func(alg, match, text string) resp {
		return alice.do(http.MethodPost, "/api/v1/classifier/test", map[string]any{
			"rule": map[string]any{"algorithm": alg, "match": match, "caseInsensitive": true}, "text": text})
	}
	if r := try("any", "国家电网 供电局", "国网北京 供电局 电费"); r.code != http.StatusOK || r.body["matched"] != true || r.body["basis"] != "关键词“供电局”" {
		t.Errorf("any: %d %v", r.code, r.body)
	}
	if r := try("all", "国家电网 供电局", "供电局"); r.body["matched"] != false || r.body["basis"] != nil {
		t.Errorf("all: %v", r.body)
	}
	if r := try("regex", "[", "x"); r.code != http.StatusBadRequest {
		t.Errorf("bad regex: %d", r.code)
	}
	if r := try("auto", "", "x"); r.code != http.StatusOK || r.body["matched"] != false {
		t.Errorf("auto: %d %v", r.code, r.body)
	}
}
