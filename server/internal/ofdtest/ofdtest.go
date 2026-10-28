// Package ofdtest makes small OFD files (v1.9.3): an e-invoice laid out
// like the real ones (a template page with the labels and the table's
// lines, the values on the page, a red stamp picture), for tests and the
// demo.
package ofdtest

import (
	"archive/zip"
	"bytes"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/png"
	"strings"
)

// Invoice describes the e-invoice.
type Invoice struct {
	Number, Date, Seller, Buyer, Item, Amount string
	// Pages: more than one repeats the page with "第 n 页" (default 1).
	Pages int
	// Sign signs it with an electronic seal (v1.11.3); nil for none.
	Sign *Signer
}

const ns = `xmlns:ofd="http://www.ofdspec.org/2016"`

// text is a TextObject at x, y (mm) with each glyph's advance given.
func text(id int, x, y, size float64, s string) string {
	// each glyph's advance: half the size for Latin letters and digits
	var adv []string
	width := 0.0
	for i, r := range []rune(s) {
		w := size
		if r < 0x80 {
			w = size * 0.55
		}
		width += w
		if i < len([]rune(s))-1 {
			adv = append(adv, fmt.Sprintf("%.2f", w))
		}
	}
	delta := ""
	if len(adv) > 0 {
		delta = ` DeltaX="` + strings.Join(adv, " ") + `"`
	}
	return fmt.Sprintf(`<ofd:TextObject ID="%d" Boundary="%.2f %.2f %.2f %.2f" Font="1" Size="%.2f"><ofd:FillColor Value="0 0 0"/><ofd:TextCode X="0" Y="%.2f"%s>%s</ofd:TextCode></ofd:TextObject>`,
		id, x, y, width+2, size+1, size, size, delta, html.EscapeString(s))
}

func line(id int, x1, y1, x2, y2 float64) string {
	return fmt.Sprintf(`<ofd:PathObject ID="%d" Boundary="0 0 210 140" LineWidth="0.25"><ofd:StrokeColor Value="128 0 0"/><ofd:AbbreviatedData>M %.2f %.2f L %.2f %.2f</ofd:AbbreviatedData></ofd:PathObject>`, id, x1, y1, x2, y2)
}

