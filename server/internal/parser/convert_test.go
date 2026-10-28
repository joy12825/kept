package parser

import (
	"context"
	"errors"
	"image"
	"image/gif"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/bmp"
)

// scanPNG renders the invoice as a 200 DPI PNG, like a scanned page.
func scanPNG(t *testing.T, dir string) (string, image.Image) {
	t.Helper()
	src := textPDF(t, dir, invoiceLines...)
	if out, err := exec.Command("pdftoppm", "-png", "-r", "200", "-singlefile", src, filepath.Join(dir, "scan")).CombinedOutput(); err != nil {
		t.Fatalf("render: %v %s", err, out)
	}
	p := filepath.Join(dir, "scan.png")
	f, _ := os.Open(p)
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return p, img
}

func TestMoreImageFormatsAreOCRed(t *testing.T) {
	needTools(t)
	dir := t.TempDir()
	scan, img := scanPNG(t, dir)
	write := func(name string, enc func(f *os.File) error) string {
		p := filepath.Join(dir, name)
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := enc(f); err != nil {
			t.Fatal(err)
		}
		f.Close()
		return p
	}
	files := map[string]string{
		"gif": write("scan.gif", func(f *os.File) error { return gif.Encode(f, img, nil) }),
		"bmp": write("scan.bmp", func(f *os.File) error { return bmp.Encode(f, img) }),
	}
	if _, err := exec.LookPath("cwebp"); err == nil {
		p := filepath.Join(dir, "scan.webp")
		if out, err := exec.Command("cwebp", "-quiet", "-lossless", scan, "-o", p).CombinedOutput(); err != nil {
			t.Fatalf("cwebp: %v %s", err, out)
		}
		files["webp"] = p
	}
	if _, err := exec.LookPath("heif-enc"); err == nil && HEICTool() != "" {
		p := filepath.Join(dir, "scan.heic")
		if out, err := exec.Command("heif-enc", "-q", "90", scan, "-o", p).CombinedOutput(); err != nil {
			t.Fatalf("heif-enc: %v %s", err, out)
		}
		files["heic"] = p
	}
	for ext, p := range files {
		res, err := Parse(context.Background(), cfg, p, filepath.Base(p), t.TempDir(), nil)
		if err != nil {
			t.Errorf("%s: %v", ext, err)
			continue
		}
		if res.Ext != ext || res.MimeType != "image/"+ext || res.ArchivePath == "" || res.ThumbnailPath == "" || !strings.Contains(strings.ToUpper(res.Content), "ACME") {
			t.Errorf("%s: result = %+v", ext, res)
		}
	}
	if len(files) < 4 {
		t.Logf("tested %d formats (cwebp / heif-enc / heif-dec missing)", len(files))
	}
}

func TestHEICWithoutTool(t *testing.T) {
	old := HEICTool
	HEICTool = func() string { return "" }
	defer func() { HEICTool = old }()
	if _, err := toPNG(context.Background(), "image/heic", "x.heic", t.TempDir()); !errors.Is(err, ErrHEICUnavailable) || !IsPermanent(err) {
		t.Errorf("err = %v", err)
	}
}

func TestHugeImageIsRefused(t *testing.T) {
	dir := t.TempDir()
	// A GIF header claiming 65535×65535 pixels: refused before decoding.
	p := filepath.Join(dir, "bomb.gif")
	_ = os.WriteFile(p, []byte("GIF89a\xff\xff\xff\xff\x00\x00\x00;"), 0o644)
	if _, err := toPNG(context.Background(), "image/gif", p, dir); err == nil || !IsPermanent(err) || !strings.Contains(err.Error(), "太大") {
		t.Errorf("err = %v", err)
	}
	if _, err := toPNG(context.Background(), "image/webp", p, dir); err == nil || !IsPermanent(err) {
		t.Errorf("a broken file: err = %v", err)
	}
}
