package watermark

import (
	"bufio"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"os"

	"golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	xfont "golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/f64"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/tiff"
	"golang.org/x/image/webp"
)

// Pictures (v1.11.2): the same watermark drawn on the picture itself, so a
// photo shared or downloaded with a watermark stays a photo. It needs the
// Chinese font only, not qpdf.

// ErrTooLarge: a picture too large to draw on.
var ErrTooLarge = errors.New("watermark: the picture is too large")

// MaxPixels bounds the pictures drawn on.
const MaxPixels = 50_000_000

// imageOut is what a picture of each type is written as.
var imageOut = map[string]string{
	"image/jpeg": "image/jpeg", "image/webp": "image/jpeg", "image/bmp": "image/jpeg", "image/tiff": "image/jpeg",
	"image/png": "image/png", "image/gif": "image/png",
}

// ImageType is the type a picture of type mime is written as with a
// watermark, or "" when it cannot have one.
func ImageType(mime string) string { return imageOut[mime] }

// ImageAvailable reports whether pictures can have a watermark here.
func ImageAvailable() bool { return Font() != "" }

func decode(f *os.File, mime string) (image.Image, error) {
	r := bufio.NewReader(f)
	switch mime {
	case "image/jpeg":
		return jpeg.Decode(r)
	case "image/png":
		return png.Decode(r)
	case "image/gif":
		return gif.Decode(r)
	case "image/webp":
		return webp.Decode(r)
	case "image/bmp":
		return bmp.Decode(r)
	case "image/tiff":
		return tiff.Decode(r)
	}
	return nil, errors.New("watermark: not a picture")
}

func decodeConfig(f *os.File, mime string) (image.Config, error) {
	r := bufio.NewReader(f)
	switch mime {
	case "image/jpeg":
		return jpeg.DecodeConfig(r)
	case "image/png":
		return png.DecodeConfig(r)
	case "image/gif":
		return gif.DecodeConfig(r)
	case "image/webp":
		return webp.DecodeConfig(r)
	case "image/bmp":
		return bmp.DecodeConfig(r)
	case "image/tiff":
		return tiff.DecodeConfig(r)
	}
	return image.Config{}, errors.New("watermark: not a picture")
}

// ApplyImage writes the picture src (of type mime, turned upright by the
// EXIF orientation) with the watermark text and footer to dst, as
// ImageType(mime). Its EXIF is not kept.
func ApplyImage(src, dst, mime string, orientation int, text, footer string) error {
	out := ImageType(mime)
	if out == "" {
		return errors.New("watermark: not a picture")
	}
	fb, err := font()
	if err != nil {
		return err
	}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	cfg, err := decodeConfig(f, mime)
	if err != nil {
		return err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return ErrTooLarge
	}
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	img, err := decode(f, mime)
	if err != nil {
		return err
	}
	canvas := upright(img, orientation)
	face, err := faces(fb)
	if err != nil {
		return err
	}
	stamp(canvas, face, text, footer)
	w, err := os.Create(dst)
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(w)
	if out == "image/png" {
		err = png.Encode(bw, canvas)
	} else {
		err = jpeg.Encode(bw, canvas, &jpeg.Options{Quality: 90})
	}
	if err == nil {
		err = bw.Flush()
	}
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst)
	}
	return err
}

// upright draws img on a new canvas turned as the EXIF orientation says.
func upright(img image.Image, o int) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if o >= 5 && o <= 8 {
		w, h = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	if o <= 1 || o > 8 {
		draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
		return dst
	}
	// where the source pixel (x, y) goes, for each orientation
	sw, sh := float64(b.Dx()), float64(b.Dy())
	var m f64.Aff3
	switch o {
	case 2: // mirrored
		m = f64.Aff3{-1, 0, sw, 0, 1, 0}
	case 3: // upside down
		m = f64.Aff3{-1, 0, sw, 0, -1, sh}
	case 4:
		m = f64.Aff3{1, 0, 0, 0, -1, sh}
	case 5:
		m = f64.Aff3{0, 1, 0, 1, 0, 0}
	case 6: // turned right
		m = f64.Aff3{0, -1, sh, 1, 0, 0}
	case 7:
		m = f64.Aff3{0, -1, sh, -1, 0, sw}
	case 8: // turned left
		m = f64.Aff3{0, 1, 0, -1, 0, sw}
	}
	m[2] -= m[0]*float64(b.Min.X) + m[1]*float64(b.Min.Y)
	m[5] -= m[3]*float64(b.Min.X) + m[4]*float64(b.Min.Y)
	draw.NearestNeighbor.Transform(dst, m, img, b, draw.Src, nil)
	return dst
}

