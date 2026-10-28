// Package classify fills in a new document's type, correspondent and tags
// (design doc 5.6): first the matching rules of the workspace's items, then
// a model learned from the documents already filed. Every value it sets is
// recorded with its source and basis in documents.field_sources.
package classify

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"kept/server/internal/search"
)

// MaxText is how much of a document (title, then text) is looked at.
const MaxText = 20000

// Text joins title and content and cuts the result to MaxText characters.
func Text(title, content string) string {
	s := title + "\n" + content
	if utf8.RuneCountInString(s) <= MaxText {
		return s
	}
	n := 0
	for i := range s {
		if n == MaxText {
			return s[:i]
		}
		n++
	}
	return s
}

// fold prepares text for rule matching: NFKC (full-width letters and digits
// become ASCII), lower case when asked, and runs of white space as one space.
func fold(s string, lower bool) string {
	s = norm.NFKC.String(s)
	if lower {
		s = strings.ToLower(s)
	}
	return strings.Join(strings.Fields(s), " ")
}

// Features are the words the model sees: the same cutting as the search
// index's queries, without stop words, pure numbers and single letters.
func Features(text string) []string {
	terms := search.Terms(text, false)
	out := terms[:0]
	for _, t := range terms {
		if stopWords[t] || isNumber(t) || (utf8.RuneCountInString(t) == 1 && t[0] < utf8.RuneSelf) {
			continue
		}
		out = append(out, t)
	}
	return out
}

func isNumber(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) && r != '.' && r != ',' && r != '-' && r != '/' && r != ':' && r != '%' {
			return false
		}
	}
	return true
}

var stopWords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`
		的 了 和 与 及 或 在 是 为 对 由 于 以 将 把 被 从 向 到 之 其 该 此 这 那 有 无 不 也 都 而 并 等 个 年 月 日 时 分 元 号 第 页 共
		我 你 他 她 它 我们 你们 他们 本 上 下 中 内 外 前 后 如 若 则 但 即 已 可 应 须 请
		a an and are as at be by for from has have in is it its of on or that the this to was were will with
		page pages no nr`) {
		m[w] = true
	}
	return m
}()
