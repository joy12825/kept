// Package pinyin computes the keys the directory sorts and groups names by:
// Chinese names by their pinyin (“国家电网” → guojiadianwang, under G),
// everything else as is, grouped under “#” (UI spec 5.8).
package pinyin

import (
	"strings"
	"unicode"

	gopinyin "github.com/mozillazg/go-pinyin"
)

var args = func() gopinyin.Args {
	a := gopinyin.NewArgs()
	a.Style = gopinyin.Normal // no tone marks
	return a
}()

// SortKey turns Han characters into toneless pinyin and lower-cases the
// rest, without separators between syllables. Polyphones take
// go-pinyin's default reading.
func SortKey(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if unicode.Is(unicode.Han, r) {
			if py := gopinyin.SinglePinyin(r, args); len(py) > 0 && py[0] != "" {
				b.WriteString(py[0])
				continue
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// Initial is the directory group of a name: the upper-case first letter of
// its pinyin when it starts with a Han character, otherwise "#" (names
// starting with Latin letters, digits or symbols).
func Initial(name string) string {
	name = strings.TrimSpace(name)
	for _, r := range name {
		if !unicode.Is(unicode.Han, r) {
			return "#"
		}
		if py := gopinyin.SinglePinyin(r, args); len(py) > 0 && py[0] != "" {
			return strings.ToUpper(py[0][:1])
		}
		return "#"
	}
	return "#"
}

// TitleKey is the sort key of a document title: SortKey with every run of
// digits zero-padded to 10 places, so “2024年3月” sorts before “2024年10月”.
func TitleKey(title string) string {
	key := SortKey(title)
	var b strings.Builder
	for i := 0; i < len(key); {
		if key[i] < '0' || key[i] > '9' {
			b.WriteByte(key[i])
			i++
			continue
		}
		j := i
		for j < len(key) && key[j] >= '0' && key[j] <= '9' {
			j++
		}
		if pad := 10 - (j - i); pad > 0 {
			b.WriteString(strings.Repeat("0", pad))
		}
		b.WriteString(key[i:j])
		i = j
	}
	return b.String()
}
