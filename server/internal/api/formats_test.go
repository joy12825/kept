package api

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"kept/server/internal/ofdtest"
	"kept/server/internal/watermark"
)

func zipOf(files map[string]string) []byte {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, body := range files {
		w, _ := z.Create(name)
		io.WriteString(w, body)
	}
	z.Close()
	return b.Bytes()
}

// v1.9.3: a zip is kept, its files listed and imported on request.
func TestZipDocuments(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	bob := e.user("bob")
	existing := alice.waitJob(alice.upload("已有.txt", "已经有的收据", nil).body["jobId"].(float64))["documentId"].(float64)
	_ = existing
	r := alice.uploadBytes("收据.zip", zipOf(map[string]string{
		"收据/停车费.txt": "停车费 20 元", "收据/已有.txt": "已经有的收据", "收据/程序.exe": "MZ\x90\x00binary", "说明.md": "# 说明",
	}), nil)
	if r.code != http.StatusAccepted {
		t.Fatalf("upload: %d %s", r.code, r.raw)
	}
	job := alice.waitJob(r.body["jobId"].(float64))
	if job["status"] != "succeeded" {
		t.Fatalf("job = %v", job)
	}
	id := itoa(job["documentId"].(float64))
	d := alice.mustDo(http.MethodGet, "/api/v1/documents/"+id, nil, http.StatusOK).body
	if d["mimeType"] != "application/zip" || !strings.Contains(d["content"].(string), "收据/停车费.txt") {
		t.Fatalf("zip document = %v", d)
	}
	list := alice.mustDo(http.MethodGet, "/api/v1/documents/"+id+"/entries", nil, http.StatusOK).body
	if len(list["entries"].([]any)) != 4 || list["truncated"] != false {
		t.Fatalf("entries = %v", list)
	}
	bob.mustDo(http.MethodGet, "/api/v1/documents/"+id+"/entries", nil, http.StatusNotFound)
	res := alice.mustDo(http.MethodPost, "/api/v1/documents/"+id+"/entries/import", map[string]any{"paths": []string{"收据/停车费.txt", "收据/已有.txt", "收据/程序.exe", "没有这个.txt"}}, http.StatusOK).body["results"].([]any)
	if len(res) != 4 {
		t.Fatalf("results = %v", res)
	}
	got := map[string]map[string]any{}
	for _, x := range res {
		got[x.(map[string]any)["path"].(string)] = x.(map[string]any)
	}
	if got["收据/停车费.txt"]["jobId"] == nil || got["收据/已有.txt"]["reason"] == nil || got["收据/程序.exe"]["reason"] != "不支持这种文件格式" || got["没有这个.txt"]["reason"] == nil {
		t.Fatalf("results = %v", got)
	}
	j := alice.waitJob(got["收据/停车费.txt"]["jobId"].(float64))
	n := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(j["documentId"].(float64)), nil, http.StatusOK).body
	if n["title"] != "停车费" || n["inbox"] != true {
		t.Errorf("imported = %v", n)
	}
	if r := alice.do(http.MethodPost, "/api/v1/documents/"+itoa(existing)+"/entries/import", map[string]any{"paths": []string{"a"}}); r.code != http.StatusBadRequest {
		t.Errorf("not a zip: %d", r.code)
	}
}

// v1.9.3: a CSV as a table; an OFD with its text and searchable PDF.
func TestSheetsAndOFD(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	csv := alice.waitJob(alice.upload("电表.csv", "日期,读数\n2026-10-01,5230\n", nil).body["jobId"].(float64))["documentId"].(float64)
	sh := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(csv)+"/sheets", nil, http.StatusOK).body["sheets"].([]any)
	rows := sh[0].(map[string]any)["rows"].([]any)
	if len(rows) != 2 || rows[1].([]any)[1] != "5230" {
		t.Fatalf("sheets = %v", sh)
	}
	txt := alice.waitJob(alice.upload("笔记.md", "# 不是表格", nil).body["jobId"].(float64))["documentId"].(float64)
	alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(txt)+"/sheets", nil, http.StatusBadRequest)

	needPDFTools(t)
	r := alice.uploadBytes("发票.ofd", ofdtest.Invoice{Number: "2431200001", Date: "2024年03月05日", Seller: "上海某某餐饮有限公司", Buyer: "王小明", Item: "餐费", Amount: "286.00"}.Build(), nil)
	job := alice.waitJob(r.body["jobId"].(float64))
	if job["status"] != "succeeded" {
		t.Fatalf("ofd job = %v", job)
	}
	d := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(job["documentId"].(float64)), nil, http.StatusOK).body
	if d["mimeType"] != "application/ofd" || !strings.Contains(d["content"].(string), "上海某某餐饮有限公司") || d["hasArchive"] != watermark.Available() {
		t.Errorf("ofd document = %v", d)
	}
}
