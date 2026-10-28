package parser

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"kept/server/internal/imgutil"
)

// EPUB (v0.4): metadata, the text of the spine and the cover, read with
// the standard library. Nothing is extracted to disk except the two
// generated JPEGs.

// BookMeta is the catalog data an e-book carries about itself.
type BookMeta struct {
	Title       string
	Authors     []string
	Publisher   string
	Published   string // "2012-08" or "2012"
	Language    string
	Description string
	ISBN        string // as written; the caller validates it
	Series      string
}

var (
	ErrDRM = &Error{Message: "这本 EPUB 有数字版权保护（DRM），无法读取", Permanent: true}
	// errEPUB wraps what is wrong with a broken EPUB.
	errEPUB = func(why string) error {
		return &Error{Message: fmt.Sprintf("EPUB 文件已损坏，无法读取（%s）", why), Permanent: true}
	}
)

const (
	maxEntry     = 50 << 20  // one file inside the EPUB
	maxTotal     = 300 << 20 // everything read from it
	maxEPUBText  = 5 << 20   // characters of text kept
	thumbWidth   = 400       // like PDF thumbnails
	epubCoverMax = 600       // books.CoverWidth
)

type epubReader struct {
	z     *zip.Reader
	files map[string]*zip.File
	read  int64
}

