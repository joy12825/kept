package parser

import (
	"archive/zip"
	"errors"
	"io"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"

	"kept/server/internal/i18n"
)

// The Word reading view (v1.9.8): a .docx read into blocks — paragraphs
// with their runs, tables, text boxes — to be laid out to the screen's
// width. Written here from ECMA-376 (WordprocessingML); what it leaves out
// (headers, columns, floating positions) the searchable PDF shows.

// WordMIME is the type the reading view is for.
const WordMIME = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

// ErrNotWord: the file is not a .docx.
var ErrNotWord = errors.New("parser: not a Word document")

type WordDoc struct {
	Blocks    []WordBlock
	Notes     []WordNote
	Images    []WordImage
	BaseSize  float64 // the body text's size, pt
	Truncated bool
}

type WordBlock struct {
	Kind       string // paragraph, table, box
	Heading    int    // 1–6; 0 for body text
	Align      string // center, right, justify; "" for the start
	List       *WordList
	Runs       []WordRun
	Rows       [][]WordCell
	HeaderRows int
	Blocks     []WordBlock // a box's content
}

type WordList struct {
	Marker string
	Level  int
}

type WordRun struct {
	Text                            string
	Bold, Italic, Underline, Strike bool
	Color, Highlight                string  // RRGGBB
	Size                            float64 // pt; 0 for the body size
	Vert                            string  // super, sub
	Link                            string  // http, https or mailto only
	Image                           int     // 1-based index into Images
	Note                            int     // a footnote or endnote reference
}

type WordCell struct {
	Blocks           []WordBlock
	ColSpan, RowSpan int
}

type WordNote struct {
	N      int
	Blocks []WordBlock
}

type WordImage struct {
	Part          string
	Width, Height int // px at 96 dpi; 0 when not given
}

// Bounds on what is read.
const (
	maxWordPart   = 30 << 20
	maxWordImage  = 20 << 20
	maxWordBlocks = 20000
	maxTableDepth = 3
)

var webImage = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp", ".bmp": "image/bmp"}

type fieldState struct {
	instr  strings.Builder
	result bool
	link   string
}

type wordReader struct {
	z        *zip.Reader
	styles   *wordStyles
	num      *numbering
	rels     map[string]wordRel
	noteRels map[string]wordRel
	images   map[string]int
	doc      *WordDoc
	blocks   int
	fields   []*fieldState
	notes    map[string]*xnode // footnotes and endnotes by "f"/"e" + id
	noteNums map[string]int
	inNote   bool
	pending  []WordBlock // text boxes met inside a paragraph
}

func openZipFile(src string) (*zip.ReadCloser, error) {
	z, err := zip.OpenReader(src)
	if err != nil {
		return nil, ErrNotWord
	}
	return z, nil
}

// IsWordType reports whether the reading view reads mime: .docx, and
// since v1.11.1 .doc and .odt.
func IsWordType(mime string) bool { return mime == WordMIME || mime == DocMIME || mime == OdtMIME }

// ReadWord reads the Word document at src for the reading view: a .docx,
// .doc or .odt, told by its content.
func ReadWord(src string) (*WordDoc, error) {
	switch wordFormat(src) {
	case "doc":
		return readDoc(src)
	case "odt":
		return readOdt(src)
	}
	return readDocx(src)
}

// wordFormat tells the three formats apart by their first bytes, and an
// OpenDocument package by its mimetype entry.
func wordFormat(src string) string {
	f, err := os.Open(src)
	if err != nil {
		return ""
	}
	defer f.Close()
	head := make([]byte, 8)
	if _, err := io.ReadFull(f, head); err != nil {
		return ""
	}
	if string(head) == "\xD0\xCF\x11\xE0\xA1\xB1\x1A\xE1" {
		return "doc"
	}
	zc, err := zip.OpenReader(src)
	if err != nil {
		return ""
	}
	defer zc.Close()
	if m := zipFile(&zc.Reader, "mimetype"); m != nil && m.UncompressedSize64 < 200 {
		if rc, err := m.Open(); err == nil {
			b, _ := io.ReadAll(rc)
			rc.Close()
			if strings.TrimSpace(string(b)) == OdtMIME {
				return "odt"
			}
		}
	}
	return "docx"
}

