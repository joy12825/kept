package parser

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// dateScanChars is how much of the beginning of the text is searched for
// the document date; dateTailChars of the end are searched too, for a
// signing date under a contract (only dates with a telling word there).
const (
	dateScanChars = 2000
	dateTailChars = 500
	maxCandidates = 5
)

// A datePattern finds dates of one way of writing; ymd reads the year,
// month and day from a match's groups.
type datePattern struct {
	re  *regexp.Regexp
	ymd func(g []string) (y, mo, d int)
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

// Written dates: 2024年3月5日, 2024-03-05 (or / .), and since v1.5.5 dates
// in Chinese numerals (二〇二四年三月五日, on contracts and certificates)
// and with English month names (March 5, 2024; 5 Mar 2024), as on
// receipts from abroad. All-digit dates with the day or month first
// (05/03/2024) are left out: which is the month cannot be told.
var datePatterns = []datePattern{
	{regexp.MustCompile(`(\d{4})\s*年\s*(\d{1,2})\s*月\s*(\d{1,2})\s*日`), func(g []string) (int, int, int) { return atoi(g[1]), atoi(g[2]), atoi(g[3]) }},
	{regexp.MustCompile(`(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})`), func(g []string) (int, int, int) { return atoi(g[1]), atoi(g[2]), atoi(g[3]) }},
	{regexp.MustCompile(`((?:[〇○零一二三四五六七八九]\s*){4})年\s*((?:[一二三四五六七八九十]\s*){1,3})月\s*((?:[一二三四五六七八九十]\s*){1,3})日`), func(g []string) (int, int, int) {
		return cnYear(g[1]), cnNumber(g[2]), cnNumber(g[3])
	}},
	{regexp.MustCompile(`(?i)\b(` + monthNames + `)\.?\s+(\d{1,2})(?:st|nd|rd|th)?,?\s+(\d{4})\b`), func(g []string) (int, int, int) { return atoi(g[3]), month(g[1]), atoi(g[2]) }},
	{regexp.MustCompile(`(?i)\b(\d{1,2})(?:st|nd|rd|th)?[\s-]+(` + monthNames + `)\.?[\s,-]+(\d{4})\b`), func(g []string) (int, int, int) { return atoi(g[3]), month(g[2]), atoi(g[1]) }},
}

const monthNames = `january|february|march|april|may|june|july|august|september|october|november|december|jan|feb|mar|apr|jun|jul|aug|sept|sep|oct|nov|dec`

// month is 1–12 for an English month name or its abbreviation.
func month(name string) int {
	n := strings.ToLower(name)
	for i, m := range []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"} {
		if strings.HasPrefix(n, m) {
			return i + 1
		}
	}
	return 0
}

var cnDigits = map[rune]int{'〇': 0, '○': 0, '零': 0, '一': 1, '二': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}

// cnYear reads a year written digit by digit: 二〇二四 → 2024.
func cnYear(s string) int {
	y := 0
	for _, r := range strings.Join(strings.Fields(s), "") { // OCR puts spaces between the characters
		y = y*10 + cnDigits[r]
	}
	return y
}

// cnNumber reads 1–31 in Chinese numerals: 五, 十, 十五, 二十, 三十一.
// Anything else (十十, 一二) is 0, an invalid month or day.
func cnNumber(s string) int {
	s = strings.Join(strings.Fields(s), "")
	r := []rune(s)
	switch i := strings.IndexRune(s, '十'); {
	case i < 0 && len(r) == 1:
		return cnDigits[r[0]]
	case i < 0:
		return 0
	}
	tens, ones := 1, 0
	parts := strings.SplitN(s, "十", 2)
	if parts[0] != "" {
		if len([]rune(parts[0])) != 1 {
			return 0
		}
		tens = cnDigits[[]rune(parts[0])[0]]
	}
	if parts[1] != "" {
		if len([]rune(parts[1])) != 1 || strings.ContainsRune(parts[1], '十') {
			return 0
		}
		ones = cnDigits[[]rune(parts[1])[0]]
	}
	if tens == 0 || ones == 0 && parts[1] != "" {
		return 0
	}
	return tens*10 + ones
}

