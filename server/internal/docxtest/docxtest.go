// Package docxtest makes Word documents (v1.9.8) for tests and the demo:
// the parts are given as WordprocessingML, and the package around them —
// content types, relationships, media — is written here.
package docxtest

import (
	"archive/zip"
	"bytes"
	"fmt"
	"sort"
)

// Namespaces for the snippets.
const (
	W   = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`
	R   = `xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
	All = W + ` ` + R + ` xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"` +
		` xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"` +
		` xmlns:wps="http://schemas.microsoft.com/office/word/2010/wordprocessingShape" xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"`
)

// Rel is a relationship of the main document.
type Rel struct {
	ID, Type, Target string // Type: "image", "hyperlink" …
	External         bool
}

// Doc is a Word document: the body's XML (what goes inside w:body), and
// optionally the styles, numbering and footnotes parts' content.
type Doc struct {
	Body      string
	Styles    string // inside w:styles
	Numbering string // inside w:numbering
	Footnotes string // inside w:footnotes
	Rels      []Rel
	Media     map[string][]byte // word/media/<name>
}

// Build writes the .docx.
func (d Doc) Build() []byte {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	put := func(name, body string) {
		w, _ := z.Create(name)
		w.Write([]byte(body))
	}
	ct := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>` +
		`<Default Extension="png" ContentType="image/png"/><Default Extension="jpeg" ContentType="image/jpeg"/><Default Extension="emf" ContentType="image/x-emf"/>` +
		`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>`
	rels := d.Rels
	add := func(id, typ, target, ctype, part string) {
		rels = append(rels, Rel{ID: id, Type: typ, Target: target})
		ct += `<Override PartName="/word/` + part + `" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.` + ctype + `+xml"/>`
	}
	if d.Styles != "" {
		add("rIdStyles", "styles", "styles.xml", "styles", "styles.xml")
		put("word/styles.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:styles `+W+`>`+d.Styles+`</w:styles>`)
	}
	if d.Numbering != "" {
		add("rIdNumbering", "numbering", "numbering.xml", "numbering", "numbering.xml")
		put("word/numbering.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:numbering `+W+`>`+d.Numbering+`</w:numbering>`)
	}
	if d.Footnotes != "" {
		add("rIdFootnotes", "footnotes", "footnotes.xml", "footnotes", "footnotes.xml")
		put("word/footnotes.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:footnotes `+All+`>`+d.Footnotes+`</w:footnotes>`)
	}
	put("[Content_Types].xml", ct+`</Types>`)
	put("_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`)
	var rb bytes.Buffer
	rb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for _, r := range rels {
		mode := ""
		if r.External {
			mode = ` TargetMode="External"`
		}
		fmt.Fprintf(&rb, `<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/%s" Target="%s"%s/>`, r.ID, r.Type, xmlAttr(r.Target), mode)
	}
	rb.WriteString(`</Relationships>`)
	put("word/_rels/document.xml.rels", rb.String())
	put("word/document.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document `+All+`><w:body>`+d.Body+
		`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr></w:body></w:document>`)
	names := make([]string, 0, len(d.Media))
	for n := range d.Media {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		w, _ := z.Create("word/media/" + n)
		w.Write(d.Media[n])
	}
	z.Close()
	return b.Bytes()
}

func xmlAttr(s string) string {
	var b bytes.Buffer
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '"':
			b.WriteString("&quot;")
		case '<':
			b.WriteString("&lt;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// P is a paragraph of plain runs.
func P(texts ...string) string {
	s := `<w:p>`
	for _, t := range texts {
		s += `<w:r><w:t xml:space="preserve">` + t + `</w:t></w:r>`
	}
	return s + `</w:p>`
}

// Image is an inline picture of rel id, w × h px.
func Image(id string, w, h int) string {
	return fmt.Sprintf(`<w:r><w:drawing><wp:inline><wp:extent cx="%d" cy="%d"/><wp:docPr id="1" name="图片"/><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture">`+
		`<pic:pic><pic:nvPicPr><pic:cNvPr id="0" name="图片"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="%s"/></pic:blipFill><pic:spPr/></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`,
		w*9525, h*9525, id)
}
