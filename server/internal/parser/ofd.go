package parser

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-pdf/fpdf"

	"kept/server/internal/watermark"
)

// OFD (v1.9.3): the Chinese fixed-layout format of e-invoices and official
// papers (GB/T 33190), a zip of XML pages. Read here with the standard
// library: the text, in reading order, and a PDF drawn with fpdf (text,
// lines and pictures; not the seals of signatures, nor annotations).

const ofdType = "application/ofd"

// ErrOFD: the file is not an OFD we can read.
var ErrOFD = &Error{Message: "OFD 文件读不出来，可能已损坏", Permanent: true}

const (
	ofdMaxPages   = 500
	ofdMaxObjects = 200000
	ofdPartMax    = 32 << 20
)

// isOFD reports whether the zip at src is an OFD (it has OFD.xml at its root).
func isOFD(src string) bool {
	z, err := zip.OpenReader(src)
	if err != nil {
		return false
	}
	defer z.Close()
	for _, f := range z.File {
		if strings.EqualFold(f.Name, "OFD.xml") {
			return true
		}
	}
	return false
}

// ofdPage is a page: its size in millimetres and what is on it, in drawing
// order.
type ofdPage struct {
	w, h float64
	objs []ofdObj
}

type ofdObj struct {
	kind     string // text, path, image
	box      [4]float64
	ctm      [6]float64 // identity when absent
	font     string
	size     float64
	fill     []int
	stroke   []int
	lineW    float64
	doFill   bool
	doStroke bool
	codes    []ofdCode
	pathData string
	image    string // the image file in the package
	format   string
	sealData []byte // a signature's seal: its picture, or an OFD (v1.11.3)
}

type ofdCode struct {
	x, y   float64
	dx, dy []float64
	text   string
}

type ofdDoc struct {
	z       io.Closer // nil for a package read from memory (a seal's picture)
	files   map[string]*zip.File
	media   map[string][2]string // ID → file, format
	pages   []ofdPage
	pageIDs []string // the pages' IDs, for where seals go (v1.11.3)
	signs   string   // the signatures' list, "" when none (v1.11.3)
	count   int
	broken  bool
}

func (d *ofdDoc) open(name string) ([]byte, error) {
	f := d.files[strings.TrimPrefix(path.Clean("/"+name), "/")]
	if f == nil {
		return nil, fmt.Errorf("ofd: no %s", name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, ofdPartMax))
}

// resolve finds a location given in the file at from: absolute ("/Doc_0/…")
// or relative to that file's folder.
func resolve(from, loc string) string {
	loc = strings.TrimSpace(loc)
	if strings.HasPrefix(loc, "/") {
		return path.Clean(loc)
	}
	return path.Join(path.Dir("/"+from), loc)
}

// el is a generic XML element: its local name, attributes, text and
// children (OFD files use a namespace prefix, ofd:, that is ignored).
type el struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Text    string     `xml:",chardata"`
	Kids    []el       `xml:",any"`
}