// Which date of a document is its own (v1.4.4): the words just before a
// date say what it is. A birth date (ID cards), the end of a validity or a
// due date is never the document's date; an issue, signing or statement
// date is.
var (
	dateNever = []string{"出生", "生日", "到期", "截止", "届满", "失效", "有效期至", "有效期到", "止日", "截至"}
	dateGood  = []string{"开票日期", "开具日期", "签发日期", "签订日期", "签署日期", "签约日期", "签单日期", "出具日期", "报告日期", "填发日期", "发证日期", "填报日期",
		"账单日", "出账日", "制表日期", "检查日期", "体检日期", "就诊日期", "交易日期", "缴费日期", "付款日期", "申请日期", "落款日期", "立约日期", "成交日期", "下单时间"}
	dateFair = []string{"日期", "时间", "Date", "date", "Issued", "issued"}
	// English labels, matched in lower case against a longer stretch
	dateNeverEN = []string{"due", "expir", "birth", "dob", "valid until", "valid thru", "valid through", "valid to"}
	dateGoodEN  = []string{"invoice date", "issue date", "date of issue", "issued", "order date", "order placed", "statement date", "billing date", "receipt date", "payment date", "date paid", "signed"}
	// a period or a validity: its first date is a weak choice, its end
	// none (validity) or a fair one (a statement's period)
	datePeriod   = []string{"周期", "期间", "账期", "有效期", "保险期", "租期", "合同期", "期限"}
	dateValidity = []string{"有效", "保险期", "租期", "合同期", "期限"}
	rangeJoin    = regexp.MustCompile(`^\s*(?:至|到|~|～|—|–|-|起至|to|through)\s*$`)
)

// DateCandidate is a date found in a document, with what told it.
type DateCandidate struct {
	Date    string // YYYY-MM-DD
	Matched string // the text, e.g. “开票日期：2024年3月5日”, for the field's basis
	Score   int    // higher is likelier the document's own date
	Pos     int
}

// FindDate returns the likeliest date of the document in text, written as
// in datePatterns, with a year from 1990 to next year.
// ok is false when there is none.
func FindDate(text string, now time.Time) (date string, ok bool) {
	date, _, ok = FindDateText(text, now)
	return date, ok
}

// FindDateText is FindDate that also returns the text that told it, the
// basis recorded in field_sources.
func FindDateText(text string, now time.Time) (date, matched string, ok bool) {
	c := DateCandidates(text, now)
	if len(c) == 0 {
		return "", "", false
	}
	return c[0].Date, c[0].Matched, true
}

// DateCandidates lists the dates that may be the document's, likeliest
// first (by score, then the earliest), one per date, at most 5. Dates that
// cannot be it (birth dates, ends of validity, due dates) are left out.
func DateCandidates(text string, now time.Time) []DateCandidate {
	head := text
	if utf8.RuneCountInString(text) > dateScanChars {
		r := []rune(text)
		head = string(r[:dateScanChars])
		tail := string(r[max(dateScanChars, len(r)-dateTailChars):])
		// the end only counts with a telling word (落款、签订日期 …)
		var good []DateCandidate
		for _, c := range scanDates(tail, now) {
			if c.Score >= 3 {
				c.Pos += len(head)
				good = append(good, c)
			}
		}
		return rank(append(scanDates(head, now), good...))
	}
	return rank(scanDates(head, now))
}

type dateMatch struct {
	start, end int
	date       string
}

