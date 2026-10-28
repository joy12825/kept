package parser

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// The reading view of a Word 97–2003 document (.doc, v1.11.1), written
// here from [MS-DOC]. The text comes from the piece table; paragraph and
// character formatting from the formatted disk pages (FKPs), on top of
// the stylesheet; lists from the list tables; tables from the paragraph
// marks and the row's table definition. Floating pictures and text boxes
// are left to the searchable PDF.

const DocMIME = "application/msword"

// ErrWordEncrypted: the document is protected with a password.
var ErrWordEncrypted = errors.New("parser: the Word document is encrypted")

// ErrWordOld: a Word 95 or older document.
var ErrWordOld = errors.New("parser: a Word document older than Word 97")

const maxDocChars = 20 << 20

// the FIB's FcLcb pairs read here (FibRgFcLcb97)
const (
	fibStshf       = 1
	fibPlcffndRef  = 2
	fibPlcffndTxt  = 3
	fibPlcfBteChpx = 12
	fibPlcfBtePapx = 13
	fibClx         = 33
	fibPlcfendRef  = 46
	fibPlcfendTxt  = 47
	fibPlfLst      = 73
	fibPlfLfo      = 74
)

type docPiece struct {
	cp0, cp1   uint32
	fc         uint32
	compressed bool
}

// docFkpRun is a run of text with the same paragraph or character
// properties: the bytes [fc0, fc1) of the WordDocument stream.
type docFkpRun struct {
	fc0, fc1 uint32
	grpprl   []byte
	istd     uint16 // paragraphs
}

type docStyle struct {
	sti    uint16
	stk    byte // 1 paragraph, 2 character
	base   uint16
	name   string
	papx   []byte
	chpx   []byte
	parsed bool
}

// docChar is a run's character formatting.
type docChar struct {
	bold, italic, strike, underline bool
	color, highlight                string
	hps                             int // half-points; 0 not set
	iss                             byte
	vanish, deleted, spec, ole, fd  bool
	picLoc                          int32
	hasPic                          bool
	istd                            uint16
	hasIstd                         bool
}

// docPara is a paragraph's formatting.
type docPara struct {
	istd                uint16
	jc                  int // -1 not set
	inTable, ttp        bool
	itap                int
	innerCell, innerTtp bool
	outline             int // -1 not set
	ilfo, ilvl          int
	hasIlfo             bool
	header              bool
	tdef                []byte
	vmerge              map[int]byte
}

type docReader struct {
	wd, tbl, data []byte
	fc            [][2]uint32
	ccp           [8]uint32 // text, footnotes, headers, macros, annotations, endnotes, text boxes, header text boxes
	text          []uint16
	fcs           []uint32
	papx, chpx    []docFkpRun
	styles        []docStyle
	num           *numbering
	baseHps       int
	doc           *WordDoc
	blocks        int
	fields        []*fieldState
	notes         map[uint32][2]int // reference cp → kind (0 foot, 1 end), index
	noteText      [2][]uint32
	noteNums      map[string]int
	inNote        bool
	images        map[int32]int
	chpxAt        int // the last CHPX run found, to look up the next one quickly
}

// readDoc reads the .doc at src for the reading view.
func readDoc(src string) (*WordDoc, error) {
	c, err := readCFB(src)
	if err != nil {
		if errors.Is(err, ErrWordTooBig) {
			return nil, err
		}
		return nil, ErrNotWord
	}
	r, err := openDoc(c)
	if err != nil {
		return nil, err
	}
	r.read()
	return r.doc, nil
}

func openDoc(c *cfbFile) (*docReader, error) {
	wd, err := c.stream("WordDocument")
	if err != nil || len(wd) < 154 {
		return nil, ErrNotWord
	}
	le := binary.LittleEndian
	if le.Uint16(wd) != 0xA5EC {
		return nil, ErrNotWord
	}
	if nFib := le.Uint16(wd[2:]); nFib < 0xC0 {
		return nil, ErrWordOld
	}
	flags := le.Uint16(wd[10:])
	if flags&0x0100 != 0 || flags&0x8000 != 0 { // fEncrypted, fObfuscated
		return nil, ErrWordEncrypted
	}
	table := "0Table"
	if flags&0x0200 != 0 {
		table = "1Table"
	}
	r := &docReader{wd: wd, doc: &WordDoc{BaseSize: 10}, notes: map[uint32][2]int{}, noteNums: map[string]int{}, images: map[int32]int{}}
	if r.tbl, err = c.stream(table); err != nil || r.tbl == nil {
		return nil, ErrNotWord
	}
	r.data, _ = c.stream("Data")
	// FibRgW97 (csw words), FibRgLw97 (cslw longs), FibRgFcLcb (cbRgFcLcb pairs)
	off := 32
	csw := int(le.Uint16(wd[off:]))
	off += 2 + 2*csw
	if off+2 > len(wd) {
		return nil, ErrNotWord
	}
	cslw := int(le.Uint16(wd[off:]))
	lw := off + 2
	off = lw + 4*cslw
	if cslw < 11 || off+2 > len(wd) {
		return nil, ErrNotWord
	}
	for i := range r.ccp {
		r.ccp[i] = le.Uint32(wd[lw+12+4*i:])
	}
	n := int(le.Uint16(wd[off:]))
	off += 2
	if off+8*n > len(wd) || n < 75 {
		if n < 34 || off+8*n > len(wd) {
			return nil, ErrNotWord
		}
	}
	for i := 0; i < n; i++ {
		r.fc = append(r.fc, [2]uint32{le.Uint32(wd[off+8*i:]), le.Uint32(wd[off+8*i+4:])})
	}
	if err := r.readText(); err != nil {
		return nil, err
	}
	r.papx = r.readFkps(fibPlcfBtePapx, true)
	r.chpx = r.readFkps(fibPlcfBteChpx, false)
	r.readStyles()
	r.readLists()
	r.readNotes()
	return r, nil
}

