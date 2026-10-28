package parser

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestFlattenAlphaKeepsResolution(t *testing.T) {
	dir := t.TempDir()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.NRGBA{A: 0})
	img.Set(2, 2, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	// 144 DPI = 5669 pixels per metre, unit 1 (metre).
	phys := []byte{0, 0, 0, 9, 'p', 'H', 'Y', 's', 0, 0, 0x16, 0x25, 0, 0, 0x16, 0x25, 1, 0x49, 0x52, 0x24, 0xf0}
	src := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(src, withPHYs(buf.Bytes(), append(buf.Bytes()[:33:33], phys...)), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := flattenAlpha(src, dir)
	if err != nil || out == src {
		t.Fatalf("flattenAlpha = %q, %v", out, err)
	}
	b, _ := os.ReadFile(out)
	if pngHasAlpha(b) {
		t.Error("the flattened image still has an alpha channel")
	}
	if got := pngChunk(b, "pHYs"); !bytes.Equal(got, phys) {
		t.Errorf("pHYs = %x, want %x", got, phys)
	}
	flat, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("decode flattened: %v", err)
	}
	if r, g, bl, _ := flat.At(1, 1).RGBA(); r != 0xffff || g != 0xffff || bl != 0xffff {
		t.Errorf("transparent pixel = %v, want white", flat.At(1, 1))
	}

	// An opaque PNG is passed through untouched.
	opaque := filepath.Join(dir, "opaque.png")
	var ob bytes.Buffer
	_ = png.Encode(&ob, image.NewGray(image.Rect(0, 0, 2, 2)))
	_ = os.WriteFile(opaque, ob.Bytes(), 0o600)
	if got, err := flattenAlpha(opaque, dir); got != opaque || err != nil {
		t.Errorf("opaque = %q, %v", got, err)
	}
}
