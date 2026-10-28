package api

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kept/server/internal/parser"
	"kept/server/internal/pdftest"
)

func (c *client) uploadBytes(filename string, content []byte, fields map[string]string) resp {
	c.env.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	fw, _ := mw.CreateFormFile("file", filename)
	_, _ = fw.Write(content)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/documents", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return c.send(req)
}

func (c *client) waitTask(id float64) map[string]any {
	c.env.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		for _, j := range c.mustDo(http.MethodGet, "/api/v1/tasks", nil, http.StatusOK).list {
			j := j.(map[string]any)
			if j["id"] == id && (j["status"] == "succeeded" || j["status"] == "failed") {
				return j
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	c.env.t.Fatalf("task %v did not finish", id)
	return nil
}

func needPDFTools(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("qpdf"); err != nil || len(parser.MissingTools()) > 0 {
		t.Skip("needs qpdf and poppler")
	}
}

func TestPageEditingSplitAndMerge(t *testing.T) {
	needPDFTools(t)
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	r := alice.uploadBytes("合同.pdf", pdftest.Text("PAGE-ONE", "PAGE-TWO", "PAGE-THREE"), nil)
	if r.code != http.StatusAccepted {
		t.Fatalf("upload: %d %s", r.code, r.raw)
	}
	id := alice.waitJob(r.body["jobId"].(float64))["documentId"].(float64)
	path := "/api/v1/documents/" + itoa(id)

	d := alice.mustDo(http.MethodPut, path+"/pages", map[string]any{"rotate": map[string]int{"1": 90}, "delete": []int{2}}, http.StatusOK).body
	if d["pageCount"] != float64(2) || d["pageEdits"] == nil || d["hasArchive"] != true || strings.Contains(d["content"].(string), "PAGE-TWO") {
		t.Fatalf("after editing: %v", d)
	}
	pages := func(p string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, p, nil)
		req.AddCookie(alice.cookie)
		e.h.ServeHTTP(rec, req)
		f := filepath.Join(t.TempDir(), "x.pdf")
		os.WriteFile(f, rec.Body.Bytes(), 0o644)
		out, _ := exec.Command("qpdf", "--show-npages", f).Output()
		n := 0
		for _, ch := range strings.TrimSpace(string(out)) {
			n = n*10 + int(ch-'0')
		}
		return n
	}
	if n := pages(path + "/preview"); n != 2 {
		t.Errorf("preview pages = %d, want 2", n)
	}
	if n := pages(path + "/preview?unedited=true"); n != 3 {
		t.Errorf("unedited preview pages = %d, want 3", n)
	}
	if r := alice.do(http.MethodPut, path+"/pages", map[string]any{"delete": []int{1, 2, 3}}); r.code != http.StatusBadRequest {
		t.Errorf("deleting every page: %d", r.code)
	}
	hist := alice.mustDo(http.MethodGet, path+"/history", nil, http.StatusOK)
	if !strings.Contains(hist.raw, "旋转了第 1 页，删除了第 2 页") {
		t.Errorf("history = %s", hist.raw)
	}

	// Split the edited document after its first page; the original goes to the trash.
	job := alice.mustDo(http.MethodPost, path+"/split", map[string]any{"after": []int{1}, "trashOriginal": true}, http.StatusAccepted).body["jobId"].(float64)
	task := alice.waitTask(job)
	if task["status"] != "succeeded" || task["label"] != "拆分文档" || task["summary"] != "拆成 2 份" {
		t.Fatalf("split task = %v", task)
	}
	list := alice.mustDo(http.MethodGet, "/api/v1/documents", nil, http.StatusOK).body["items"].([]any)
	var parts []float64
	for _, it := range list {
		if strings.HasPrefix(it.(map[string]any)["title"].(string), "合同（") {
			parts = append(parts, it.(map[string]any)["id"].(float64))
		}
	}
	if len(parts) != 2 || alice.do(http.MethodGet, path, nil).code != http.StatusNotFound {
		t.Fatalf("parts = %v; the original should be in the trash", list)
	}

	// Merge the two parts back into one.
	job = alice.mustDo(http.MethodPost, "/api/v1/documents/merge", map[string]any{"ids": parts}, http.StatusAccepted).body["jobId"].(float64)
	if task := alice.waitTask(job); task["status"] != "succeeded" || task["summary"] != "2 份合并为一份" {
		t.Fatalf("merge task = %v", task)
	}
	if r := alice.do(http.MethodPost, "/api/v1/documents/merge", map[string]any{"ids": []float64{parts[0]}}); r.code != http.StatusBadRequest {
		t.Errorf("merging one document: %d", r.code)
	}
}

func TestUploadEncryptedPDF(t *testing.T) {
	needPDFTools(t)
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	dir := t.TempDir()
	plain, enc := filepath.Join(dir, "p.pdf"), filepath.Join(dir, "e.pdf")
	os.WriteFile(plain, pdftest.Text("LOCKED-PAGE"), 0o644)
	if out, err := exec.Command("qpdf", "--encrypt", "pw", "owner", "256", "--", plain, enc).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	f, _ := os.Open(enc)
	b, _ := io.ReadAll(f)
	f.Close()
	if r := alice.uploadBytes("locked.pdf", b, nil); r.code != http.StatusBadRequest || code(r) != "pdf_encrypted" {
		t.Errorf("without a password: %d %s", r.code, r.raw)
	}
	r := alice.uploadBytes("locked.pdf", b, map[string]string{"password": "pw"})
	if r.code != http.StatusAccepted {
		t.Fatalf("with the password: %d %s", r.code, r.raw)
	}
	id := alice.waitJob(r.body["jobId"].(float64))["documentId"].(float64)
	d := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(id), nil, http.StatusOK).body
	if d["originalEncrypted"] != true || !strings.Contains(d["content"].(string), "LOCKED") {
		t.Errorf("document = %v", d)
	}
}
