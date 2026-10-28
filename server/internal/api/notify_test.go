package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestEmailDocumentsAndNotificationPrefs(t *testing.T) {
	e := newEnv(t, "auto", nil)
	admin := e.setupAdmin()
	alice := e.user("alice")

	me := alice.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK).body
	if me["features"].(map[string]any)["smtp"] != true {
		t.Fatalf("features: %v", me["features"])
	}
	r := alice.upload("电费账单.txt", "国家电网 本期电费 128.50 元", nil)
	doc := alice.waitJob(r.body["jobId"].(float64))["documentId"].(float64)
	adminDoc := admin.waitJob(admin.upload("别人的.txt", "别人的文档", nil).body["jobId"].(float64))["documentId"].(float64)

	if r := alice.do(http.MethodPost, "/api/v1/documents/email", map[string]any{"ids": []float64{doc}, "to": []string{"bob@"}}); r.code != http.StatusBadRequest {
		t.Fatalf("bad address: %d %s", r.code, r.raw)
	}
	if r := alice.do(http.MethodPost, "/api/v1/documents/email", map[string]any{"ids": []float64{adminDoc}, "to": []string{"bob@example.com"}}); r.code != http.StatusNotFound {
		t.Fatalf("another workspace's document: %d %s", r.code, r.raw)
	}
	acc := alice.mustDo(http.MethodPost, "/api/v1/documents/email", map[string]any{
		"ids": []float64{doc}, "to": []string{"bob@example.com"}, "message": "请看一下", "file": "original",
	}, http.StatusAccepted)
	task := alice.waitTask(acc.body["jobId"].(float64))
	if task["status"] != "succeeded" || task["label"] != "发送邮件" || task["summary"] != "发给 bob@example.com，1 份附件" {
		t.Fatalf("task: %v", task)
	}
	msgs := e.smtp.Messages()
	if len(msgs) != 1 || msgs[0].To[0] != "bob@example.com" || !strings.Contains(msgs[0].Data, "Reply-To: <alice@example.com>") {
		t.Fatalf("messages: %+v", msgs)
	}
	hist := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(doc)+"/history", nil, http.StatusOK)
	if !strings.Contains(hist.raw, "通过邮件把“电费账单”发给 bob@example.com") {
		t.Fatalf("history: %s", hist.raw)
	}

	p := alice.mustDo(http.MethodGet, "/api/v1/me/notifications", nil, http.StatusOK).body
	if p["emailAlerts"] != false || p["email"] != "alice@example.com" || p["smtp"] != true {
		t.Fatalf("prefs: %v", p)
	}
	p = alice.mustDo(http.MethodPut, "/api/v1/me/notifications", map[string]any{"emailAlerts": true}, http.StatusOK).body
	if p["emailAlerts"] != true {
		t.Fatalf("prefs after: %v", p)
	}

	alice.mustDo(http.MethodPost, "/api/v1/admin/smtp/test", map[string]any{"to": "alice@example.com"}, http.StatusForbidden)
	admin.mustDo(http.MethodPost, "/api/v1/admin/smtp/test", map[string]any{"to": "admin@example.com"}, http.StatusNoContent)
	if msgs := e.smtp.Messages(); len(msgs) != 2 || msgs[1].To[0] != "admin@example.com" {
		t.Fatalf("test message: %+v", msgs)
	}
	sys := admin.mustDo(http.MethodGet, "/api/v1/admin/system", nil, http.StatusOK)
	smtp := sys.body["smtp"].(map[string]any)
	if smtp["configured"] != true || smtp["host"] != "127.0.0.1" || smtp["security"] != "none" || strings.Contains(sys.raw, "password") {
		t.Fatalf("system smtp: %v", smtp)
	}
}
