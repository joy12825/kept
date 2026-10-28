package doctest

import (
	"bytes"
	"encoding/binary"
	"unicode/utf16"
)

// Doc is a Word 97–2003 document: paragraphs and tables in the styles
// below, with the lists below.
//
// Styles (istd): 0 正文 (12 pt), 1 heading 1, 2 heading 2, 3 “标题 3” (a
// style of the user's, a heading by its name), 4 “醒目” (a character
// style: bold and red).
//
// Lists: 1 numbers level 0 “%1.”, level 1 “（%2）” in Chinese numbers,
// level 2 bullets; 2 is the same list starting at 5.
type Doc struct {
	Blocks []Block
}

// Block is a paragraph or a table.
type Block struct {
	Para  *Para
	Table []Row
}

type Para struct {
	Style int
	Align int // 0 left, 1 center, 2 right, 3 justify
	List  int // 1-based; 0 none
	Level int
	Runs  []Run
}

type Run struct {
	Text            string
	Bold, Italic    bool
	Color           string // RRGGBB
	Hidden, Deleted bool
	Style           int    // a character style; 0 none
	Link            string // a HYPERLINK field around the text
	Picture         []byte // a PNG shown inline, Width × Height pixels
	Width, Height   int
	Footnote        string // a footnote referenced after the text
	Endnote         string
}

type Row struct {
	Cells  []Cell
	Widths []int // twips; one per cell
	Header bool
}

type Cell struct {
	Paras []Para
	Merge byte // vertical merge: 0 none, 3 the first cell, 1 merged into the one above
}

// sprms used here
func sprm(op uint16, arg ...byte) []byte {
	return append([]byte{byte(op), byte(op >> 8)}, arg...)
}

func u16(v int) []byte { return []byte{byte(v), byte(v >> 8)} }
func u32(v int) []byte { return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)} }

type chpxRun struct {
	cp0, cp1 int
	grpprl   []byte
}

type papxRun struct {
	cp0, cp1 int
	istd     int
	grpprl   []byte
}

type docWriter struct {
	text      []uint16
	chpx      []chpxRun
	papx      []papxRun
	data      bytes.Buffer
	notes     [2][]string
	refs      [2][]int // the reference marks' CPs
	paraStart int
}

func (w *docWriter) chars(s string, grpprl []byte) {
	cp := len(w.text)
	w.text = append(w.text, utf16.Encode([]rune(s))...)
	if len(w.text) > cp {
		w.chpx = append(w.chpx, chpxRun{cp, len(w.text), grpprl})
	}
}

func (w *docWriter) mark(ch uint16, istd int, grpprl []byte) {
	w.chars(string(rune(ch)), nil)
	w.papx = append(w.papx, papxRun{w.paraStart, len(w.text), istd, grpprl})
	w.paraStart = len(w.text)
}

func runSprms(r Run) []byte {
	var g []byte
	if r.Style != 0 {
		g = append(g, sprm(0x4A30, u16(r.Style)...)...)
	}
	if r.Bold {
		g = append(g, sprm(0x0835, 1)...)
	}
	if r.Italic {
		g = append(g, sprm(0x0836, 1)...)
	}
	if len(r.Color) == 6 {
		var c [3]byte
		for i := range c {
			var v byte
			for _, h := range r.Color[2*i : 2*i+2] {
				v <<= 4
				switch {
				case h >= '0' && h <= '9':
					v |= byte(h - '0')
				case h >= 'A' && h <= 'F':
					v |= byte(h - 'A' + 10)
				}
			}
			c[i] = v
		}
		g = append(g, sprm(0x6870, c[0], c[1], c[2], 0)...)
	}
	if r.Hidden {
		g = append(g, sprm(0x083C, 1)...)
	}
	if r.Deleted {
		g = append(g, sprm(0x0800, 1)...)
	}
	return g
}

func (w *docWriter) para(p Para, istdExtra []byte, end uint16) {
	g := append([]byte(nil), istdExtra...)
	if p.Align != 0 {
		g = append(g, sprm(0x2403, byte(p.Align))...)
	}
	if p.List != 0 {
		g = append(g, sprm(0x460B, u16(p.List)...)...)
		g = append(g, sprm(0x260A, byte(p.Level))...)
	}
	for _, r := range p.Runs {
		w.run(r)
	}
	w.mark(end, p.Style, g)
}

