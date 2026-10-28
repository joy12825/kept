package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Styles and numbering for the Word reader (v1.9.8).

// runProps are the formatting of a run; nil fields are not set at this
// level and come from the one below.
type runProps struct {
	bold, italic, underline, strike, hidden *bool
	color, highlight, vert                  *string
	size                                    *float64 // pt
}

func (p runProps) over(q runProps) runProps {
	pick := func(a, b *bool) *bool {
		if b != nil {
			return b
		}
		return a
	}
	picks := func(a, b *string) *string {
		if b != nil {
			return b
		}
		return a
	}
	out := runProps{
		bold: pick(p.bold, q.bold), italic: pick(p.italic, q.italic), underline: pick(p.underline, q.underline),
		strike: pick(p.strike, q.strike), hidden: pick(p.hidden, q.hidden),
		color: picks(p.color, q.color), highlight: picks(p.highlight, q.highlight), vert: picks(p.vert, q.vert), size: p.size,
	}
	if q.size != nil {
		out.size = q.size
	}
	return out
}

var hexColor = regexp.MustCompile(`^[0-9A-Fa-f]{6}$`)

var highlightColors = map[string]string{
	"yellow": "FFFF00", "green": "00FF00", "cyan": "00FFFF", "magenta": "FF00FF", "blue": "0000FF", "red": "FF0000",
	"darkBlue": "000080", "darkCyan": "008080", "darkGreen": "008000", "darkMagenta": "800080", "darkRed": "800000",
	"darkYellow": "808000", "darkGray": "808080", "lightGray": "C0C0C0", "black": "000000", "white": "FFFFFF",
}

// readRunProps reads a w:rPr.
func readRunProps(r *xnode) runProps {
	var p runProps
	if r == nil {
		return p
	}
	flag := func(names ...string) *bool {
		for _, n := range names {
			if v, ok := r.on(n); ok {
				return &v
			}
		}
		return nil
	}
	p.bold, p.italic, p.hidden = flag("b"), flag("i"), flag("vanish")
	p.strike = flag("strike", "dstrike")
	if v, ok := r.val("u"); ok {
		on := v != "none" && v != "0"
		p.underline = &on
	}
	if v, ok := r.val("color"); ok {
		c := ""
		if hexColor.MatchString(v) && !strings.EqualFold(v, "000000") {
			c = strings.ToUpper(v)
		}
		p.color = &c
	}
	if v, ok := r.val("highlight"); ok {
		c := highlightColors[v]
		p.highlight = &c
	} else if shd := r.w("shd"); shd != nil {
		if f := shd.at("fill"); hexColor.MatchString(f) && !strings.EqualFold(f, "FFFFFF") {
			c := strings.ToUpper(f)
			p.highlight = &c
		}
	}
	if v, ok := r.val("sz"); ok {
		if n, err := strconv.ParseFloat(v, 64); err == nil && n > 0 && n < 4000 {
			pt := n / 2
			p.size = &pt
		}
	}
	if v, ok := r.val("vertAlign"); ok {
		s := ""
		switch v {
		case "superscript":
			s = "super"
		case "subscript":
			s = "sub"
		}
		p.vert = &s
	}
	return p
}

type wordStyle struct {
	name, basedOn string
	jc            string // the alignment it gives; "" not set
	outline       int    // -1: not set
	rpr           runProps
	numID         string
	ilvl          int
	hasNum        bool
}

type wordStyles struct {
	byID     map[string]*wordStyle
	base     runProps // document defaults
	baseSize float64
	defPara  string // the default paragraph style
}

func readStyles(n *xnode) *wordStyles {
	s := &wordStyles{byID: map[string]*wordStyle{}, baseSize: 10}
	if n == nil {
		return s
	}
	root := n.find("styles")
	if root == nil {
		return s
	}
	if dd := root.w("docDefaults"); dd != nil {
		if rd := dd.w("rPrDefault"); rd != nil {
			s.base = readRunProps(rd.w("rPr"))
		}
	}
	for _, st := range root.kids {
		if st.local != "style" {
			continue
		}
		ws := &wordStyle{outline: -1, ilvl: 0}
		ws.name, _ = st.val("name")
		ws.basedOn, _ = st.val("basedOn")
		ws.rpr = readRunProps(st.w("rPr"))
		if ppr := st.w("pPr"); ppr != nil {
			ws.jc, _ = ppr.val("jc")
			if v, ok := ppr.val("outlineLvl"); ok {
				if n, err := strconv.Atoi(v); err == nil {
					ws.outline = n
				}
			}
			if np := ppr.w("numPr"); np != nil {
				ws.hasNum = true
				ws.numID, _ = np.val("numId")
				if v, ok := np.val("ilvl"); ok {
					ws.ilvl, _ = strconv.Atoi(v)
				}
			}
		}
		id := st.at("styleId")
		s.byID[id] = ws
		if st.at("type") == "paragraph" && (st.at("default") == "1" || st.at("default") == "true") {
			s.defPara = id
		}
	}
	if def := s.chain(s.defPara); len(def) > 0 {
		// the body text size: the defaults, then the default paragraph style
		p := s.base
		for i := len(def) - 1; i >= 0; i-- {
			p = p.over(def[i].rpr)
		}
		s.base = p
	}
	if s.base.size != nil {
		s.baseSize = *s.base.size
	}
	return s
}