// part is a structure of the table stream named by the FIB ("" when absent).
func (r *docReader) part(i int) []byte {
	if i >= len(r.fc) {
		return nil
	}
	fc, lcb := r.fc[i][0], r.fc[i][1]
	if lcb == 0 || uint64(fc)+uint64(lcb) > uint64(len(r.tbl)) {
		return nil
	}
	return r.tbl[fc : fc+lcb]
}

// readText reads the characters of every subdocument through the piece table.
func (r *docReader) readText() error {
	clx := r.part(fibClx)
	le := binary.LittleEndian
	var pcdt []byte
	for i := 0; i < len(clx); {
		switch clx[i] {
		case 1: // Prc: formatting of pieces, skipped
			if i+3 > len(clx) {
				return ErrNotWord
			}
			i += 3 + int(int16(le.Uint16(clx[i+1:])))
		case 2:
			if i+5 > len(clx) {
				return ErrNotWord
			}
			lcb := int(le.Uint32(clx[i+1:]))
			if i+5+lcb > len(clx) {
				return ErrNotWord
			}
			pcdt = clx[i+5 : i+5+lcb]
			i = len(clx)
		default:
			return ErrNotWord
		}
	}
	if len(pcdt) < 4 || (len(pcdt)-4)%12 != 0 {
		return ErrNotWord
	}
	n := (len(pcdt) - 4) / 12
	var total uint64
	for _, v := range r.ccp {
		total += uint64(v)
	}
	if total > maxDocChars {
		return ErrWordTooBig
	}
	for i := 0; i < n; i++ {
		p := docPiece{cp0: le.Uint32(pcdt[4*i:]), cp1: le.Uint32(pcdt[4*i+4:])}
		pcd := pcdt[4*(n+1)+8*i:]
		fc := le.Uint32(pcd[2:])
		p.compressed = fc&0x40000000 != 0
		p.fc = fc & 0x3FFFFFFF
		if p.compressed {
			p.fc /= 2
		}
		if p.cp1 < p.cp0 || p.cp1 > maxDocChars {
			return ErrNotWord
		}
		for cp := p.cp0; cp < p.cp1; cp++ {
			k := cp - p.cp0
			if p.compressed {
				at := uint64(p.fc) + uint64(k)
				if at >= uint64(len(r.wd)) {
					return ErrNotWord
				}
				r.text = append(r.text, cp1252(r.wd[at]))
				r.fcs = append(r.fcs, uint32(at))
			} else {
				at := uint64(p.fc) + 2*uint64(k)
				if at+2 > uint64(len(r.wd)) {
					return ErrNotWord
				}
				r.text = append(r.text, le.Uint16(r.wd[at:]))
				r.fcs = append(r.fcs, uint32(at))
			}
		}
	}
	return nil
}

// cp1252High: Windows-1252's 0x80–0x9F; the rest is Latin-1
var cp1252High = [32]uint16{0x20AC, 0x81, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021, 0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x8D, 0x017D, 0x8F,
	0x90, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014, 0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x9D, 0x017E, 0x0178}

// cp1252 is the character of a byte of a compressed piece.
func cp1252(b byte) uint16 {
	if b >= 0x80 && b < 0xA0 {
		return cp1252High[b-0x80]
	}
	return uint16(b)
}

// readFkps reads every formatted disk page of paragraphs or characters.
func (r *docReader) readFkps(which int, para bool) []docFkpRun {
	plc := r.part(which)
	if len(plc) < 4 || (len(plc)-4)%8 != 0 {
		return nil
	}
	le := binary.LittleEndian
	n := (len(plc) - 4) / 8
	var out []docFkpRun
	for i := 0; i < n; i++ {
		pn := le.Uint32(plc[4*(n+1)+4*i:]) & 0x3FFFFF
		off := int(pn) * 512
		if off+512 > len(r.wd) {
			continue
		}
		page := r.wd[off : off+512]
		crun := int(page[511])
		if crun == 0 || 4*(crun+1) > 511 {
			continue
		}
		for j := 0; j < crun; j++ {
			run := docFkpRun{fc0: le.Uint32(page[4*j:]), fc1: le.Uint32(page[4*j+4:])}
			if para {
				// BxPap: 13 bytes, the first the word offset of the PapxInFkp
				bx := 4*(crun+1) + 13*j
				if bx >= 511 {
					break
				}
				at := int(page[bx]) * 2
				if at != 0 && at < 511 {
					cb := int(page[at])
					start, size := at+1, 2*cb-1
					if cb == 0 && at+1 < 511 {
						start, size = at+2, 2*int(page[at+1])
					}
					if size >= 2 && start+size <= 511 {
						run.istd = le.Uint16(page[start:])
						run.grpprl = page[start+2 : start+size]
					}
				}
			} else {
				b := 4*(crun+1) + j
				at := int(page[b]) * 2
				if at != 0 && at < 511 {
					cb := int(page[at])
					if at+1+cb <= 511 {
						run.grpprl = page[at+1 : at+1+cb]
					}
				}
			}
			out = append(out, run)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].fc0 < out[b].fc0 })
	return out
}