// readDocx reads a .docx.
func readDocx(src string) (*WordDoc, error) {
	zc, err := openZipFile(src)
	if err != nil {
		return nil, err
	}
	defer zc.Close()
	z := &zc.Reader
	body, err := readPart(z, "word/document.xml", maxWordPart)
	if err != nil {
		return nil, err
	}
	if body == nil || body.find("document") == nil {
		return nil, ErrNotWord
	}
	r := &wordReader{z: z, images: map[string]int{}, doc: &WordDoc{}, notes: map[string]*xnode{}, noteNums: map[string]int{}}
	st, _ := readPart(z, "word/styles.xml", maxWordPart)
	r.styles = readStyles(st)
	nb, _ := readPart(z, "word/numbering.xml", maxWordPart)
	r.num = readNumbering(nb)
	r.rels = readRels(z, "word/document.xml")
	r.doc.BaseSize = r.styles.baseSize
	for kind, part := range map[string]string{"f": "word/footnotes.xml", "e": "word/endnotes.xml"} {
		n, _ := readPart(z, part, maxWordPart)
		if n == nil {
			continue
		}
		for _, x := range n.all(map[string]string{"f": "footnote", "e": "endnote"}[kind], nil) {
			if t := x.at("type"); t == "" || t == "normal" {
				r.notes[kind+x.at("id")] = x
			}
		}
	}
	r.noteRels = readRels(z, "word/footnotes.xml")
	for k, v := range readRels(z, "word/endnotes.xml") {
		r.noteRels[k] = v
	}
	b := body.find("body")
	if b == nil {
		return nil, ErrNotWord
	}
	r.doc.Blocks = r.blockList(b.kids, 0)
	return r.doc, nil
}

// blockList reads block-level content: paragraphs, tables, content controls.
func (r *wordReader) blockList(kids []*xnode, depth int) []WordBlock {
	var out []WordBlock
	add := func(b WordBlock) {
		if r.blocks >= maxWordBlocks {
			r.doc.Truncated = true
			return
		}
		// one empty paragraph is enough for a gap
		if b.Kind == "paragraph" && len(b.Runs) == 0 && b.List == nil && (len(out) == 0 || out[len(out)-1].Kind == "paragraph" && len(out[len(out)-1].Runs) == 0) {
			return
		}
		r.blocks++
		out = append(out, b)
	}
	for _, k := range kids {
		if r.doc.Truncated {
			break
		}
		switch {
		case k.local == "p" && k.isW():
			p := r.paragraph(k)
			add(p)
			for _, box := range r.pending {
				add(box)
			}
			r.pending = nil
		case k.local == "tbl" && k.isW():
			add(r.table(k, depth+1))
		case k.local == "sdt" && k.isW():
			out = append(out, r.blockList(k.w("sdtContent").kidsOrNil(), depth)...)
		case k.local == "customXml" || k.local == "ins" || k.local == "smartTag":
			out = append(out, r.blockList(k.kids, depth)...)
		case k.local == "AlternateContent":
			if c := k.find("Choice"); c != nil {
				out = append(out, r.blockList(c.kids, depth)...)
			}
		}
	}
	// a trailing empty paragraph says nothing
	for len(out) > 0 && out[len(out)-1].Kind == "paragraph" && len(out[len(out)-1].Runs) == 0 && out[len(out)-1].List == nil {
		out = out[:len(out)-1]
	}
	return out
}

func (n *xnode) kidsOrNil() []*xnode {
	if n == nil {
		return nil
	}
	return n.kids
}

func (r *wordReader) paragraph(p *xnode) WordBlock {
	b := WordBlock{Kind: "paragraph"}
	ppr := p.w("pPr")
	styleID, _ := ppr.val("pStyle")
	if styleID == "" {
		styleID = r.styles.defPara
	}
	b.Heading = r.styles.heading(styleID)
	if v, ok := ppr.val("outlineLvl"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			b.Heading = 0
			if n >= 0 && n < 9 {
				b.Heading = min(n+1, 6)
			}
		}
	}
	jc := r.styles.align(styleID)
	if v, ok := ppr.val("jc"); ok {
		jc = v
	}
	if jc != "" {
		switch jc {
		case "center":
			b.Align = "center"
		case "right", "end":
			b.Align = "right"
		case "both", "distribute", "lowKashida", "mediumKashida", "highKashida", "thaiDistribute":
			b.Align = "justify"
		}
	}
	numID, ilvl, hasNum := r.styles.styleNum(styleID)
	if np := ppr.w("numPr"); np != nil {
		if v, ok := np.val("numId"); ok {
			numID, hasNum = v, true
		}
		if v, ok := np.val("ilvl"); ok {
			ilvl, _ = strconv.Atoi(v)
		}
	}
	if hasNum && !r.inNote {
		if m, ok := r.num.marker(numID, ilvl); ok {
			b.List = &WordList{Marker: m, Level: min(max(ilvl, 0), 8)}
		}
	}
	base := r.styles.base.over(r.styles.runStyle(styleID))
	b.Runs = mergeRuns(r.runs(p.kids, base, ""))
	if b.Heading > 0 && len(b.Runs) == 0 {
		b.Heading = 0
	}
	return b
}