func (r *epubReader) open(name string) ([]byte, error) {
	f, ok := r.files[name]
	if !ok {
		return nil, fmt.Errorf("缺少 %s", name)
	}
	if f.UncompressedSize64 > maxEntry {
		return nil, fmt.Errorf("%s 太大", name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxEntry+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxEntry {
		return nil, fmt.Errorf("%s 太大", name)
	}
	r.read += int64(len(data))
	if r.read > maxTotal {
		return nil, errors.New("解压后太大")
	}
	return data, nil
}

type opfPackage struct {
	Version  string `xml:"version,attr"`
	Metadata struct {
		Titles      []string `xml:"title"`
		Creators    []string `xml:"creator"`
		Publisher   string   `xml:"publisher"`
		Dates       []string `xml:"date"`
		Language    string   `xml:"language"`
		Description string   `xml:"description"`
		Identifiers []struct {
			Scheme string `xml:"scheme,attr"`
			Value  string `xml:",chardata"`
		} `xml:"identifier"`
		Metas []struct {
			Name     string `xml:"name,attr"`
			Content  string `xml:"content,attr"`
			Property string `xml:"property,attr"`
			Value    string `xml:",chardata"`
		} `xml:"meta"`
	} `xml:"metadata"`
	Manifest []struct {
		ID         string `xml:"id,attr"`
		Href       string `xml:"href,attr"`
		MediaType  string `xml:"media-type,attr"`
		Properties string `xml:"properties,attr"`
	} `xml:"manifest>item"`
	Spine []struct {
		IDRef string `xml:"idref,attr"`
	} `xml:"spine>itemref"`
}

// parseEPUB fills res from the EPUB at src; the thumbnail and the cover
// are written into workDir.
func parseEPUB(src, workDir string, res *Result, progress Progress) error {
	progress("inspect", 20)
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	z, err := zip.NewReader(f, st.Size())
	if err != nil {
		return errEPUB("不是 ZIP 文件")
	}
	r := &epubReader{z: z, files: map[string]*zip.File{}}
	for _, zf := range z.File {
		r.files[zf.Name] = zf
	}
	if err := checkDRM(r); err != nil {
		return err
	}
	opfPath, err := rootfile(r)
	if err != nil {
		return errEPUB(err.Error())
	}
	data, err := r.open(opfPath)
	if err != nil {
		return errEPUB(err.Error())
	}
	var pkg opfPackage
	if err := xml.Unmarshal(data, &pkg); err != nil {
		return errEPUB("OPF 无法解析")
	}
	base := path.Dir(opfPath)
	resolve := func(href string) string {
		href, _, _ = strings.Cut(href, "#")
		if p, err := url.PathUnescape(href); err == nil {
			href = p
		}
		if base == "." {
			return path.Clean(href)
		}
		return path.Clean(path.Join(base, href))
	}
	res.Book = bookMeta(&pkg)

	progress("text", 40)
	byID := map[string]int{}
	for i, it := range pkg.Manifest {
		byID[it.ID] = i
	}
	var text strings.Builder
	for _, ref := range pkg.Spine {
		i, ok := byID[ref.IDRef]
		if !ok {
			continue
		}
		it := pkg.Manifest[i]
		if !strings.Contains(it.MediaType, "html") && !strings.Contains(it.MediaType, "xml") {
			continue
		}
		page, err := r.open(resolve(it.Href))
		if err != nil {
			if strings.Contains(err.Error(), "太大") {
				return errEPUB(err.Error())
			}
			continue // a missing chapter: keep the rest
		}
		if text.Len() > 0 {
			text.WriteString("\n\n")
		}
		text.WriteString(htmlText(page))
		if text.Len() > maxEPUBText {
			break
		}
	}
	content := strings.TrimSpace(text.String())
	if len(content) > maxEPUBText {
		content = content[:maxEPUBText]
		for !utf8.ValidString(content) {
			content = content[:len(content)-1]
		}
	}
	res.Content = content

	progress("thumbnail", 80)
	if href := coverHref(&pkg, r, resolve); href != "" {
		if img, err := r.open(href); err == nil {
			thumb, terr := imgutil.ToJPEG(img, thumbWidth)
			cover, cerr := imgutil.ToJPEG(img, epubCoverMax)
			if terr == nil && cerr == nil {
				res.ThumbnailPath = filepath.Join(workDir, "thumbnail.jpg")
				res.CoverPath = filepath.Join(workDir, "cover.jpg")
				if err := os.WriteFile(res.ThumbnailPath, thumb, 0o644); err != nil {
					return err
				}
				if err := os.WriteFile(res.CoverPath, cover, 0o644); err != nil {
					return err
				}
			} else {
				res.Warnings = append(res.Warnings, "封面图无法读取")
			}
		}
	}
	return nil
}

// checkDRM refuses EPUBs whose content is encrypted. Font obfuscation
// (IDPF and Adobe) is not DRM.
func checkDRM(r *epubReader) error {
	if _, ok := r.files["META-INF/rights.xml"]; ok {
		return ErrDRM
	}
	data, err := r.open("META-INF/encryption.xml")
	if err != nil {
		return nil
	}
	var enc struct {
		Data []struct {
			Method struct {
				Algorithm string `xml:"Algorithm,attr"`
			} `xml:"EncryptionMethod"`
			Ref struct {
				URI string `xml:"URI,attr"`
			} `xml:"CipherReference>CipherReference"`
		} `xml:"EncryptedData"`
	}
	if xml.Unmarshal(data, &enc) != nil {
		return ErrDRM
	}
	for _, d := range enc.Data {
		a := d.Method.Algorithm
		if a != "http://www.idpf.org/2008/embedding" && a != "http://ns.adobe.com/pdf/enc#RC" {
			return ErrDRM
		}
	}
	return nil
}

func rootfile(r *epubReader) (string, error) {
	data, err := r.open("META-INF/container.xml")
	if err != nil {
		return "", err
	}
	var c struct {
		Rootfiles []struct {
			FullPath  string `xml:"full-path,attr"`
			MediaType string `xml:"media-type,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	if err := xml.Unmarshal(data, &c); err != nil || len(c.Rootfiles) == 0 {
		return "", errors.New("container.xml 无法解析")
	}
	for _, rf := range c.Rootfiles {
		if rf.MediaType == "application/oebps-package+xml" || rf.MediaType == "" {
			return rf.FullPath, nil
		}
	}
	return c.Rootfiles[0].FullPath, nil
}

var isbnRe = regexp.MustCompile(`(?i)(?:isbn[:\s]*)?((?:97[89][-\s]?)?\d[\d\-\s]{7,15}[\dX])`)

func bookMeta(pkg *opfPackage) *BookMeta {
	m := pkg.Metadata
	b := &BookMeta{Publisher: clean(m.Publisher), Language: languageName(clean(m.Language)), Description: htmlText([]byte(m.Description))}
	if len(m.Titles) > 0 {
		b.Title = clean(m.Titles[0])
	}
	for _, c := range m.Creators {
		if c = clean(c); c != "" {
			b.Authors = append(b.Authors, c)
		}
	}
	for _, d := range m.Dates {
		if d = clean(d); len(d) >= 4 {
			b.Published = epubDate(d)
			break
		}
	}
	for _, id := range m.Identifiers {
		v := clean(id.Value)
		if strings.EqualFold(id.Scheme, "isbn") || strings.Contains(strings.ToLower(v), "isbn") || isbnLike(v) {
			if g := isbnRe.FindStringSubmatch(strings.TrimPrefix(strings.ToLower(v), "urn:")); g != nil && isbnLike(g[1]) {
				b.ISBN = strings.ToUpper(g[1])
				break
			}
		}
	}
	for _, meta := range m.Metas {
		switch {
		case meta.Name == "calibre:series" && b.Series == "":
			b.Series = clean(meta.Content)
		case meta.Property == "belongs-to-collection" && b.Series == "":
			b.Series = clean(meta.Value)
		}
	}
	if b.Title == "" && len(b.Authors) == 0 && b.ISBN == "" {
		return &BookMeta{} // still a book, with nothing known
	}
	return b
}

// isbnLike reports 10 or 13 digits (X allowed last) once separators go.
func isbnLike(s string) bool {
	d := strings.NewReplacer("-", "", " ", "").Replace(strings.TrimPrefix(strings.ToLower(s), "urn:isbn:"))
	if len(d) != 10 && len(d) != 13 {
		return false
	}
	for i, c := range d {
		if !(c >= '0' && c <= '9') && !(i == len(d)-1 && (c == 'x' || c == 'X')) {
			return false
		}
	}
	return true
}

// epubDate keeps the year and month of an OPF date (2012-08-01T00:00:00Z).
func epubDate(d string) string {
	if len(d) >= 7 && d[4] == '-' && d[5] >= '0' && d[5] <= '9' {
		return d[:7]
	}
	return d[:4]
}

func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

var epubLanguages = map[string]string{"zh": "中文", "en": "英文", "ja": "日文", "fr": "法文", "de": "德文", "ru": "俄文", "es": "西班牙文", "ko": "韩文"}

func languageName(code string) string {
	short, _, _ := strings.Cut(strings.ToLower(code), "-")
	if n, ok := epubLanguages[short]; ok {
		return n
	}
	return code
}

// coverHref finds the cover image: EPUB 3 properties, the EPUB 2 meta,
// then an image named like a cover.
func coverHref(pkg *opfPackage, r *epubReader, resolve func(string) string) string {
	for _, it := range pkg.Manifest {
		if strings.Contains(" "+it.Properties+" ", " cover-image ") {
			return resolve(it.Href)
		}
	}
	for _, meta := range pkg.Metadata.Metas {
		if meta.Name == "cover" {
			for _, it := range pkg.Manifest {
				if it.ID == meta.Content && strings.HasPrefix(it.MediaType, "image/") {
					return resolve(it.Href)
				}
			}
		}
	}
	for _, it := range pkg.Manifest {
		if strings.HasPrefix(it.MediaType, "image/") && strings.Contains(strings.ToLower(it.ID+" "+it.Href), "cover") {
			return resolve(it.Href)
		}
	}
	return ""
}

var blockTags = map[string]bool{
	"p": true, "div": true, "br": true, "li": true, "tr": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"blockquote": true, "section": true, "article": true, "pre": true, "dt": true, "dd": true, "table": true, "hr": true,
}

var (
	tagRe      = regexp.MustCompile(`(?s)<[^>]*>`)
	blockTagRe = regexp.MustCompile(`(?is)</?(?:p|div|br|li|tr|h[1-6]|blockquote|section|article|pre|dt|dd|table|hr)\b[^>]*>`)
	skipRe     = regexp.MustCompile(`(?is)<(head|script|style)\b.*?</(?:head|script|style)>`)
)

// htmlText extracts the text of an XHTML page: block elements end lines,
// head, script and style are skipped. Pages that are not well-formed XML
// fall back to dropping the tags.
func htmlText(page []byte) string {
	d := xml.NewDecoder(bytes.NewReader(page))
	d.Strict = false
	d.AutoClose = xml.HTMLAutoClose
	d.Entity = xml.HTMLEntity
	var b strings.Builder
	skip := 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Not well-formed: block tags end lines, other tags vanish.
			s := blockTagRe.ReplaceAllString(skipRe.ReplaceAllString(string(page), ""), "\n")
			return normalizeText(html.UnescapeString(tagRe.ReplaceAllString(s, "")))
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch strings.ToLower(t.Name.Local) {
			case "head", "script", "style":
				skip++
			}
			if n := strings.ToLower(t.Name.Local); n == "br" || n == "hr" {
				b.WriteByte('\n')
			}
		case xml.EndElement:
			switch strings.ToLower(t.Name.Local) {
			case "head", "script", "style":
				if skip > 0 {
					skip--
				}
			}
			if n := strings.ToLower(t.Name.Local); blockTags[n] && n != "br" && n != "hr" {
				b.WriteByte('\n')
			}
		case xml.CharData:
			if skip == 0 {
				b.Write(t)
			}
		}
	}
	return normalizeText(b.String())
}

var spaceRe = regexp.MustCompile(`[ \t\r\f\v\x{00a0}\x{3000}]+`)
var blankRe = regexp.MustCompile(`\n\s*\n\s*(\n\s*)+`)

func normalizeText(s string) string {
	lines := strings.Split(spaceRe.ReplaceAllString(s, " "), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.TrimSpace(blankRe.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}
