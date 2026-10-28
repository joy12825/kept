package classify

import (
	"bytes"
	"encoding/gob"
	"math"
	"sort"
)

// FormatVersion is stored with every model; a stored model of another
// version is treated as missing (and retrained at the next check).
const FormatVersion = 1

// Model parameters (design doc 5.6).
const (
	minDocFreq = 2     // a word must appear in at least this many documents
	maxTerms   = 50000 // vocabulary size limit

	alpha = 0.01 // smoothing
	// scale and tagScale sharpen the Naive Bayes scores of L2-normalized
	// TF-IDF vectors into usable probabilities (a softmax temperature).
	// These values did best in `kept classifier eval` on the sample data
	// (larger ones made tags over-eager).
	scale    = 4
	tagScale = 2

	TypeThreshold          = 0.6
	CorrespondentThreshold = 0.6
	PersonThreshold        = 0.6
	TagThreshold           = 0.5
)

// Sample is one filed document as the trainer sees it. Type and
// Correspondent are 0 when the document has none (or one that does not
// learn); the model learns that “other” class too, so documents unlike any
// learned item get nothing.
type Sample struct {
	Features      []string
	Type          int64
	Correspondent int64
	Person        int64 // v1.6.3
	Tags          []int64
}

// Model is a trained classifier: Complement Naive Bayes for type and
// correspondent (one class per item, plus 0 for other), and one binary
// Naive Bayes per tag. It is gob-encoded into classifier_models.
type Model struct {
	Terms          []string
	IDF            []float32
	Types          *Multi
	Correspondents *Multi
	Persons        *Multi // v1.6.3; nil in older models
	Tags           []Binary

	index map[string]int32
}

// Multi scores one class per item; W is classes × terms.
type Multi struct {
	Classes []int64
	W       [][]float32
}

// Binary says whether a document has a tag: logit = B + scale·(x·W).
type Binary struct {
	ID int64
	W  []float32
	B  float32
}

// Guess is a predicted item with its probability.
type Guess struct {
	ID int64
	P  float64
}

// Prediction is the model's output before thresholds. A nil Type or
// Correspondent means the model has no opinion (no model for it, or
// “other” won).
type Prediction struct {
	Type          *Guess
	Correspondent *Guess
	Person        *Guess
	Tags          []Guess
}

type entry struct {
	i int32
	v float32
}

// vector is the L2-normalized TF-IDF vector (sublinear term frequency) of
// a document's features.
func (m *Model) vector(features []string) []entry {
	if m.index == nil {
		m.index = make(map[string]int32, len(m.Terms))
		for i, t := range m.Terms {
			m.index[t] = int32(i)
		}
	}
	return tfidf(features, m.index, m.IDF)
}

func tfidf(features []string, index map[string]int32, idf []float32) []entry {
	counts := map[int32]int{}
	for _, f := range features {
		if i, ok := index[f]; ok {
			counts[i]++
		}
	}
	out := make([]entry, 0, len(counts))
	var norm float64
	for i, n := range counts {
		v := (1 + math.Log(float64(n))) * float64(idf[i])
		out = append(out, entry{i, float32(v)})
		norm += v * v
	}
	if norm > 0 {
		k := float32(1 / math.Sqrt(norm))
		for j := range out {
			out[j].v *= k
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].i < out[b].i })
	return out
}

// Predict scores a document's features.
func (m *Model) Predict(features []string) Prediction {
	x := m.vector(features)
	var p Prediction
	if g, ok := m.Types.best(x); ok {
		p.Type = &g
	}
	if g, ok := m.Correspondents.best(x); ok {
		p.Correspondent = &g
	}
	if g, ok := m.Persons.best(x); ok {
		p.Person = &g
	}
	for _, b := range m.Tags {
		s := float64(b.B)
		for _, e := range x {
			s += tagScale * float64(e.v*b.W[e.i])
		}
		p.Tags = append(p.Tags, Guess{ID: b.ID, P: 1 / (1 + math.Exp(-s))})
	}
	sort.SliceStable(p.Tags, func(i, j int) bool { return p.Tags[i].P > p.Tags[j].P })
	return p
}