// runs reads the inline content of a paragraph (or a part of one).
func (r *wordReader) runs(kids []*xnode, base runProps, link string) []WordRun {
	var out []WordRun
	for _, k := range kids {
		switch k.local {
		case "r":
			out = append(out, r.run(k, base, link)...)
		case "hyperlink":
			l := link
			if rel, ok := r.rel(k.at("id")); ok && rel.external {
				l = safeLink(rel.target)
			}
			out = append(out, r.runs(k.kids, base, l)...)
		case "fldSimple":
			l := link
			if u := fieldLink(k.at("instr")); u != "" {
				l = u
			}
			out = append(out, r.runs(k.kids, base, l)...)
		case "ins", "smartTag", "customXml", "bdo", "dir":
			out = append(out, r.runs(k.kids, base, link)...)
		case "sdt":
			out = append(out, r.runs(k.w("sdtContent").kidsOrNil(), base, link)...)
		case "AlternateContent":
			if c := k.find("Choice"); c != nil {
				out = append(out, r.runs(c.kids, base, link)...)
			}
		case "oMath", "oMathPara":
			var t strings.Builder
			for _, x := range k.all("t", nil) {
				t.WriteString(x.text)
			}
			if t.Len() > 0 && r.visible() {
				out = append(out, WordRun{Text: t.String(), Italic: true})
			}
		}
		// del, moveFrom: deleted text, not shown
	}
	return out
}

func (r *wordReader) rel(id string) (wordRel, bool) {
	if r.inNote {
		if v, ok := r.noteRels[id]; ok {
			return v, true
		}
	}
	v, ok := r.rels[id]
	return v, ok
}

// visible: outside fields, or in the shown result of every field.
func (r *wordReader) visible() bool {
	for _, f := range r.fields {
		if !f.result {
			return false
		}
	}
	return true
}

func (r *wordReader) fieldLink() string {
	for i := len(r.fields) - 1; i >= 0; i-- {
		if r.fields[i].link != "" {
			return r.fields[i].link
		}
	}
	return ""
}

var quoted = regexp.MustCompile(`"([^"]*)"`)

// fieldLink is where a HYPERLINK field points ("" for any other field, or a
// link within the document).
func fieldLink(instr string) string {
	f := strings.Fields(instr)
	if len(f) < 2 || !strings.EqualFold(f[0], "HYPERLINK") || strings.Contains(instr, `\l`) {
		return ""
	}
	if m := quoted.FindStringSubmatch(instr); m != nil {
		return safeLink(m[1])
	}
	return safeLink(f[1])
}

// safeLink keeps a web or mail address; anything else is not a link.
func safeLink(s string) string {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host == "" {
			return ""
		}
		return u.String()
	case "mailto":
		return u.String()
	}
	return ""
}

