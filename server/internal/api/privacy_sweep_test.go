package api

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

// v1.9.10: a private document leaves no trace for those who may not see it.
// Every read operation of the API is called as two such members (the owner
// and a viewer) before and after bob adds a private document for his mother
// — with a tag, a drawer, a note, a location, an alert date; their answers
// must not change. A new operation is covered without anyone remembering
// to filter it: it is read from the API description.

// sweepSkip lists read operations the sweep does not compare, and why.
var sweepSkip = map[string]string{
	"getevents":       "a stream; TestPrivateDocumentEventsStayPrivate covers it",
	"admingetsystem":  "instance-wide: an administrator's view of storage and queues",
	"adminlistaudit":  "instance-wide administration",
	"exportworkspace": "writes an activity entry each time it is read",
	"getstoragelocation": "the owner sees how many private documents and bytes the storage holds, as one total " +
		"(v1.8.0: moving storage moves them too); TestPrivateDocumentsDoNotShowToOtherMembers checks it",
	"listmemberuploads": "upload quotas count every byte a member stored, private ones too: the quota is real storage",
}

// sweepVolatile are fields that change between two calls whatever happens.
var sweepVolatile = regexp.MustCompile(`(?i)(At|Time|Until|now|generated|uptime|duration|took|elapsed|lastRun|nextRun|checkedAt|age)$`)

// sweepParams fills the path parameters the sweep knows.
type sweepParams map[string]string

func (p sweepParams) fill(path string) (string, bool) {
	ok := true
	out := regexp.MustCompile(`\{([^}]+)\}`).ReplaceAllStringFunc(path, func(m string) string {
		name := m[1 : len(m)-1]
		// "{id}" means what the segment before it names
		if name == "id" {
			i := strings.Index(path, m)
			seg := strings.Trim(path[:i], "/")
			seg = seg[strings.LastIndex(seg, "/")+1:]
			name = seg + ".id"
		}
		v, found := p[name]
		if !found {
			ok = false
		}
		return url.PathEscape(v)
	})
	return out, ok
}

func normalise(raw string) string {
	var v any
	if json.Unmarshal([]byte(raw), &v) != nil {
		return raw
	}
	var walk func(any) any
	walk = func(x any) any {
		switch t := x.(type) {
		case map[string]any:
			for k, val := range t {
				if sweepVolatile.MatchString(k) {
					t[k] = "~"
					continue
				}
				t[k] = walk(val)
			}
		case []any:
			for i := range t {
				t[i] = walk(t[i])
			}
		}
		return x
	}
	b, _ := json.Marshal(walk(v))
	return string(b)
}
