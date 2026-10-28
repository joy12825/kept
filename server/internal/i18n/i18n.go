// Package i18n translates what the server says to people (v1.10.1): error
// messages, task names and reasons, generated summaries. The code keeps
// writing Chinese; the text is translated where it leaves the server, by
// the request's language (Accept-Language: the web sends the account's
// choice, the phone its system language).
//
// The English catalog is keyed by the Chinese text as written in the code.
// For a message made with fmt.Sprintf the key is the format string
// ("文件超过 %d MB 的上限"): a finished message is matched against the
// formats, and the values found are put into the English one, in order or
// by explicit index (%[2]s). Values that are themselves messages ("复制失败：
// 文件已存在") are translated too.
package i18n

import (
	"context"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Lang is a language the server speaks.
type Lang string

const (
	Zh Lang = "zh"
	En Lang = "en"
)

// FromHeader picks the language of an Accept-Language header: Chinese when
// the first language named is any Chinese, English for any other, Chinese
// without a header (scripts, curl).
func FromHeader(h string) Lang {
	for _, part := range strings.Split(h, ",") {
		tag := strings.ToLower(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]))
		if tag == "" || tag == "*" {
			continue
		}
		if strings.HasPrefix(tag, "zh") {
			return Zh
		}
		return En
	}
	return Zh
}

// FromRequest is FromHeader of the request's Accept-Language.
func FromRequest(r *http.Request) Lang { return FromHeader(r.Header.Get("Accept-Language")) }

type ctxKey struct{}

// With returns ctx carrying the language.
func With(ctx context.Context, l Lang) context.Context { return context.WithValue(ctx, ctxKey{}, l) }

// From is the language in ctx; Chinese when none was set (background jobs).
func From(ctx context.Context) Lang {
	if l, ok := ctx.Value(ctxKey{}).(Lang); ok {
		return l
	}
	return Zh
}

// N marks a text the server shows to people but does not build an error
// from (a task's name, a reason): it returns s unchanged. The catalog test
// finds every N and checks it has English; translate where it is sent, with T.
func N(s string) string { return s }

// T translates a message to the language in ctx.
func T(ctx context.Context, msg string) string { return Message(From(ctx), msg) }

// Message translates msg to l; a message the catalog does not know stays
// as it is.
func Message(l Lang, msg string) string {
	if l == Zh || msg == "" {
		return msg
	}
	c := catalogFor(l)
	if c == nil {
		return msg
	}
	return c.translate(msg, 0)
}

// catalog is one language's entries: exact texts, and formats tried in
// order (the most literal text first, so the more specific wins).
type catalog struct {
	exact   map[string]string
	formats []format
}

type format struct {
	re      *regexp.Regexp
	en      string
	literal int
	quoted  []bool // a value inside quotes (“%s”, 《%s》) is a name: never translated
}

var (
	mu       sync.Mutex
	entries  = map[Lang]map[string]string{}
	compiled = map[Lang]*catalog{}
)

// add registers entries for a language (called from the catalog files'
// init functions).
func add(l Lang, m map[string]string) {
	mu.Lock()
	defer mu.Unlock()
	if entries[l] == nil {
		entries[l] = map[string]string{}
	}
	for k, v := range m {
		entries[l][k] = v
	}
	delete(compiled, l)
}

// Entries returns a copy of a language's catalog (for the catalog test).
func Entries(l Lang) map[string]string {
	mu.Lock()
	defer mu.Unlock()
	out := make(map[string]string, len(entries[l]))
	for k, v := range entries[l] {
		out[k] = v
	}
	return out
}

func catalogFor(l Lang) *catalog {
	mu.Lock()
	defer mu.Unlock()
	if c, ok := compiled[l]; ok {
		return c
	}
	m := entries[l]
	if m == nil {
		return nil
	}
	c := &catalog{exact: map[string]string{}}
	for k, v := range m {
		if !hasVerb(k) {
			c.exact[k] = v
			continue
		}
		if re, lit, quoted, ok := formatRegexp(k); ok {
			c.formats = append(c.formats, format{re: re, en: v, literal: lit, quoted: quoted})
		}
	}
	sort.SliceStable(c.formats, func(i, j int) bool { return c.formats[i].literal > c.formats[j].literal })
	compiled[l] = c
	return c
}

