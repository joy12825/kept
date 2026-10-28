package watermark

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/bmp"
)

// picture is a w×h picture: the left half dark, the right half light.
func picture(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{30, 40, 50, 255}
			if x >= w/2 {
				c = color.RGBA{240, 235, 225, 255}
			}
			img.Set(x, y, c)
		}
	}
	// a red mark in the top left corner, to see how it was turned
	for y := 0; y < h/10; y++ {
		for x := 0; x < w/10; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	return img
}

func write(t *testing.T, name string, enc func(*os.File) error) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
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

func readImage(t *testing.T, p string) (image.Image, string) {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, format, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img, format
}

func TestApplyImage(t *testing.T) {
	if !ImageAvailable() {
		t.Skip("no Chinese font")
	}
	src := picture(800, 600)
	files := map[string]string{
		"image/jpeg": write(t, "a.jpg", func(f *os.File) error { return jpeg.Encode(f, src, nil) }),
		"image/png":  write(t, "a.png", func(f *os.File) error { return png.Encode(f, src) }),
		"image/gif":  write(t, "a.gif", func(f *os.File) error { return gif.Encode(f, src, nil) }),
		"image/bmp":  write(t, "a.bmp", func(f *os.File) error { return bmp.Encode(f, src) }),
	}
	for mime, p := range files {
		dst := filepath.Join(t.TempDir(), "out")
		if err := ApplyImage(p, dst, mime, 0, "仅供办理房贷使用", "仅供办理房贷使用 · 2026-10-19"); err != nil {
			t.Fatalf("%s: %v", mime, err)
		}
		out, format := readImage(t, dst)
		if want := map[string]string{"image/jpeg": "jpeg", "image/png": "png"}[ImageType(mime)]; format != want {
			t.Errorf("%s written as %s", mime, format)
		}
		if out.Bounds().Dx() != 800 || out.Bounds().Dy() != 600 {
			t.Errorf("%s: size %v", mime, out.Bounds())
		}
		// the watermark changed pixels on both halves, and the footer is near the foot
		changed := func(x0, y0, x1, y1 int) int {
			n := 0
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					r1, g1, b1, _ := out.At(x, y).RGBA()
					r0, g0, b0, _ := src.At(x, y).RGBA()
					if d := int(r1>>8) - int(r0>>8) + int(g1>>8) - int(g0>>8) + int(b1>>8) - int(b0>>8); d > 24 || d < -24 {
						n++
					}
				}
			}
			return n
		}
		if changed(100, 150, 400, 450) < 200 || changed(450, 150, 750, 450) < 200 {
			t.Errorf("%s: the watermark is not there (dark %d, light %d)", mime, changed(100, 150, 400, 450), changed(450, 150, 750, 450))
		}
		if changed(250, 560, 550, 598) < 200 {
			t.Errorf("%s: no footer", mime)
		}
	}
}

func TestApplyImageTurnsUpright(t *testing.T) {
	if !ImageAvailable() {
		t.Skip("no Chinese font")
	}
	p := write(t, "a.png", func(f *os.File) error { return png.Encode(f, picture(400, 200)) })
	for o, corner := range map[int]image.Point{6: {199 - 5, 5}, 8: {5, 399 - 5}, 3: {399 - 5, 199 - 5}, 1: {5, 5}} {
		dst := filepath.Join(t.TempDir(), "out")
		if err := ApplyImage(p, dst, "image/png", o, "水印", ""); err != nil {
			t.Fatal(err)
		}
		out, _ := readImage(t, dst)
		w, h := out.Bounds().Dx(), out.Bounds().Dy()
		if (o == 6 || o == 8) != (w == 200 && h == 400) {
			t.Errorf("orientation %d: %dx%d", o, w, h)
		}
		if r, g, _, _ := out.At(corner.X, corner.Y).RGBA(); r>>8 < 200 || g>>8 > 60 {
			t.Errorf("orientation %d: the red corner is not at %v", o, corner)
		}
	}
}

func TestApplyImageRefuses(t *testing.T) {
	if !ImageAvailable() {
		t.Skip("no Chinese font")
	}
	// a PNG that says it is 10000×10000, without the pixels
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr, 10000)
	binary.BigEndian.PutUint32(ihdr[4:], 10000)
	ihdr[8], ihdr[9] = 8, 2
	binary.Write(&b, binary.BigEndian, uint32(13))
	b.WriteString("IHDR")
	b.Write(ihdr)
	binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(append([]byte("IHDR"), ihdr...)))
	p := filepath.Join(t.TempDir(), "big.png")
	os.WriteFile(p, b.Bytes(), 0o644)
	if err := ApplyImage(p, p+".out", "image/png", 0, "x", ""); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too large: %v", err)
	}
	if err := ApplyImage(p, p+".out", "image/heic", 0, "x", ""); err == nil {
		t.Error("a HEIC picture cannot be drawn on")
	}
	if ImageType("image/heic") != "" || ImageType("image/webp") != "image/jpeg" || ImageType("image/gif") != "image/png" {
		t.Error("ImageType")
	}
}
