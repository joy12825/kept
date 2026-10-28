package parser

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kept/server/internal/epubtest"
)

func pngCover(w, h int) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)))
	return b.Bytes()
}

func parseBytes(t *testing.T, data []byte) (Result, string, error) {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "book.epub")
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Parse(context.Background(), Config{}, src, "book.epub", dir, nil)
	return res, dir, err
}

func TestEPUB3(t *testing.T) {
	data := epubtest.Build(epubtest.Book{
		Title: "唐诗选", Authors: []string{"李白", " 杜甫 "}, Publisher: "人民文学出版社", Date: "2012-08-01T00:00:00Z", Language: "zh-CN",
		Description: "<p>收录唐诗&amp;注释</p>", ISBN: "978-7-02-000220-7", Series: "古典文学",
		Chapters: []epubtest.Chapter{{Title: "静夜思", Paras: []string{"床前明月光，", "疑是地上霜。"}}, {Title: "春望", Paras: []string{"国破山河在"}}},
		Cover:    pngCover(800, 1200),
	})
	res, _, err := parseBytes(t, data)
	if err != nil {
		t.Fatal(err)
	}
	if res.MimeType != "application/epub+zip" || res.Ext != "epub" || res.PageCount != 0 || res.ArchivePath != "" {
		t.Errorf("result = %+v", res)
	}
	b := res.Book
	if b == nil || b.Title != "唐诗选" || strings.Join(b.Authors, ",") != "李白,杜甫" || b.Publisher != "人民文学出版社" || b.Published != "2012-08" ||
		b.Language != "中文" || b.Description != "收录唐诗&注释" || b.ISBN != "978-7-02-000220-7" || b.Series != "古典文学" {
		t.Errorf("book = %+v", b)
	}
	if !strings.Contains(res.Content, "静夜思\n床前明月光，\n疑是地上霜。\n\n春望") || !strings.Contains(res.Content, "国破山河在") || strings.Contains(res.Content, "margin") {
		t.Errorf("content = %q", res.Content)
	}
	for p, w := range map[string]int{res.ThumbnailPath: 400, res.CoverPath: 600} {
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		img, err := jpeg.Decode(f)
		f.Close()
		if err != nil || img.Bounds().Dx() != w {
			t.Errorf("%s: %v %v", filepath.Base(p), err, img.Bounds())
		}
	}
}

func TestEPUB2AndNoCover(t *testing.T) {
	res, _, err := parseBytes(t, epubtest.Build(epubtest.Book{Title: "Old", EPUB2: true, Cover: pngCover(100, 150), Chapters: []epubtest.Chapter{{Title: "One"}}}))
	if err != nil || res.CoverPath == "" || res.ThumbnailPath == "" {
		t.Fatalf("epub2 cover: %+v %v", res, err)
	}
	res, _, err = parseBytes(t, epubtest.Build(epubtest.Book{Title: "Plain", Chapters: []epubtest.Chapter{{Title: "One", Paras: []string{"text"}}}}))
	if err != nil || res.CoverPath != "" || res.ThumbnailPath != "" || res.Book.Title != "Plain" {
		t.Fatalf("no cover: %+v %v", res, err)
	}
	// A bad cover image is a warning, not a failure.
	res, _, err = parseBytes(t, epubtest.Build(epubtest.Book{Title: "Bad", Cover: []byte("not an image"), Chapters: []epubtest.Chapter{{Title: "One"}}}))
	if err != nil || res.CoverPath != "" || len(res.Warnings) != 1 {
		t.Fatalf("bad cover: %+v %v", res, err)
	}
}

func TestEPUBRefused(t *testing.T) {
	drm := epubtest.Build(epubtest.Book{Title: "Locked", Chapters: []epubtest.Chapter{{Title: "One"}}, Extra: map[string][]byte{
		"META-INF/encryption.xml": []byte(`<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container" xmlns:enc="http://www.w3.org/2001/04/xmlenc#">
<enc:EncryptedData><enc:EncryptionMethod Algorithm="http://www.w3.org/2001/04/xmlenc#aes128-cbc"/><enc:CipherData><enc:CipherReference URI="OEBPS/text/ch1.xhtml"/></enc:CipherData></enc:EncryptedData></encryption>`),
	}})
	if _, _, err := parseBytes(t, drm); !errors.Is(err, ErrDRM) {
		t.Errorf("drm: %v", err)
	}
	fonts := epubtest.Build(epubtest.Book{Title: "Fonts", Chapters: []epubtest.Chapter{{Title: "One"}}, Extra: map[string][]byte{
		"META-INF/encryption.xml": []byte(`<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container" xmlns:enc="http://www.w3.org/2001/04/xmlenc#">
<enc:EncryptedData><enc:EncryptionMethod Algorithm="http://www.idpf.org/2008/embedding"/></enc:EncryptedData></encryption>`),
	}})
	if _, _, err := parseBytes(t, fonts); err != nil {
		t.Errorf("obfuscated fonts are fine: %v", err)
	}

	// A zip that says it is an EPUB but has no container.
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	w, _ := z.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	_, _ = w.Write([]byte("application/epub+zip"))
	_ = z.Close()
	if _, _, err := parseBytes(t, buf.Bytes()); err == nil || !IsPermanent(err) || !strings.Contains(err.Error(), "EPUB 文件已损坏") {
		t.Errorf("broken: %v", err)
	}

	// A chapter that inflates past the limit (60 MB of one letter compresses to little).
	bomb := epubtest.Build(epubtest.Book{Title: "Bomb", Chapters: []epubtest.Chapter{{Title: "One", Paras: []string{strings.Repeat("a", 60<<20)}}}})
	if len(bomb) > 1<<20 {
		t.Fatalf("bomb is %d bytes", len(bomb))
	}
	if _, _, err := parseBytes(t, bomb); err == nil || !strings.Contains(err.Error(), "太大") {
		t.Errorf("bomb: %v", err)
	}
}

func TestHTMLText(t *testing.T) {
	for in, want := range map[string]string{
		`<html><head><title>x</title></head><body><p>一&nbsp;二</p><p>三<br/>四</p><script>bad()</script></body></html>`: "一 二\n三\n四",
		`<p>unclosed <b>bold<p>next<style>x{}</style>`: "unclosed bold\nnext",
	} {
		if got := htmlText([]byte(in)); got != want {
			t.Errorf("%q = %q, want %q", in, got, want)
		}
	}
}
