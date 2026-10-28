package search

import (
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

// QueryOptions tune Parse.
type QueryOptions struct {
	// Prefix treats the last plain word as the beginning of a word, for
	// search-as-you-type (⌘K).
	Prefix bool
}

// Parse turns what a person typed into a query (design doc 5.4):
//
//	电费 发票            every word must appear (title, text, notes, tags, …)
//	"专用发票"           a phrase
//	-草稿                must not appear
//	tag:发票 type:合同 from:国家电网   in that field only
//	created:2024  created:2024-03  created:2024-01..2024-06
//	asn:123
//
// Malformed parts (an unclosed quote, an unknown field, a bad date) are
// searched as plain words rather than rejected. It returns nil when there
// is nothing to search for.
func Parse(input string, opt QueryOptions) query.Query {
	parts := splitQuery(input)
	var must, mustNot []query.Query
	lastPlain := -1
	for i, p := range parts {
		if !p.negated && !p.quoted && p.field == "" {
			lastPlain = i
		}
	}
	for i, p := range parts {
		q := p.query(opt.Prefix && i == lastPlain)
		if q == nil {
			continue
		}
		if p.negated {
			mustNot = append(mustNot, q)
		} else {
			must = append(must, q)
		}
	}
	if len(must) == 0 && len(mustNot) == 0 {
		return nil
	}
	b := bleve.NewBooleanQuery()
	if len(must) == 0 {
		must = []query.Query{bleve.NewMatchAllQuery()}
	}
	b.AddMust(must...)
	b.AddMustNot(mustNot...)
	return b
}

// part is one space-separated piece of the input.
type part struct {
	negated bool
	field   string // "" for a plain word or phrase
	value   string
	quoted  bool
}

func splitQuery(input string) []part {
	var out []part
	rs := []rune(strings.TrimSpace(input))
	for i := 0; i < len(rs); {
		if unicode.IsSpace(rs[i]) {
			i++
			continue
		}
		var p part
		if rs[i] == '-' && i+1 < len(rs) && !unicode.IsSpace(rs[i+1]) {
			p.negated = true
			i++
		}
		start := i
		// field:
		for i < len(rs) && rs[i] != ':' && rs[i] != '"' && !unicode.IsSpace(rs[i]) {
			i++
		}
		if i < len(rs) && rs[i] == ':' && i > start && knownField(string(rs[start:i])) {
			p.field = strings.ToLower(string(rs[start:i]))
			i++
		} else {
			i = start
		}
		if i < len(rs) && rs[i] == '"' {
			end := i + 1
			for end < len(rs) && rs[end] != '"' {
				end++
			}
			if end < len(rs) { // closed
				p.value, p.quoted = string(rs[i+1:end]), true
				i = end + 1
				out = append(out, p)
				continue
			}
			i++ // an unclosed quote is ignored
		}
		start = i
		for i < len(rs) && !unicode.IsSpace(rs[i]) {
			i++
		}
		p.value = string(rs[start:i])
		if p.value != "" || p.field != "" {
			out = append(out, p)
		}
	}
	return out
}

var fieldOf = map[string]string{"tag": fieldTags, "type": fieldType, "from": fieldCorrespondent, "person": fieldPerson}

func knownField(f string) bool {
	switch strings.ToLower(f) {
	case "tag", "type", "from", "person", "created", "asn":
		return true
	}
	return false
}

func (p part) query(prefix bool) query.Query {
	switch p.field {
	case "created":
		if q := dateRange(p.value); q != nil {
			return q
		}
		return words("created:"+p.value, false, textFields...)
	case "asn":
		if n, err := strconv.ParseFloat(strings.TrimLeft(p.value, "#0"), 64); err == nil && n >= 1 {
			on := true
			q := bleve.NewNumericRangeInclusiveQuery(&n, &n, &on, &on)
			q.SetField(fieldASN)
			return q
		}
		return words("asn:"+p.value, false, textFields...)
	case "tag", "type", "from":
		f := []struct {
			name  string
			boost float64
		}{{fieldOf[p.field], 1}}
		if p.quoted {
			return phrase(p.value, f...)
		}
		return words(p.value, false, f...)
	}
	if p.quoted {
		return phrase(p.value, textFields...)
	}
	return words(p.value, prefix, textFields...)
}

// words matches every term of text in one of the fields; with prefix the
// last term may be the beginning of a longer one.
func words(text string, prefix bool, fields ...struct {
	name  string
	boost float64
}) query.Query {
	terms := Terms(text, false)
	if len(terms) == 0 {
		return nil
	}
	var any []query.Query
	for _, f := range fields {
		var all []query.Query
		for i, t := range terms {
			var q query.FieldableQuery
			if prefix && i == len(terms)-1 {
				q = bleve.NewPrefixQuery(t)
			} else {
				q = bleve.NewTermQuery(t)
			}
			q.SetField(f.name)
			all = append(all, q)
		}
		c := bleve.NewConjunctionQuery(all...)
		c.SetBoost(f.boost)
		any = append(any, c)
	}
	return bleve.NewDisjunctionQuery(any...)
}

func phrase(text string, fields ...struct {
	name  string
	boost float64
}) query.Query {
	if len(Terms(text, false)) == 0 {
		return nil
	}
	var any []query.Query
	for _, f := range fields {
		q := bleve.NewMatchPhraseQuery(text)
		q.SetField(f.name)
		q.Analyzer = queryAnalyzer
		q.SetBoost(f.boost)
		any = append(any, q)
	}
	return bleve.NewDisjunctionQuery(any...)
}

// dateRange reads 2024, 2024-03, 2024-03-05 and a..b of those.
func dateRange(v string) query.Query {
	from, to, ok := strings.Cut(v, "..")
	if !ok {
		to = from
	}
	start, _, ok1 := period(from)
	_, end, ok2 := period(to)
	if !ok1 || !ok2 || !end.After(start) {
		return nil
	}
	yes, no := true, false
	q := bleve.NewDateRangeInclusiveQuery(start, end, &yes, &no)
	q.SetField(fieldCreated)
	return q
}

// period is the span a partial date stands for, in local time.
func period(s string) (start, end time.Time, ok bool) {
	for _, l := range []struct {
		layout string
		years  int
		months int
		days   int
	}{{"2006", 1, 0, 0}, {"2006-01", 0, 1, 0}, {"2006-01-02", 0, 0, 1}} {
		if t, err := time.ParseInLocation(l.layout, s, time.Local); err == nil && len(s) == len(l.layout) {
			return t, t.AddDate(l.years, l.months, l.days), true
		}
	}
	return time.Time{}, time.Time{}, false
}