// find is the run holding the byte fc.
func findRun(runs []docFkpRun, fc uint32, hint *int) *docFkpRun {
	if hint != nil && *hint < len(runs) {
		if rr := &runs[*hint]; rr.fc0 <= fc && fc < rr.fc1 {
			return rr
		}
		if *hint+1 < len(runs) {
			if rr := &runs[*hint+1]; rr.fc0 <= fc && fc < rr.fc1 {
				*hint++
				return rr
			}
		}
	}
	i := sort.Search(len(runs), func(i int) bool { return runs[i].fc1 > fc })
	if i < len(runs) && runs[i].fc0 <= fc {
		if hint != nil {
			*hint = i
		}
		return &runs[i]
	}
	return nil
}

// ---------- property modifiers (sprms) ----------

// eachSprm calls fn with each sprm of grpprl and its operand.
func eachSprm(grpprl []byte, fn func(op uint16, arg []byte)) {
	le := binary.LittleEndian
	for i := 0; i+2 <= len(grpprl); {
		op := le.Uint16(grpprl[i:])
		i += 2
		var size int
		switch op >> 13 {
		case 0, 1:
			size = 1
		case 2, 4, 5:
			size = 2
		case 3:
			size = 4
		case 7:
			size = 3
		case 6:
			if i >= len(grpprl) {
				return
			}
			switch op {
			case 0xC615: // sprmPChgTabs: a length of 255 is followed by its own; not used here
				if grpprl[i] == 255 {
					return
				}
				size = int(grpprl[i]) + 1
			case 0xD608, 0xD606: // sprmTDefTable, sprmTDefTable10: a 2-byte length
				if i+2 > len(grpprl) {
					return
				}
				size = int(le.Uint16(grpprl[i:])) + 1
			default:
				size = int(grpprl[i]) + 1
			}
		}
		if i+size > len(grpprl) {
			return
		}
		fn(op, grpprl[i:i+size])
		i += size
	}
}

func toggle(cur bool, v byte) bool {
	switch v {
	case 0:
		return false
	case 1:
		return true
	case 0x81:
		return !cur
	}
	return cur
}

// ico: the 16 colours of Word 97
var icoColors = []string{"", "000000", "0000FF", "00FFFF", "00FF00", "FF00FF", "FF0000", "FFFF00", "FFFFFF",
	"000080", "008080", "008000", "800080", "800000", "808000", "808080", "C0C0C0"}

func (c *docChar) apply(grpprl []byte) {
	le := binary.LittleEndian
	eachSprm(grpprl, func(op uint16, a []byte) {
		switch op {
		case 0x0835:
			c.bold = toggle(c.bold, a[0])
		case 0x0836:
			c.italic = toggle(c.italic, a[0])
		case 0x0837:
			c.strike = toggle(c.strike, a[0])
		case 0x2A53:
			c.strike = a[0] != 0 || c.strike
		case 0x2A3E:
			c.underline = a[0] != 0
		case 0x2A42:
			if int(a[0]) < len(icoColors) {
				c.color = icoColors[a[0]]
			}
		case 0x6870:
			if len(a) == 4 && a[3] == 0 {
				c.color = fmt.Sprintf("%02X%02X%02X", a[0], a[1], a[2])
			} else {
				c.color = ""
			}
		case 0x2A0C:
			if int(a[0]) < len(icoColors) {
				c.highlight = icoColors[a[0]]
			}
		case 0x4A43:
			c.hps = int(le.Uint16(a))
		case 0x2A48:
			c.iss = a[0]
		case 0x083C:
			c.vanish = toggle(c.vanish, a[0])
		case 0x0800:
			c.deleted = a[0] != 0
		case 0x0855:
			c.spec = a[0] != 0
		case 0x080A:
			c.ole = a[0] != 0
		case 0x0806: // sprmCFData: the character holds data (a form field, a link), not a picture
			c.fd = a[0] != 0
		case 0x6A03:
			c.picLoc, c.hasPic = int32(le.Uint32(a)), true
		case 0x4A30:
			c.istd, c.hasIstd = le.Uint16(a), true
		}
	})
}

func (p *docPara) apply(grpprl []byte) {
	le := binary.LittleEndian
	eachSprm(grpprl, func(op uint16, a []byte) {
		switch op {
		case 0x2403, 0x2461: // sprmPJc80, sprmPJc
			p.jc = int(a[0])
		case 0x2416:
			p.inTable = a[0] != 0
			if p.inTable && p.itap == 0 {
				p.itap = 1
			}
		case 0x2417:
			p.ttp = a[0] != 0
		case 0x6649:
			p.itap = int(int32(le.Uint32(a)))
		case 0x244B:
			p.innerCell = a[0] != 0
		case 0x244C:
			p.innerTtp = a[0] != 0
		case 0x2640:
			p.outline = int(a[0])
		case 0x460B:
			p.ilfo, p.hasIlfo = int(int16(le.Uint16(a))), true
		case 0x260A:
			p.ilvl = int(a[0])
		case 0x3404:
			p.header = a[0] != 0
		case 0xD608:
			p.tdef = a[2:]
		case 0xD62B: // sprmTVertMerge: itc, how
			if len(a) >= 3 {
				if p.vmerge == nil {
					p.vmerge = map[int]byte{}
				}
				p.vmerge[int(a[1])] = a[2]
			}
		}
	})
}