func (w *docWriter) run(r Run) {
	g := runSprms(r)
	spec := append(append([]byte(nil), g...), sprm(0x0855, 1)...)
	switch {
	case r.Picture != nil:
		at := w.data.Len()
		w.picture(r)
		w.chars("\x01", append(spec, sprm(0x6A03, u32(at)...)...))
	case r.Link != "":
		w.chars("\x13", sprm(0x0855, 1))
		w.chars(` HYPERLINK "`+r.Link+`" `, nil)
		w.chars("\x14", sprm(0x0855, 1))
		w.chars(r.Text, g)
		w.chars("\x15", sprm(0x0855, 1))
	default:
		w.chars(r.Text, g)
	}
	for kind, note := range [2]string{r.Footnote, r.Endnote} {
		if note != "" {
			w.refs[kind] = append(w.refs[kind], len(w.text))
			w.notes[kind] = append(w.notes[kind], note)
			w.chars("\x02", sprm(0x0855, 1))
		}
	}
}

// picture writes a PICF and its OfficeArt picture to the Data stream.
func (w *docWriter) picture(r Run) {
	blip := append(append(make([]byte, 16), 0xFF), r.Picture...)
	blipRec := append(append(u16(0x6E0<<4), u16(0xF01E)...), u32(len(blip))...)
	blipRec = append(blipRec, blip...)
	fbse := make([]byte, 36)
	fbse[0], fbse[1] = 6, 6
	fbse[18] = 0xFF
	copy(fbse[20:], u32(len(blipRec)))
	copy(fbse[24:], u32(1))
	fbse = append(fbse, blipRec...)
	art := append([]byte{0x0F, 0x00, 0x04, 0xF0, 0, 0, 0, 0}, append(append(u16(6<<4|2), u16(0xF007)...), u32(len(fbse))...)...)
	art = append(art, fbse...)
	picf := make([]byte, 68)
	copy(picf, u32(68+len(art)))
	copy(picf[4:], u16(68))
	copy(picf[6:], u16(0x64))
	copy(picf[28:], u16(r.Width*15))
	copy(picf[30:], u16(r.Height*15))
	copy(picf[32:], u16(1000))
	copy(picf[34:], u16(1000))
	w.data.Write(picf)
	w.data.Write(art)
}

// Build writes the .doc.
func (d Doc) Build() []byte {
	w := &docWriter{}
	for _, b := range d.Blocks {
		if b.Para != nil {
			w.para(*b.Para, nil, 0x0D)
			continue
		}
		cell := append(sprm(0x2416, 1), sprm(0x6649, u32(1)...)...)
		for _, row := range b.Table {
			for _, c := range row.Cells {
				for i, p := range c.Paras {
					end := uint16(0x0D)
					if i == len(c.Paras)-1 {
						end = 0x07
					}
					w.para(p, cell, end)
				}
			}
			// the row's end: its definition
			def := []byte{byte(len(row.Cells))}
			x := 0
			def = append(def, u16(x)...)
			for _, wd := range row.Widths {
				x += wd
				def = append(def, u16(x)...)
			}
			for _, c := range row.Cells {
				tc := make([]byte, 20)
				copy(tc, u16(int(c.Merge)<<5))
				def = append(def, tc...)
			}
			ttp := append(append([]byte(nil), cell...), sprm(0x2417, 1)...)
			if row.Header {
				ttp = append(ttp, sprm(0x3404, 1)...)
			}
			ttp = append(ttp, sprm(0xD608, append(u16(len(def)+1), def...)...)...)
			w.mark(0x07, 0, ttp)
		}
	}
	ccpText := len(w.text)
	var noteTxt [2][]int
	var ccp [2]int
	for kind := range w.notes {
		start := len(w.text)
		w.paraStart = start
		for _, n := range w.notes[kind] {
			noteTxt[kind] = append(noteTxt[kind], len(w.text)-start)
			w.chars("\x02", sprm(0x0855, 1))
			w.chars(" "+n, nil)
			w.mark(0x0D, 0, nil)
		}
		if len(w.notes[kind]) > 0 {
			noteTxt[kind] = append(noteTxt[kind], len(w.text)-start)
			w.mark(0x0D, 0, nil)
			noteTxt[kind] = append(noteTxt[kind], len(w.text)-start)
		}
		ccp[kind] = len(w.text) - start
	}
	return d.assemble(w, ccpText, ccp, noteTxt)
}

