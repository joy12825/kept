package classify

import "sort"

// Training limits (design doc 5.6).
const (
	MinDocs    = 10 // filed documents a workspace needs before training
	MinSamples = 2  // documents an item needs to be learned
)

// Learners are the items whose matching is “auto”: only they are learned
// and predicted.
type Learners struct {
	Types, Correspondents, Persons, Tags map[int64]bool
}

// Summary describes a training run for the settings page.
type Summary struct {
	DocCount int           `json:"docCount"`
	Skipped  string        `json:"skipped,omitempty"` // why no model was trained
	Items    []ItemSummary `json:"items"`
}

// ItemSummary is one auto item: how many filed documents it has and
// whether that was enough to learn it.
type ItemSummary struct {
	Kind    string `json:"kind"` // type, correspondent, person or tag
	ID      int64  `json:"id"`
	Samples int    `json:"samples"`
	Learned bool   `json:"learned"`
}

// Train fits a model to samples. It returns a nil model, with the reason in
// the summary, when there is too little to learn from.
func Train(samples []Sample, learn Learners) (*Model, Summary) {
	sum := Summary{DocCount: len(samples), Items: []ItemSummary{}}
	counts := map[string]map[int64]int{"type": {}, "correspondent": {}, "person": {}, "tag": {}}
	for _, s := range samples {
		counts["type"][s.Type]++
		counts["correspondent"][s.Correspondent]++
		counts["person"][s.Person]++
		for _, t := range s.Tags {
			counts["tag"][t]++
		}
	}
	learnable := func(kind string, set map[int64]bool) map[int64]bool {
		ok := map[int64]bool{}
		ids := make([]int64, 0, len(set))
		for id := range set {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
		for _, id := range ids {
			n := counts[kind][id]
			enough := n >= MinSamples && len(samples)-n >= MinSamples
			ok[id] = enough
			sum.Items = append(sum.Items, ItemSummary{Kind: kind, ID: id, Samples: n, Learned: enough && len(samples) >= MinDocs})
		}
		return ok
	}
	types := learnable("type", learn.Types)
	corrs := learnable("correspondent", learn.Correspondents)
	persons := learnable("person", learn.Persons)
	tags := learnable("tag", learn.Tags)
	if len(samples) < MinDocs {
		sum.Skipped = "too_few_documents"
		return nil, sum
	}

	docs := make([][]string, len(samples))
	for i, s := range samples {
		docs[i] = s.Features
	}
	m := &Model{}
	m.Terms, m.IDF = vocabulary(docs)
	if len(m.Terms) == 0 {
		sum.Skipped = "no_words"
		return nil, sum
	}
	xs := make([][]entry, len(samples))
	for i, s := range samples {
		xs[i] = m.vector(s.Features)
	}

	// Type, correspondent and person: items that learn are classes; documents with
	// no item or one that does not learn are “other”; documents of an item
	// with too few samples are left out.
	multi := func(label func(Sample) int64, learn map[int64]bool, ok map[int64]bool) *Multi {
		if !anyTrue(ok) {
			return nil
		}
		var xx [][]entry
		var ls []int64
		for i, s := range samples {
			l := label(s)
			switch {
			case ok[l]:
			case learn[l]:
				continue
			default:
				l = 0
			}
			xx = append(xx, xs[i])
			ls = append(ls, l)
		}
		return trainMulti(xx, ls, len(m.Terms))
	}
	m.Types = multi(func(s Sample) int64 { return s.Type }, learn.Types, types)
	m.Correspondents = multi(func(s Sample) int64 { return s.Correspondent }, learn.Correspondents, corrs)
	m.Persons = multi(func(s Sample) int64 { return s.Person }, learn.Persons, persons)
	for _, item := range sum.Items {
		if item.Kind != "tag" || !tags[item.ID] {
			continue
		}
		has := make([]bool, len(samples))
		for i, s := range samples {
			for _, t := range s.Tags {
				if t == item.ID {
					has[i] = true
				}
			}
		}
		m.Tags = append(m.Tags, trainBinary(item.ID, xs, has, len(m.Terms)))
	}
	for i, item := range sum.Items {
		if (item.Kind == "type" && m.Types == nil) || (item.Kind == "correspondent" && m.Correspondents == nil) || (item.Kind == "person" && m.Persons == nil) {
			sum.Items[i].Learned = false
		}
	}
	if m.Types == nil && m.Correspondents == nil && m.Persons == nil && len(m.Tags) == 0 {
		sum.Skipped = "nothing_to_learn"
		for i := range sum.Items {
			sum.Items[i].Learned = false
		}
		return nil, sum
	}
	return m, sum
}

func anyTrue(m map[int64]bool) bool {
	for _, v := range m {
		if v {
			return true
		}
	}
	return false
}