// ---------- styles ----------

func (r *docReader) readStyles() {
	st := r.part(fibStshf)
	le := binary.LittleEndian
	r.baseHps = 20
	if len(st) < 2 {
		return
	}
	cbStshi := int(le.Uint16(st))
	if 2+cbStshi > len(st) || cbStshi < 4 {
		return
	}
	stshi := st[2 : 2+cbStshi]
	cstd := int(le.Uint16(stshi))
	cbBase := int(le.Uint16(stshi[2:]))
	off := 2 + cbStshi
	for i := 0; i < cstd && off+2 <= len(st); i++ {
		cb := int(le.Uint16(st[off:]))
		off += 2
		if off+cb > len(st) {
			break
		}
		std := st[off : off+cb]
		off += cb
		s := docStyle{base: 0x0FFF}
		if cb >= cbBase && cbBase >= 10 {
			s.sti = le.Uint16(std) & 0x0FFF
			s.stk = std[2] & 0x0F
			s.base = le.Uint16(std[2:]) >> 4
			cupx := int(std[4] & 0x0F)
			p := cbBase
			if p+2 <= len(std) {
				cch := int(le.Uint16(std[p:]))
				p += 2
				if p+2*cch <= len(std) {
					u := make([]uint16, cch)
					for k := range u {
						u[k] = le.Uint16(std[p+2*k:])
					}
					s.name = string(utf16.Decode(u))
					p += 2*cch + 2
				}
			}
			for u := 0; u < cupx && p+2 <= len(std); u++ {
				n := int(le.Uint16(std[p:]))
				p += 2
				if p+n > len(std) {
					break
				}
				upx := std[p : p+n]
				switch {
				case s.stk == 1 && u == 0 && n >= 2:
					s.papx = upx[2:]
				case s.stk == 1 && u == 1, s.stk == 2 && u == 0:
					s.chpx = upx
				}
				p += n + n%2
			}
			s.parsed = true
		}
		r.styles = append(r.styles, s)
	}
	if hps := r.styleChar(0, docChar{}).hps; hps > 0 {
		r.baseHps = hps
	}
	r.doc.BaseSize = float64(r.baseHps) / 2
}

// chain is the style and those it is based on, the root first.
func (r *docReader) chain(istd uint16) []*docStyle {
	var out []*docStyle
	for i := 0; i < 11 && int(istd) < len(r.styles); i++ {
		s := &r.styles[istd]
		if !s.parsed {
			break
		}
		out = append(out, s)
		if s.base == 0x0FFF || s.base == istd {
			break
		}
		istd = s.base
	}
	for a, b := 0, len(out)-1; a < b; a, b = a+1, b-1 {
		out[a], out[b] = out[b], out[a]
	}
	return out
}

func (r *docReader) styleChar(istd uint16, c docChar) docChar {
	for _, s := range r.chain(istd) {
		c.apply(s.chpx)
	}
	return c
}

// heading is the level a paragraph style makes (0: body text).
func (r *docReader) heading(istd uint16) int {
	c := r.chain(istd)
	for i := len(c) - 1; i >= 0; i-- {
		s := c[i]
		if s.sti >= 1 && s.sti <= 9 {
			return min(int(s.sti), 6)
		}
		if s.sti == 62 {
			return 1
		}
		if s.sti == 74 {
			return 2
		}
		if m := headingName.FindStringSubmatch(strings.TrimSpace(s.name)); m != nil {
			n, _ := strconv.Atoi(m[2])
			return min(n, 6)
		}
	}
	return 0
}

// ---------- lists ----------

// nfc: the number formats of a list level, as WordprocessingML names them
var nfcNames = map[byte]string{0: "decimal", 1: "upperRoman", 2: "lowerRoman", 3: "upperLetter", 4: "lowerLetter",
	10: "ideographDigital", 11: "japaneseCounting", 18: "decimalEnclosedCircle", 22: "decimalZero", 23: "bullet",
	28: "decimalEnclosedCircleChinese", 30: "ideographTraditional", 31: "ideographZodiac", 33: "taiwaneseCounting",
	35: "taiwaneseCountingThousand", 37: "chineseCounting", 38: "chineseLegalSimplified", 39: "chineseCountingThousand", 255: "none"}