const fcText = 0x400

func (d Doc) assemble(w *docWriter, ccpText int, ccp [2]int, noteTxt [2][]int) []byte {
	le := binary.LittleEndian
	wd := make([]byte, fcText)
	for _, c := range w.text {
		wd = append(wd, u16(int(c))...)
	}
	fc := func(cp int) int { return fcText + 2*cp }
	pad := func() {
		for len(wd)%512 != 0 {
			wd = append(wd, 0)
		}
	}
	pad()
	// character FKPs
	type bte struct {
		fcs []int
		pns []int
	}
	var chpxBte, papxBte bte
	for i := 0; i < len(w.chpx); {
		page := make([]byte, 512)
		free := 511
		var fcs []int
		var offs []int
		j := i
		for ; j < len(w.chpx) && j-i < 100; j++ {
			r := w.chpx[j]
			n := j - i + 1
			need := 0
			if len(r.grpprl) > 0 {
				need = 1 + len(r.grpprl)
			}
			at := (free - need) &^ 1
			if 4*(n+1)+n > at {
				break
			}
			if need > 0 {
				page[at] = byte(len(r.grpprl))
				copy(page[at+1:], r.grpprl)
				offs = append(offs, at/2)
				free = at
			} else {
				offs = append(offs, 0)
			}
			fcs = append(fcs, fc(r.cp0))
		}
		n := j - i
		fcs = append(fcs, fc(w.chpx[j-1].cp1))
		for k, f := range fcs {
			copy(page[4*k:], u32(f))
		}
		for k, o := range offs {
			page[4*(n+1)+k] = byte(o)
		}
		page[511] = byte(n)
		chpxBte.fcs = append(chpxBte.fcs, fcs[0])
		chpxBte.pns = append(chpxBte.pns, len(wd)/512)
		wd = append(wd, page...)
		i = j
		if i == len(w.chpx) {
			chpxBte.fcs = append(chpxBte.fcs, fcs[n])
		}
	}
	// paragraph FKPs
	for i := 0; i < len(w.papx); {
		page := make([]byte, 512)
		free := 511
		var fcs []int
		var offs []int
		j := i
		for ; j < len(w.papx) && j-i < 30; j++ {
			r := w.papx[j]
			n := j - i + 1
			content := append(u16(r.istd), r.grpprl...)
			var blob []byte
			if len(content)%2 == 1 {
				blob = append([]byte{byte((len(content) + 1) / 2)}, content...)
			} else {
				blob = append([]byte{0, byte(len(content) / 2)}, content...)
			}
			at := (free - len(blob)) &^ 1
			if 4*(n+1)+13*n > at {
				break
			}
			copy(page[at:], blob)
			offs = append(offs, at/2)
			free = at
			fcs = append(fcs, fc(r.cp0))
		}
		n := j - i
		fcs = append(fcs, fc(w.papx[j-1].cp1))
		for k, f := range fcs {
			copy(page[4*k:], u32(f))
		}
		for k, o := range offs {
			page[4*(n+1)+13*k] = byte(o)
		}
		page[511] = byte(n)
		papxBte.fcs = append(papxBte.fcs, fcs[0])
		papxBte.pns = append(papxBte.pns, len(wd)/512)
		wd = append(wd, page...)
		i = j
		if i == len(w.papx) {
			papxBte.fcs = append(papxBte.fcs, fcs[n])
		}
	}

	// the table stream
	var tbl []byte
	fcLcb := make([][2]int, 93)
	put := func(i int, b []byte) {
		fcLcb[i] = [2]int{len(tbl), len(b)}
		tbl = append(tbl, b...)
	}
	put(1, stylesheet())
	for kind, which := range [2][2]int{{2, 3}, {46, 47}} {
		if len(w.refs[kind]) == 0 {
			continue
		}
		var ref []byte
		for _, cp := range w.refs[kind] {
			ref = append(ref, u32(cp)...)
		}
		ref = append(ref, u32(ccpText)...)
		for range w.refs[kind] {
			ref = append(ref, u16(1)...)
		}
		put(which[0], ref)
		var txt []byte
		for _, cp := range noteTxt[kind] {
			txt = append(txt, u32(cp)...)
		}
		put(which[1], txt)
	}
	plcBte := func(b bte) []byte {
		var out []byte
		for _, f := range b.fcs {
			out = append(out, u32(f)...)
		}
		for _, p := range b.pns {
			out = append(out, u32(p)...)
		}
		return out
	}
	put(12, plcBte(chpxBte))
	put(13, plcBte(papxBte))
	total := len(w.text)
	pcdt := append(u32(0), u32(total)...)
	pcdt = append(pcdt, 0, 0)
	pcdt = append(pcdt, u32(fcText)...)
	pcdt = append(pcdt, 0, 0)
	put(33, append(append([]byte{2}, u32(len(pcdt))...), pcdt...))
	lst, lvls := lists()
	put(73, lst)
	tbl = append(tbl, lvls...)
	put(74, overrides())

	// the FIB
	fib := wd[:fcText]
	le.PutUint16(fib[0:], 0xA5EC)
	le.PutUint16(fib[2:], 0x00C1)
	le.PutUint16(fib[6:], 0x0804)
	le.PutUint16(fib[10:], 0x0200|0x1000) // fWhichTblStm, fExtChar
	le.PutUint16(fib[12:], 0x00BF)
	le.PutUint16(fib[32:], 14)
	le.PutUint16(fib[62:], 22)
	le.PutUint32(fib[64:], uint32(len(wd)))
	le.PutUint32(fib[76:], uint32(ccpText))
	le.PutUint32(fib[80:], uint32(ccp[0]))
	le.PutUint32(fib[96:], uint32(ccp[1]))
	le.PutUint16(fib[152:], 93)
	for i, p := range fcLcb {
		le.PutUint32(fib[154+8*i:], uint32(p[0]))
		le.PutUint32(fib[158+8*i:], uint32(p[1]))
	}
	return compound([]stream{{"WordDocument", wd}, {"1Table", tbl}, {"Data", w.data.Bytes()}})
}

