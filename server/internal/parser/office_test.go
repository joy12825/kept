package parser

import (
	"archive/zip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// docx writes a minimal Word document (enough for type detection).
func docx(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "letter.docx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, body := range map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"word/document.xml":   `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"/>`,
	} {
		w, _ := z.Create(name)
		io.WriteString(w, body)
	}
	z.Close()
	f.Close()
	return path
}

func TestOfficeIsConvertedByGotenberg(t *testing.T) {
	dir := t.TempDir()
	src := docx(t, dir)
	mime, ext, err := Detect(src, "letter.docx")
	if err != nil || ext != "docx" || !IsOffice(mime) {
		t.Fatalf("Detect = %q %q %v", mime, ext, err)
	}
	// Without Gotenberg (v1.9.1): kept, its text read, waiting for the PDF.
	if res, err := Parse(context.Background(), cfg, src, "letter.docx", dir, nil); err != nil || res.Pending != PendingOffice || res.ArchivePath != "" || res.ThumbnailPath != "" {
		t.Fatalf("without Gotenberg: %+v %v", res, err)
	}

	needTools(t)
	pdf := textPDF(t, t.TempDir(), invoiceLines...)
	var gotName string
	gb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/forms/libreoffice/convert" {
			http.NotFound(w, r)
			return
		}
		_, h, err := r.FormFile("files")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotName = h.Filename
		http.ServeFile(w, r, pdf)
	}))
	defer gb.Close()
	c := cfg
	c.GotenbergURL = gb.URL
	res, err := Parse(context.Background(), c, src, "letter.docx", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if gotName != "document.docx" || res.Ext != "docx" || !strings.Contains(res.Content, "ACME POWER COMPANY") || res.ArchivePath == "" || res.ThumbnailPath == "" {
		t.Errorf("result = %+v (sent %q)", res, gotName)
	}

	// A file Gotenberg refuses fails for good.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "nope", http.StatusBadRequest) }))
	defer bad.Close()
	c.GotenbergURL = bad.URL
	if _, err := Parse(context.Background(), c, src, "letter.docx", t.TempDir(), nil); !IsPermanent(err) {
		t.Errorf("refused conversion: %v", err)
	}
}

// zipped writes an Office file of the given parts.
func zipped(t *testing.T, path string, parts map[string]string) string {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, body := range parts {
		w, _ := z.Create(name)
		io.WriteString(w, body)
	}
	z.Close()
	f.Close()
	return path
}

// v1.9.1: the text of the zipped XML formats, read without Gotenberg.
func TestOfficeTextWithoutGotenberg(t *testing.T) {
	dir := t.TempDir()
	const ct = `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`
	cases := []struct {
		name  string
		parts map[string]string
		want  []string
	}{
		{"合同.docx", map[string]string{"[Content_Types].xml": ct, "word/document.xml": `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:r><w:t>租赁</w:t></w:r><w:r><w:t xml:space="preserve">合同 </w:t></w:r></w:p>
<w:p><w:r><w:t>甲方：</w:t><w:tab/><w:t>张三</w:t></w:r></w:p></w:body></w:document>`}, []string{"租赁合同", "甲方：\t张三"}},
		{"账目.xlsx", map[string]string{"[Content_Types].xml": ct, "xl/workbook.xml": `<workbook/>`, "xl/sharedStrings.xml": `<?xml version="1.0"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><t>电费</t></si><si><r><t>水</t></r><r><t>费</t></r></si></sst>`}, []string{"电费\n水费"}},
		{"汇报.pptx", map[string]string{"[Content_Types].xml": ct, "ppt/presentation.xml": `<p/>`,
			"ppt/slides/slide10.xml": `<p:sld xmlns:p="p" xmlns:a="a"><a:p><a:r><a:t>第十页</a:t></a:r></a:p></p:sld>`,
			"ppt/slides/slide2.xml":  `<p:sld xmlns:p="p" xmlns:a="a"><a:p><a:r><a:t>第二页</a:t></a:r></a:p></p:sld>`}, []string{"第二页\n\n第十页"}},
		{"笔记.odt", map[string]string{"mimetype": "application/vnd.oasis.opendocument.text", "content.xml": `<?xml version="1.0"?>
<office:document-content xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0">
<office:body><office:text><text:h>标题</text:h><text:p>第一段<text:s/>有<text:span>强调</text:span></text:p></office:text></office:body></office:document-content>`}, []string{"标题\n第一段 有强调"}},
	}
	for _, c := range cases {
		src := zipped(t, filepath.Join(dir, c.name), c.parts)
		ext := strings.TrimPrefix(filepath.Ext(c.name), ".")
		got := officeText(src, ext)
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: text %q, want %q", c.name, got, w)
			}
		}
	}
	// not a zip (an old .doc): no text, no error
	bin := filepath.Join(dir, "old.doc")
	os.WriteFile(bin, []byte("\xd0\xcf\x11\xe0 not a zip"), 0o600)
	if got := officeText(bin, "doc"); got != "" {
		t.Errorf("old doc: %q", got)
	}
}
