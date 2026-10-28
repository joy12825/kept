package imgutil

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestToJPEG(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 900, 1200))
	for i := range src.Pix {
		src.Pix[i] = 0 // fully transparent: white after compositing
	}
	src.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var p bytes.Buffer
	_ = png.Encode(&p, src)
	out, err := ToJPEG(p.Bytes(), 600)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil || img.Bounds().Dx() != 600 || img.Bounds().Dy() != 800 {
		t.Fatalf("%v %v", err, img.Bounds())
	}
	if r, g, b, _ := img.At(300, 400).RGBA(); r>>8 < 250 || g>>8 < 250 || b>>8 < 250 {
		t.Errorf("transparent should be white, got %d %d %d", r>>8, g>>8, b>>8)
	}
	if _, err := ToJPEG(p.Bytes(), 600, "jpeg"); !errors.Is(err, ErrFormat) {
		t.Errorf("png refused when only jpeg is allowed: %v", err)
	}
	if _, err := ToJPEG([]byte("GIF89a garbage"), 600); !errors.Is(err, ErrFormat) {
		t.Errorf("garbage: %v", err)
	}
	small := image.NewRGBA(image.Rect(0, 0, 100, 50))
	var q bytes.Buffer
	_ = png.Encode(&q, small)
	out, _ = ToJPEG(q.Bytes(), 600)
	if img, _ := jpeg.Decode(bytes.NewReader(out)); img.Bounds().Dx() != 100 {
		t.Errorf("small images keep their size: %v", img.Bounds())
	}
}
