package api

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"kept/server/internal/pdftest"
	"kept/server/internal/watermark"
)

// v1.9.2: a share with a watermark serves stamped PDF pages; a view-only
// share refuses downloads.
func TestShareProtection(t *testing.T) {
	needPDFTools(t)
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	anon := e.anon()
	text := alice.waitJob(alice.upload("笔记.txt", "只有文字", nil).body["jobId"].(float64))["documentId"].(float64)
	r := alice.uploadBytes("合同.pdf", pdftest.Text("CONTRACT-PAGE-ONE", "CONTRACT-PAGE-TWO"), nil)
	pdf := alice.waitJob(r.body["jobId"].(float64))["documentId"].(float64)

	// view-only, no watermark: the page works, downloads do not
	l := alice.mustDo(http.MethodPost, "/api/v1/documents/"+itoa(text)+"/share-links", map[string]any{"days": 3, "file": "original", "allowDownload": false}, http.StatusCreated).body
	if l["allowDownload"] != false || l["watermark"] != nil {
		t.Fatalf("view-only link: %v", l)
	}
	tok := l["token"].(string)
	pub := anon.mustDo(http.MethodGet, "/api/v1/public/shares/"+tok, nil, http.StatusOK).body
	if pub["allowDownload"] != false || pub["watermarked"] != false {
		t.Fatalf("public: %v", pub)
	}
	if r := anon.do(http.MethodGet, "/api/v1/public/shares/"+tok+"/file?download=true", nil); r.code != http.StatusForbidden || code(r) != "view_only" {
		t.Fatalf("download of a view-only link: %d %s", r.code, r.raw)
	}
	if f := anon.mustDo(http.MethodGet, "/api/v1/public/shares/"+tok+"/file", nil, http.StatusOK); !strings.Contains(f.raw, "只有文字") || f.header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("view: %v", f.header)
	}
	hist := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(text)+"/history", nil, http.StatusOK)
	if !strings.Contains(hist.raw, "只能在线查看") {
		t.Errorf("history: %s", hist.raw)
	}

	if !watermark.Available() {
		t.Skip("no Chinese font for watermarks")
	}
	// a watermark needs a PDF
	if r := alice.do(http.MethodPost, "/api/v1/documents/"+itoa(text)+"/share-links", map[string]any{"days": 3, "file": "original", "watermark": "仅供查看"}); r.code != http.StatusBadRequest {
		t.Fatalf("watermark on text: %d %s", r.code, r.raw)
	}
	if r := alice.do(http.MethodPost, "/api/v1/documents/"+itoa(pdf)+"/share-links", map[string]any{"days": 3, "file": "original", "watermark": strings.Repeat("长", 41)}); r.code != http.StatusBadRequest {
		t.Fatalf("too long: %d", r.code)
	}
	l = alice.mustDo(http.MethodPost, "/api/v1/documents/"+itoa(pdf)+"/share-links", map[string]any{"days": 3, "file": "original", "watermark": " 仅供某某银行\n办理房贷使用 "}, http.StatusCreated).body
	if l["watermark"] != "仅供某某银行 办理房贷使用" || l["allowDownload"] != true {
		t.Fatalf("watermarked link: %v", l)
	}
	tok = l["token"].(string)
	stamped := func(path string) string {
		t.Helper()
		f := anon.mustDo(http.MethodGet, path, nil, http.StatusOK)
		p := filepath.Join(t.TempDir(), "x.pdf")
		os.WriteFile(p, []byte(f.raw), 0o644)
		out, err := exec.Command("pdftotext", p, "-").Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.ReplaceAll(string(out), " ", "")
	}
	got := stamped("/api/v1/public/shares/" + tok + "/file?download=true")
	// two pages, each with the document's text and the footer (pdftotext
	// mixes the diagonal watermark into the page's own text)
	if strings.Count(got, "\f") != 2 || strings.Count(got, "CONTRACT") != 2 || strings.Count(got, "仅供某某银行办理房贷使用·分享于") != 2 {
		t.Fatalf("stamped: %q", got)
	}
	// the second time comes from the cache
	if again := stamped("/api/v1/public/shares/" + tok + "/file"); again != got {
		t.Error("cached copy differs")
	}

	if !eeBuild { // share bundles are paid (v1.13.2)
		return
	}
	// a bundle: a document without a PDF is named; view-only refuses the zip
	if r := alice.do(http.MethodPost, "/api/v1/share-bundles", map[string]any{"ids": []float64{pdf, text}, "days": 3, "file": "archive", "watermark": "仅供查看"}); r.code != http.StatusBadRequest || !strings.Contains(r.raw, "笔记") {
		t.Fatalf("bundle with text: %d %s", r.code, r.raw)
	}
	b := alice.mustDo(http.MethodPost, "/api/v1/share-bundles", map[string]any{"ids": []float64{pdf}, "days": 3, "file": "archive", "watermark": "仅供查看", "allowDownload": false}, http.StatusCreated).body
	btok := b["token"].(string)
	pb := anon.mustDo(http.MethodGet, "/api/v1/public/bundles/"+btok, nil, http.StatusOK).body
	if pb["allowDownload"] != false || pb["watermarked"] != true {
		t.Fatalf("public bundle: %v", pb)
	}
	if r := anon.do(http.MethodGet, "/api/v1/public/bundles/"+btok+"/zip", nil); r.code != http.StatusForbidden {
		t.Fatalf("zip of a view-only bundle: %d", r.code)
	}
	if got := stamped("/api/v1/public/bundles/" + btok + "/documents/" + itoa(pdf) + "/file"); !strings.Contains(got, "仅供查看") {
		t.Fatalf("bundle file: %q", got)
	}
	act := alice.mustDo(http.MethodGet, "/api/v1/share-links", nil, http.StatusOK)
	if !strings.Contains(act.raw, `"watermark":"仅供查看"`) || !strings.Contains(act.raw, `"allowDownload":false`) {
		t.Errorf("active shares: %s", act.raw)
	}
}

// v1.9.2: the text of a version, for comparing.
func TestVersionText(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	bob := e.user("bob")
	doc := alice.waitJob(alice.upload("合同.txt", "第一稿\n房租 3000 元", nil).body["jobId"].(float64))["documentId"].(float64)
	alice.waitJob(alice.addVersion(doc, "合同二稿.txt", []byte("第二稿\n房租 3200 元"), "第二稿").body["jobId"].(float64))
	base := "/api/v1/documents/" + itoa(doc) + "/versions"
	var first, second float64
	for _, v := range alice.mustDo(http.MethodGet, base, nil, http.StatusOK).list {
		v := v.(map[string]any)
		if v["number"] == float64(1) {
			first = v["id"].(float64)
		} else {
			second = v["id"].(float64)
		}
	}
	old := alice.mustDo(http.MethodGet, base+"/"+itoa(first)+"/text", nil, http.StatusOK).body
	if old["source"] != "file" || old["text"] != "第一稿\n房租 3000 元" {
		t.Errorf("first: %v", old)
	}
	cur := alice.mustDo(http.MethodGet, base+"/"+itoa(second)+"/text", nil, http.StatusOK).body
	if cur["source"] != "content" || !strings.Contains(cur["text"].(string), "3200") {
		t.Errorf("current: %v", cur)
	}
	bob.mustDo(http.MethodGet, base+"/"+itoa(first)+"/text", nil, http.StatusNotFound)
	f := alice.mustDo(http.MethodGet, base+"/"+itoa(first)+"/file?inline=true", nil, http.StatusOK)
	if !strings.HasPrefix(f.header.Get("Content-Disposition"), "inline") {
		t.Errorf("inline: %v", f.header)
	}
}
