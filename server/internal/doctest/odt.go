package doctest

import (
	"archive/zip"
	"bytes"
	"sort"
)

// Namespaces for the snippets of an .odt.
const OdtNS = `xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:style="urn:oasis:names:tc:opendocument:xmlns:style:1.0"` +
	` xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0" xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0"` +
	` xmlns:draw="urn:oasis:names:tc:opendocument:xmlns:drawing:1.0" xmlns:fo="urn:oasis:names:tc:opendocument:xmlns:xsl-fo-compatible:1.0"` +
	` xmlns:xlink="http://www.w3.org/1999/xlink" xmlns:svg="urn:oasis:names:tc:opendocument:xmlns:svg-compatible:1.0" office:version="1.3"`

// Odt is an OpenDocument text: what goes inside office:text, the
// automatic styles of content.xml, and the styles of styles.xml.
type Odt struct {
	Body       string
	Automatic  string // inside office:automatic-styles of content.xml
	Styles     string // inside office:styles of styles.xml
	Outline    string // a text:outline-style, inside office:styles
	Pictures   map[string][]byte
	NoMimetype bool
}

// Build writes the .odt.
func (d Odt) Build() []byte {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	put := func(name string, data []byte, store bool) {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		if store {
			h.Method = zip.Store
		}
		w, _ := z.CreateHeader(h)
		w.Write(data)
	}
	if !d.NoMimetype {
		put("mimetype", []byte("application/vnd.oasis.opendocument.text"), true)
	}
	put("content.xml", []byte(`<?xml version="1.0" encoding="UTF-8"?><office:document-content `+OdtNS+`><office:automatic-styles>`+d.Automatic+
		`</office:automatic-styles><office:body><office:text>`+d.Body+`</office:text></office:body></office:document-content>`), false)
	put("styles.xml", []byte(`<?xml version="1.0" encoding="UTF-8"?><office:document-styles `+OdtNS+`><office:styles>`+d.Styles+d.Outline+
		`</office:styles></office:document-styles>`), false)
	names := make([]string, 0, len(d.Pictures))
	for n := range d.Pictures {
		names = append(names, n)
	}
	sort.Strings(names)
	manifest := `<?xml version="1.0" encoding="UTF-8"?><manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0">` +
		`<manifest:file-entry manifest:full-path="/" manifest:media-type="application/vnd.oasis.opendocument.text"/>` +
		`<manifest:file-entry manifest:full-path="content.xml" manifest:media-type="text/xml"/>` +
		`<manifest:file-entry manifest:full-path="styles.xml" manifest:media-type="text/xml"/>`
	for _, n := range names {
		put("Pictures/"+n, d.Pictures[n], true)
		manifest += `<manifest:file-entry manifest:full-path="Pictures/` + n + `" manifest:media-type="image/png"/>`
	}
	put("META-INF/manifest.xml", []byte(manifest+`</manifest:manifest>`), false)
	z.Close()
	return buf.Bytes()
}
