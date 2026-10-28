package classify

import (
	"context"
	"math/rand/v2"
	"slices"
)

// Score counts one item's results in an evaluation.
type Score struct {
	Kind    string `json:"kind"`
	ID      int64  `json:"id"`
	Samples int    `json:"samples"` // held-out documents that have the item
	Found   int    `json:"found"`   // of those, how many the model gave it
	Wrong   int    `json:"wrong"`   // documents the model gave it that do not have it
}

// Report is the outcome of Eval.
type Report struct {
	Documents int     `json:"documents"`
	Folds     int     `json:"folds"`
	Skipped   string  `json:"skipped,omitempty"`
	Scores    []Score `json:"scores"`
	// Accuracy of type and correspondent over all held-out documents:
	// right item, or rightly nothing for a document whose item does not learn.
	TypeAccuracy          float64 `json:"typeAccuracy"`
	CorrespondentAccuracy float64 `json:"correspondentAccuracy"`
}

// Eval cross-validates the workspace's classifier with k folds (design doc
// 5.6, `kept classifier eval`): it trains on k−1 parts of the filed
// documents and predicts the rest, with the same thresholds as ingestion.
func (s *Service) Eval(ctx context.Context, ws int64, k int) (Report, error) {
	_, learn, err := s.Rules(ctx, ws)
	if err != nil {
		return Report{}, err
	}
	samples, _, err := s.samples(ctx, ws, learn)
	if err != nil {
		return Report{}, err
	}
	return evaluate(samples, learn, k), nil
}

func evaluate(samples []Sample, learn Learners, k int) Report {
	k = max(2, min(k, len(samples)))
	rep := Report{Documents: len(samples), Folds: k}
	order := rand.New(rand.NewPCG(1, 2)).Perm(len(samples))
	scores := map[[2]any]*Score{}
	score := func(kind string, id int64) *Score {
		key := [2]any{kind, id}
		if scores[key] == nil {
			scores[key] = &Score{Kind: kind, ID: id}
		}
		return scores[key]
	}
	var typeRight, corrRight, total int
	for f := range k {
		var train, test []Sample
		for i, j := range order {
			if i%k == f {
				test = append(test, samples[j])
			} else {
				train = append(train, samples[j])
			}
		}
		m, sum := Train(train, learn)
		if m == nil {
			rep.Skipped = sum.Skipped
			return rep
		}
		for _, s := range test {
			p := m.Predict(s.Features)
			total++
			check := func(kind string, truth int64, g *Guess, threshold float64, learns map[int64]bool) bool {
				var got int64
				if g != nil && g.P >= threshold {
					got = g.ID
				}
				if !learns[truth] {
					truth = 0
				}
				if truth != 0 {
					sc := score(kind, truth)
					sc.Samples++
					if got == truth {
						sc.Found++
					}
				}
				if got != 0 && got != truth {
					score(kind, got).Wrong++
				}
				return got == truth
			}
			if check("type", s.Type, p.Type, TypeThreshold, learn.Types) {
				typeRight++
			}
			if check("correspondent", s.Correspondent, p.Correspondent, CorrespondentThreshold, learn.Correspondents) {
				corrRight++
			}
			for _, g := range p.Tags {
				has := slices.Contains(s.Tags, g.ID)
				if has {
					score("tag", g.ID).Samples++
				}
				if g.P >= TagThreshold {
					if has {
						score("tag", g.ID).Found++
					} else {
						score("tag", g.ID).Wrong++
					}
				}
			}
		}
	}
	if total > 0 {
		rep.TypeAccuracy = float64(typeRight) / float64(total)
		rep.CorrespondentAccuracy = float64(corrRight) / float64(total)
	}
	for _, sc := range scores {
		rep.Scores = append(rep.Scores, *sc)
	}
	slices.SortFunc(rep.Scores, func(a, b Score) int {
		if a.Kind != b.Kind {
			return map[string]int{"type": 0, "correspondent": 1, "tag": 2}[a.Kind] - map[string]int{"type": 0, "correspondent": 1, "tag": 2}[b.Kind]
		}
		return int(a.ID - b.ID)
	})
	return rep
}