func xst(s string) []byte {
	u := utf16.Encode([]rune(s))
	out := u16(len(u))
	for _, c := range u {
		out = append(out, u16(int(c))...)
	}
	return out
}

// stylesheet: the styles named on Doc.
func stylesheet() []byte {
	type st struct {
		sti, stk, base int
		name           string
		papx, chpx     []byte
	}
	hps := func(n int) []byte { return sprm(0x4A43, u16(n)...) }
	styles := []st{
		{0, 1, 0xFFF, "Normal", nil, hps(24)},
		{1, 1, 0, "heading 1", nil, append(sprm(0x0835, 1), hps(32)...)},
		{2, 1, 0, "heading 2", nil, append(sprm(0x0835, 1), hps(28)...)},
		{4094, 1, 0, "标题 3", nil, sprm(0x0835, 1)},
		{4094, 2, 0xFFF, "醒目", nil, append(sprm(0x0835, 1), sprm(0x6870, 0xC0, 0, 0, 0)...)},
	}
	stshi := make([]byte, 20)
	copy(stshi, u16(len(styles)))
	copy(stshi[2:], u16(10))
	out := append(u16(len(stshi)), stshi...)
	upx := func(b []byte) []byte {
		o := append(u16(len(b)), b...)
		if len(b)%2 == 1 {
			o = append(o, 0)
		}
		return o
	}
	for _, s := range styles {
		cupx := 1
		if s.stk == 1 {
			cupx = 2
		}
		std := append(u16(s.sti), u16(s.stk|s.base<<4)...)
		std = append(std, u16(cupx)...)
		std = append(std, 0, 0, 0, 0)
		name := xst(s.name)
		std = append(std, append(name, 0, 0)...)
		if s.stk == 1 {
			std = append(std, upx(append(u16(0), s.papx...))...)
		}
		std = append(std, upx(s.chpx)...)
		out = append(out, u16(len(std))...)
		out = append(out, std...)
	}
	return out
}

// lists: one list of three levels (PlfLst, then its levels).
func lists() ([]byte, []byte) {
	lstf := make([]byte, 28)
	copy(lstf, u32(100))
	for i := 0; i < 9; i++ {
		copy(lstf[8+2*i:], u16(0xFFF))
	}
	plf := append(u16(1), lstf...)
	var lvls []byte
	for i := 0; i < 9; i++ {
		f := make([]byte, 28)
		copy(f, u32(1))
		var text string
		switch i {
		case 0:
			f[4], text = 0, "\x00."
		case 1:
			f[4], text = 37, "（\x01）"
		default:
			f[4], text = 23, ""
		}
		lvls = append(lvls, f...)
		lvls = append(lvls, xst(text)...)
	}
	return plf, lvls
}

