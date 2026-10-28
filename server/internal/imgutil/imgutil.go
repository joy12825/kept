// Package imgutil shrinks images for covers and thumbnails with the
// standard library only.
package imgutil

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	_ "image/gif" // EPUB covers may be GIF
	"image/jpeg"
	_ "image/png"
)

// ErrFormat means the data is not a JPEG, PNG or GIF image.
var ErrFormat = errors.New("not a JPEG, PNG or GIF image")

// ToJPEG decodes data and re-encodes it as a JPEG at most width pixels
// wide; formats lists what is accepted ("jpeg", "png", "gif"; none: all).
func ToJPEG(data []byte, width int, formats ...string) ([]byte, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, ErrFormat
	}
	if len(formats) > 0 && !contains(formats, format) {
		return nil, ErrFormat
	}
	// Refuse absurd dimensions before allocating (a 40000×40000 PNG is tiny on disk).
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > 50_000_000 {
		return nil, ErrFormat
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrFormat
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, Scale(img, width), &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Scale shrinks img to at most w pixels wide by averaging the source
// pixels that fall into each target pixel (box filter); smaller images
// keep their size. Transparent parts become white.
func Scale(img image.Image, w int) image.Image {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= 0 || sh <= 0 {
		return img
	}
	tw, th := sw, sh
	if sw > w {
		tw, th = w, max(1, sh*w/sw)
	}
	out := image.NewRGBA(image.Rect(0, 0, tw, th))
	for y := 0; y < th; y++ {
		y0, y1 := y*sh/th, max((y+1)*sh/th, y*sh/th+1)
		for x := 0; x < tw; x++ {
			x0, x1 := x*sw/tw, max((x+1)*sw/tw, x*sw/tw+1)
			var r, g, bl, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, ca := img.At(b.Min.X+sx, b.Min.Y+sy).RGBA()
					white := 0xffff - ca // composite onto white
					r, g, bl, n = r+uint64(cr+white), g+uint64(cg+white), bl+uint64(cb+white), n+1
				}
			}
			out.Set(x, y, color.RGBA64{R: uint16(r / n), G: uint16(g / n), B: uint16(bl / n), A: 0xffff})
		}
	}
	return out
}
