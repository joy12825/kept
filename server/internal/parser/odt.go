package parser

import (
	"archive/zip"
	"io"
	"math"
	"path"
	"strconv"
	"strings"
)

// The reading view of an OpenDocument text (.odt, v1.11.1), written here
// from OASIS OpenDocument 1.2/1.3: content.xml read into the same blocks
// as a .docx, with the styles of content.xml and styles.xml.

const OdtMIME = "application/vnd.oasis.opendocument.text"

type odtStyle struct {
	parent    string
	display   string
	rpr       runProps
	align     string
	outline   int // default outline level; 0 none
	listStyle string
}

// odtLevel is a level of a list style.
type odtLevel struct {
	bullet         string // a bullet list's character; "" for numbers
	format         string // numberText's names; "none" for no number
	prefix, suffix string
	display        int // how many levels' numbers show
	start          int
}

type odtReader struct {
	z        *zip.Reader
	styles   map[string]*odtStyle  // "paragraph:P1", "text:T1"
	lists    map[string][]odtLevel // list styles by name
	outline  []odtLevel            // the outline style: numbering of headings
	counts   map[string]*[10]int   // list counters by list style
	headings [10]int
	doc      *WordDoc
	blocks   int
	images   map[string]int
	inNote   bool
	pending  []WordBlock
	base     runProps
}

// readOdt reads the .odt at src for the reading view.
func readOdt(src string) (*WordDoc, error) {
	zc, err := openZipFile(src)
	if err != nil {
		return nil, err
	}
	defer zc.Close()
	z := &zc.Reader
	content, err := readMixed(z, "content.xml")
	if err != nil {
		return nil, err
	}
	if content == nil {
		return nil, ErrNotWord
	}
	body := content.find("body")
	var text *xnode
	if body != nil {
		for _, k := range body.kids {
			if k.local == "text" {
				text = k
			}
		}
	}
	if text == nil {
		return nil, ErrNotWord
	}
	r := &odtReader{z: z, styles: map[string]*odtStyle{}, lists: map[string][]odtLevel{}, counts: map[string]*[10]int{},
		doc: &WordDoc{BaseSize: 12}, images: map[string]int{}}
	if st, _ := readMixed(z, "styles.xml"); st != nil {
		r.readStyles(st)
	}
	r.readStyles(content)
	r.doc.BaseSize = 12
	if s := r.runStyle("paragraph", "Standard"); s.size != nil {
		r.doc.BaseSize = *s.size
	} else if r.base.size != nil {
		r.doc.BaseSize = *r.base.size
	}
	r.doc.Blocks = r.blockList(text.kids, 0, 0)
	return r.doc, nil
}

