// Package epubtest builds small EPUBs for tests and the demo data.
package epubtest

import (
	"archive/zip"
	"bytes"
	"fmt"
	"html"
	"strings"
)

// Book describes the EPUB to build.
type Book struct {
	Title, Publisher, Date, Language, Description, ISBN, Series string
	Authors                                                     []string
	Chapters                                                    []Chapter
	Cover                                                       []byte // image bytes; nil for none
	CoverType                                                   string // media type, default image/png
	EPUB2                                                       bool   // EPUB 2 cover meta instead of EPUB 3 properties
	Extra                                                       map[string][]byte
}

// Chapter is one XHTML page of the spine.
type Chapter struct {
	Title string
	Paras []string
}

// Build returns the EPUB file. The OPF lives in OEBPS/ so hrefs are relative.
func Build(b Book) []byte {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	// mimetype first and stored, as the spec asks.
	w, _ := z.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	_, _ = w.Write([]byte("application/epub+zip"))
	put := func(name, body string) {
		w, _ := z.Create(name)
		_, _ = w.Write([]byte(body))
	}
	put("META-INF/container.xml", `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`)
	var meta, manifest, spine strings.Builder
	esc := html.EscapeString
	fmt.Fprintf(&meta, "<dc:title>%s</dc:title>", esc(b.Title))
	for _, a := range b.Authors {
		fmt.Fprintf(&meta, "<dc:creator>%s</dc:creator>", esc(a))
	}
	for k, v := range map[string]string{"publisher": b.Publisher, "date": b.Date, "language": b.Language, "description": b.Description} {
		if v != "" {
			fmt.Fprintf(&meta, "<dc:%s>%s</dc:%s>", k, esc(v), k)
		}
	}
	if b.ISBN != "" {
		fmt.Fprintf(&meta, `<dc:identifier id="isbn">urn:isbn:%s</dc:identifier>`, esc(b.ISBN))
	}
	fmt.Fprintf(&meta, `<dc:identifier id="uid">urn:uuid:%s</dc:identifier>`, "00000000-0000-4000-8000-000000000000")
	if b.Series != "" {
		fmt.Fprintf(&meta, `<meta name="calibre:series" content="%s"/>`, esc(b.Series))
	}
	if b.Cover != nil {
		ct := b.CoverType
		if ct == "" {
			ct = "image/png"
		}
		ext := strings.TrimPrefix(ct, "image/")
		if b.EPUB2 {
			fmt.Fprintf(&meta, `<meta name="cover" content="img1"/>`)
			fmt.Fprintf(&manifest, `<item id="img1" href="images/front.%s" media-type="%s"/>`, ext, ct)
		} else {
			fmt.Fprintf(&manifest, `<item id="img1" href="images/front.%s" media-type="%s" properties="cover-image"/>`, ext, ct)
		}
		w, _ := z.Create("OEBPS/images/front." + ext)
		_, _ = w.Write(b.Cover)
	}
	// The table of contents: a nav document (EPUB 3) or an NCX (EPUB 2).
	var navList, ncx strings.Builder
	for i, c := range b.Chapters {
		fmt.Fprintf(&navList, `<li><a href="text/ch%d.xhtml">%s</a></li>`, i+1, esc(c.Title))
		fmt.Fprintf(&ncx, `<navPoint id="n%d" playOrder="%d"><navLabel><text>%s</text></navLabel><content src="text/ch%d.xhtml"/></navPoint>`, i+1, i+1, esc(c.Title), i+1)
	}
	if b.EPUB2 {
		manifest.WriteString(`<item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>`)
		put("OEBPS/toc.ncx", `<?xml version="1.0" encoding="utf-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1"><head/><docTitle><text>`+esc(b.Title)+`</text></docTitle><navMap>`+ncx.String()+`</navMap></ncx>`)
	} else {
		manifest.WriteString(`<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>`)
		put("OEBPS/nav.xhtml", `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><head><title>目录</title></head>
<body><nav epub:type="toc"><ol>`+navList.String()+`</ol></nav></body></html>`)
	}
	for i, c := range b.Chapters {
		name := fmt.Sprintf("text/ch%d.xhtml", i+1)
		fmt.Fprintf(&manifest, `<item id="ch%d" href="%s" media-type="application/xhtml+xml"/>`, i+1, name)
		fmt.Fprintf(&spine, `<itemref idref="ch%d"/>`, i+1)
		var body strings.Builder
		fmt.Fprintf(&body, "<h1>%s</h1>", esc(c.Title))
		for _, p := range c.Paras {
			fmt.Fprintf(&body, "<p>%s</p>", esc(p))
		}
		put("OEBPS/"+name, `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>`+esc(c.Title)+`</title><style>p { margin: 0 }</style></head>
<body>`+body.String()+`</body></html>`)
	}
	version := "3.0"
	if b.EPUB2 {
		version = "2.0"
	}
	put("OEBPS/content.opf", `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="`+version+`" unique-identifier="uid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">`+meta.String()+`</metadata>
  <manifest>`+manifest.String()+`</manifest>
  <spine`+map[bool]string{true: ` toc="ncx"`, false: ""}[b.EPUB2]+`>`+spine.String()+`</spine>
</package>`)
	for name, body := range b.Extra {
		w, _ := z.Create(name)
		_, _ = w.Write(body)
	}
	_ = z.Close()
	return buf.Bytes()
}