// chain is the style and the ones it is based on, nearest first; a loop
// stops it.
func (s *wordStyles) chain(id string) []*wordStyle {
	var out []*wordStyle
	seen := map[string]bool{}
	for id != "" && !seen[id] && len(out) < 20 {
		seen[id] = true
		st := s.byID[id]
		if st == nil {
			break
		}
		out = append(out, st)
		id = st.basedOn
	}
	return out
}

// runStyle is the formatting a style gives, its bases applied first.
func (s *wordStyles) runStyle(id string) runProps {
	c := s.chain(id)
	var p runProps
	for i := len(c) - 1; i >= 0; i-- {
		p = p.over(c[i].rpr)
	}
	return p
}

var headingName = regexp.MustCompile(`^(?i)(heading|标题)\s*([1-9])$`)

// heading is the level a paragraph style makes (0: body text).
func (s *wordStyles) heading(id string) int {
	for _, st := range s.chain(id) {
		if st.outline >= 0 && st.outline < 9 {
			return min(st.outline+1, 6)
		}
		name := strings.TrimSpace(st.name)
		if m := headingName.FindStringSubmatch(name); m != nil {
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

// align is the alignment a paragraph style gives ("" for none).
func (s *wordStyles) align(id string) string {
	for _, st := range s.chain(id) {
		if st.jc != "" {
			return st.jc
		}
	}
	return ""
}

// styleNum is the list a paragraph style puts its paragraphs in.
func (s *wordStyles) styleNum(id string) (numID string, ilvl int, ok bool) {
	for _, st := range s.chain(id) {
		if st.hasNum {
			return st.numID, st.ilvl, true
		}
	}
	return "", 0, false
}

// ---------- numbering ----------

type numLevel struct {
	format, text string
	start        int
	legal        bool // isLgl: the numbers of the levels above in Arabic digits
}

type numbering struct {
	abstract map[string]map[int]numLevel
	nums     map[string]numInstance
	counters map[string]*listCount // by abstract list
	used     map[string]bool       // nums whose start overrides were applied
}

// listCount is where a list is: the count at each level, and whether the
// level has started (a deeper level starts again after a shallower one).
type listCount struct {
	n       [9]int
	started [9]bool
}

type numInstance struct {
	abs       string
	overrides map[int]int
}

func readNumbering(n *xnode) *numbering {
	nb := &numbering{abstract: map[string]map[int]numLevel{}, nums: map[string]numInstance{}, counters: map[string]*listCount{}, used: map[string]bool{}}
	if n == nil {
		return nb
	}
	root := n.find("numbering")
	if root == nil {
		return nb
	}
	for _, k := range root.kids {
		switch k.local {
		case "abstractNum":
			levels := map[int]numLevel{}
			for _, l := range k.kids {
				if l.local != "lvl" {
					continue
				}
				i, err := strconv.Atoi(l.at("ilvl"))
				if err != nil || i < 0 || i > 8 {
					continue
				}
				lv := numLevel{start: 1}
				lv.format, _ = l.val("numFmt")
				lv.text, _ = l.val("lvlText")
				lv.legal, _ = l.on("isLgl")
				if v, ok := l.val("start"); ok {
					lv.start, _ = strconv.Atoi(v)
				}
				levels[i] = lv
			}
			nb.abstract[k.at("abstractNumId")] = levels
		case "num":
			abs, _ := k.val("abstractNumId")
			inst := numInstance{abs: abs, overrides: map[int]int{}}
			for _, o := range k.kids {
				if o.local != "lvlOverride" {
					continue
				}
				i, _ := strconv.Atoi(o.at("ilvl"))
				if v, ok := o.val("startOverride"); ok {
					inst.overrides[i], _ = strconv.Atoi(v)
				}
			}
			nb.nums[k.at("numId")] = inst
		}
	}
	return nb
}

// marker counts the paragraph in its list and returns its number or
// bullet ("" when the list shows none).
func (nb *numbering) marker(numID string, ilvl int) (string, bool) {
	inst, ok := nb.nums[numID]
	if !ok || numID == "0" {
		return "", false
	}
	levels := nb.abstract[inst.abs]
	if levels == nil {
		return "", false
	}
	ilvl = min(max(ilvl, 0), 8)
	c := nb.counters[inst.abs]
	if c == nil {
		c = &listCount{}
		nb.counters[inst.abs] = c
	}
	if !nb.used[numID] {
		nb.used[numID] = true
		for i, v := range inst.overrides {
			if i >= 0 && i < 9 {
				c.n[i], c.started[i] = v-1, true
			}
		}
	}
	lv := levels[ilvl]
	if !c.started[ilvl] {
		c.n[ilvl], c.started[ilvl] = lv.start-1, true
	}
	c.n[ilvl]++
	for i := ilvl + 1; i < 9; i++ {
		c.started[i] = false
	}
	if lv.format == "none" {
		return "", true
	}
	if lv.format == "bullet" {
		return bullet(lv.text), true
	}
	text := lv.text
	if text == "" {
		text = fmt.Sprintf("%%%d.", ilvl+1)
	}
	var b strings.Builder
	for i := 0; i < len(text); i++ {
		if text[i] == '%' && i+1 < len(text) && text[i+1] >= '1' && text[i+1] <= '9' {
			k := int(text[i+1] - '1')
			v := c.n[k]
			if !c.started[k] {
				v = levels[k].start
			}
			f := levels[k].format
			if lv.legal && k != ilvl {
				f = "decimal"
			}
			b.WriteString(numberText(v, f))
			i++
			continue
		}
		b.WriteByte(text[i])
	}
	return b.String(), true
}

// bullet turns a symbol font's private character into one that shows.
func bullet(t string) string {
	r := []rune(t)
	if len(r) == 0 {
		return "•"
	}
	switch r[0] {
	case 0xF0B7, 0xF06C, 0xF09F:
		return "•"
	case 0xF06F, 0x006F:
		return "◦"
	case 0xF0A7, 0xF06E:
		return "▪"
	case 0xF0D8, 0xF0E8:
		return "➢"
	case 0xF0FC:
		return "✓"
	case 0xF076:
		return "❖"
	}
	if r[0] >= 0xE000 && r[0] <= 0xF8FF {
		return "•"
	}
	return string(r[0])
}

var cnNumerals = []rune("零一二三四五六七八九")

func chineseNumber(n int) string {
	if n <= 0 || n >= 10000 {
		return strconv.Itoa(n)
	}
	units := []string{"", "十", "百", "千"}
	digits := []int{n / 1000, n / 100 % 10, n / 10 % 10, n % 10}
	var b strings.Builder
	zero := false
	for i, d := range digits {
		u := units[3-i]
		if d == 0 {
			if b.Len() > 0 {
				zero = true
			}
			continue
		}
		if zero {
			b.WriteRune('零')
			zero = false
		}
		if !(d == 1 && u == "十" && b.Len() == 0) { // 十一, not 一十一
			b.WriteRune(cnNumerals[d])
		}
		b.WriteString(u)
	}
	return b.String()
}

func letters(n int, upper bool) string {
	if n <= 0 {
		return strconv.Itoa(n)
	}
	base := 'a'
	if upper {
		base = 'A'
	}
	// Word repeats the letter: a … z, aa … zz
	return strings.Repeat(string(base+rune((n-1)%26)), (n-1)/26+1)
}

func roman(n int, upper bool) string {
	if n <= 0 || n >= 4000 {
		return strconv.Itoa(n)
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"m", "cm", "d", "cd", "c", "xc", "l", "xl", "x", "ix", "v", "iv", "i"}
	var b strings.Builder
	for i, v := range vals {
		for n >= v {
			b.WriteString(syms[i])
			n -= v
		}
	}
	if upper {
		return strings.ToUpper(b.String())
	}
	return b.String()
}

func numberText(n int, format string) string {
	switch format {
	case "lowerLetter":
		return letters(n, false)
	case "upperLetter":
		return letters(n, true)
	case "lowerRoman":
		return roman(n, false)
	case "upperRoman":
		return roman(n, true)
	case "chineseCounting", "chineseCountingThousand", "chineseLegalSimplified", "japaneseCounting", "taiwaneseCounting", "taiwaneseCountingThousand", "ideographDigital":
		if format == "chineseLegalSimplified" {
			return strings.NewReplacer("一", "壹", "二", "贰", "三", "叁", "四", "肆", "五", "伍", "六", "陆", "七", "柒", "八", "捌", "九", "玖", "十", "拾", "百", "佰", "千", "仟").Replace(chineseNumber(n))
		}
		return chineseNumber(n)
	case "ideographTraditional":
		if n >= 1 && n <= 10 {
			return string([]rune("甲乙丙丁戊己庚辛壬癸")[n-1])
		}
	case "ideographZodiac":
		if n >= 1 && n <= 12 {
			return string([]rune("子丑寅卯辰巳午未申酉戌亥")[n-1])
		}
	case "decimalEnclosedCircle", "decimalEnclosedCircleChinese":
		if n >= 1 && n <= 20 {
			return string(rune('①' + n - 1))
		}
	case "decimalZero":
		return fmt.Sprintf("%02d", n)
	}
	return strconv.Itoa(n)
}
