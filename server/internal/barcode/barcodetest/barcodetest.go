// Package barcodetest makes scanned-looking PDFs with barcodes for tests.
package barcodetest

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"strings"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/oned"
	"github.com/makiuchi-d/gozxing/qrcode"
)

// Blank is an empty A4 page at 150 DPI.
func Blank() image.Image {
	page := image.NewGray(image.Rect(0, 0, 1240, 1754))
	for i := range page.Pix {
		page.Pix[i] = 255
	}
	return page
}

// QR is a page with a QR code holding text.
func QR(text string) image.Image {
	return draw(qrcode.NewQRCodeWriter(), gozxing.BarcodeFormat_QR_CODE, text, 300, 300)
}

// Code128 is a page with a Code 128 barcode holding text.
func Code128(text string) image.Image {
	return draw(oned.NewCode128Writer(), gozxing.BarcodeFormat_CODE_128, text, 500, 120)
}

func draw(w gozxing.Writer, format gozxing.BarcodeFormat, text string, cw, ch int) image.Image {
	m, err := w.Encode(text, format, cw, ch, nil)
	if err != nil {
		panic(err)
	}
	page := Blank().(*image.Gray)
	for y := 0; y < m.GetHeight(); y++ {
		for x := 0; x < m.GetWidth(); x++ {
			if m.Get(x, y) {
				page.SetGray(200+x, 200+y, color.Gray{})
			}
		}
	}
	return page
}

// PDF puts one image per A4 page (JPEG, like a scanner).
func PDF(pages ...image.Image) []byte {
	var objs []string
	var streams [][]byte
	add := func(body string, stream []byte) int {
		objs = append(objs, body)
		streams = append(streams, stream)
		return len(objs)
	}
	add("<< /Type /Catalog /Pages 2 0 R >>", nil)
	add("", nil)
	var kids []string
	for _, img := range pages {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
			panic(err)
		}
		b := img.Bounds()
		im := add(fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>", b.Dx(), b.Dy(), buf.Len()), buf.Bytes())
		content := []byte(fmt.Sprintf("q 595 0 0 842 0 0 cm /Im%d Do Q", im))
		c := add(fmt.Sprintf("<< /Length %d >>", len(content)), content)
		p := add(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /XObject << /Im%d %d 0 R >> >> /Contents %d 0 R >>", im, im, c), nil)
		kids = append(kids, fmt.Sprintf("%d 0 R", p))
	}
	objs[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pages))
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\n", i+1, o)
		if streams[i] != nil {
			out.WriteString("stream\n")
			out.Write(streams[i])
			out.WriteString("\nendstream\n")
		}
		out.WriteString("endobj\n")
	}
	x := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&out, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return out.Bytes()
}
