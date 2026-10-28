package ingest

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"kept/server/internal/parser"
)

func docx(t *testing.T, text string) []byte {
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
	z.Close()
	return b.Bytes()
}

// v1.9.7: an earlier Word version is converted once for the side-by-side
// view, then served from the cache.
func TestVersionPreviewConvertsOnce(t *testing.T) {
	if m := parser.MissingTools(); len(m) > 0 {
		t.Skipf("missing tools: %v", m)
	}
	e := newEnv(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/forms/libreoffice/convert" {
			http.NotFound(w, r)
			return
		}
		hits.Add(1)
		w.Write(pdfBytes(t, "Renovation contract"))
	}))
	defer srv.Close()
	e.s.opt.Parser.GotenbergURL = srv.URL
	sub, cancel := e.bus.Subscribe(1)
	defer cancel()

	job, err := e.s.Submit(e.ctx, e.scope, "装修合同.docx", bytes.NewReader(docx(t, "总价 68000 元")), Overrides{}, Upload{})
	if err != nil {
		t.Fatal(err)
	}
	doc := e.wait(sub, job).DocumentID
	job, err = e.s.AddVersion(e.ctx, e.scope, doc, "装修合同.docx", bytes.NewReader(docx(t, "总价 65000 元")), "第二稿")
	if err != nil {
		t.Fatal(err)
	}
	if je := e.wait(sub, job); je.Status != "succeeded" {
		t.Fatalf("version job: %+v", je)
	}
	vs, err := e.s.Versions(e.ctx, e.scope, doc)
	if err != nil || len(vs) != 2 {
		t.Fatalf("versions %+v %v", vs, err)
	}
	var first, current Version
	for _, v := range vs {
		if v.Current {
			current = v
		} else {
			first = v
		}
	}
	before := hits.Load()
	for range 2 {
		loc, name, err := e.s.VersionPreview(e.ctx, e.scope, doc, first.ID)
		if err != nil || name != "装修合同.pdf" {
			t.Fatalf("preview: %q %v", name, err)
		}
		b, err := os.ReadFile(loc.Path)
		if err != nil || !bytes.HasPrefix(b, []byte("%PDF")) {
			t.Fatalf("preview file: %v", err)
		}
	}
	if n := hits.Load() - before; n != 1 {
		t.Errorf("converted %d times; want once", n)
	}
	// the current version is the document's searchable PDF: no conversion
	if _, _, err := e.s.VersionPreview(e.ctx, e.scope, doc, current.ID); err != nil || hits.Load()-before != 1 {
		t.Errorf("current: %v, conversions %d", err, hits.Load()-before)
	}
}
