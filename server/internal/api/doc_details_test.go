package api

import (
	"net/http"
	"strings"
	"testing"
)

// v1.3.7: the recognised text can be corrected, and the file info reads
// what the original carries.
func TestEditContentAndFileInfo(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	id := itoa(alice.waitJob(alice.upload("电费.txt", "国家电网 电费 326.4O 元", nil).body["jobId"].(float64))["documentId"].(float64))
	d := alice.mustDo(http.MethodGet, "/api/v1/documents/"+id, nil, http.StatusOK).body
	if d["contentEditedAt"] != nil {
		t.Fatalf("not edited yet: %v", d["contentEditedAt"])
	}
	d = alice.mustDo(http.MethodPatch, "/api/v1/documents/"+id, map[string]any{"content": "国家电网 电费 326.40 元", "modifiedAt": d["modifiedAt"]}, http.StatusOK).body
	if d["content"] != "国家电网 电费 326.40 元" || d["contentEditedAt"] == nil {
		t.Fatalf("after editing: %v / %v", d["content"], d["contentEditedAt"])
	}
	h := alice.mustDo(http.MethodGet, "/api/v1/documents/"+id+"/history", nil, http.StatusOK).body
	if !strings.Contains(h["items"].([]any)[0].(map[string]any)["summary"].(string), "识别文字") {
		t.Errorf("history: %v", h)
	}

	info := alice.mustDo(http.MethodGet, "/api/v1/documents/"+id+"/file-info", nil, http.StatusOK).body
	if info["mimeType"] != "text/plain" || len(info["props"].([]any)) != 0 || info["hasLocation"] != false {
		t.Errorf("file info: %v", info)
	}
	e.user("bob").mustDo(http.MethodGet, "/api/v1/documents/"+id+"/file-info", nil, http.StatusNotFound)
}

// Recognising again replaces a corrected text and clears the mark.
func TestReprocessClearsEditedContent(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	id := itoa(alice.waitJob(alice.upload("便条.txt", "买牛奶", nil).body["jobId"].(float64))["documentId"].(float64))
	alice.mustDo(http.MethodPatch, "/api/v1/documents/"+id, map[string]any{"content": "买酸奶"}, http.StatusOK)
	job := alice.mustDo(http.MethodPost, "/api/v1/documents/"+id+"/reprocess", nil, http.StatusAccepted).body["jobId"].(float64)
	alice.waitJob(job)
	d := alice.mustDo(http.MethodGet, "/api/v1/documents/"+id, nil, http.StatusOK).body
	if d["content"] != "买牛奶" || d["contentEditedAt"] != nil {
		t.Fatalf("after reprocessing: %v / %v", d["content"], d["contentEditedAt"])
	}
}