type fontFaces struct {
	f *sfnt.Font
}

func faces(data []byte) (fontFaces, error) {
	f, err := opentype.Parse(data)
	if err != nil {
		c, cerr := opentype.ParseCollection(data)
		if cerr != nil {
			return fontFaces{}, err
		}
		if f, err = c.Font(0); err != nil {
			return fontFaces{}, err
		}
	}
	return fontFaces{f}, nil
}

func (ff fontFaces) at(size float64) (xfont.Face, error) {
	return opentype.NewFace(ff.f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: xfont.HintingNone})
}

// textImage draws text in c on a transparent picture just large enough.
func textImage(face xfont.Face, text string, c color.NRGBA, pad int) *image.RGBA {
	m := face.Metrics()
	w := xfont.MeasureString(face, text).Ceil()
	h := (m.Ascent + m.Descent).Ceil()
	img := image.NewRGBA(image.Rect(0, 0, w+2*pad, h+2*pad))
	d := xfont.Drawer{Dst: img, Src: image.NewUniform(c), Face: face, Dot: fixed.P(pad, pad+m.Ascent.Ceil())}
	d.DrawString(text)
	return img
}

// stamp draws the watermark as on a PDF page: the text tiled diagonally,
// light, and the footer at the foot. Each line is drawn in grey over a
// faint white shadow, to show on dark pictures too.
func stamp(canvas *image.RGBA, ff fontFaces, text, footer string) {
	b := canvas.Bounds()
	w, h := float64(b.Dx()), float64(b.Dy())
	size := math.Max(14, math.Min(w, h)/18)
	face, err := ff.at(size)
	if err != nil {
		return
	}
	shadow := textImage(face, text, color.NRGBA{255, 255, 255, 46}, 2)
	tile := image.NewRGBA(shadow.Bounds())
	draw.Draw(tile, tile.Bounds().Add(image.Pt(1, 1)), shadow, image.Point{}, draw.Over)
	draw.Draw(tile, tile.Bounds(), textImage(face, text, color.NRGBA{90, 90, 90, 56}, 2), image.Point{}, draw.Over)
	tw, th := float64(tile.Bounds().Dx()), float64(tile.Bounds().Dy())
	cx, cy := w/2, h/2
	diag := math.Hypot(w, h)
	stepX, stepY := tw+size*3, size*5
	sin, cos := math.Sincos(-30 * math.Pi / 180) // rising to the right
	row := 0
	for y := -diag / 2; y <= diag/2; y += stepY {
		shift := float64(row%2) * stepX / 2 // brick pattern
		for x := -diag/2 - shift; x <= diag/2; x += stepX {
			// the tile's top left, turned about the centre
			px, py := x, y-th
			m := f64.Aff3{cos, -sin, cx + cos*px - sin*py, sin, cos, cy + sin*px + cos*py}
			// skip tiles that land outside
			ex, ey := m[2]+cos*tw/2, m[5]+sin*tw/2
			if ex < -tw || ex > w+tw || ey < -tw || ey > h+tw {
				continue
			}
			draw.BiLinear.Transform(canvas, m, tile, tile.Bounds(), draw.Over, nil)
		}
		row++
	}
	if footer == "" {
		return
	}
	fsize := math.Max(12, math.Min(w, h)/60)
	fface, err := ff.at(fsize)
	if err != nil {
		return
	}
	line := textImage(fface, footer, color.NRGBA{60, 60, 60, 220}, int(fsize/3))
	lw, lh := line.Bounds().Dx(), line.Bounds().Dy()
	at := image.Pt((b.Dx()-lw)/2, b.Dy()-lh-int(fsize/2))
	box := image.Rectangle{Min: at, Max: at.Add(image.Pt(lw, lh))}
	draw.Draw(canvas, box, image.NewUniform(color.NRGBA{255, 255, 255, 150}), image.Point{}, draw.Over)
	draw.Draw(canvas, box, line, image.Point{}, draw.Over)
}