var verbRe = regexp.MustCompile(`%(\[\d+\])?[-+# 0]*\d*(\.\d+)?[vsdqfgxXtcw]|%%`)

func hasVerb(s string) bool {
	for _, m := range verbRe.FindAllString(s, -1) {
		if m != "%%" {
			return true
		}
	}
	return false
}

// Verbs returns the formatting verbs of s in order, without %% (for the
// catalog test: the English must use as many values).
func Verbs(s string) []string {
	var out []string
	for _, m := range verbRe.FindAllString(s, -1) {
		if m != "%%" {
			out = append(out, m)
		}
	}
	return out
}

// openQuotes are the marks a name is quoted with in the messages.
var openQuotes = map[string]string{"“": "”", "《": "》", "「": "」", "‘": "’", `"`: `"`}

// formatRegexp turns a format into a regexp matching what it prints; the
// number of literal characters ranks it among the formats. quoted tells,
// per value, whether the format puts it in quotes.
func formatRegexp(f string) (*regexp.Regexp, int, []bool, bool) {
	var b strings.Builder
	b.WriteString(`^`)
	lit := 0
	last := 0
	var quoted []bool
	for _, loc := range verbRe.FindAllStringIndex(f, -1) {
		if v := f[loc[0]:loc[1]]; v != "%%" {
			q := strings.HasSuffix(v, "q")
			for open, closeMark := range openQuotes {
				if strings.HasSuffix(f[:loc[0]], open) && strings.HasPrefix(f[loc[1]:], closeMark) {
					q = true
				}
			}
			quoted = append(quoted, q)
		}
		b.WriteString(regexp.QuoteMeta(f[last:loc[0]]))
		lit += len(f[last:loc[0]])
		v := f[loc[0]:loc[1]]
		switch {
		case v == "%%":
			b.WriteString(`%`)
		case strings.HasSuffix(v, "d"):
			b.WriteString(`(-?\d+)`)
		case strings.HasSuffix(v, "q"):
			b.WriteString(`"(.*?)"`)
		default:
			b.WriteString(`(.+?)`)
		}
		last = loc[1]
	}
	b.WriteString(regexp.QuoteMeta(f[last:]))
	lit += len(f[last:])
	b.WriteString(`$`)
	re, err := regexp.Compile(b.String())
	return re, lit, quoted, err == nil
}

func (c *catalog) translate(msg string, depth int) string {
	if v, ok := c.exact[msg]; ok {
		return v
	}
	if depth > 8 {
		return msg
	}
	for _, f := range c.formats {
		m := f.re.FindStringSubmatch(msg)
		if m == nil {
			continue
		}
		args := m[1:]
		for i, a := range args {
			// a value may itself be a message ("复制失败：文件已存在"); a quoted
			// one is a name the person chose ("合同") and stays as it is
			if i < len(f.quoted) && f.quoted[i] {
				continue
			}
			args[i] = c.translate(a, depth+1)
		}
		return fill(f.en, args)
	}
	return msg
}

// fill puts the values into an English format: verbs in order, or by
// explicit index (%[2]s).
func fill(en string, args []string) string {
	next := 0
	return verbRe.ReplaceAllStringFunc(en, func(v string) string {
		if v == "%%" {
			return "%"
		}
		i := next
		if strings.HasPrefix(v, "%[") {
			n, err := strconv.Atoi(v[2:strings.Index(v, "]")])
			if err != nil {
				return v
			}
			i = n - 1
		} else {
			next++
		}
		if i < 0 || i >= len(args) {
			return v
		}
		if strings.HasSuffix(v, "q") {
			return strconv.Quote(args[i])
		}
		return args[i]
	})
}
