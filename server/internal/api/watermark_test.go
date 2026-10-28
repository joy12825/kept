package api

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"
)

func photoBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(40 + x%200), uint8(60 + y%180), 120, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func decodeJPEG(t *testing.T, raw string) image.Image {
	t.Helper()
	img, format, err := image.Decode(strings.NewReader(raw))
	if err != nil || format != "jpeg" {
		t.Fatalf("not a JPEG: %s %v", format, err)
	}
	return img
}
