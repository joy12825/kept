package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// upload posts a file as multipart form data, preset fields first.
func (c *client) upload(filename, content string, fields map[string]string) resp {
	c.env.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	fw, _ := mw.CreateFormFile("file", filename)
	_, _ = io.WriteString(fw, content)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/documents", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return c.send(req)
}

// send runs a prepared request with the client's credentials.
func (c *client) send(req *http.Request) resp {
	c.env.t.Helper()
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	c.env.h.ServeHTTP(rec, req)
	out := resp{code: rec.Code, header: rec.Header(), raw: rec.Body.String()}
	_ = jsonUnmarshal(rec.Body.Bytes(), &out)
	return out
}

// waitJob polls the job list until the job finishes.
func (c *client) waitJob(id float64) map[string]any {
	c.env.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		for _, j := range c.mustDo(http.MethodGet, "/api/v1/jobs", nil, http.StatusOK).list {
			j := j.(map[string]any)
			if j["id"] == id && (j["status"] == "succeeded" || j["status"] == "failed") {
				return j
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	c.env.t.Fatalf("job %v did not finish", id)
	return nil
}

func TestUploadAndRead(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	typeID := alice.mustDo(http.MethodPost, "/api/v1/document-types", map[string]any{"name": "账单"}, http.StatusCreated).body["id"].(float64)

	r := alice.upload("2024年3月电费账单.txt", "国家电网\n开票日期：2024年3月5日\n本期电费 128.50 元", map[string]string{"documentTypeId": itoa(typeID)})
	if r.code != http.StatusAccepted {
		t.Fatalf("upload: %d %s", r.code, r.raw)
	}
	job := alice.waitJob(r.body["jobId"].(float64))
	if job["status"] != "succeeded" || job["filename"] != "2024年3月电费账单.txt" || job["progress"] != float64(100) {
		t.Fatalf("job = %v", job)
	}
	docID := job["documentId"].(float64)

	inbox := alice.mustDo(http.MethodGet, "/api/v1/documents?filter="+url.QueryEscape(`{"field":"inbox","op":"eq","value":true}`)+"", nil, http.StatusOK).body
	items := inbox["items"].([]any)
	if inbox["total"] != float64(1) || items[0].(map[string]any)["title"] != "2024年3月电费账单" {
		t.Errorf("inbox = %v", inbox)
	}
	d := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(docID), nil, http.StatusOK).body
	if d["createdDate"] != "2024-03-05" || d["documentTypeId"] != typeID || !strings.Contains(d["content"].(string), "国家电网") || d["inbox"] != true {
		t.Errorf("document = %v", d)
	}

	// Files: the original as an attachment; the preview inline; ranges work.
	orig := alice.do(http.MethodGet, "/api/v1/documents/"+itoa(docID)+"/original", nil)
	if orig.code != http.StatusOK || !strings.HasPrefix(orig.header.Get("Content-Disposition"), "attachment;") || !strings.Contains(orig.raw, "国家电网") ||
		orig.header.Get("Content-Security-Policy") != "sandbox" {
		t.Errorf("original: %d %q", orig.code, orig.header.Get("Content-Disposition"))
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/documents/"+itoa(docID)+"/preview", nil)
	req.Header.Set("Range", "bytes=0-3")
	if pr := alice.send(req); pr.code != http.StatusPartialContent || len(pr.raw) != 4 || pr.header.Get("ETag") == "" {
		t.Errorf("ranged preview: %d %q", pr.code, pr.raw)
	}
	// Links that cannot carry headers name the workspace in the query string.
	personal := alice.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK).body["workspace"].(map[string]any)["id"].(float64)
	if r := e.user("mallory").do(http.MethodGet, "/api/v1/documents/"+itoa(docID)+"/original?workspace="+itoa(personal), nil); r.code != http.StatusNotFound {
		t.Errorf("another account naming alice's workspace: %d", r.code)
	}
	if r := alice.do(http.MethodGet, "/api/v1/documents/"+itoa(docID)+"/original?workspace="+itoa(personal), nil); r.code != http.StatusOK {
		t.Errorf("workspace in the query string: %d", r.code)
	}
	if r := alice.do(http.MethodGet, "/api/v1/documents/"+itoa(docID)+"/thumbnail", nil); r.code != http.StatusNotFound {
		t.Errorf("text has no thumbnail: %d", r.code)
	}

	// The same file again: 409 pointing at the existing document.
	dup := alice.upload("copy.txt", "国家电网\n开票日期：2024年3月5日\n本期电费 128.50 元", nil)
	if dup.code != http.StatusConflict || dup.body["details"].(map[string]any)["documentId"] != docID {
		t.Errorf("duplicate: %d %v", dup.code, dup.body)
	}
	if r := alice.upload("empty.txt", "", nil); r.code != http.StatusBadRequest {
		t.Errorf("empty file: %d", r.code)
	}
	if r := alice.do(http.MethodGet, "/api/v1/documents?filter=nope", nil); r.code != http.StatusBadRequest || code(r) != "invalid_filter" {
		t.Errorf("a malformed filter: %d %v", r.code, r.body)
	}

	// Reprocess keeps the document.
	rp := alice.mustDo(http.MethodPost, "/api/v1/documents/"+itoa(docID)+"/reprocess", nil, http.StatusAccepted)
	if j := alice.waitJob(rp.body["jobId"].(float64)); j["status"] != "succeeded" || j["documentId"] != docID {
		t.Errorf("reprocess job = %v", j)
	}

	// An unsupported file fails with a reason the tray can show.
	bad := alice.upload("a.zip", "PK\x03\x04\x14\x00\x06\x00 not supported", nil)
	if j := alice.waitJob(bad.body["jobId"].(float64)); j["status"] != "failed" || !strings.HasPrefix(j["error"].(string), "不支持这种文件格式") {
		t.Errorf("unsupported job = %v", j)
	}
}

func TestEventStream(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	srv := httptest.NewServer(e.h)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/events", nil)
	req.AddCookie(alice.cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type = %q", ct)
	}
	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	alice.upload("hello.txt", "你好", nil)
	timeout := time.After(20 * time.Second)
	seen := map[string]bool{}
	for !(seen["event: job.updated"] && seen["event: inbox.changed"]) {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatal("stream closed")
			}
			seen[l] = true
		case <-timeout:
			t.Fatalf("events seen: %v", seen)
		}
	}
}

func TestEventStreamAcceptsTheWorkspaceAsAQueryParameter(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, bob := e.user("alice"), e.user("bob")
	homeID := alice.mustDo(http.MethodPost, "/api/v1/workspaces", map[string]any{"name": "王家", "kind": "family"}, http.StatusCreated).body["id"].(float64)
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	get := func(c *client) int {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/events?workspace="+itoa(homeID), nil)
		req.AddCookie(c.cookie)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := get(bob); code != http.StatusNotFound {
		t.Errorf("a non-member's stream: %d, want 404", code)
	}
	if code := get(alice); code != http.StatusOK {
		t.Errorf("a member's stream: %d", code)
	}
}

// jsonUnmarshal fills out.body or out.list like client.do does.
func jsonUnmarshal(b []byte, out *resp) error {
	if len(b) == 0 {
		return nil
	}
	if b[0] == '[' {
		return json.Unmarshal(b, &out.list)
	}
	return json.Unmarshal(b, &out.body)
}