// best is the most probable class, unless that is “other” (0).
func (mc *Multi) best(x []entry) (Guess, bool) {
	if mc == nil || len(mc.Classes) < 2 {
		return Guess{}, false
	}
	jll := make([]float64, len(mc.Classes))
	top := 0
	for c, w := range mc.W {
		for _, e := range x {
			jll[c] += scale * float64(e.v*w[e.i])
		}
		if jll[c] > jll[top] {
			top = c
		}
	}
	var sum float64
	for _, v := range jll {
		sum += math.Exp(v - jll[top])
	}
	if mc.Classes[top] == 0 {
		return Guess{}, false
	}
	return Guess{ID: mc.Classes[top], P: 1 / sum}, true
}

// trainMulti fits Complement Naive Bayes: each class is scored by how
// unlike its complement (all other classes) the document is.
func trainMulti(xs [][]entry, labels []int64, terms int) *Multi {
	classIndex := map[int64]int{}
	var classes []int64
	for _, l := range labels {
		if _, ok := classIndex[l]; !ok {
			classIndex[l] = len(classes)
			classes = append(classes, l)
		}
	}
	if len(classes) < 2 {
		return nil
	}
	sort.Slice(classes, func(a, b int) bool { return classes[a] < classes[b] })
	for i, c := range classes {
		classIndex[c] = i
	}
	count := make([][]float64, len(classes))
	for c := range count {
		count[c] = make([]float64, terms)
	}
	all := make([]float64, terms)
	for d, x := range xs {
		c := classIndex[labels[d]]
		for _, e := range x {
			count[c][e.i] += float64(e.v)
			all[e.i] += float64(e.v)
		}
	}
	m := &Multi{Classes: classes, W: make([][]float32, len(classes))}
	for c := range classes {
		comp := make([]float64, terms)
		var total float64
		for i := range comp {
			comp[i] = all[i] - count[c][i] + alpha
			total += comp[i]
		}
		w := make([]float32, terms)
		for i := range w {
			w[i] = float32(-math.Log(comp[i] / total))
		}
		m.W[c] = w
	}
	return m
}

// trainBinary fits a two-class Naive Bayes for one tag.
func trainBinary(id int64, xs [][]entry, has []bool, terms int) Binary {
	pos, neg := make([]float64, terms), make([]float64, terms)
	var nPos, nNeg float64
	for d, x := range xs {
		into := neg
		if has[d] {
			into, nPos = pos, nPos+1
		} else {
			nNeg++
		}
		for _, e := range x {
			into[e.i] += float64(e.v)
		}
	}
	var sumPos, sumNeg float64
	for i := 0; i < terms; i++ {
		sumPos += pos[i] + alpha
		sumNeg += neg[i] + alpha
	}
	w := make([]float32, terms)
	for i := range w {
		w[i] = float32(math.Log((pos[i]+alpha)/sumPos) - math.Log((neg[i]+alpha)/sumNeg))
	}
	return Binary{ID: id, W: w, B: float32(math.Log(nPos / nNeg))}
}

// vocabulary keeps the words in at least minDocFreq documents, the
// maxTerms most frequent, with their inverse document frequency.
func vocabulary(docs [][]string) ([]string, []float32) {
	df := map[string]int{}
	for _, d := range docs {
		seen := map[string]bool{}
		for _, f := range d {
			if !seen[f] {
				seen[f] = true
				df[f]++
			}
		}
	}
	var terms []string
	for t, n := range df {
		if n >= minDocFreq {
			terms = append(terms, t)
		}
	}
	sort.Slice(terms, func(a, b int) bool {
		if df[terms[a]] != df[terms[b]] {
			return df[terms[a]] > df[terms[b]]
		}
		return terms[a] < terms[b]
	})
	if len(terms) > maxTerms {
		terms = terms[:maxTerms]
	}
	idf := make([]float32, len(terms))
	n := float64(len(docs))
	for i, t := range terms {
		idf[i] = float32(math.Log((1+n)/(1+float64(df[t]))) + 1)
	}
	return terms, idf
}

// Encode serializes the model for classifier_models.blob.
func (m *Model) Encode() ([]byte, error) {
	var b bytes.Buffer
	err := gob.NewEncoder(&b).Encode(m)
	return b.Bytes(), err
}

// Decode reads a model written by Encode.
func Decode(b []byte) (*Model, error) {
	m := &Model{}
	if err := gob.NewDecoder(bytes.NewReader(b)).Decode(m); err != nil {
		return nil, err
	}
	return m, nil
}