func (e el) attr(name string) string {
	for _, a := range e.Attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func (e el) kid(name string) *el {
	for i := range e.Kids {
		if e.Kids[i].XMLName.Local == name {
			return &e.Kids[i]
		}
	}
	return nil
}

func (e el) all(name string) []el {
	var out []el
	for _, k := range e.Kids {
		if k.XMLName.Local == name {
			out = append(out, k)
		}
	}
	return out
}

func (d *ofdDoc) xml(name string) (el, error) {
	b, err := d.open(name)
	if err != nil {
		return el{}, err
	}
	var root el
	dec := xml.NewDecoder(bytes.NewReader(b))
	dec.Strict = false
	if err := dec.Decode(&root); err != nil {
		return el{}, err
	}
	return root, nil
}

func floats(s string) []float64 {
	var out []float64
	for _, f := range strings.Fields(s) {
		if v, err := strconv.ParseFloat(f, 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// deltas expands a DeltaX/DeltaY list: "g 3 2.5" is 2.5 three times.
func deltas(s string) []float64 {
	f := strings.Fields(s)
	var out []float64
	for i := 0; i < len(f); i++ {
		if f[i] == "g" && i+2 < len(f) {
			n, _ := strconv.Atoi(f[i+1])
			v, _ := strconv.ParseFloat(f[i+2], 64)
			for k := 0; k < n && len(out) < 100000; k++ {
				out = append(out, v)
			}
			i += 2
			continue
		}
		if v, err := strconv.ParseFloat(f[i], 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}

func ofdColor(e *el) []int {
	if e == nil {
		return nil
	}
	v := floats(e.attr("Value"))
	switch len(v) {
	case 1:
		return []int{int(v[0]), int(v[0]), int(v[0])}
	case 3, 4:
		return []int{int(v[0]), int(v[1]), int(v[2])}
	}
	return nil
}

func box(s string) (b [4]float64, ok bool) {
	v := floats(s)
	if len(v) != 4 {
		return b, false
	}
	return [4]float64{v[0], v[1], v[2], v[3]}, true
}

// readRes notes the pictures a resource file lists.
func (d *ofdDoc) readRes(name string) {
	r, err := d.xml(name)
	if err != nil {
		return
	}
	base := resolve(name, r.attr("BaseLoc"))
	if r.attr("BaseLoc") == "" {
		base = path.Dir("/" + name)
	}
	var walk func(e el)
	walk = func(e el) {
		for _, k := range e.Kids {
			if k.XMLName.Local == "MultiMedia" {
				if mf := k.kid("MediaFile"); mf != nil {
					d.media[k.attr("ID")] = [2]string{resolve(strings.TrimPrefix(base, "/")+"/x", strings.TrimSpace(mf.Text)), strings.ToLower(k.attr("Format"))}
				}
				continue
			}
			walk(k)
		}
	}
	walk(r)
}

// layer collects the drawable objects of a layer or page block.
func (d *ofdDoc) layer(e el, out *[]ofdObj) {
	for _, k := range e.Kids {
		if d.count >= ofdMaxObjects {
			return
		}
		switch k.XMLName.Local {
		case "PageBlock":
			d.layer(k, out)
			continue
		case "TextObject", "PathObject", "ImageObject":
		default:
			continue
		}
		b, ok := box(k.attr("Boundary"))
		if !ok {
			continue
		}
		o := ofdObj{box: b, ctm: [6]float64{1, 0, 0, 1, 0, 0}, lineW: 0.353}
		if c := floats(k.attr("CTM")); len(c) == 6 {
			copy(o.ctm[:], c)
		}
		if lw := floats(k.attr("LineWidth")); len(lw) == 1 && lw[0] > 0 {
			o.lineW = lw[0]
		}
		o.fill, o.stroke = ofdColor(k.kid("FillColor")), ofdColor(k.kid("StrokeColor"))
		switch k.XMLName.Local {
		case "TextObject":
			o.kind, o.font = "text", k.attr("Font")
			if s := floats(k.attr("Size")); len(s) == 1 {
				o.size = s[0]
			}
			if o.size <= 0 {
				o.size = 3.5
			}
			for _, tc := range k.all("TextCode") {
				c := ofdCode{text: tc.Text, dx: deltas(tc.attr("DeltaX")), dy: deltas(tc.attr("DeltaY"))}
				c.x, _ = strconv.ParseFloat(strings.TrimSpace(tc.attr("X")), 64)
				c.y, _ = strconv.ParseFloat(strings.TrimSpace(tc.attr("Y")), 64)
				o.codes = append(o.codes, c)
			}
		case "PathObject":
			o.kind = "path"
			o.doStroke = k.attr("Stroke") != "false"
			o.doFill = k.attr("Fill") == "true"
			if ad := k.kid("AbbreviatedData"); ad != nil {
				o.pathData = ad.Text
			}
		case "ImageObject":
			o.kind = "image"
			m, ok := d.media[k.attr("ResourceID")]
			if !ok {
				continue
			}
			o.image, o.format = m[0], m[1]
		}
		d.count++
		*out = append(*out, o)
	}
}

// readOFD reads the first document of the OFD at src.
func readOFD(src string) (*ofdDoc, error) {
	z, err := zip.OpenReader(src)
	if err != nil {
		return nil, ErrOFD
	}
	return readOFDFrom(&z.Reader, z)
}

// readOFDFrom reads the first document of an OFD package; closer is
// closed with it (nil for one read from memory).
func readOFDFrom(z *zip.Reader, closer io.Closer) (*ofdDoc, error) {
	d := &ofdDoc{z: closer, files: map[string]*zip.File{}, media: map[string][2]string{}}
	for _, f := range z.File {
		d.files[strings.TrimPrefix(path.Clean("/"+f.Name), "/")] = f
		if strings.EqualFold(f.Name, "OFD.xml") {
			d.files["OFD.xml"] = f
		}
	}
	fail := func() (*ofdDoc, error) { d.Close(); return nil, ErrOFD }
	root, err := d.xml("OFD.xml")
	if err != nil {
		return fail()
	}
	body := root.kid("DocBody")
	if body == nil || body.kid("DocRoot") == nil {
		return fail()
	}
	docPath := strings.TrimPrefix(resolve("OFD.xml", body.kid("DocRoot").Text), "/")
	if sg := body.kid("Signatures"); sg != nil && strings.TrimSpace(sg.Text) != "" {
		d.signs = strings.TrimPrefix(resolve("OFD.xml", sg.Text), "/")
	}
	doc, err := d.xml(docPath)
	if err != nil {
		return fail()
	}
	var defW, defH float64 = 210, 297
	templates := map[string]string{}
	if cd := doc.kid("CommonData"); cd != nil {
		if pa := cd.kid("PageArea"); pa != nil && pa.kid("PhysicalBox") != nil {
			if b, ok := box(pa.kid("PhysicalBox").Text); ok && b[2] > 0 && b[3] > 0 {
				defW, defH = b[2], b[3]
			}
		}
		for _, name := range []string{"PublicRes", "DocumentRes"} {
			for _, r := range cd.all(name) {
				d.readRes(strings.TrimPrefix(resolve(docPath, r.Text), "/"))
			}
		}
		for _, t := range cd.all("TemplatePage") {
			templates[t.attr("ID")] = strings.TrimPrefix(resolve(docPath, t.attr("BaseLoc")), "/")
		}
	}
	pages := doc.kid("Pages")
	if pages == nil {
		return fail()
	}
	for _, p := range pages.all("Page") {
		if len(d.pages) >= ofdMaxPages {
			break
		}
		loc := strings.TrimPrefix(resolve(docPath, p.attr("BaseLoc")), "/")
		pg, err := d.xml(loc)
		if err != nil {
			d.broken = true
			continue
		}
		page := ofdPage{w: defW, h: defH}
		if a := pg.kid("Area"); a != nil && a.kid("PhysicalBox") != nil {
			if b, ok := box(a.kid("PhysicalBox").Text); ok && b[2] > 0 && b[3] > 0 {
				page.w, page.h = b[2], b[3]
			}
		}
		for _, r := range pg.all("PageRes") {
			d.readRes(strings.TrimPrefix(resolve(loc, r.Text), "/"))
		}
		var back, front []ofdObj
		for _, t := range pg.all("Template") {
			tl, ok := templates[t.attr("TemplateID")]
			if !ok {
				continue
			}
			tp, err := d.xml(tl)
			if err != nil {
				continue
			}
			var objs []ofdObj
			if c := tp.kid("Content"); c != nil {
				for _, l := range c.all("Layer") {
					d.layer(l, &objs)
				}
			}
			if t.attr("ZOrder") == "Foreground" {
				front = append(front, objs...)
			} else {
				back = append(back, objs...)
			}
		}
		page.objs = back
		if c := pg.kid("Content"); c != nil {
			for _, l := range c.all("Layer") {
				d.layer(l, &page.objs)
			}
		}
		page.objs = append(page.objs, front...)
		d.pages = append(d.pages, page)
		d.pageIDs = append(d.pageIDs, p.attr("ID"))
	}
	if len(d.pages) == 0 {
		return fail()
	}
	return d, nil
}

func (d *ofdDoc) Close() error {
	if d.z == nil {
		return nil
	}
	return d.z.Close()
}

// apply maps a point within an object's boundary to the page.
func (o ofdObj) apply(x, y float64) (float64, float64) {
	c := o.ctm
	return o.box[0] + c[0]*x + c[2]*y + c[4], o.box[1] + c[1]*x + c[3]*y + c[5]
}

func (o ofdObj) scale() float64 {
	c := o.ctm
	s := math.Sqrt(math.Abs(c[0]*c[3] - c[1]*c[2]))
	if s == 0 {
		return 1
	}
	return s
}

type glyphRun struct {
	x, y, size float64
	text       string
}

// runs lays out an object's text: one run per glyph when the positions are
// given, else per text code.
func (o ofdObj) runs() []glyphRun {
	var out []glyphRun
	size := o.size * o.scale()
	for _, c := range o.codes {
		text := strings.TrimRight(c.text, "\r\n")
		if len(c.dx) == 0 && len(c.dy) == 0 {
			x, y := o.apply(c.x, c.y)
			out = append(out, glyphRun{x, y, size, text})
			continue
		}
		x, y := c.x, c.y
		i := 0
		for _, r := range text {
			px, py := o.apply(x, y)
			out = append(out, glyphRun{px, py, size, string(r)})
			if i < len(c.dx) {
				x += c.dx[i]
			} else {
				x += o.size
			}
			if i < len(c.dy) {
				y += c.dy[i]
			}
			i++
		}
	}
	return out
}

// text is the page's text in reading order: runs grouped into lines by
// their baseline, lines top to bottom, runs left to right.
func (p ofdPage) text() string {
	var runs []glyphRun
	for _, o := range p.objs {
		if o.kind == "text" {
			runs = append(runs, o.runs()...)
		}
	}
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].y < runs[j].y })
	var lines [][]glyphRun
	for _, r := range runs {
		if n := len(lines); n > 0 {
			last := lines[n-1]
			if math.Abs(last[0].y-r.y) <= math.Max(0.6, r.size*0.4) {
				lines[n-1] = append(last, r)
				continue
			}
		}
		lines = append(lines, []glyphRun{r})
	}
	var b strings.Builder
	for _, l := range lines {
		sort.SliceStable(l, func(i, j int) bool { return l[i].x < l[j].x })
		end := -1.0
		for k, r := range l {
			// a gap wider than a character is a space
			if k > 0 && r.x-end > r.size*0.9 {
				b.WriteByte(' ')
			}
			b.WriteString(r.text)
			end = r.x + float64(utf8.RuneCountInString(r.text))*r.size*0.9
			if utf8.RuneCountInString(r.text) == 1 && r.text[0] < 0x80 {
				end = r.x + r.size*0.55
			}
		}
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

// Text is the whole document's text, pages apart by a blank line.
func (d *ofdDoc) Text() string {
	parts := make([]string, 0, len(d.pages))
	for _, p := range d.pages {
		if t := p.text(); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "\n\n")
}

// errNoFont: there is no Chinese font to draw the pages with.
var errNoFont = errors.New("ofd: no Chinese font")

// PDF draws the document; the text with the Chinese font, sizes in mm.
func (d *ofdDoc) PDF(w io.Writer) error {
	fb, err := watermark.FontData()
	if err != nil {
		return errNoFont
	}
	pdf := fpdf.NewCustom(&fpdf.InitType{UnitStr: "mm", Size: fpdf.SizeType{Wd: d.pages[0].w, Ht: d.pages[0].h}})
	pdf.SetMargins(0, 0, 0)
	pdf.SetAutoPageBreak(false, 0)
	pdf.SetCompression(true)
	pdf.AddUTF8FontFromBytes("cjk", "", fb)
	images := map[string]bool{}
	for i, p := range d.pages {
		pdf.AddPageFormat("P", fpdf.SizeType{Wd: p.w, Ht: p.h})
		d.draw(pdf, p.objs, images, fmt.Sprintf("seal-%d-", i))
	}
	return pdf.Output(w)
}

// draw draws a page's objects; prefix names the seals' pictures.
func (d *ofdDoc) draw(pdf *fpdf.Fpdf, objs []ofdObj, images map[string]bool, prefix string) {
	for n, o := range objs {
		switch o.kind {
		case "text":
			c := o.fill
			if c == nil {
				c = []int{0, 0, 0}
			}
			pdf.SetTextColor(c[0], c[1], c[2])
			for _, r := range o.runs() {
				if strings.TrimSpace(r.text) == "" {
					continue
				}
				pdf.SetFont("cjk", "", r.size*72/25.4)
				pdf.Text(r.x, r.y, r.text)
			}
		case "path":
			drawPath(pdf, o)
		case "image":
			d.drawImage(pdf, o, images)
		case "seal":
			drawSeal(pdf, o, images, fmt.Sprintf("%s%d", prefix, n))
		}
	}
}

// drawSeal draws a signature's seal in its boundary: a picture, or the
// first page of a vector seal (an OFD) scaled to fit.
func drawSeal(pdf *fpdf.Fpdf, o ofdObj, images map[string]bool, name string) {
	b := o.box
	switch o.format {
	case "png", "jpg":
		kind := strings.ToUpper(o.format)
		pdf.RegisterImageOptionsReader(name, fpdf.ImageOptions{ImageType: kind}, bytes.NewReader(o.sealData))
		if !pdf.Ok() {
			pdf.ClearError()
			return
		}
		pdf.ImageOptions(name, b[0], b[1], b[2], b[3], false, fpdf.ImageOptions{ImageType: kind}, 0, "")
	case "ofd":
		sd, err := sealDoc(o.sealData)
		if err != nil {
			return
		}
		defer sd.Close()
		pg := sd.pages[0]
		if pg.w <= 0 || pg.h <= 0 {
			return
		}
		pdf.TransformBegin()
		pdf.TransformTranslate(b[0], b[1])
		pdf.TransformScale(b[2]/pg.w*100, b[3]/pg.h*100, 0, 0)
		sd.draw(pdf, pg.objs, images, name+"-")
		pdf.TransformEnd()
	}
}

// drawPath draws an AbbreviatedData path: S/M move, L line, Q and B
// curves, A arcs (as a line to their end), C closes.
func drawPath(pdf *fpdf.Fpdf, o ofdObj) {
	if !o.doStroke && !o.doFill {
		return
	}
	f := strings.Fields(o.pathData)
	num := func(i int) float64 {
		if i < len(f) {
			v, _ := strconv.ParseFloat(f[i], 64)
			return v
		}
		return 0
	}
	started := false
	for i := 0; i < len(f); {
		cmd := f[i]
		switch cmd {
		case "S", "M":
			x, y := o.apply(num(i+1), num(i+2))
			pdf.MoveTo(x, y)
			started, i = true, i+3
		case "L":
			x, y := o.apply(num(i+1), num(i+2))
			if started {
				pdf.LineTo(x, y)
			}
			i += 3
		case "Q":
			cx, cy := o.apply(num(i+1), num(i+2))
			x, y := o.apply(num(i+3), num(i+4))
			if started {
				pdf.CurveTo(cx, cy, x, y)
			}
			i += 5
		case "B":
			x1, y1 := o.apply(num(i+1), num(i+2))
			x2, y2 := o.apply(num(i+3), num(i+4))
			x, y := o.apply(num(i+5), num(i+6))
			if started {
				pdf.CurveBezierCubicTo(x1, y1, x2, y2, x, y)
			}
			i += 7
		case "A":
			x, y := o.apply(num(i+6), num(i+7))
			if started {
				pdf.LineTo(x, y)
			}
			i += 8
		case "C":
			if started {
				pdf.ClosePath()
			}
			i++
		default:
			i++
		}
	}
	if !started {
		return
	}
	style := ""
	if o.doFill {
		c := o.fill
		if c == nil {
			c = []int{0, 0, 0}
		}
		pdf.SetFillColor(c[0], c[1], c[2])
		style += "F"
	}
	if o.doStroke {
		c := o.stroke
		if c == nil {
			c = []int{0, 0, 0}
		}
		pdf.SetDrawColor(c[0], c[1], c[2])
		pdf.SetLineWidth(o.lineW * o.scale())
		style += "D"
	}
	pdf.DrawPath(style)
}

func (d *ofdDoc) drawImage(pdf *fpdf.Fpdf, o ofdObj, seen map[string]bool) {
	kind := strings.ToUpper(strings.TrimPrefix(path.Ext(o.image), "."))
	switch strings.ToUpper(o.format) {
	case "PNG", "JPG", "JPEG", "GIF":
		kind = strings.ToUpper(o.format)
	}
	if kind == "JPEG" {
		kind = "JPG"
	}
	if kind != "PNG" && kind != "JPG" && kind != "GIF" {
		return // other formats (JB2, BMP) are left out
	}
	if !seen[o.image] {
		b, err := d.open(o.image)
		if err != nil {
			return
		}
		pdf.RegisterImageOptionsReader(o.image, fpdf.ImageOptions{ImageType: kind}, bytes.NewReader(b))
		if !pdf.Ok() {
			pdf.ClearError()
			return
		}
		seen[o.image] = true
	}
	// the CTM maps the unit square to the picture's size within the boundary
	w, h := o.ctm[0], o.ctm[3]
	if w <= 0 || h <= 0 {
		w, h = o.box[2], o.box[3]
	}
	pdf.ImageOptions(o.image, o.box[0]+o.ctm[4], o.box[1]+o.ctm[5], w, h, false, fpdf.ImageOptions{ImageType: kind}, 0, "")
}

// parseOFD reads the text and draws the searchable PDF (none without a
// Chinese font: a warning, and the text alone).
func parseOFD(src, workDir string, res *Result) error {
	d, err := readOFD(src)
	if err != nil {
		return err
	}
	defer d.Close()
	res.Content, res.PageCount = d.Text(), len(d.pages)
	d.sealPictures() // v1.11.3: the seals of signatures, where they were stamped
	if d.broken {
		res.Warnings = append(res.Warnings, "OFD 里有的页面读不出来，已跳过")
	}
	var b bytes.Buffer
	if err := d.PDF(&b); err != nil {
		if errors.Is(err, errNoFont) {
			res.Warnings = append(res.Warnings, "服务器没有中文字体，OFD 只读出了文字，没有生成可搜索版")
			return nil
		}
		res.Warnings = append(res.Warnings, "OFD 没能画成 PDF："+err.Error())
		return nil
	}
	dst := filepath.Join(workDir, "ofd.pdf")
	if err := os.WriteFile(dst, b.Bytes(), 0o600); err != nil {
		return err
	}
	res.ArchivePath = dst
	return nil
}
