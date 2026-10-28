package api

import (
	"archive/zip"
	"bytes"
	"net/http"
	"strings"
	"testing"

	"kept/server/internal/ofdtest"
	"kept/server/internal/pdftest"
	"kept/server/internal/watermark"
)

// versionIDs lists a document's versions by number.
func versionIDs(c *client, doc float64) map[float64]float64 {
	out := map[float64]float64{}
	for _, v := range c.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(doc)+"/versions", nil, http.StatusOK).list {
		v := v.(map[string]any)
		out[v["number"].(float64)] = v["id"].(float64)
	}
	return out
}

// v1.9.7: versions side by side — each version as a PDF.
func TestVersionPreview(t *testing.T) {
	needPDFTools(t)
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	bob := e.user("bob")

	// a PDF version is its own file
	doc := alice.waitJob(alice.uploadBytes("合同.pdf", pdftest.Text("FIRST-DRAFT"), nil).body["jobId"].(float64))["documentId"].(float64)
	alice.waitJob(alice.addVersion(doc, "合同二稿.pdf", pdftest.Text("SECOND-DRAFT"), "").body["jobId"].(float64))
	ids := versionIDs(alice, doc)
	base := "/api/v1/documents/" + itoa(doc) + "/versions/"
	r := alice.mustDo(http.MethodGet, base+itoa(ids[1])+"/preview", nil, http.StatusOK)
	if !bytes.HasPrefix([]byte(r.raw), []byte("%PDF")) || r.header.Get("Content-Type") != "application/pdf" {
		t.Errorf("pdf version: %q %v", r.raw[:min(20, len(r.raw))], r.header)
	}
	alice.mustDo(http.MethodGet, base+itoa(ids[2])+"/preview", nil, http.StatusOK)
	bob.mustDo(http.MethodGet, base+itoa(ids[1])+"/preview", nil, http.StatusNotFound)

	// text is shown as it is, not as a PDF
	txt := alice.waitJob(alice.upload("笔记.txt", "第一稿", nil).body["jobId"].(float64))["documentId"].(float64)
	alice.waitJob(alice.addVersion(txt, "笔记.txt", []byte("第二稿"), "").body["jobId"].(float64))
	if r := alice.do(http.MethodGet, "/api/v1/documents/"+itoa(txt)+"/versions/"+itoa(versionIDs(alice, txt)[1])+"/preview", nil); r.code != http.StatusConflict || !strings.Contains(r.raw, "no_preview") {
		t.Errorf("text version: %d %s", r.code, r.raw)
	}

	// an earlier OFD is drawn on demand, then taken from the cache
	inv := func(n string) []byte {
		return ofdtest.Invoice{Number: n, Date: "2024年03月05日", Seller: "上海某某餐饮有限公司", Buyer: "王小明", Item: "餐费", Amount: "286.00"}.Build()
	}
	ofd := alice.waitJob(alice.uploadBytes("发票.ofd", inv("2431200001"), nil).body["jobId"].(float64))["documentId"].(float64)
	alice.waitJob(alice.addVersion(ofd, "发票红冲.ofd", inv("2431200002"), "").body["jobId"].(float64))
	first := "/api/v1/documents/" + itoa(ofd) + "/versions/" + itoa(versionIDs(alice, ofd)[1]) + "/preview"
	if !watermark.Available() {
		alice.mustDo(http.MethodGet, first, nil, http.StatusConflict) // no Chinese font: not drawn
		return
	}
	a := alice.mustDo(http.MethodGet, first, nil, http.StatusOK)
	b := alice.mustDo(http.MethodGet, first, nil, http.StatusOK)
	if !strings.HasPrefix(a.raw, "%PDF") || a.raw != b.raw {
		t.Errorf("ofd version: %d / %d bytes", len(a.raw), len(b.raw))
	}
}

// v1.9.7: an earlier Office version needs the conversion service.
func TestVersionPreviewWithoutConverter(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	docx := docxBytes(t, "第一稿")
	doc := alice.waitJob(alice.uploadBytes("合同.docx", docx, nil).body["jobId"].(float64))["documentId"].(float64)
	alice.waitJob(alice.addVersion(doc, "合同.docx", docxBytes(t, "第二稿"), "").body["jobId"].(float64))
	r := alice.do(http.MethodGet, "/api/v1/documents/"+itoa(doc)+"/versions/"+itoa(versionIDs(alice, doc)[1])+"/preview", nil)
	if r.code != http.StatusConflict || !strings.Contains(r.raw, "no_converter") {
		t.Errorf("docx version: %d %s", r.code, r.raw)
	}
}

// v1.9.7: downloading or printing a sensitive document is noted in its
// history; showing it, and other documents, are not.
func TestSensitiveTakenIsRecorded(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	plain := alice.waitJob(alice.upload("便条.txt", "买菜", nil).body["jobId"].(float64))["documentId"].(float64)
	sens := alice.waitJob(alice.upload("身份证.txt", "身份证号码", nil).body["jobId"].(float64))["documentId"].(float64)
	alice.mustDo(http.MethodPatch, "/api/v1/documents/"+itoa(sens), map[string]any{"sensitive": true}, http.StatusOK)
	alice.mustDo(http.MethodPost, "/api/v1/auth/verify", map[string]any{"password": "password-123"}, http.StatusOK)

	for _, d := range []float64{plain, sens} {
		p := "/api/v1/documents/" + itoa(d)
		alice.mustDo(http.MethodGet, p+"/preview", nil, http.StatusOK)
		alice.mustDo(http.MethodGet, p+"/original?purpose=download", nil, http.StatusOK)
		alice.mustDo(http.MethodGet, p+"/preview?purpose=print", nil, http.StatusOK)
	}
	summaries := func(d float64) []string {
		var out []string
		for _, it := range alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(d)+"/history", nil, http.StatusOK).body["items"].([]any) {
			out = append(out, it.(map[string]any)["summary"].(string))
		}
		return out
	}
	got := strings.Join(summaries(sens), "\n")
	if !strings.Contains(got, "下载了“身份证”的原件") || !strings.Contains(got, "打印了“身份证”") || strings.Count(got, "下载了")+strings.Count(got, "打印了") != 2 {
		t.Errorf("sensitive history:\n%s", got)
	}
	if got := strings.Join(summaries(plain), "\n"); strings.Contains(got, "下载了") || strings.Contains(got, "打印了") {
		t.Errorf("plain history:\n%s", got)
	}
}

// docxBytes is a small Word document saying text.
func docxBytes(t *testing.T, text string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, body := range map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"word/document.xml": `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>` +
			text + `</w:t></w:r></w:p></w:body></w:document>`,
	} {
		w, _ := z.Create(name)
		w.Write([]byte(body))
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
