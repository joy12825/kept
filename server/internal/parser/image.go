package parser

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
)

// flattenAlpha returns a copy of a PNG with transparency composited onto
// white, because ocrmypdf refuses images with an alpha channel (phone
// screenshots are usually RGBA). Other files are returned unchanged.
func flattenAlpha(src, workDir string) (string, error) {
	b, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	if !pngHasAlpha(b) {
		return src, nil
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return "", &Error{Message: ErrCorrupt.Message, Permanent: true, Err: err}
	}
	flat := image.NewRGBA(img.Bounds())
	draw.Draw(flat, flat.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), img, img.Bounds().Min, draw.Over)
	// An opaque *image.RGBA is written as RGB without alpha.
	var out bytes.Buffer
	if err := png.Encode(&out, flat); err != nil {
		return "", err
	}
	dst := filepath.Join(workDir, "flat.png")
	return dst, os.WriteFile(dst, withPHYs(out.Bytes(), b), 0o600)
}

// withPHYs copies the resolution chunk (pHYs) of orig into encoded, right
// after IHDR. Go's encoder drops it, and OCR quality depends on the DPI.
func withPHYs(encoded, orig []byte) []byte {
	chunk := pngChunk(orig, "pHYs")
	if chunk == nil || len(encoded) < 33 {
		return encoded
	}
	out := make([]byte, 0, len(encoded)+len(chunk))
	out = append(out, encoded[:33]...) // signature + IHDR
	out = append(out, chunk...)
	return append(out, encoded[33:]...)
}

// pngChunk returns the whole chunk (length, type, data, CRC) of the given
// type, or nil.
func pngChunk(b []byte, typ string) []byte {
	for i := 8; i+12 <= len(b); {
		n := int(binary.BigEndian.Uint32(b[i:]))
		end := i + 12 + n
		if n < 0 || end > len(b) {
			return nil
		}
		switch string(b[i+4 : i+8]) {
		case typ:
			return b[i:end]
		case "IDAT":
			return nil
		}
		i = end
	}
	return nil
}

// pngHasAlpha reports whether b is a PNG whose colour type carries alpha
// (grey+alpha or RGBA) or which has a transparency chunk.
func pngHasAlpha(b []byte) bool {
	const sig = "\x89PNG\r\n\x1a\n"
	if len(b) < 33 || string(b[:8]) != sig {
		return false
	}
	if ct := b[25]; ct == 4 || ct == 6 { // IHDR colour type
		return true
	}
	return pngChunk(b, "tRNS") != nil
}