func scanDates(text string, now time.Time) []DateCandidate {
	var ms []dateMatch
	for _, p := range datePatterns {
		for _, m := range p.re.FindAllStringSubmatchIndex(text, -1) {
			g := make([]string, len(m)/2)
			for k := range g {
				g[k] = text[m[2*k]:m[2*k+1]]
			}
			y, mo, d := p.ymd(g)
			if date, ok := validYMD(y, mo, d, now); ok {
				ms = append(ms, dateMatch{m[0], m[1], date})
			}
		}
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].start < ms[j].start })
	var out []DateCandidate
	for i, m := range ms {
		before := lineBefore(text, m.start, 12)
		en := strings.ToLower(lineBefore(text, m.start, 24)) // English labels are longer
		score, skip := 0, false
		switch {
		case containsAny(before, dateNever) || containsAny(en, dateNeverEN):
			skip = true
		case containsAny(before, dateGood) || containsAny(en, dateGoodEN):
			score = 3
		case containsAny(before, dateFair):
			score = 1
		}
		// the second date of a range “A 至 B”
		if i > 0 && rangeJoin.MatchString(text[ms[i-1].end:m.start]) {
			if containsAny(lineBefore(text, ms[i-1].start, 12), dateValidity) {
				skip = true // the end of a validity
			} else if score < 1 {
				score = 1 // the end of a statement's period
			}
		} else if i+1 < len(ms) && rangeJoin.MatchString(text[m.end:ms[i+1].start]) && containsAny(before, datePeriod) && score < 3 {
			score = -1 // the start of a period
		}
		if skip {
			continue
		}
		label := trimLabel(before, 8)
		if strings.IndexFunc(before, isLatin) >= 0 {
			label = trimLabel(lineBefore(text, m.start, 24), 16) // “Order Date: ”
		}
		matched := strings.TrimSpace(label + text[m.start:m.end])
		out = append(out, DateCandidate{Date: m.date, Matched: matched, Score: score, Pos: m.start})
	}
	return out
}

func rank(c []DateCandidate) []DateCandidate {
	sort.SliceStable(c, func(i, j int) bool {
		if c[i].Score != c[j].Score {
			return c[i].Score > c[j].Score
		}
		return c[i].Pos < c[j].Pos
	})
	seen := map[string]bool{}
	out := c[:0]
	for _, x := range c {
		if !seen[x.Date] && len(out) < maxCandidates {
			seen[x.Date] = true
			out = append(out, x)
		}
	}
	return out
}

func validYMD(y, mo, d int, now time.Time) (string, bool) {
	if y < 1990 || y > now.Year()+1 {
		return "", false
	}
	t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC)
	if t.Year() != y || int(t.Month()) != mo || t.Day() != d { // e.g. 2月30日
		return "", false
	}
	return t.Format("2006-01-02"), true
}

// lineBefore is up to n characters before i on the same line.
func lineBefore(text string, i, n int) string {
	s := text[:i]
	if j := strings.LastIndexAny(s, "\n\r"); j >= 0 {
		s = s[j+1:]
	}
	if r := []rune(s); len(r) > n {
		s = string(r[len(r)-n:])
	}
	return s
}

// trimLabel keeps the words of a label just before a date (“开票日期：”)
// for the basis, and drops what is further away.
func trimLabel(before string, limit int) string {
	r := []rune(before)
	i, kept := len(r), 0
	for i > 0 && kept < limit && !strings.ContainsRune("，,。;；|", r[i-1]) {
		i--
		if r[i] != ' ' && r[i] != '\t' {
			kept++
		}
	}
	return string(r[i:])
}

func isLatin(r rune) bool { return r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' }

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

var nameDate = []*regexp.Regexp{
	// IMG_20240305_123456.jpg, 微信图片_20240305120000.jpg, Scan20240305.pdf
	regexp.MustCompile(`(?:^|\D)((?:19|20)\d{2})(\d{2})(\d{2})(?:\d{4}|\d{6})?(?:\D|$)`),
	// 扫描 2024-03-05.pdf, 2024_3_5 发票.pdf, 2024年3月5日.pdf
	regexp.MustCompile(`(?:^|\D)((?:19|20)\d{2})[-_.年](\d{1,2})[-_.月](\d{1,2})(?:日|\D|$)`),
}

// FileNameDate finds a date in a file's name (v1.4.4): phones and scanners
// name files by when they were made.
func FileNameDate(name string, now time.Time) (string, bool) {
	for _, re := range nameDate {
		for _, m := range re.FindAllStringSubmatch(name, -1) {
			y, _ := strconv.Atoi(m[1])
			mo, _ := strconv.Atoi(m[2])
			d, _ := strconv.Atoi(m[3])
			if date, ok := validYMD(y, mo, d, now); ok {
				return date, true
			}
		}
	}
	return "", false
}
