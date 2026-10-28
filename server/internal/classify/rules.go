package classify

import (
	"fmt"
	"kept/server/internal/i18n"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"kept/server/internal/taxonomy"
)

// Match is the outcome of testing one rule against a text.
type Match struct {
	Matched bool
	Basis   string   // e.g. 关键词“国家电网”, for field_sources and “试一试”
	Hits    []string // the matched pieces of the text
}

// fuzzyMin is the similarity a fuzzy rule needs (edit distance, design doc 5.6).
const fuzzyMin = 0.9

// Test checks rule against text (already cut with Text). Rules of
// algorithm none and auto never match here; auto is the model's job.
func Test(rule taxonomy.MatchRule, text string) Match {
	pattern := strings.TrimSpace(rule.Match)
	if pattern == "" {
		return Match{}
	}
	switch rule.Algorithm {
	case "any", "all":
		t := fold(text, rule.CaseInsensitive)
		var hits []string
		for _, k := range Keywords(pattern) {
			if contains(t, fold(k, rule.CaseInsensitive)) {
				hits = append(hits, k)
			} else if rule.Algorithm == "all" {
				return Match{}
			}
		}
		if len(hits) == 0 {
			return Match{}
		}
		return Match{Matched: true, Basis: fmt.Sprintf(i18n.N("关键词%s"), quoteAll(hits)), Hits: hits}
	case "literal":
		if contains(fold(text, rule.CaseInsensitive), fold(pattern, rule.CaseInsensitive)) {
			return Match{Matched: true, Basis: fmt.Sprintf(i18n.N("包含%s"), quote(pattern)), Hits: []string{pattern}}
		}
	case "regex":
		expr := pattern
		if rule.CaseInsensitive {
			expr = "(?i)" + expr
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return Match{}
		}
		if loc := re.FindStringIndex(text); loc != nil {
			hit := strings.TrimSpace(text[loc[0]:loc[1]])
			return Match{Matched: true, Basis: fmt.Sprintf(i18n.N("正则匹配%s"), quote(clip(hit, 40))), Hits: []string{hit}}
		}
	case "fuzzy":
		if hit, ok := fuzzy(fold(text, rule.CaseInsensitive), fold(pattern, rule.CaseInsensitive)); ok {
			return Match{Matched: true, Basis: fmt.Sprintf(i18n.N("近似匹配%s"), quote(hit)), Hits: []string{hit}}
		}
	}
	return Match{}
}

// Keywords splits a rule's keywords at spaces and commas; "…" keeps a
// phrase with spaces together.
func Keywords(s string) []string {
	var out []string
	var b strings.Builder
	quoted := false
	flush := func() {
		if w := strings.TrimSpace(b.String()); w != "" {
			out = append(out, w)
		}
		b.Reset()
	}
	for _, r := range s {
		switch {
		case r == '"' || r == '“' || r == '”':
			flush()
			quoted = !quoted
		case !quoted && (unicode.IsSpace(r) || strings.ContainsRune(",，、;；", r)):
			flush()
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return out
}

// contains finds word in text; a word that begins or ends with a Latin
// letter or digit must not be part of a longer word there (“tax” does not
// match “taxi”). Chinese has no word boundaries, so it matches anywhere.
func contains(text, word string) bool {
	if word == "" {
		return false
	}
	for from := 0; ; {
		i := strings.Index(text[from:], word)
		if i < 0 {
			return false
		}
		i += from
		end := i + len(word)
		before, _ := utf8.DecodeLastRuneInString(text[:i])
		after, _ := utf8.DecodeRuneInString(text[end:])
		first, _ := utf8.DecodeRuneInString(word)
		last, _ := utf8.DecodeLastRuneInString(word)
		if !(latin(first) && i > 0 && latin(before)) && !(latin(last) && end < len(text) && latin(after)) {
			return true
		}
		from = i + utf8.RuneLen(first)
	}
}

func latin(r rune) bool { return r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r)) }

// fuzzy slides a window as long as pattern over text and returns the first
// piece within fuzzyMin similarity. Short patterns allow no edits at all.
func fuzzy(text, pattern string) (string, bool) {
	if contains(text, pattern) {
		return pattern, true
	}
	p := []rune(pattern)
	maxDist := int(float64(len(p)) * (1 - fuzzyMin))
	if maxDist == 0 {
		return "", false
	}
	t := []rune(text)
	// Windows a little shorter or longer than the pattern catch a missing
	// or extra character.
	for size := len(p) - maxDist; size <= len(p)+maxDist; size++ {
		if size <= 0 || size > len(t) {
			continue
		}
		for i := 0; i+size <= len(t); i++ {
			if t[i] != p[0] && t[i+size-1] != p[len(p)-1] && maxDist < 2 {
				continue
			}
			if levenshtein(t[i:i+size], p, maxDist) <= maxDist {
				return string(t[i : i+size]), true
			}
		}
	}
	return "", false
}

// levenshtein is the edit distance of a and b, or max+1 once it exceeds max.
func levenshtein(a, b []rune, max int) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		low := cur[0]
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			low = min(low, cur[j])
		}
		if low > max {
			return max + 1
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func quote(s string) string { return "“" + s + "”" }

func quoteAll(ws []string) string {
	var b strings.Builder
	for _, w := range ws {
		b.WriteString(quote(w))
	}
	return b.String()
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