func (r *wordReader) run(n *xnode, base runProps, link string) []WordRun {
	rpr := n.w("rPr")
	p := base
	if id, ok := rpr.val("rStyle"); ok {
		p = p.over(r.styles.runStyle(id))
	}
	p = p.over(readRunProps(rpr))
	if p.hidden != nil && *p.hidden {
		return nil
	}
	mk := func(text string) WordRun {
		w := WordRun{Text: text, Link: link}
		if l := r.fieldLink(); l != "" {
			w.Link = l
		}
		deref := func(b *bool) bool { return b != nil && *b }
		w.Bold, w.Italic, w.Underline, w.Strike = deref(p.bold), deref(p.italic), deref(p.underline), deref(p.strike)
		if p.color != nil {
			w.Color = *p.color
		}
		if p.highlight != nil {
			w.Highlight = *p.highlight
		}
		if p.vert != nil {
			w.Vert = *p.vert
		}
		if p.size != nil && (*p.size-r.doc.BaseSize > 0.5 || r.doc.BaseSize-*p.size > 0.5) {
			w.Size = *p.size
		}
		return w
	}
	var out []WordRun
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			out = append(out, mk(text.String()))
			text.Reset()
		}
	}
	for _, k := range n.kids {
		switch k.local {
		case "fldChar":
			switch k.at("fldCharType") {
			case "begin":
				flush()
				r.fields = append(r.fields, &fieldState{})
			case "separate":
				flush()
				if len(r.fields) > 0 {
					f := r.fields[len(r.fields)-1]
					f.result, f.link = true, fieldLink(f.instr.String())
				}
			case "end":
				flush()
				if len(r.fields) > 0 {
					r.fields = r.fields[:len(r.fields)-1]
				}
			}
			continue
		case "instrText":
			if len(r.fields) > 0 && !r.fields[len(r.fields)-1].result {
				r.fields[len(r.fields)-1].instr.WriteString(k.text)
			}
			continue
		}
		if !r.visible() {
			continue
		}
		switch k.local {
		case "t":
			text.WriteString(k.text)
		case "tab", "ptab":
			text.WriteString("\t")
		case "br":
			if t := k.at("type"); t == "" || t == "textWrapping" {
				text.WriteString("\n")
			}
		case "cr":
			text.WriteString("\n")
		case "noBreakHyphen":
			text.WriteString("‑")
		case "sym":
			if c, err := strconv.ParseUint(k.at("char"), 16, 32); err == nil {
				text.WriteString(bullet(string(rune(c))))
			}
		case "footnoteReference", "endnoteReference":
			flush()
			kind := "f"
			if k.local == "endnoteReference" {
				kind = "e"
			}
			if nn := r.note(kind, k.at("id")); nn > 0 {
				w := mk("")
				w.Note, w.Text, w.Vert = nn, "", "super"
				out = append(out, w)
			}
		case "drawing", "pict", "object":
			flush()
			out = append(out, r.drawing(k, mk)...)
		case "AlternateContent":
			if c := k.find("Choice"); c != nil {
				flush()
				out = append(out, r.run(&xnode{local: "r", kids: c.kids}, p, link)...)
			}
		}
	}
	flush()
	return out
}

// note numbers a footnote or endnote at its first reference and reads it.
func (r *wordReader) note(kind, id string) int {
	key := kind + id
	if n, ok := r.noteNums[key]; ok {
		return n
	}
	x := r.notes[key]
	if x == nil || r.inNote {
		return 0
	}
	n := len(r.doc.Notes) + 1
	r.noteNums[key] = n
	r.inNote = true
	saved := r.fields
	r.fields = nil
	blocks := r.blockList(x.kids, 1)
	r.fields, r.inNote = saved, false
	r.doc.Notes = append(r.doc.Notes, WordNote{N: n, Blocks: blocks})
	return n
}

// What a Word document's reading view shows for a drawing it cannot show
// (v1.10.1: translated where they are sent, see IsWordPlaceholder).
var (
	wordChart    = i18n.N("[图表]")
	wordSmartArt = i18n.N("[SmartArt 图形]")
	wordPicture  = i18n.N("[图片]")
)

// IsWordPlaceholder reports whether a run's text is one the reader put in
// for a drawing, rather than the document's own text.
func IsWordPlaceholder(s string) bool { return s == wordChart || s == wordSmartArt || s == wordPicture }

// drawing is a picture (or a placeholder for what cannot be shown); its
// text boxes are kept to follow the paragraph.
func (r *wordReader) drawing(n *xnode, mk func(string) WordRun) []WordRun {
	for _, tb := range n.all("txbxContent", nil) {
		if blocks := r.blockList(tb.kids, 1); len(blocks) > 0 {
			r.pending = append(r.pending, WordBlock{Kind: "box", Blocks: blocks})
		}
	}
	switch {
	case n.find("chart") != nil:
		return []WordRun{mk(wordChart)}
	case n.find("relIds") != nil:
		return []WordRun{mk(wordSmartArt)}
	}
	id := ""
	if b := n.find("blip"); b != nil {
		id = b.at("embed")
	} else if im := n.find("imagedata"); im != nil {
		id = im.at("id")
	}
	if id == "" {
		return nil
	}
	rel, ok := r.rel(id)
	if !ok || rel.external {
		return nil
	}
	if _, ok := webImage[strings.ToLower(path.Ext(rel.target))]; !ok {
		return []WordRun{mk(wordPicture)}
	}
	idx, ok := r.images[rel.target]
	if !ok {
		img := WordImage{Part: rel.target}
		if e := n.find("extent"); e != nil {
			cx, _ := strconv.Atoi(e.at("cx"))
			cy, _ := strconv.Atoi(e.at("cy"))
			img.Width, img.Height = cx/9525, cy/9525
		}
		r.doc.Images = append(r.doc.Images, img)
		idx = len(r.doc.Images)
		r.images[rel.target] = idx
	}
	w := mk("")
	w.Image = idx
	return []WordRun{w}
}

