package parser

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"path"
	"slices"
	"strconv"
	"strings"
)

// Office text without a conversion service (v1.9.1): the zipped XML
// formats (docx, xlsx, pptx and the OpenDocument ones) carry their text in
// plain XML parts, read here with the standard library so the document can
// be searched while its PDF waits for Gotenberg. The old binary formats
// (doc, xls, ppt, rtf) give no text.

// Limits on what one file may make us read.
const (
	officePartMax = 64 << 20 // uncompressed bytes of one XML part
	officeTextMax = 2 << 20  // bytes of text kept
	officeParts   = 500      // parts read (slides)
)

// officeText reads the text of the Office file at src ("" when there is
// none or the file cannot be read).
func officeText(src, ext string) string {
	z, err := zip.OpenReader(src)
	if err != nil {
		return ""
	}
	defer z.Close()
	var parts []*zip.File
	byName := map[string]*zip.File{}
	for _, f := range z.File {
		byName[f.Name] = f
	}
	switch ext {
	case "docx":
		parts = appendPart(parts, byName["word/document.xml"])
	case "xlsx":
		parts = appendPart(parts, byName["xl/sharedStrings.xml"])
	case "pptx":
		var slides []*zip.File
		for _, f := range z.File {
			if strings.HasPrefix(f.Name, "ppt/slides/slide") && path.Ext(f.Name) == ".xml" {
				slides = append(slides, f)
			}
		}
		slices.SortFunc(slides, func(a, b *zip.File) int { return slideNumber(a.Name) - slideNumber(b.Name) })
		parts = slides
	case "odt", "ods", "odp":
		parts = appendPart(parts, byName["content.xml"])
	}
	var b strings.Builder
	for i, f := range parts {
		if i == officeParts || b.Len() >= officeTextMax {
			break
		}
		readXMLText(f, &b)
	}
	text := b.String()
	if len(text) > officeTextMax {
		text = strings.ToValidUTF8(text[:officeTextMax], "")
	}
	return strings.TrimSpace(collapseBlankLines(text))
}

func appendPart(parts []*zip.File, f *zip.File) []*zip.File {
	if f == nil {
		return parts
	}
	return append(parts, f)
}

// slideNumber is N in ppt/slides/slideN.xml.
func slideNumber(name string) int {
	n, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "ppt/slides/slide"), ".xml"))
	return n
}

// readXMLText appends the text of one part: the characters inside text runs
// (w:t, a:t, t; in OpenDocument everything inside text:p and text:h), a line
// break after each paragraph, row or shared string, a tab or space where
// the format marks one.
func readXMLText(f *zip.File, b *strings.Builder) {
	rc, err := f.Open()
	if err != nil {
		return
	}
	defer rc.Close()
	d := xml.NewDecoder(io.LimitReader(rc, officePartMax))
	d.Strict = false
	inRun, inPara := 0, 0
	for b.Len() < officeTextMax {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inRun++
			case "p", "h":
				if isODF(t.Name.Space) {
					inPara++
				}
			case "tab":
				b.WriteByte('\t')
			case "s":
				if isODF(t.Name.Space) {
					b.WriteByte(' ')
				}
			case "br", "line-break", "cr":
				b.WriteByte('\n')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inRun = max(inRun-1, 0)
			case "p", "h", "si", "tr", "table-row":
				if (t.Name.Local == "p" || t.Name.Local == "h") && isODF(t.Name.Space) {
					inPara = max(inPara-1, 0)
				}
				b.WriteByte('\n')
			}
		case xml.CharData:
			if inRun > 0 || inPara > 0 {
				b.Write(t)
			}
		}
	}
	b.WriteByte('\n')
}

// isODF reports whether an element is in OpenDocument's text namespace.
func isODF(ns string) bool {
	return strings.HasPrefix(ns, "urn:oasis:names:tc:opendocument:xmlns:text")
}

// collapseBlankLines keeps at most one empty line in a row and trims the
// spaces at line ends.
func collapseBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	out := lines[:0]
	blank := false
	for _, l := range lines {
		l = strings.TrimRight(l, " \t\r")
		if l == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}