// readLists turns the list tables into the numbering of .docx: a list
// (LSTF) is an abstract numbering, a list format override (LFO) a num.
func (r *docReader) readLists() {
	r.num = &numbering{abstract: map[string]map[int]numLevel{}, nums: map[string]numInstance{}, counters: map[string]*listCount{}, used: map[string]bool{}}
	le := binary.LittleEndian
	lst := r.part(fibPlfLst)
	if len(lst) >= 2 {
		n := int(int16(le.Uint16(lst)))
		type lstf struct {
			id     string
			simple bool
		}
		var lists []lstf
		off := 2
		for i := 0; i < n && off+28 <= len(lst); i++ {
			lists = append(lists, lstf{id: strconv.FormatUint(uint64(le.Uint32(lst[off:])), 10), simple: lst[off+26]&1 != 0})
			off += 28
		}
		// the levels follow, from the end of the PlfLst in the table stream
		data := r.tbl
		pos := int(r.fc[fibPlfLst][0]) + off
		for _, l := range lists {
			count := 9
			if l.simple {
				count = 1
			}
			levels := map[int]numLevel{}
			for k := 0; k < count; k++ {
				lv, next, ok := readLvl(data, pos)
				if !ok {
					return
				}
				levels[k] = lv
				pos = next
			}
			r.num.abstract[l.id] = levels
		}
	}
	lfo := r.part(fibPlfLfo)
	if len(lfo) < 4 {
		return
	}
	n := int(le.Uint32(lfo))
	off := 4
	type entry struct {
		lsid string
		clv  int
	}
	var entries []entry
	for i := 0; i < n && off+16 <= len(lfo); i++ {
		entries = append(entries, entry{lsid: strconv.FormatUint(uint64(le.Uint32(lfo[off:])), 10), clv: int(lfo[off+12])})
		off += 16
	}
	for i, e := range entries {
		inst := numInstance{abs: e.lsid, overrides: map[int]int{}}
		// LFOData: cp, then the level overrides
		if off+4 > len(lfo) {
			r.num.nums[strconv.Itoa(i+1)] = inst
			continue
		}
		off += 4
		for k := 0; k < e.clv && off+8 <= len(lfo); k++ {
			start := int(int32(le.Uint32(lfo[off:])))
			fl := le.Uint32(lfo[off+4:])
			off += 8
			ilvl := int(fl & 0x0F)
			if fl&0x10 != 0 {
				inst.overrides[ilvl] = start
			}
			if fl&0x20 != 0 { // a whole level follows
				lv, next, ok := readLvl(lfo, off)
				if !ok {
					break
				}
				off = next
				if levels := r.num.abstract[e.lsid]; levels != nil {
					cp := map[int]numLevel{}
					for k, v := range levels {
						cp[k] = v
					}
					cp[ilvl] = lv
					key := e.lsid + "/" + strconv.Itoa(i+1)
					r.num.abstract[key] = cp
					inst.abs = key
				}
			}
		}
		r.num.nums[strconv.Itoa(i+1)] = inst
	}
}

// readLvl reads a list level (LVLF, its properties and number text) at pos.
func readLvl(b []byte, pos int) (numLevel, int, bool) {
	le := binary.LittleEndian
	if pos+28 > len(b) {
		return numLevel{}, 0, false
	}
	f := b[pos : pos+28]
	lv := numLevel{start: int(int32(le.Uint32(f))), legal: f[5]&0x04 != 0}
	lv.format = nfcNames[f[4]]
	if lv.format == "" {
		lv.format = "decimal"
	}
	pos += 28 + int(f[25]) + int(f[24]) // grpprlPapx, grpprlChpx
	if pos+2 > len(b) {
		return numLevel{}, 0, false
	}
	cch := int(le.Uint16(b[pos:]))
	pos += 2
	if pos+2*cch > len(b) {
		return numLevel{}, 0, false
	}
	var t strings.Builder
	for k := 0; k < cch; k++ {
		c := le.Uint16(b[pos+2*k:])
		if c < 9 {
			t.WriteString("%" + strconv.Itoa(int(c)+1))
		} else {
			t.WriteString(string(utf16.Decode([]uint16{c})))
		}
	}
	lv.text = t.String()
	if lv.text == "" && lv.format != "bullet" {
		lv.format = "none"
	}
	return lv, pos + 2*cch, true
}

// ---------- notes ----------

func (r *docReader) readNotes() {
	le := binary.LittleEndian
	for kind, which := range [2][2]int{{fibPlcffndRef, fibPlcffndTxt}, {fibPlcfendRef, fibPlcfendTxt}} {
		ref := r.part(which[0])
		if len(ref) < 4 || (len(ref)-4)%6 != 0 {
			continue
		}
		n := (len(ref) - 4) / 6
		for i := 0; i < n; i++ {
			r.notes[le.Uint32(ref[4*i:])] = [2]int{kind, i}
		}
		txt := r.part(which[1])
		for i := 0; i+4 <= len(txt); i += 4 {
			r.noteText[kind] = append(r.noteText[kind], le.Uint32(txt[i:]))
		}
	}
}

// subdocument starts: footnotes after the main text, endnotes after the
// headers, macros and annotations
func (r *docReader) noteStart(kind int) uint32 {
	if kind == 0 {
		return r.ccp[0]
	}
	return r.ccp[0] + r.ccp[1] + r.ccp[2] + r.ccp[3] + r.ccp[4]
}

func (r *docReader) note(cp uint32) int {
	ref, ok := r.notes[cp]
	if !ok || r.inNote {
		return 0
	}
	key := strconv.Itoa(ref[0]) + "/" + strconv.Itoa(ref[1])
	if n, ok := r.noteNums[key]; ok {
		return n
	}
	txt := r.noteText[ref[0]]
	if ref[1]+1 >= len(txt) {
		return 0
	}
	n := len(r.doc.Notes) + 1
	r.noteNums[key] = n
	base := r.noteStart(ref[0])
	r.inNote = true
	saved := r.fields
	r.fields = nil
	blocks := r.blockRange(base+txt[ref[1]], base+txt[ref[1]+1])
	r.fields, r.inNote = saved, false
	r.doc.Notes = append(r.doc.Notes, WordNote{N: n, Blocks: blocks})
	return n
}

// ---------- paragraphs and tables ----------

func (r *docReader) read() {
	r.doc.Blocks = r.blockRange(0, min(r.ccp[0], uint32(len(r.text))))
}