func readMixed(z *zip.Reader, name string) (*xnode, error) {
	f := zipFile(z, name)
	if f == nil {
		return nil, nil
	}
	if f.UncompressedSize64 > maxWordPart {
		return nil, ErrWordTooBig
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return parseXMLMixed(io.LimitReader(rc, maxWordPart), true)
}

// ---------- styles ----------

func (r *odtReader) readStyles(root *xnode) {
	var walk func(n *xnode, depth int)
	walk = func(n *xnode, depth int) {
		for _, k := range n.kids {
			switch k.local {
			case "styles", "automatic-styles", "master-styles", "document-styles", "document-content":
				if depth < 3 {
					walk(k, depth+1)
				}
			case "default-style":
				if k.at("family") == "paragraph" {
					r.base = r.base.over(odtTextProps(k))
				}
			case "style":
				display := k.at("display-name")
				if display == "" {
					display = strings.ReplaceAll(k.at("name"), "_20_", " ")
				}
				st := &odtStyle{parent: k.at("parent-style-name"), display: display, rpr: odtTextProps(k), listStyle: k.at("list-style-name")}
				if v := k.at("default-outline-level"); v != "" {
					st.outline, _ = strconv.Atoi(v)
				}
				for _, p := range k.kids {
					if p.local == "paragraph-properties" {
						switch p.at("text-align") {
						case "center":
							st.align = "center"
						case "end", "right":
							st.align = "right"
						case "justify":
							st.align = "justify"
						}
					}
				}
				r.styles[k.at("family")+":"+k.at("name")] = st
			case "list-style":
				r.lists[k.at("name")] = odtLevels(k)
			case "outline-style":
				r.outline = odtLevels(k)
			}
		}
	}
	walk(root, 0)
}

func odtLevels(k *xnode) []odtLevel {
	levels := make([]odtLevel, 10)
	for i := range levels {
		levels[i] = odtLevel{format: "none", display: 1, start: 1}
	}
	for _, l := range k.kids {
		n, err := strconv.Atoi(l.at("level"))
		if err != nil || n < 1 || n > 10 {
			continue
		}
		lv := odtLevel{display: 1, start: 1, prefix: l.at("num-prefix"), suffix: l.at("num-suffix")}
		if d, err := strconv.Atoi(l.at("display-levels")); err == nil && d > 0 {
			lv.display = d
		}
		if s, err := strconv.Atoi(l.at("start-value")); err == nil {
			lv.start = s
		}
		switch l.local {
		case "list-level-style-bullet":
			lv.bullet = bullet(l.at("bullet-char"))
		case "list-level-style-image":
			lv.bullet = "•"
		default:
			lv.format = odtFormat(l.at("num-format"))
		}
		levels[n-1] = lv
	}
	return levels
}

// odtFormat names a num-format as numberText does.
func odtFormat(f string) string {
	switch {
	case f == "":
		return "none"
	case f == "1":
		return "decimal"
	case f == "a":
		return "lowerLetter"
	case f == "A":
		return "upperLetter"
	case f == "i":
		return "lowerRoman"
	case f == "I":
		return "upperRoman"
	case strings.HasPrefix(f, "一"), strings.HasPrefix(f, "〇"):
		return "chineseCounting"
	case strings.HasPrefix(f, "壹"):
		return "chineseLegalSimplified"
	case strings.HasPrefix(f, "甲"):
		return "ideographTraditional"
	case strings.HasPrefix(f, "子"):
		return "ideographZodiac"
	case strings.HasPrefix(f, "①"):
		return "decimalEnclosedCircle"
	}
	return "decimal"
}

// odtTextProps reads a style's style:text-properties.
func odtTextProps(k *xnode) runProps {
	var p runProps
	var tp *xnode
	for _, c := range k.kids {
		if c.local == "text-properties" {
			tp = c
		}
	}
	if tp == nil {
		return p
	}
	t, f := true, false
	pick := func(on bool) *bool {
		if on {
			return &t
		}
		return &f
	}
	if v := tp.at("font-weight"); v != "" {
		n, err := strconv.Atoi(v)
		p.bold = pick(v == "bold" || err == nil && n >= 600)
	}
	if v := tp.at("font-style"); v != "" {
		p.italic = pick(v == "italic" || v == "oblique")
	}
	if v := tp.at("text-underline-style"); v != "" {
		p.underline = pick(v != "none")
	}
	if v := tp.at("text-line-through-style"); v != "" {
		p.strike = pick(v != "none")
	}
	if v := tp.at("display"); v != "" {
		p.hidden = pick(v == "none")
	}
	if v := strings.TrimPrefix(tp.at("color"), "#"); hexColor.MatchString(v) {
		v = strings.ToUpper(v)
		p.color = &v
	}
	if v := strings.TrimPrefix(tp.at("background-color"), "#"); hexColor.MatchString(v) {
		v = strings.ToUpper(v)
		p.highlight = &v
	} else if tp.at("background-color") == "transparent" {
		none := ""
		p.highlight = &none
	}
	if v := tp.at("font-size"); strings.HasSuffix(v, "pt") {
		if n, err := strconv.ParseFloat(strings.TrimSuffix(v, "pt"), 64); err == nil && n > 0 && n < 400 {
			p.size = &n
		}
	}
	if v := tp.at("text-position"); v != "" {
		pos := strings.Fields(v)[0]
		vert := ""
		switch {
		case pos == "super":
			vert = "super"
		case pos == "sub":
			vert = "sub"
		case strings.HasSuffix(pos, "%"):
			if n, err := strconv.ParseFloat(strings.TrimSuffix(pos, "%"), 64); err == nil {
				if n > 0 {
					vert = "super"
				} else if n < 0 {
					vert = "sub"
				}
			}
		}
		p.vert = &vert
	}
	return p
}

// chain is a style and its parents, the nearest first.
func (r *odtReader) chain(family, name string) []*odtStyle {
	var out []*odtStyle
	seen := map[string]bool{}
	for name != "" && !seen[name] && len(out) < 20 {
		seen[name] = true
		st := r.styles[family+":"+name]
		if st == nil {
			break
		}
		out = append(out, st)
		name = st.parent
	}
	return out
}

func (r *odtReader) runStyle(family, name string) runProps {
	c := r.chain(family, name)
	var p runProps
	for i := len(c) - 1; i >= 0; i-- {
		p = p.over(c[i].rpr)
	}
	return p
}

// ---------- blocks ----------

func (r *odtReader) add(out []WordBlock, b WordBlock) []WordBlock {
	if r.blocks >= maxWordBlocks {
		r.doc.Truncated = true
		return out
	}
	if b.Kind == "paragraph" && len(b.Runs) == 0 && b.List == nil && (len(out) == 0 || out[len(out)-1].Kind == "paragraph" && len(out[len(out)-1].Runs) == 0) {
		return out
	}
	r.blocks++
	return append(out, b)
}

// blockList reads block content: paragraphs, headings, lists, tables,
// sections and indexes. depth counts the tables around.
func (r *odtReader) blockList(kids []*xnode, depth, listDepth int) []WordBlock {
	var out []WordBlock
	for _, k := range kids {
		if r.doc.Truncated {
			break
		}
		switch k.local {
		case "p", "h":
			out = r.add(out, r.paragraph(k, nil))
			for _, box := range r.pending {
				out = r.add(out, box)
			}
			r.pending = nil
		case "list":
			out = append(out, r.list(k, depth, 0, k.at("style-name"))...)
		case "table":
			if k.space == "" || strings.Contains(k.space, ":table:") {
				out = r.add(out, r.table(k, depth+1))
			}
		case "section", "index-body":
			out = append(out, r.blockList(k.kids, depth, listDepth)...)
		case "table-of-content", "illustration-index", "table-index", "object-index", "user-index", "alphabetical-index", "bibliography":
			for _, c := range k.kids {
				if c.local == "index-body" {
					out = append(out, r.blockList(c.kids, depth, listDepth)...)
				}
			}
		case "soft-page-break", "tracked-changes", "sequence-decls", "variable-decls", "user-field-decls", "forms":
		}
	}
	for len(out) > 0 && out[len(out)-1].Kind == "paragraph" && len(out[len(out)-1].Runs) == 0 && out[len(out)-1].List == nil {
		out = out[:len(out)-1]
	}
	return out
}

// list reads a list: its items' paragraphs get the list's marker.
func (r *odtReader) list(l *xnode, depth, level int, style string) []WordBlock {
	if s := l.at("style-name"); s != "" {
		style = s
	}
	if level == 0 && l.at("continue-numbering") != "true" && l.at("continue-list") == "" {
		delete(r.counts, style)
	}
	var out []WordBlock
	for _, item := range l.kids {
		if item.local != "list-item" && item.local != "list-header" {
			continue
		}
		first := true
		if v := item.at("start-value"); v != "" && item.local == "list-item" {
			if n, err := strconv.Atoi(v); err == nil {
				c := r.counter(style)
				c[min(level, 9)] = n - 1
			}
		}
		for _, k := range item.kids {
			switch k.local {
			case "p", "h":
				var lst *WordList
				if first && item.local == "list-item" && !r.inNote {
					if m, ok := r.marker(style, level, k); ok {
						lst = &WordList{Marker: m, Level: min(level, 8)}
					}
				}
				first = false
				out = r.add(out, r.paragraph(k, lst))
				for _, box := range r.pending {
					out = r.add(out, box)
				}
				r.pending = nil
			case "list":
				out = append(out, r.list(k, depth, level+1, style)...)
			case "table":
				out = r.add(out, r.table(k, depth+1))
			}
		}
	}
	return out
}

func (r *odtReader) counter(style string) *[10]int {
	c := r.counts[style]
	if c == nil {
		c = &[10]int{}
		for i := range c {
			c[i] = -1
		}
		r.counts[style] = c
	}
	return c
}

// marker counts a list item and returns its number or bullet. A
// text:number the application wrote wins.
func (r *odtReader) marker(style string, level int, p *xnode) (string, bool) {
	level = min(level, 9)
	levels := r.lists[style]
	if levels == nil {
		levels = r.lists[r.paraListStyle(p)]
	}
	c := r.counter(style)
	var lv odtLevel
	if levels != nil {
		lv = levels[level]
	} else {
		lv = odtLevel{bullet: "•"}
	}
	if c[level] < 0 {
		c[level] = lv.start - 1
	}
	c[level]++
	for i := level + 1; i < 10; i++ {
		c[i] = -1
	}
	for _, k := range p.kids {
		if k.local == "number" {
			return strings.TrimSpace(k.text), true
		}
	}
	if lv.bullet != "" {
		return lv.bullet, true
	}
	if lv.format == "none" {
		return strings.TrimSpace(lv.prefix + lv.suffix), lv.prefix+lv.suffix != ""
	}
	var parts []string
	for i := max(level-lv.display+1, 0); i <= level; i++ {
		n := c[i]
		if n < 0 && levels != nil {
			n = levels[i].start
		}
		f := lv.format
		if levels != nil && i != level {
			f = levels[i].format
		}
		parts = append(parts, numberText(n, f))
	}
	return lv.prefix + strings.Join(parts, ".") + lv.suffix, true
}

// paraListStyle is the list style a paragraph's style names.
func (r *odtReader) paraListStyle(p *xnode) string {
	for _, st := range r.chain("paragraph", p.at("style-name")) {
		if st.listStyle != "" {
			return st.listStyle
		}
	}
	return ""
}

// heading is the level of a text:h, or of a paragraph whose style says so.
func (r *odtReader) heading(p *xnode) int {
	if p.local == "h" {
		n, err := strconv.Atoi(p.at("outline-level"))
		if err != nil || n < 1 {
			n = 1
		}
		return min(n, 6)
	}
	for _, st := range r.chain("paragraph", p.at("style-name")) {
		if st.outline > 0 {
			return min(st.outline, 6)
		}
		name := strings.TrimSpace(st.display)
		if m := headingName.FindStringSubmatch(strings.ReplaceAll(name, "_20_", " ")); m != nil {
			n, _ := strconv.Atoi(m[2])
			return min(n, 6)
		}
		switch strings.ToLower(name) {
		case "title", "标题":
			return 1
		case "subtitle", "副标题":
			return 2
		}
	}
	return 0
}

// outlineMarker numbers a heading with the outline style.
func (r *odtReader) outlineMarker(p *xnode, level int) string {
	for _, k := range p.kids {
		if k.local == "number" {
			return strings.TrimSpace(k.text)
		}
	}
	if r.outline == nil || p.at("is-list-header") == "true" {
		return ""
	}
	level = min(max(level, 1), 10) - 1
	lv := r.outline[level]
	if lv.format == "none" {
		return ""
	}
	if v, err := strconv.Atoi(p.at("restart-numbering")); err == nil && v != 0 {
		r.headings[level] = 0
	}
	if r.headings[level] == 0 {
		r.headings[level] = lv.start - 1
	}
	r.headings[level]++
	for i := level + 1; i < 10; i++ {
		r.headings[i] = 0
	}
	var parts []string
	for i := max(level-lv.display+1, 0); i <= level; i++ {
		n := r.headings[i]
		if n == 0 {
			n = r.outline[i].start
		}
		parts = append(parts, numberText(n, r.outline[i].format))
	}
	return lv.prefix + strings.Join(parts, ".") + lv.suffix
}

func (r *odtReader) paragraph(p *xnode, lst *WordList) WordBlock {
	b := WordBlock{Kind: "paragraph", List: lst}
	if p.at("is-hidden") == "true" {
		return WordBlock{Kind: "paragraph"}
	}
	style := p.at("style-name")
	b.Heading = r.heading(p)
	for _, st := range r.chain("paragraph", style) {
		if st.align != "" {
			b.Align = st.align
			break
		}
	}
	if b.Heading > 0 && lst == nil && !r.inNote {
		lv, _ := strconv.Atoi(p.at("outline-level"))
		if p.local == "h" {
			if m := r.outlineMarker(p, max(lv, 1)); m != "" {
				b.List = &WordList{Marker: m}
			}
		}
	}
	base := r.base.over(r.runStyle("paragraph", style))
	b.Runs = mergeRuns(r.runs(p.kids, base, ""))
	if b.Heading > 0 && len(b.Runs) == 0 {
		b.Heading, b.List = 0, nil
	}
	return b
}

// runs reads a paragraph's inline content.
func (r *odtReader) runs(kids []*xnode, p runProps, link string) []WordRun {
	var out []WordRun
	mk := func(text string) WordRun {
		w := WordRun{Text: text, Link: link}
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
		if p.size != nil && math.Abs(*p.size-r.doc.BaseSize) > 0.5 && w.Vert == "" {
			w.Size = *p.size
		}
		return w
	}
	if p.hidden != nil && *p.hidden {
		return nil
	}
	for _, k := range kids {
		switch k.local {
		case textNode:
			if t := odtSpace(k.text); t != "" {
				out = append(out, mk(t))
			}
		case "span":
			out = append(out, r.runs(k.kids, p.over(r.runStyle("text", k.at("style-name"))), link)...)
		case "a":
			out = append(out, r.runs(k.kids, p.over(r.runStyle("text", k.at("style-name"))), safeLink(k.at("href")))...)
		case "s":
			n, err := strconv.Atoi(k.at("c"))
			if err != nil || n < 1 {
				n = 1
			}
			out = append(out, mk(strings.Repeat(" ", min(n, 100))))
		case "tab":
			out = append(out, mk("\t"))
		case "line-break":
			out = append(out, mk("\n"))
		case "note":
			if r.inNote {
				continue
			}
			if n := r.note(k); n > 0 {
				w := mk("")
				w.Note, w.Vert, w.Size = n, "super", 0
				out = append(out, w)
			}
		case "frame":
			out = append(out, r.frame(k, mk)...)
		case "hidden-text", "annotation", "annotation-end", "bookmark", "bookmark-start", "bookmark-end", "reference-mark",
			"reference-mark-start", "reference-mark-end", "change", "change-start", "change-end", "soft-page-break", "number", "toc-mark",
			"alphabetical-index-mark", "user-index-mark", "toc-mark-start", "toc-mark-end":
		case "ruby":
			for _, c := range k.kids {
				if c.local == "ruby-base" {
					out = append(out, r.runs(c.kids, p, link)...)
				}
			}
		default:
			// fields and other inline elements: what they show
			if len(k.kids) > 0 {
				out = append(out, r.runs(k.kids, p, link)...)
			}
		}
	}
	return out
}

// odtSpace collapses white space as OpenDocument does inside a paragraph.
func odtSpace(s string) string {
	var b strings.Builder
	space := false
	for _, c := range s {
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if !space {
				b.WriteByte(' ')
			}
			space = true
			continue
		}
		space = false
		b.WriteRune(c)
	}
	return b.String()
}

