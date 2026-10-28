// Package dedupe finds near-duplicate documents (v0.3): a 64-bit SimHash
// of each document's text, compared within a workspace (design doc, part
// 8, sub-project 3).
package dedupe

import (
	"hash/fnv"
	"math/bits"
	"strings"
	"unicode"
)

const (
	// MinChars is the least text (letters and digits) that gets a
	// fingerprint; shorter documents are not compared: in a short text a
	// shared template outweighs what differs.
	MinChars = 120
	// MaxDistance is the largest Hamming distance counted as near.
	MaxDistance = 6
	// numberWeight makes digits count more: a scan and the e-version of
	// one invoice share their numbers; this month's bill and last month's
	// do not.
	numberWeight = 4
)

// Fingerprint returns the SimHash of text, or false when it is too short.
// Features: pairs of adjacent CJK (or other non-Latin) characters, Latin
// words and runs of digits; spaces and punctuation are dropped, so OCR
// spacing does not matter.
func Fingerprint(text string) (uint64, bool) {
	feats := map[string]int{}
	chars := 0
	var cur []rune // the run being collected
	kind := 0      // 0 none, 1 digits, 2 latin, 3 other letters
	flush := func() {
		switch kind {
		case 1:
			feats["#"+string(cur)] += numberWeight
		case 2:
			feats[string(cur)]++
		case 3:
			if len(cur) == 1 {
				feats[string(cur)]++
			}
			for i := 0; i+1 < len(cur); i++ {
				feats[string(cur[i:i+2])]++
			}
		}
		cur, kind = cur[:0], 0
	}
	for _, r := range strings.ToLower(text) {
		k := 0
		switch {
		case r >= '0' && r <= '9':
			k = 1
		case r == '.' && kind == 1: // a decimal point inside a number
			k = 1
		case r < 128 && unicode.IsLetter(r):
			k = 2
		case unicode.IsLetter(r):
			k = 3
		}
		if k == 0 {
			flush()
			continue
		}
		if k != kind {
			flush()
			kind = k
		}
		cur = append(cur, r)
		chars++
	}
	flush()
	if chars < MinChars {
		return 0, false
	}
	var v [64]int
	h := fnv.New64a()
	for f, w := range feats {
		h.Reset()
		h.Write([]byte(f))
		x := h.Sum64()
		for i := range 64 {
			if x&(1<<i) != 0 {
				v[i] += w
			} else {
				v[i] -= w
			}
		}
	}
	var out uint64
	for i := range 64 {
		if v[i] > 0 {
			out |= 1 << i
		}
	}
	return out, true
}

// Distance is the number of differing bits.
func Distance(a, b uint64) int { return bits.OnesCount64(a ^ b) }

// minSharedNumbers is the share of numbers (runs of two or more digits)
// two near texts must have in common: a scan and the e-version of one
// receipt agree on them, documents made from one template mostly do not.
const minSharedNumbers = 0.75

func numbers(text string) map[string]bool {
	out := map[string]bool{}
	var cur []rune
	flush := func() {
		if len(cur) >= 2 {
			out[string(cur)] = true
		}
		cur = cur[:0]
	}
	for _, r := range text {
		if r >= '0' && r <= '9' || (r == '.' && len(cur) > 0) {
			cur = append(cur, r)
			continue
		}
		flush()
	}
	flush()
	return out
}

// strictDistance is the fingerprint distance texts with too few numbers
// to compare must be within.
const strictDistance = 3

// Confirm checks a near pair (fingerprints distance apart): their numbers
// mostly agree (Jaccard index). Texts with fewer than three numbers must
// have closer fingerprints instead.
func Confirm(a, b string, distance int) bool {
	na, nb := numbers(a), numbers(b)
	if len(na) < 3 || len(nb) < 3 {
		return distance <= strictDistance
	}
	both := 0
	for n := range na {
		if nb[n] {
			both++
		}
	}
	return float64(both)/float64(len(na)+len(nb)-both) >= minSharedNumbers
}