// overrides: list 1, and list 2 starting at 5.
func overrides() []byte {
	lfo := func(clv int) []byte {
		b := make([]byte, 16)
		copy(b, u32(100))
		b[12] = byte(clv)
		return b
	}
	out := append(u32(2), lfo(0)...)
	out = append(out, lfo(1)...)
	out = append(out, u32(0)...)                         // list 1's data
	out = append(out, u32(0)...)                         // list 2's data
	out = append(out, append(u32(5), u32(0|0x10)...)...) // level 0 starts at 5
	return out
}

type stream struct {
	name string
	data []byte
}

// compound writes a compound file (version 3) with the streams in its
// root storage, each at least 4096 bytes so none is in the mini stream.
func compound(streams []stream) []byte {
	le := binary.LittleEndian
	const end, free, fatSect = 0xFFFFFFFE, 0xFFFFFFFF, 0xFFFFFFFD
	var sectors [][]byte
	starts := make([]int, len(streams))
	nSec := 1 // the directory
	for i := range streams {
		for len(streams[i].data) < 4096 || len(streams[i].data)%512 != 0 {
			streams[i].data = append(streams[i].data, 0)
		}
		nSec += len(streams[i].data) / 512
	}
	nFAT := 1
	for (nSec+nFAT)*4 > nFAT*512 {
		nFAT++
	}
	fat := make([]uint32, nFAT*128)
	for i := range fat {
		fat[i] = free
	}
	for i := 0; i < nFAT; i++ {
		fat[i] = fatSect
	}
	dirSect := nFAT
	fat[dirSect] = end
	next := nFAT + 1
	for i, s := range streams {
		n := len(s.data) / 512
		starts[i] = next
		for k := 0; k < n; k++ {
			if k == n-1 {
				fat[next+k] = end
			} else {
				fat[next+k] = uint32(next + k + 1)
			}
		}
		next += n
	}
	for i := 0; i < nFAT; i++ {
		sec := make([]byte, 512)
		for k := 0; k < 128; k++ {
			le.PutUint32(sec[4*k:], fat[i*128+k])
		}
		sectors = append(sectors, sec)
	}
	dir := make([]byte, 512)
	entry := func(i int, name string, kind byte, left, right, child uint32, start uint32, size int) {
		e := dir[128*i : 128*(i+1)]
		u := utf16.Encode([]rune(name))
		for k, c := range u {
			le.PutUint16(e[2*k:], c)
		}
		le.PutUint16(e[64:], uint16(2*len(u)+2))
		e[66], e[67] = kind, 1
		le.PutUint32(e[68:], left)
		le.PutUint32(e[72:], right)
		le.PutUint32(e[76:], child)
		le.PutUint32(e[116:], start)
		le.PutUint64(e[120:], uint64(size))
	}
	entry(0, "Root Entry", 5, free, free, 1, end, 0)
	entry(1, streams[0].name, 2, 2, 3, free, uint32(starts[0]), len(streams[0].data))
	entry(2, streams[1].name, 2, free, free, free, uint32(starts[1]), len(streams[1].data))
	entry(3, streams[2].name, 2, free, free, free, uint32(starts[2]), len(streams[2].data))
	sectors = append(sectors, dir)
	for _, s := range streams {
		for k := 0; k < len(s.data); k += 512 {
			sectors = append(sectors, s.data[k:k+512])
		}
	}
	hdr := make([]byte, 512)
	copy(hdr, "\xD0\xCF\x11\xE0\xA1\xB1\x1A\xE1")
	le.PutUint16(hdr[24:], 0x3E)
	le.PutUint16(hdr[26:], 3)
	le.PutUint16(hdr[28:], 0xFFFE)
	le.PutUint16(hdr[30:], 9)
	le.PutUint16(hdr[32:], 6)
	le.PutUint32(hdr[44:], uint32(nFAT))
	le.PutUint32(hdr[48:], uint32(dirSect))
	le.PutUint32(hdr[56:], 4096)
	le.PutUint32(hdr[60:], end)
	le.PutUint32(hdr[68:], end)
	for i := 0; i < 109; i++ {
		v := uint32(free)
		if i < nFAT {
			v = uint32(i)
		}
		le.PutUint32(hdr[76+4*i:], v)
	}
	out := hdr
	for _, s := range sectors {
		out = append(out, s...)
	}
	return out
}
