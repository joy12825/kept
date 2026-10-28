package parser

import (
	"context"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// v1.3.9: a page scanned upside down is turned upright before recognition,
// so its text is read; without rotation it is not.
func TestUpsideDownScanIsTurnedUpright(t *testing.T) {
	needTools(t)
	dir := t.TempDir()
	// a full page, as real documents are: a few lines alone leave
	// tesseract unsure of the orientation
	lines := append([]string{}, invoiceLines...)
	for i := range 25 {
		lines = append(lines, "Meter reading "+strconv.Itoa(i)+": previous 1000 kWh, current 1100 kWh, usage 100 kWh")
	}
	src := textPDF(t, dir, lines...)
	if out, err := exec.Command("pdftoppm", "-png", "-r", "200", "-singlefile", src, filepath.Join(dir, "scan")).CombinedOutput(); err != nil {
		t.Fatalf("render: %v %s", err, out)
	}
	upsideDown := filepath.Join(dir, "upside-down.png")
	rotate180(t, filepath.Join(dir, "scan.png"), upsideDown)

	read := func(c Config) string {
		work := t.TempDir()
		res, err := Parse(context.Background(), c, upsideDown, "upside-down.png", work, nil)
		if err != nil {
			t.Fatal(err)
		}
		return strings.ToUpper(res.Content)
	}
	on := cfg
	on.Rotate, on.Deskew, on.RotateThreshold = true, true, 5
	if got := read(on); !strings.Contains(got, "ACME") {
		t.Errorf("turned upright: %q", got)
	}
	off := cfg
	off.Rotate, off.Deskew = false, false
	if got := read(off); strings.Contains(got, "ACME") {
		t.Skip("this tesseract reads upside-down text by itself; nothing to compare")
	}
}

func TestLayoutFlags(t *testing.T) {
	c := Config{Rotate: true, Deskew: true}
	if got := strings.Join(layoutFlags(c, []string{"--skip-text"}), " "); got != "--rotate-pages --deskew" {
		t.Errorf("skip: %s", got)
	}
	if got := strings.Join(layoutFlags(Config{Rotate: true, RotateThreshold: 5}, nil), " "); got != "--rotate-pages --rotate-pages-threshold 5" {
		t.Errorf("threshold: %s", got)
	}
	if got := strings.Join(layoutFlags(c, []string{"--redo-ocr"}), " "); got != "--rotate-pages" {
		t.Errorf("redo cannot deskew: %s", got)
	}
	if got := layoutFlags(Config{}, nil); len(got) != 0 {
		t.Errorf("off: %v", got)
	}
}

func rotate180(t *testing.T, from, to string) {
	t.Helper()
	f, err := os.Open(from)
	if err != nil {
		t.Fatal(err)
	}
	src, err := png.Decode(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	b := src.Bounds()
	dst := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(b.Max.X-1-(x-b.Min.X), b.Max.Y-1-(y-b.Min.Y), src.At(x, y))
		}
	}
	out, err := os.Create(to)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if err := png.Encode(out, dst); err != nil {
		t.Fatal(err)
	}
}
