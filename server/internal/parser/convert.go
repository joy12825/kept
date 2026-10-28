package parser

import (
	"bytes"
	"context"
	"image"
	"image/draw"
	_ "image/gif" // first frame
	"image/png"
	"os"
	"os/exec"
	"path/filepath"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

// More image formats (v1.5.5, after paperless-ngx): iPhone photos (HEIC),
// pictures saved from WeChat (WebP), GIF and BMP. They are turned into a
// PNG that goes through the usual image path (OCR, searchable PDF); the
// original keeps its format.

// convertTypes are the formats converted to PNG before OCR.
var convertTypes = map[string]string{
	"image/heic": "heic",
	"image/heif": "heif",
	"image/webp": "webp",
	"image/gif":  "gif",
	"image/bmp":  "bmp",
}

// HEICTool is the libheif command that decodes HEIC (heif-dec; older
// libheif calls it heif-convert). Without it HEIC is refused.
var HEICTool = func() string {
	for _, name := range []string{"heif-dec", "heif-convert"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// ErrHEICUnavailable: the server has no libheif tool.
var ErrHEICUnavailable = &Error{Message: "服务器没有安装 HEIC 解码工具（libheif），请把照片转成 JPG 再上传", Permanent: true}

const maxPixels = 80_000_000 // a 80-megapixel scan is the most decoded

// toPNG converts src (one of convertTypes) into a PNG in workDir.
func toPNG(ctx context.Context, mime, src, workDir string) (string, error) {
	dst := filepath.Join(workDir, "converted.png")
	if mime == "image/heic" || mime == "image/heif" {
		tool := HEICTool()
		if tool == "" {
			return "", ErrHEICUnavailable
		}
		// Only the primary image; a Live Photo's or burst's others are ignored.
		if _, err := run(ctx, Timeout(1), tool, "--quiet", src, dst); err != nil {
			return "", &Error{Message: ErrCorrupt.Message, Permanent: true, Err: err}
		}
		return dst, nil
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return "", &Error{Message: ErrCorrupt.Message, Permanent: true, Err: err}
	}
	if cfg.Width*cfg.Height > maxPixels {
		return "", &Error{Message: "图片太大，无法处理", Permanent: true}
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return "", &Error{Message: ErrCorrupt.Message, Permanent: true, Err: err}
	}
	// Onto white: ocrmypdf refuses transparency, and GIF/WebP often have it.
	flat := image.NewRGBA(img.Bounds())
	draw.Draw(flat, flat.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), img, img.Bounds().Min, draw.Over)
	f, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	if err := png.Encode(f, flat); err != nil {
		f.Close()
		return "", err
	}
	return dst, f.Close()
}

// IsConverted reports whether a document's original is an image converted
// before OCR (its archive is the only page image the browser can show).
func IsConverted(mime string) bool { _, ok := convertTypes[mime]; return ok }
