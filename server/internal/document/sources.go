package document

import (
	"encoding/json"
	"maps"
)

// Where a field's value came from (design doc 3.3, documents.field_sources).
const (
	SourcePreset = "preset" // chosen when uploading
	SourceRule   = "rule"   // an item's matching rule
	SourceModel  = "model"  // the learned classifier
	SourceText   = "text"   // recognised in the document's text (the date)
	SourceFile   = "file"   // the file's name or a photo's capture time (the date, v1.4.4)
	SourceUser   = "user"   // a person changed it; classification leaves it alone
	// SourceWorkflow is a workflow's assign action; Basis is its name.
	SourceWorkflow = "workflow"
	// SourceAgent is the intake agent (AI); Basis quotes the text.
	SourceAgent = "agent"
	// SourceSimilar is the vote of the most similar filed documents
	// (vectors, v0.5); Basis names the closest one.
	SourceSimilar = "similar"
)

// Automatic reports whether classification set the value (a rule, the
// learned model or similar documents) and may replace it.
func (s *Source) Automatic() bool {
	return s != nil && (s.Source == SourceRule || s.Source == SourceModel || s.Source == SourceSimilar)
}

// Source is one field's origin. Basis explains it (关键词“国家电网”, the
// matched date); Confidence is the model's probability.
type Source struct {
	Source     string  `json:"source"`
	Basis      string  `json:"basis,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
	// Alternatives are the other dates found (v1.4.4), offered on the
	// details page.
	Alternatives []DateOption `json:"alternatives,omitempty"`
}

// Same reports whether two sources say the same (alternatives aside).
func (s Source) Same(o Source) bool {
	return s.Source == o.Source && s.Basis == o.Basis && s.Confidence == o.Confidence
}

// DateOption is another date the document may have, with what told it.
type DateOption struct {
	Date  string `json:"date"`
	Basis string `json:"basis"`
}

// FieldSources is documents.field_sources. Tags are keyed by tag ID; a
// user entry for a tag the document does not have means a person removed
// it, so classification does not add it back.
type FieldSources struct {
	Type          *Source          `json:"type,omitempty"`
	Correspondent *Source          `json:"correspondent,omitempty"`
	Person        *Source          `json:"person,omitempty"` // v1.6.3
	Date          *Source          `json:"date,omitempty"`
	Drawer        *Source          `json:"drawer,omitempty"`
	Tags          map[int64]Source `json:"tags,omitempty"`
}

// ParseSources reads the stored JSON; anything unreadable is empty.
func ParseSources(s string) FieldSources {
	var fs FieldSources
	_ = json.Unmarshal([]byte(s), &fs)
	return fs
}

func (fs FieldSources) String() string {
	b, _ := json.Marshal(fs)
	return string(b)
}

func (fs FieldSources) clone() FieldSources {
	out := fs
	out.Tags = maps.Clone(fs.Tags)
	return out
}

// UserSet reports whether a person set the field, which classification
// must not change.
func (s *Source) UserSet() bool { return s != nil && s.Source == SourceUser }

var userSource = Source{Source: SourceUser}

// pick is the source to record for a field a patch changed: the patch's
// own (automatic changes) or the user.
func pick(origin *FieldSources, field func(*FieldSources) *Source) *Source {
	if origin == nil {
		s := userSource
		return &s
	}
	if s := field(origin); s != nil {
		c := *s
		return &c
	}
	return nil
}
