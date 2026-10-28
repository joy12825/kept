// Package search keeps the full-text index (design doc part 5): a Bleve
// index of documents' text with a gse-based Chinese analyzer, the query
// syntax of 5.4, recall by relevance and highlighted snippets. SQLite stays
// in charge of every structured filter (5.1); this package only answers
// "which documents match these words, and how well".
package search

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/blevesearch/bleve/v2/analysis"
	_ "github.com/blevesearch/bleve/v2/analysis/analyzer/custom" // the "custom" analyzer type
	"github.com/blevesearch/bleve/v2/registry"
	"github.com/go-ego/gse"
	"golang.org/x/text/unicode/norm"
)

// Analyzer names. Documents are indexed in gse's search mode (a word and
// its sub-words, "国家电网" → 国家电网, 国家, 电网), queries are cut in its
// precise mode, so a query word matches the word or any word containing it.
const (
	indexAnalyzer = "kept_index"
	queryAnalyzer = "kept_query"
)

var (
	segmenter gse.Segmenter
	segOnce   sync.Once
)

// LoadDictionary loads gse's embedded simplified-Chinese dictionary (about
// 110 MB of memory, 0.3 s) once per process. Tokenizing waits for it.
func LoadDictionary() {
	segOnce.Do(func() {
		if err := segmenter.LoadDictEmbed("zh_s"); err != nil {
			panic("search: load the gse dictionary: " + err.Error())
		}
	})
}

func init() {
	register := func(name string, search bool) {
		registry.RegisterTokenizer(name, func(map[string]any, *registry.Cache) (analysis.Tokenizer, error) {
			return tokenizer{search: search}, nil
		})
	}
	register("kept_gse_index", true)
	register("kept_gse_query", false)
}

type tokenizer struct{ search bool }

// minSubword is the shortest sub-word indexed: single characters would make
// the index much bigger for little gain.
const minSubword = 2

// Tokenize cuts text with gse. The text is NFKC-normalized first (full-width
// “ＡＢＣ２０２４” becomes abc2024 and is cut as one word); offsets are mapped
// back into the original text so highlights line up. Sub-words share their
// word's position, so phrase queries still work.
func (t tokenizer) Tokenize(input []byte) analysis.TokenStream {
	LoadDictionary()
	text, orig := normalizeWithOffsets(input)
	segs := segmenter.Segment(text)
	out := make(analysis.TokenStream, 0, len(segs))
	pos := 0
	var emit func(s *gse.Segment, base int, top bool)
	emit = func(s *gse.Segment, base int, top bool) {
		start, end := base+s.Start(), base+s.End()
		term := normalize(text[start:end])
		if term == "" {
			return
		}
		if top {
			pos++
		} else if utf8.RuneCountInString(term) < minSubword {
			return
		}
		out = append(out, &analysis.Token{Term: []byte(term), Start: orig[start], End: orig[end], Position: pos, Type: tokenType(term)})
		if t.search {
			for _, sub := range s.Token().Segments() {
				emit(sub, start, false)
			}
		}
	}
	for i := range segs {
		emit(&segs[i], 0, true)
	}
	return out
}

// normalizeWithOffsets applies NFKC rune by rune and lower-cases, and maps
// each byte offset of the result (and its end) to the original's.
func normalizeWithOffsets(input []byte) ([]byte, []int) {
	out := make([]byte, 0, len(input))
	orig := make([]int, 0, len(input)+1)
	for i := 0; i < len(input); {
		_, size := utf8.DecodeRune(input[i:])
		n := []byte(strings.ToLower(norm.NFKC.String(string(input[i : i+size]))))
		for range n {
			orig = append(orig, i)
		}
		out = append(out, n...)
		i += size
	}
	return out, append(orig, len(input))
}

// normalize trims a cut piece of the (already normalized) text; whitespace
// and punctuation make no terms.
func normalize(b []byte) string {
	return strings.TrimFunc(string(b), func(r rune) bool { return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) })
}

func tokenType(term string) analysis.TokenType {
	r, _ := utf8.DecodeRuneInString(term)
	if unicode.Is(unicode.Han, r) {
		return analysis.Ideographic
	}
	if unicode.IsDigit(r) {
		return analysis.Numeric
	}
	return analysis.AlphaNumeric
}

// Terms returns the query terms of text, for tests and the query parser.
func Terms(text string, indexing bool) []string {
	var out []string
	for _, tk := range (tokenizer{search: indexing}).Tokenize([]byte(text)) {
		out = append(out, string(tk.Term))
	}
	return out
}