func (r *docReader) para(cp uint32) docPara {
	p := docPara{jc: -1, outline: -1}
	if int(cp) >= len(r.fcs) {
		return p
	}
	run := findRun(r.papx, r.fcs[cp], nil)
	if run == nil {
		return p
	}
	p.istd = run.istd
	for _, s := range r.chain(run.istd) {
		p.apply(s.papx)
	}
	p.apply(run.grpprl)
	return p
}

type docCell struct {
	blocks []WordBlock
}

type docRow struct {
	cells  []docCell
	edges  []int // cell boundaries, from the table definition
	merge  []byte
	vmerge []byte
	header bool
}

// blockRange reads the paragraphs of [cp0, cp1).
func (r *docReader) blockRange(cp0, cp1 uint32) []WordBlock {
	if cp1 > uint32(len(r.text)) {
		cp1 = uint32(len(r.text))
	}
	var out []WordBlock
	add := func(b WordBlock) {
		if r.blocks >= maxWordBlocks {
			r.doc.Truncated = true
			return
		}
		if b.Kind == "paragraph" && len(b.Runs) == 0 && b.List == nil && (len(out) == 0 || out[len(out)-1].Kind == "paragraph" && len(out[len(out)-1].Runs) == 0) {
			return
		}
		r.blocks++
		out = append(out, b)
	}
	var rows []docRow
	var cells []docCell
	var cur []WordBlock
	flushTable := func() {
		if len(rows) > 0 || len(cells) > 0 || len(cur) > 0 {
			if len(cur) > 0 || len(cells) > 0 { // a row without its end mark
				cells = append(cells, docCell{blocks: cur})
				rows = append(rows, docRow{cells: cells})
			}
			add(docTable(rows))
		}
		rows, cells, cur = nil, nil, nil
	}
	start := cp0
	for cp := cp0; cp < cp1 && !r.doc.Truncated; cp++ {
		ch := r.text[cp]
		mark := ch == 0x0D || ch == 0x07 || ch == 0x0C
		if !mark && cp+1 != cp1 {
			continue
		}
		end := cp // the mark is not text
		if !mark {
			end = cp + 1
		}
		p := r.para(cp)
		inTable := p.itap >= 1 || p.inTable
		switch {
		case inTable && p.itap <= 1 && p.ttp:
			// the end of a row: its definition
			row := docRow{cells: cells, header: p.header}
			row.edges, row.merge, row.vmerge = tableDef(p.tdef, len(cells))
			for itc, v := range p.vmerge {
				if itc < len(row.vmerge) {
					row.vmerge[itc] = v
				}
			}
			rows = append(rows, row)
			cells = nil
		case inTable && p.itap > 1 && (p.innerTtp || p.ttp):
			// the end of a row of a nested table: its text is in its cells
		case inTable:
			b := r.paragraph(start, end, p)
			if b.Kind == "paragraph" && (len(b.Runs) > 0 || b.List != nil || len(cur) == 0) {
				cur = append(cur, b)
			}
			if ch == 0x07 && p.itap <= 1 {
				cells = append(cells, docCell{blocks: trimEmpty(cur)})
				cur = nil
			}
		default:
			flushTable()
			add(r.paragraph(start, end, p))
		}
		start = cp + 1
	}
	flushTable()
	for len(out) > 0 && out[len(out)-1].Kind == "paragraph" && len(out[len(out)-1].Runs) == 0 && out[len(out)-1].List == nil {
		out = out[:len(out)-1]
	}
	return out
}

func trimEmpty(bs []WordBlock) []WordBlock {
	var out []WordBlock
	for _, b := range bs {
		if b.Kind == "paragraph" && len(b.Runs) == 0 && b.List == nil {
			continue
		}
		out = append(out, b)
	}
	return out
}

// tableDef reads a row's cell boundaries and merges from sprmTDefTable.
func tableDef(def []byte, cells int) (edges []int, merge, vmerge []byte) {
	le := binary.LittleEndian
	merge, vmerge = make([]byte, cells), make([]byte, cells)
	if len(def) < 1 {
		return nil, merge, vmerge
	}
	n := int(def[0])
	if 1+2*(n+1) > len(def) {
		return nil, merge, vmerge
	}
	for i := 0; i <= n; i++ {
		edges = append(edges, int(int16(le.Uint16(def[1+2*i:]))))
	}
	tc := def[1+2*(n+1):]
	for i := 0; i < n && i < cells && 20*i+2 <= len(tc); i++ {
		g := le.Uint16(tc[20*i:])
		merge[i] = byte(g & 3)
		vmerge[i] = byte(g >> 5 & 3)
	}
	if n != cells {
		return nil, merge, vmerge
	}
	return edges, merge, vmerge
}