func (r *odtReader) note(n *xnode) int {
	var body *xnode
	for _, k := range n.kids {
		if k.local == "note-body" {
			body = k
		}
	}
	if body == nil {
		return 0
	}
	num := len(r.doc.Notes) + 1
	r.inNote = true
	saved := r.pending
	r.pending = nil
	blocks := r.blockList(body.kids, 1, 0)
	r.pending, r.inNote = saved, false
	r.doc.Notes = append(r.doc.Notes, WordNote{N: num, Blocks: blocks})
	return num
}

// frame is a picture, or a text box kept to follow the paragraph.
func (r *odtReader) frame(f *xnode, mk func(string) WordRun) []WordRun {
	for _, k := range f.kids {
		switch k.local {
		case "text-box":
			if blocks := r.blockList(k.kids, 1, 0); len(blocks) > 0 {
				r.pending = append(r.pending, WordBlock{Kind: "box", Blocks: blocks})
			}
			return nil
		case "object", "object-ole":
			return []WordRun{mk(wordChart)}
		case "image":
			href := k.at("href")
			if href == "" || strings.Contains(href, "://") || strings.HasPrefix(href, "/") {
				return []WordRun{mk(wordPicture)}
			}
			part := path.Clean(strings.TrimPrefix(href, "./"))
			if _, ok := webImage[strings.ToLower(path.Ext(part))]; !ok || zipFile(r.z, part) == nil {
				return []WordRun{mk(wordPicture)}
			}
			idx, ok := r.images[part]
			if !ok {
				img := WordImage{Part: part, Width: odtPixels(f.at("width")), Height: odtPixels(f.at("height"))}
				r.doc.Images = append(r.doc.Images, img)
				idx = len(r.doc.Images)
				r.images[part] = idx
			}
			w := mk("")
			w.Image = idx
			return []WordRun{w}
		}
	}
	return nil
}

