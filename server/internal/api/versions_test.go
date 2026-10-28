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

	"kept/server/internal/pdftest"
)

func (c *client) addVersion(doc float64, filename string, content []byte, label string) resp {
	c.env.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if label != "" {
		_ = mw.WriteField("label", label)
	}
	fw, _ := mw.CreateFormFile("file", filename)
	_, _ = fw.Write(content)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/documents/"+itoa(doc)+"/versions", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return c.send(req)
}

func TestDocumentVersions(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	doc := alice.waitJob(alice.upload("租房合同.txt", "第一稿：房租 3000 元", nil).body["jobId"].(float64))["documentId"].(float64)
	other := alice.waitJob(alice.upload("别的.txt", "别的文档", nil).body["jobId"].(float64))["documentId"].(float64)
	base := "/api/v1/documents/" + itoa(doc)

	vs := alice.mustDo(http.MethodGet, base+"/versions", nil, http.StatusOK)
	if len(vs.list) != 1 || vs.list[0].(map[string]any)["id"] != float64(0) || vs.list[0].(map[string]any)["current"] != true {
		t.Fatalf("one version: %s", vs.raw)
	}
	// The same file as now, or as another document: refused.
	if r := alice.addVersion(doc, "a.txt", []byte("第一稿：房租 3000 元"), ""); r.code != http.StatusConflict {
		t.Fatalf("same as current: %d %s", r.code, r.raw)
	}
	if r := alice.addVersion(doc, "a.txt", []byte("别的文档"), ""); r.code != http.StatusConflict || r.body["details"].(map[string]any)["documentId"] != other {
		t.Fatalf("same as another: %d %s", r.code, r.raw)
	}

	r := alice.addVersion(doc, "租房合同-二稿.txt", []byte("第二稿：房租 3200 元"), "第二稿")
	if r.code != http.StatusAccepted {
		t.Fatalf("add: %d %s", r.code, r.raw)
	}
	alice.waitJob(r.body["jobId"].(float64))
	d := alice.mustDo(http.MethodGet, base, nil, http.StatusOK).body
	if !strings.Contains(d["content"].(string), "3200") || d["originalFilename"] != "租房合同-二稿.txt" || d["title"] != "租房合同" {
		t.Fatalf("after adding: %v", d)
	}
	vs = alice.mustDo(http.MethodGet, base+"/versions", nil, http.StatusOK)
	if len(vs.list) != 2 {
		t.Fatalf("versions: %s", vs.raw)
	}
	v2, v1 := vs.list[0].(map[string]any), vs.list[1].(map[string]any)
	if v2["number"] != float64(2) || v2["current"] != true || v2["label"] != "第二稿" || v2["creatorName"] == "" || v1["current"] != false {
		t.Fatalf("versions: %s", vs.raw)
	}
	if f := alice.mustDo(http.MethodGet, base+"/versions/"+itoa(v1["id"].(float64))+"/file", nil, http.StatusOK); f.raw != "第一稿：房租 3000 元" {
		t.Fatalf("version 1 file: %q", f.raw)
	}
	if f := alice.mustDo(http.MethodGet, base+"/original", nil, http.StatusOK); f.raw != "第二稿：房租 3200 元" {
		t.Fatalf("original: %q", f.raw)
	}
	if r := alice.addVersion(doc, "a.txt", []byte("第一稿：房租 3000 元"), ""); r.code != http.StatusConflict || !strings.Contains(r.raw, "第 1 版") {
		t.Fatalf("same as version 1: %d %s", r.code, r.raw)
	}

	// Back to version 1; version 2 can then be deleted.
	alice.mustDo(http.MethodPost, base+"/versions/"+itoa(v2["id"].(float64))+"/current", nil, http.StatusConflict)
	r = alice.mustDo(http.MethodPost, base+"/versions/"+itoa(v1["id"].(float64))+"/current", nil, http.StatusAccepted)
	alice.waitJob(r.body["jobId"].(float64))
	if d := alice.mustDo(http.MethodGet, base, nil, http.StatusOK).body; !strings.Contains(d["content"].(string), "3000") {
		t.Fatalf("after restoring: %v", d)
	}
	alice.mustDo(http.MethodDelete, base+"/versions/"+itoa(v1["id"].(float64)), nil, http.StatusConflict)
	alice.mustDo(http.MethodDelete, base+"/versions/"+itoa(v2["id"].(float64)), nil, http.StatusNoContent)
	if vs := alice.mustDo(http.MethodGet, base+"/versions", nil, http.StatusOK); len(vs.list) != 1 {
		t.Fatalf("after delete: %s", vs.raw)
	}
	hist := alice.mustDo(http.MethodGet, base+"/history", nil, http.StatusOK).raw
	for _, want := range []string{"上传了“租房合同”的第 2 版", "把“租房合同”的当前版本换成了第 1 版", "删除了“租房合同”的第 2 版"} {
		if !strings.Contains(hist, want) {
			t.Errorf("history lacks %q: %s", want, hist)
		}
	}
	// Another workspace sees nothing.
	bob := e.user("bob")
	bob.mustDo(http.MethodGet, base+"/versions", nil, http.StatusNotFound)
}

func TestRemovePasswordMakesAVersion(t *testing.T) {
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
	doc := alice.waitJob(alice.uploadBytes("locked.pdf", b, map[string]string{"password": "pw"}).body["jobId"].(float64))["documentId"].(float64)
	base := "/api/v1/documents/" + itoa(doc)
	if r := alice.addVersion(doc, "again.pdf", b, ""); r.code != http.StatusBadRequest || code(r) != "pdf_encrypted" {
		t.Fatalf("encrypted version: %d %s", r.code, r.raw)
	}
	if r := alice.do(http.MethodPost, base+"/versions/decrypt", map[string]any{"password": "nope"}); code(r) != "wrong_password" {
		t.Fatalf("wrong password: %d %s", r.code, r.raw)
	}
	r := alice.mustDo(http.MethodPost, base+"/versions/decrypt", map[string]any{"password": "pw"}, http.StatusAccepted)
	alice.waitJob(r.body["jobId"].(float64))
	d := alice.mustDo(http.MethodGet, base, nil, http.StatusOK).body
	if d["originalEncrypted"] == true || !strings.Contains(d["content"].(string), "LOCKED") {
		t.Fatalf("decrypted: %v", d)
	}
	vs := alice.mustDo(http.MethodGet, base+"/versions", nil, http.StatusOK)
	v1 := vs.list[1].(map[string]any)
	if len(vs.list) != 2 || vs.list[0].(map[string]any)["label"] != "移除了密码" || v1["encrypted"] != true {
		t.Fatalf("versions: %s", vs.raw)
	}
	// The encrypted version stays downloadable but cannot be made current.
	if r := alice.do(http.MethodPost, base+"/versions/"+itoa(v1["id"].(float64))+"/current", nil); r.code != http.StatusBadRequest {
		t.Fatalf("encrypted current: %d %s", r.code, r.raw)
	}
	alice.mustDo(http.MethodPost, base+"/versions/decrypt", map[string]any{"password": "pw"}, http.StatusBadRequest) // no longer encrypted
}