// stamp is a red ring, as a PNG.
func stamp() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 120, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 120; x++ {
			dx, dy := float64(x-60)/58, float64(y-40)/38
			if r := dx*dx + dy*dy; r <= 1 && r >= 0.8 {
				img.Set(x, y, color.NRGBA{200, 30, 30, 230})
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// Build makes the OFD file.
func (in Invoice) Build() []byte {
	if in.Pages < 1 {
		in.Pages = 1
	}
	files := map[string]string{
		"OFD.xml":               `<?xml version="1.0" encoding="UTF-8"?><ofd:OFD ` + ns + ` Version="1.1" DocType="OFD"><ofd:DocBody><ofd:DocInfo><ofd:DocID>demo</ofd:DocID></ofd:DocInfo><ofd:DocRoot>Doc_0/Document.xml</ofd:DocRoot></ofd:DocBody></ofd:OFD>`,
		"Doc_0/PublicRes.xml":   `<?xml version="1.0" encoding="UTF-8"?><ofd:Res ` + ns + ` BaseLoc="Res"><ofd:Fonts><ofd:Font ID="1" FontName="宋体" FamilyName="宋体"/></ofd:Fonts></ofd:Res>`,
		"Doc_0/DocumentRes.xml": `<?xml version="1.0" encoding="UTF-8"?><ofd:Res ` + ns + ` BaseLoc="Res"><ofd:MultiMedias><ofd:MultiMedia ID="90" Type="Image" Format="PNG"><ofd:MediaFile>stamp.png</ofd:MediaFile></ofd:MultiMedia></ofd:MultiMedias></ofd:Res>`,
	}
	var pages strings.Builder
	for i := 0; i < in.Pages; i++ {
		fmt.Fprintf(&pages, `<ofd:Page ID="%d" BaseLoc="Pages/Page_%d/Content.xml"/>`, 10+i, i)
	}
	files["Doc_0/Document.xml"] = `<?xml version="1.0" encoding="UTF-8"?><ofd:Document ` + ns + `><ofd:CommonData><ofd:MaxUnitID>999</ofd:MaxUnitID>` +
		`<ofd:PageArea><ofd:PhysicalBox>0 0 210 140</ofd:PhysicalBox></ofd:PageArea><ofd:PublicRes>PublicRes.xml</ofd:PublicRes><ofd:DocumentRes>DocumentRes.xml</ofd:DocumentRes>` +
		`<ofd:TemplatePage ID="2" BaseLoc="Tpls/Tpl_0/Content.xml"/></ofd:CommonData><ofd:Pages>` + pages.String() + `</ofd:Pages></ofd:Document>`
	// the template: title, labels and the table's lines
	tpl := text(100, 70, 8, 6, "电子发票（普通发票）") +
		text(101, 10, 30, 3.5, "购买方：") + text(102, 10, 45, 3.5, "销售方：") +
		text(103, 10, 62, 3.5, "项目名称") + text(104, 150, 62, 3.5, "金额") +
		text(105, 10, 100, 3.5, "价税合计（小写）") +
		line(106, 8, 25, 202, 25) + line(107, 8, 58, 202, 58) + line(108, 8, 95, 202, 95)
	files["Doc_0/Tpls/Tpl_0/Content.xml"] = `<?xml version="1.0" encoding="UTF-8"?><ofd:Page ` + ns + `><ofd:Content><ofd:Layer ID="3" Type="Background">` + tpl + `</ofd:Layer></ofd:Content></ofd:Page>`
	for i := 0; i < in.Pages; i++ {
		body := text(200, 150, 16, 3.5, "发票号码："+in.Number) + text(201, 150, 21, 3.5, "开票日期："+in.Date) +
			text(202, 30, 30, 3.5, in.Buyer) + text(203, 30, 45, 3.5, in.Seller) +
			text(204, 10, 70, 3.5, in.Item) + text(205, 150, 70, 3.5, in.Amount) +
			text(206, 60, 100, 3.5, "¥"+in.Amount)
		if in.Pages > 1 {
			body += text(207, 95, 130, 3, fmt.Sprintf("第 %d 页", i+1))
		}
		// the PageBlock nests objects, as real files do
		body += `<ofd:PageBlock ID="208"><ofd:ImageObject ID="209" Boundary="160 100 30 20" CTM="30 0 0 20 0 0" ResourceID="90"/></ofd:PageBlock>`
		files[fmt.Sprintf("Doc_0/Pages/Page_%d/Content.xml", i)] = `<?xml version="1.0" encoding="UTF-8"?><ofd:Page ` + ns + `><ofd:Template TemplateID="2" ZOrder="Background"/>` +
			`<ofd:Content><ofd:Layer ID="4">` + body + `</ofd:Layer></ofd:Content></ofd:Page>`
	}
	var entries []entry
	for _, name := range []string{"OFD.xml", "Doc_0/Document.xml", "Doc_0/PublicRes.xml", "Doc_0/DocumentRes.xml", "Doc_0/Tpls/Tpl_0/Content.xml"} {
		entries = append(entries, entry{name, []byte(files[name])})
	}
	for i := 0; i < in.Pages; i++ {
		name := fmt.Sprintf("Doc_0/Pages/Page_%d/Content.xml", i)
		entries = append(entries, entry{name, []byte(files[name])})
	}
	entries = append(entries, entry{"Doc_0/Res/stamp.png", stamp()})
	if in.Sign != nil {
		entries = in.Sign.apply(entries, in.Amount)
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, e := range entries {
		w, _ := z.Create(e.name)
		w.Write(e.data)
	}
	z.Close()
	return b.Bytes()
}

type entry struct {
	name string
	data []byte
}

type zipWriter struct{ z *zip.Writer }

func newZip(b *bytes.Buffer) zipWriter { return zipWriter{zip.NewWriter(b)} }

func (w zipWriter) add(name string, data []byte) {
	f, _ := w.z.Create(name)
	f.Write(data)
}

func (w zipWriter) close() { w.z.Close() }