// odtPixels turns a length (2.5cm, 1in, 72pt, 20mm) into pixels at 96 dpi.
func odtPixels(v string) int {
	units := map[string]float64{"cm": 96 / 2.54, "mm": 96 / 25.4, "in": 96, "pt": 96.0 / 72, "pc": 16, "px": 1}
	for u, k := range units {
		if strings.HasSuffix(v, u) {
			if n, err := strconv.ParseFloat(strings.TrimSuffix(v, u), 64); err == nil && n > 0 && n < 100000 {
				return int(math.Round(n * k))
			}
		}
	}
	return 0
}

func (r *odtReader) table(t *xnode, depth int) WordBlock {
	if depth > maxTableDepth {
		var b strings.Builder
		var text func(n *xnode)
		text = func(n *xnode) {
			for _, k := range n.kids {
				if k.local == textNode {
					b.WriteString(k.text)
				} else {
					text(k)
					if k.local == "p" || k.local == "h" {
						b.WriteString(" ")
					}
				}
			}
		}
		text(t)
		return WordBlock{Kind: "paragraph", Runs: []WordRun{{Text: strings.TrimSpace(odtSpace(b.String()))}}}
	}
	out := WordBlock{Kind: "table"}
	header := true
	var rows func(kids []*xnode, isHeader bool)
	rows = func(kids []*xnode, isHeader bool) {
		for _, k := range kids {
			switch k.local {
			case "table-header-rows":
				rows(k.kids, true)
			case "table-rows", "table-row-group":
				rows(k.kids, false)
			case "table-row":
				var row []WordCell
				for _, c := range k.kids {
					if c.local != "table-cell" {
						continue // covered cells are part of a span
					}
					cs, _ := strconv.Atoi(c.at("number-columns-spanned"))
					rs, _ := strconv.Atoi(c.at("number-rows-spanned"))
					rep, _ := strconv.Atoi(c.at("number-columns-repeated"))
					cell := WordCell{Blocks: r.blockList(c.kids, depth, 0), ColSpan: min(max(cs, 1), 64), RowSpan: min(max(rs, 1), 1000)}
					for i := 0; i < min(max(rep, 1), 64); i++ {
						row = append(row, cell)
					}
				}
				if isHeader && header {
					out.HeaderRows++
				} else {
					header = false
				}
				rep, _ := strconv.Atoi(k.at("number-rows-repeated"))
				for i := 0; i < min(max(rep, 1), 64); i++ {
					out.Rows = append(out.Rows, row)
				}
			}
		}
	}
	rows(t.kids, false)
	return out
}
