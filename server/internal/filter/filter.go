// Package filter parses the filter JSON of design doc 11.9 and compiles it
// into parameterized SQL over the documents table. The document list, its
// facet counts and saved views use it now; drawer rules (milestone 1.8) and
// search (1.6) will share it.
//
//	{"all": [
//	  {"field": "type", "op": "in", "value": [3, 5]},
//	  {"any": [{"field": "correspondent", "op": "in", "value": [7]},
//	           {"field": "tag", "op": "has_any", "value": [2], "descendants": true}]},
//	  {"field": "created", "op": "between", "value": ["2024-01-01", "2024-12-31"]},
//	  {"field": "cf:12", "op": "gt", "value": "1000.00"},
//	  {"text": "电费"}
//	]}
package filter

import (
	"bytes"
	"encoding/json"
	"strings"

	"kept/server/internal/apperr"
)

// Limits on a filter (design doc 11.9).
const (
	MaxDepth      = 3  // nested all / any / not groups
	MaxConditions = 50 // field and text conditions
)

// Cond is one node: a group (All, Any or Not), a field condition (Field,
// Op, Value) or a text condition (Text). The zero Cond matches everything.
type Cond struct {
	All         []Cond          `json:"all,omitempty"`
	Any         []Cond          `json:"any,omitempty"`
	Not         *Cond           `json:"not,omitempty"`
	Field       string          `json:"field,omitempty"`
	Op          string          `json:"op,omitempty"`
	Value       json.RawMessage `json:"value,omitempty"`
	Descendants bool            `json:"descendants,omitempty"` // tag: include child tags
	Text        *string         `json:"text,omitempty"`
	Prefix      bool            `json:"prefix,omitempty"` // text: the last word may be the start of a word (search-as-you-type)
}

func invalid(msg string) error { return apperr.WithCode(apperr.KindInvalid, "invalid_filter", msg) }

// Parse decodes and checks the shape of a filter. Empty input is the empty
// filter.
func Parse(raw []byte) (Cond, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return Cond{}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var c Cond
	if err := dec.Decode(&c); err != nil {
		return Cond{}, invalid("筛选条件的格式不正确")
	}
	if dec.More() {
		return Cond{}, invalid("筛选条件的格式不正确")
	}
	n := 0
	if err := c.check(0, &n); err != nil {
		return Cond{}, err
	}
	return c, nil
}

// ParseString is Parse for a query parameter.
func ParseString(s string) (Cond, error) { return Parse([]byte(s)) }

// IsZero reports whether c matches everything.
func (c Cond) IsZero() bool {
	return c.All == nil && c.Any == nil && c.Not == nil && c.Field == "" && c.Text == nil
}

func (c Cond) kinds() int {
	n := 0
	for _, b := range []bool{c.All != nil, c.Any != nil, c.Not != nil, c.Field != "", c.Text != nil} {
		if b {
			n++
		}
	}
	return n
}

func (c Cond) check(depth int, count *int) error {
	if c.kinds() > 1 {
		return invalid("一个筛选条件只能是 all、any、not、字段条件或文字条件之一")
	}
	if (c.Op != "" || c.Value != nil || c.Descendants) && c.Field == "" {
		return invalid("筛选条件缺少字段")
	}
	if c.Prefix && c.Text == nil {
		return invalid("prefix 只能用于文字条件")
	}
	switch {
	case c.All != nil || c.Any != nil || c.Not != nil:
		if depth >= MaxDepth {
			return invalid("筛选条件最多嵌套 3 层")
		}
		children := append(append([]Cond{}, c.All...), c.Any...)
		if c.Not != nil {
			if c.Not.IsZero() {
				return invalid("not 里缺少条件")
			}
			children = append(children, *c.Not)
		}
		for _, ch := range children {
			if err := ch.check(depth+1, count); err != nil {
				return err
			}
		}
	case c.Field != "" || c.Text != nil:
		*count++
		if *count > MaxConditions {
			return invalid("筛选条件最多 50 个")
		}
		if c.Field != "" && c.Op == "" {
			return invalid("筛选条件缺少运算")
		}
	}
	return nil
}

// Uses reports whether any condition in c is on field.
func (c Cond) Uses(field string) bool {
	if c.Field == field {
		return true
	}
	for _, ch := range append(append([]Cond{}, c.All...), c.Any...) {
		if ch.Uses(field) {
			return true
		}
	}
	return c.Not != nil && c.Not.Uses(field)
}

// Texts returns the text conditions a matching document satisfies (those
// not under a not), in order: the first is the one results are ranked and
// highlighted by.
func (c Cond) Texts() []Cond {
	var out []Cond
	var walk func(Cond)
	walk = func(c Cond) {
		if c.Text != nil {
			if strings.TrimSpace(*c.Text) != "" {
				out = append(out, c)
			}
		}
		for _, ch := range c.All {
			walk(ch)
		}
		for _, ch := range c.Any {
			walk(ch)
		}
	}
	walk(c)
	return out
}

// Without drops the top-level conditions that only concern field, for facet
// counts: the count next to each type is computed as if no type were
// selected yet (the prototype's matchDoc(…, except)). A top-level condition
// is dropped when it is a condition on field, or an any / all group made
// only of such conditions.
func (c Cond) Without(field string) Cond {
	if c.onlyField(field) {
		return Cond{}
	}
	if c.All == nil {
		return c
	}
	out := Cond{All: []Cond{}}
	for _, ch := range c.All {
		if !ch.onlyField(field) {
			out.All = append(out.All, ch)
		}
	}
	return out
}

func (c Cond) onlyField(field string) bool {
	if c.Field != "" {
		return c.Field == field
	}
	group := append(append([]Cond{}, c.All...), c.Any...)
	if len(group) == 0 || c.Not != nil {
		return false
	}
	for _, ch := range group {
		if !ch.onlyField(field) {
			return false
		}
	}
	return true
}

// ReplaceIDs rewrites the conditions on field (type, correspondent, tag,
// drawer) so the IDs in from become their mapped ID (taxonomy merges). It
// reports whether anything changed; lists keep each ID once.
func ReplaceIDs(c Cond, field string, from map[int64]int64) (Cond, bool) {
	changed := false
	walk := func(cs []Cond) []Cond {
		out := make([]Cond, len(cs))
		for i, ch := range cs {
			var ok bool
			out[i], ok = ReplaceIDs(ch, field, from)
			changed = changed || ok
		}
		return out
	}
	if c.All != nil {
		c.All = walk(c.All)
	}
	if c.Any != nil {
		c.Any = walk(c.Any)
	}
	if c.Not != nil {
		n, ok := ReplaceIDs(*c.Not, field, from)
		c.Not, changed = &n, changed || ok
	}
	if c.Field == field && len(c.Value) > 0 {
		var ids []int64
		if json.Unmarshal(c.Value, &ids) == nil {
			seen := map[int64]bool{}
			var out []int64
			hit := false
			for _, id := range ids {
				if to, ok := from[id]; ok {
					id, hit = to, true
				}
				if !seen[id] {
					seen[id] = true
					out = append(out, id)
				}
			}
			if hit {
				c.Value, _ = json.Marshal(out)
				changed = true
			}
		}
	}
	return c, changed
}