func (r *wordReader) table(t *xnode, depth int) WordBlock {
	if depth > maxTableDepth {
		var b strings.Builder
		for _, x := range t.all("t", nil) {
			b.WriteString(x.text)
			b.WriteString(" ")
		}
		return WordBlock{Kind: "paragraph", Runs: []WordRun{{Text: strings.TrimSpace(b.String())}}}
	}
	out := WordBlock{Kind: "table"}
	type origin struct{ row, cell int }
	open := map[int]origin{} // vertical merges by grid column
	header := true
	for _, tr := range flatten(t.kids, "tr") {
		trPr := tr.w("trPr")
		col := 0
		if v, ok := trPr.val("gridBefore"); ok {
			n, _ := strconv.Atoi(v)
			col += max(n, 0)
		}
		var row []WordCell
		for _, tc := range flatten(tr.kids, "tc") {
			tcPr := tc.w("tcPr")
			span := 1
			if v, ok := tcPr.val("gridSpan"); ok {
				if n, err := strconv.Atoi(v); err == nil && n > 1 && n < 64 {
					span = n
				}
			}
			vm, merged := tcPr.val("vMerge")
			if merged && vm != "restart" {
				if o, ok := open[col]; ok {
					out.Rows[o.row][o.cell].RowSpan++
					col += span
					continue
				}
			}
			cell := WordCell{Blocks: r.blockList(tc.kids, depth), ColSpan: span, RowSpan: 1}
			if merged && vm == "restart" {
				open[col] = origin{len(out.Rows), len(row)}
			} else {
				delete(open, col)
			}
			row = append(row, cell)
			col += span
		}
		if v, ok := trPr.on("tblHeader"); ok && v && header {
			out.HeaderRows++
		} else {
			header = false
		}
		out.Rows = append(out.Rows, row)
	}
	return out
}

// flatten lists the children named local, looking through content
// controls and custom markup around them.
func flatten(kids []*xnode, local string) []*xnode {
	var out []*xnode
	for _, k := range kids {
		switch {
		case k.local == local && k.isW():
			out = append(out, k)
		case k.local == "sdt":
			out = append(out, flatten(k.w("sdtContent").kidsOrNil(), local)...)
		case k.local == "customXml":
			out = append(out, flatten(k.kids, local)...)
		}
	}
	return out
}

// mergeRuns joins neighbouring runs formatted alike.
func mergeRuns(in []WordRun) []WordRun {
	var out []WordRun
	for _, w := range in {
		if n := len(out); n > 0 && w.Image == 0 && w.Note == 0 && out[n-1].Image == 0 && out[n-1].Note == 0 {
			a, b := out[n-1], w
			a.Text, b.Text = "", ""
			if a == b {
				out[n-1].Text += w.Text
				continue
			}
		}
		if w.Text == "" && w.Image == 0 && w.Note == 0 {
			continue
		}
		out = append(out, w)
	}
	return out
}

// ReadWordImage returns the n-th picture (from 1) of the reading view, and
// its type. Only pictures a browser shows are given.
func ReadWordImage(src string, n int) ([]byte, string, error) {
	doc, err := ReadWord(src)
	if err != nil {
		return nil, "", err
	}
	if n < 1 || n > len(doc.Images) {
		return nil, "", ErrNotWord
	}
	part := doc.Images[n-1].Part
	if off, ok := strings.CutPrefix(part, "doc:"); ok { // a .doc's picture: its place in the Data stream
		at, err := strconv.ParseInt(off, 10, 32)
		if err != nil {
			return nil, "", ErrNotWord
		}
		return readDocImage(src, int32(at))
	}
	mime := webImage[strings.ToLower(path.Ext(part))]
	zc, err := openZipFile(src)
	if err != nil {
		return nil, "", err
	}
	defer zc.Close()
	f := zipFile(&zc.Reader, part)
	if f == nil || f.UncompressedSize64 > maxWordImage {
		return nil, "", ErrNotWord
	}
	rc, err := f.Open()
	if err != nil {
		return nil, "", err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, maxWordImage))
	return b, mime, err
}