// docTable lays the rows on a grid made of every cell boundary.
func docTable(rows []docRow) WordBlock {
	out := WordBlock{Kind: "table"}
	grid := map[int]bool{}
	for _, rw := range rows {
		for _, e := range rw.edges {
			grid[e] = true
		}
	}
	var xs []int
	for x := range grid {
		xs = append(xs, x)
	}
	sort.Ints(xs)
	col := func(x int) int { return sort.SearchInts(xs, x) }
	type origin struct{ row, cell int }
	open := map[int]origin{}
	header := true
	for _, rw := range rows {
		var row []WordCell
		for i := 0; i < len(rw.cells); i++ {
			c := rw.cells[i]
			span, at := 1, i
			right := i
			// cells merged into this one (horizontal merge, Word 97)
			if i < len(rw.merge) && rw.merge[i] == 1 {
				for right+1 < len(rw.cells) && right+1 < len(rw.merge) && rw.merge[right+1] >= 2 {
					right++
					c.blocks = append(c.blocks, rw.cells[right].blocks...)
				}
			}
			if rw.edges != nil {
				at = col(rw.edges[i])
				span = max(col(rw.edges[right+1])-at, 1)
			} else {
				span = right - i + 1
			}
			vm := byte(0)
			if i < len(rw.vmerge) {
				vm = rw.vmerge[i]
			}
			if vm == 1 || vm == 2 {
				if o, ok := open[at]; ok {
					out.Rows[o.row][o.cell].RowSpan++
					i = right
					continue
				}
			}
			if vm == 3 {
				open[at] = origin{len(out.Rows), len(row)}
			} else {
				delete(open, at)
			}
			row = append(row, WordCell{Blocks: c.blocks, ColSpan: span, RowSpan: 1})
			i = right
		}
		if rw.header && header {
			out.HeaderRows++
		} else {
			header = false
		}
		out.Rows = append(out.Rows, row)
	}
	return out
}

// paragraph reads the paragraph [start, end], end being its mark.
func (r *docReader) paragraph(start, end uint32, p docPara) WordBlock {
	b := WordBlock{Kind: "paragraph"}
	b.Heading = r.heading(p.istd)
	if p.outline >= 0 {
		b.Heading = 0
		if p.outline < 9 {
			b.Heading = min(p.outline+1, 6)
		}
	}
	switch p.jc {
	case 1:
		b.Align = "center"
	case 2:
		b.Align = "right"
	case 3, 4:
		b.Align = "justify"
	}
	if p.hasIlfo && p.ilfo > 0 && p.ilfo != 2047 && !r.inNote {
		if m, ok := r.num.marker(strconv.Itoa(p.ilfo), p.ilvl); ok {
			b.List = &WordList{Marker: m, Level: min(max(p.ilvl, 0), 8)}
		}
	}
	base := r.styleChar(p.istd, docChar{})
	b.Runs = mergeRuns(r.runRange(start, end, base))
	if b.Heading > 0 && len(b.Runs) == 0 {
		b.Heading = 0
	}
	return b
}

func (r *docReader) charAt(cp uint32, base docChar) docChar {
	c := base
	run := findRun(r.chpx, r.fcs[cp], &r.chpxAt)
	if run == nil {
		return c
	}
	var probe docChar
	probe.apply(run.grpprl)
	if probe.hasIstd {
		c = r.styleChar(probe.istd, c)
	}
	c.apply(run.grpprl)
	return c
}

func (r *docReader) mkRun(text string, c docChar) WordRun {
	w := WordRun{Text: text, Bold: c.bold, Italic: c.italic, Underline: c.underline, Strike: c.strike, Color: c.color, Highlight: c.highlight}
	if c.color == "000000" {
		w.Color = ""
	}
	switch c.iss {
	case 1:
		w.Vert = "super"
	case 2:
		w.Vert = "sub"
	}
	if c.hps > 0 && c.hps != r.baseHps {
		if s := float64(c.hps) / 2; s-r.doc.BaseSize > 0.5 || r.doc.BaseSize-s > 0.5 {
			w.Size = s
		}
	}
	for i := len(r.fields) - 1; i >= 0; i-- {
		if f := r.fields[i]; f.result && f.link != "" {
			w.Link = f.link
			break
		}
	}
	return w
}

// runRange reads the characters [start, end) of a paragraph.
func (r *docReader) runRange(start, end uint32, base docChar) []WordRun {
	var out []WordRun
	var text []uint16
	var cur docChar
	flush := func() {
		if len(text) > 0 {
			out = append(out, r.mkRun(string(utf16.Decode(text)), cur))
			text = text[:0]
		}
	}
	for cp := start; cp < end; cp++ {
		ch := r.text[cp]
		c := r.charAt(cp, base)
		switch ch {
		case 0x13:
			flush()
			r.fields = append(r.fields, &fieldState{})
			continue
		case 0x14:
			flush()
			if n := len(r.fields); n > 0 {
				f := r.fields[n-1]
				f.result, f.link = true, fieldLink(f.instr.String())
			}
			continue
		case 0x15:
			flush()
			if n := len(r.fields); n > 0 {
				r.fields = r.fields[:n-1]
			}
			continue
		}
		if n := len(r.fields); n > 0 && !r.fields[n-1].result {
			r.fields[n-1].instr.WriteString(string(utf16.Decode([]uint16{ch})))
			continue
		}
		if c.vanish || c.deleted {
			continue
		}
		if len(text) > 0 && c != cur {
			flush()
		}
		cur = c
		if c.spec {
			switch ch {
			case 0x01: // an inline picture
				flush()
				if c.fd {
					continue
				}
				if w, ok := r.picture(c); ok {
					out = append(out, w)
				}
				continue
			case 0x02: // a footnote or endnote reference
				flush()
				if n := r.note(cp); n > 0 {
					w := r.mkRun("", c)
					w.Note, w.Vert = n, "super"
					out = append(out, w)
				}
				continue
			case 0x08, 0x05, 0x03, 0x04: // drawn objects, annotation marks, separators
				continue
			case 0x28: // a symbol
				continue
			}
		}
		switch ch {
		case 0x09:
			text = append(text, '\t')
		case 0x0B:
			text = append(text, '\n')
		case 0x1E:
			text = append(text, 0x2011)
		case 0x1F, 0x00, 0xFFFC: // an optional hyphen; where an object was
		case 0xA0:
			text = append(text, ' ')
		default:
			if ch >= 0x20 || ch == 0x0A {
				text = append(text, ch)
			}
		}
	}
	flush()
	return out
}

