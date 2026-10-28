package store

import (
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// The queries are written for SQLite (sqlc and by hand); on Postgres they
// are rewritten once each, here (v1.0 stage 38, design doc decision 25):
//
//   - ? placeholders become $1, $2, …; ?NNN becomes $NNN
//   - INSERT OR IGNORE INTO … becomes INSERT INTO … ON CONFLICT DO NOTHING
//   - json_each( becomes kept_json_each( (arrays: key and value), char( becomes chr(
//   - LIKE becomes ILIKE: SQLite's LIKE ignores case
//   - INTEGER becomes BIGINT (CAST(… AS INTEGER)); x IS ? becomes x IS NOT DISTINCT FROM $n
//
// Text in string literals, quoted identifiers and comments is left alone.
// json_extract, instr and group_concat exist on Postgres as functions the
// baseline migration creates.

var rewritten sync.Map // query → rewritten query

// RewriteForPostgres turns a SQLite query into its Postgres form.
func RewriteForPostgres(q string) string {
	if v, ok := rewritten.Load(q); ok {
		return v.(string)
	}
	out := rewrite(q)
	rewritten.Store(q, out)
	return out
}

func isWord(r byte) bool {
	return r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

// hasWordAt reports whether s has word (case-insensitive) at i, not inside
// a longer word.
func hasWordAt(s string, i int, word string) bool {
	if i+len(word) > len(s) || !strings.EqualFold(s[i:i+len(word)], word) {
		return false
	}
	if i > 0 && isWord(s[i-1]) {
		return false
	}
	return i+len(word) == len(s) || !isWord(s[i+len(word)])
}

// skipSpace returns the index of the first non-space byte from i.
func skipSpace(s string, i int) int {
	for i < len(s) && unicode.IsSpace(rune(s[i])) {
		i++
	}
	return i
}

func isPlaceholder(q string, i int) bool { return i < len(q) && q[i] == '?' }

func rewrite(q string) string {
	var b strings.Builder
	b.Grow(len(q) + 16)
	n := 0
	conflict := false // INSERT OR IGNORE: add ON CONFLICT DO NOTHING
	returning := -1   // where a top-level RETURNING starts in the output
	depth := 0
	for i := 0; i < len(q); {
		c := q[i]
		switch {
		case c == '\'' || c == '"':
			j := i + 1
			for j < len(q) {
				if q[j] == c {
					if j+1 < len(q) && q[j+1] == c { // doubled quote inside
						j += 2
						continue
					}
					break
				}
				j++
			}
			end := min(j+1, len(q))
			b.WriteString(q[i:end])
			i = end
		case c == '-' && i+1 < len(q) && q[i+1] == '-':
			j := strings.IndexByte(q[i:], '\n')
			if j < 0 {
				j = len(q) - i
			}
			b.WriteString(q[i : i+j])
			i += j
		case c == '/' && i+1 < len(q) && q[i+1] == '*':
			j := strings.Index(q[i+2:], "*/")
			end := len(q)
			if j >= 0 {
				end = i + 2 + j + 2
			}
			b.WriteString(q[i:end])
			i = end
		case c == '?':
			j := i + 1
			for j < len(q) && q[j] >= '0' && q[j] <= '9' {
				j++
			}
			if j > i+1 { // ?NNN: the NNNth argument (sqlc's named arguments)
				b.WriteString("$" + q[i+1:j])
				i = j
				continue
			}
			n++
			b.WriteString("$" + strconv.Itoa(n))
			i++
		case c == '(':
			depth++
			b.WriteByte(c)
			i++
		case c == ')':
			depth--
			b.WriteByte(c)
			i++
		case hasWordAt(q, i, "INSERT"):
			j := skipSpace(q, i+len("INSERT"))
			if hasWordAt(q, j, "OR") {
				k := skipSpace(q, j+2)
				if hasWordAt(q, k, "IGNORE") {
					b.WriteString(q[i : i+len("INSERT")])
					conflict = true
					i = k + len("IGNORE")
					continue
				}
			}
			b.WriteString(q[i : i+len("INSERT")])
			i += len("INSERT")
		case depth == 0 && hasWordAt(q, i, "RETURNING"):
			returning = b.Len()
			b.WriteString(q[i : i+len("RETURNING")])
			i += len("RETURNING")
		case hasWordAt(q, i, "json_each") && strings.HasPrefix(q[skipSpace(q, i+len("json_each")):], "("):
			b.WriteString("kept_json_each")
			i += len("json_each")
		case hasWordAt(q, i, "char") && strings.HasPrefix(q[skipSpace(q, i+len("char")):], "("):
			b.WriteString("chr")
			i += len("char")
		case hasWordAt(q, i, "INTEGER"):
			b.WriteString("BIGINT") // SQLite's INTEGER is 64 bits; Postgres's is 32
			i += len("INTEGER")
		case hasWordAt(q, i, "IS") && isPlaceholder(q, skipSpace(q, i+2)):
			b.WriteString("IS NOT DISTINCT FROM") // SQLite's IS compares values, NULL included
			i += 2
		case hasWordAt(q, i, "LIKE"):
			b.WriteString("ILIKE")
			i += len("LIKE")
		case isWord(c):
			j := i
			for j < len(q) && isWord(q[j]) {
				j++
			}
			b.WriteString(q[i:j])
			i = j
		default:
			b.WriteByte(c)
			i++
		}
	}
	out := b.String()
	if conflict {
		if returning >= 0 {
			out = out[:returning] + "ON CONFLICT DO NOTHING " + out[returning:]
		} else {
			trimmed := strings.TrimRight(out, " \t\r\n;")
			out = trimmed + " ON CONFLICT DO NOTHING" + out[len(trimmed):]
		}
	}
	return out
}