// picture is an inline picture's run: its index, or a placeholder for
// what a browser cannot show.
func (r *docReader) picture(c docChar) (WordRun, bool) {
	if c.ole || !c.hasPic {
		return r.mkRun(wordPicture, c), true
	}
	if idx, ok := r.images[c.picLoc]; ok {
		w := r.mkRun("", c)
		w.Image = idx
		return w, true
	}
	_, mime, w, h, ok := docBlip(r.data, c.picLoc)
	if !ok || mime == "" {
		return r.mkRun(wordPicture, c), true
	}
	r.doc.Images = append(r.doc.Images, WordImage{Part: "doc:" + strconv.Itoa(int(c.picLoc)), Width: w, Height: h})
	idx := len(r.doc.Images)
	r.images[c.picLoc] = idx
	run := r.mkRun("", c)
	run.Image = idx
	return run, true
}

// docBlip finds the picture data of a PICF at off in the Data stream:
// its bytes and type ("" for one a browser cannot show) and its size in
// pixels.
func docBlip(data []byte, off int32) (img []byte, mime string, w, h int, ok bool) {
	le := binary.LittleEndian
	if off < 0 || int(off)+68 > len(data) {
		return nil, "", 0, 0, false
	}
	picf := data[off:]
	lcb := int(le.Uint32(picf))
	cbHeader := int(le.Uint16(picf[4:]))
	if lcb < cbHeader || cbHeader < 68 || int(off)+lcb > len(data) {
		return nil, "", 0, 0, false
	}
	dxa, dya := int(le.Uint16(picf[28:])), int(le.Uint16(picf[30:]))
	mx, my := int(le.Uint16(picf[32:])), int(le.Uint16(picf[34:]))
	if mx == 0 {
		mx = 1000
	}
	if my == 0 {
		my = 1000
	}
	w, h = dxa*mx/15000, dya*my/15000
	body := picf[cbHeader:lcb]
	if le.Uint16(picf[6:]) == 0x66 && len(body) > 0 { // MM_SHAPEFILE: a name first
		body = body[1+int(body[0]):]
	}
	img, mime = findBlip(body, 0)
	return img, mime, w, h, true
}

// findBlip looks through OfficeArt records for a picture.
func findBlip(b []byte, depth int) ([]byte, string) {
	le := binary.LittleEndian
	if depth > 4 {
		return nil, ""
	}
	for i := 0; i+8 <= len(b); {
		verInst := le.Uint16(b[i:])
		typ := le.Uint16(b[i+2:])
		size := int(le.Uint32(b[i+4:]))
		if size < 0 || i+8+size > len(b) {
			size = len(b) - i - 8
		}
		rec := b[i+8 : i+8+size]
		inst := verInst >> 4
		switch {
		case typ == 0xF007: // FBSE: the picture follows its fields and name
			if len(rec) >= 36 {
				if img, m := findBlip(rec[36+int(rec[33]):], depth+1); img != nil || m != "" {
					return img, m
				}
			}
		case typ >= 0xF018 && typ <= 0xF117:
			uids := 16
			if inst&1 == 1 {
				uids = 32
			}
			switch typ {
			case 0xF01D, 0xF02A, 0xF01E, 0xF01F:
				if len(rec) <= uids+1 {
					return nil, ""
				}
				data := rec[uids+1:]
				switch typ {
				case 0xF01E:
					return data, "image/png"
				case 0xF01F:
					return dibToBMP(data), "image/bmp"
				}
				return data, "image/jpeg"
			}
			return nil, ""
		case verInst&0x0F == 0x0F: // a container: look inside
			if img, m := findBlip(rec, depth+1); img != nil || m != "" {
				return img, m
			}
		}
		i += 8 + size
	}
	return nil, ""
}

// dibToBMP puts a BMP file header in front of a device-independent bitmap.
func dibToBMP(dib []byte) []byte {
	le := binary.LittleEndian
	if len(dib) < 40 {
		return nil
	}
	hdr := int(le.Uint32(dib))
	bits := int(le.Uint16(dib[14:]))
	comp := le.Uint32(dib[16:])
	colors := int(le.Uint32(dib[32:]))
	if colors == 0 && bits <= 8 {
		colors = 1 << bits
	}
	pal := 4 * colors
	if comp == 3 && hdr == 40 {
		pal += 12
	}
	out := make([]byte, 14, 14+len(dib))
	out[0], out[1] = 'B', 'M'
	le.PutUint32(out[2:], uint32(14+len(dib)))
	le.PutUint32(out[10:], uint32(14+hdr+pal))
	return append(out, dib...)
}

// readDocImage returns the picture at the Data stream offset off.
func readDocImage(src string, off int32) ([]byte, string, error) {
	c, err := readCFB(src)
	if err != nil {
		return nil, "", ErrNotWord
	}
	data, err := c.stream("Data")
	if err != nil || data == nil {
		return nil, "", ErrNotWord
	}
	img, mime, _, _, ok := docBlip(data, off)
	if !ok || img == nil || mime == "" || len(img) > maxWordImage {
		return nil, "", ErrNotWord
	}
	return img, mime, nil
}
