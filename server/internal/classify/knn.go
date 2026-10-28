package classify

// Classification by similar documents (v0.5 stage 34): once a document has
// vectors, its nearest filed documents vote on its type, correspondent and
// tags. It only fills what the rules and the learned model left open, for
// items still learned automatically, so it supplements naive Bayes where
// the words differ but the meaning is close.

import (
	"context"
	"fmt"
	"kept/server/internal/i18n"
	"slices"
	"strings"

	"kept/server/internal/audit"
	"kept/server/internal/document"
	"kept/server/internal/tenant"
)

const (
	knnK         = 10
	knnMinScore  = 0.6 // cosine similarity a neighbour needs to vote
	knnMinVotes  = 3   // neighbours that must vote
	knnThreshold = 0.6 // share of the votes the winner needs
)

// NeighborFunc finds a document's nearest documents (ID → similarity).
type NeighborFunc func(ctx context.Context, scope tenant.Scope, docID int64, k int) (map[int64]float64, error)

// SetNeighbors turns classification by similar documents on (the semantic
// index is made after the classifier).
func (s *Service) SetNeighbors(f NeighborFunc) { s.neighbors = f }

type voter struct {
	id                    int64
	score                 float64
	title                 string
	typeID, correspondent int64
	person                int64
	tags                  []int64
}

// voters are the filed neighbours close enough to vote, closest first.
func (s *Service) voters(ctx context.Context, scope tenant.Scope, docID int64) ([]voter, error) {
	if s.neighbors == nil {
		return nil, nil
	}
	near, err := s.neighbors(ctx, scope, docID, knnK)
	if err != nil || len(near) == 0 {
		return nil, nil // no vectors yet: nothing to add
	}
	var ids []any
	var marks []string
	for id, score := range near {
		if score >= knnMinScore {
			ids = append(ids, id)
			marks = append(marks, "?")
		}
	}
	if len(ids) < knnMinVotes {
		return nil, nil
	}
	args := append([]any{scope.WorkspaceID()}, ids...)
	rows, err := s.db.QueryContext(ctx, `SELECT d.id, d.title, coalesce(d.document_type_id, 0), coalesce(d.correspondent_id, 0), coalesce(d.person_id, 0),
		coalesce((SELECT group_concat(tag_id) FROM document_tags t WHERE t.document_id = d.id), '')
		FROM documents d WHERE d.workspace_id = ? AND d.deleted_at IS NULL AND d.inbox = 0 AND d.id IN (`+strings.Join(marks, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []voter
	for rows.Next() {
		var v voter
		var tags string
		if err := rows.Scan(&v.id, &v.title, &v.typeID, &v.correspondent, &v.person, &tags); err != nil {
			return nil, err
		}
		for _, t := range strings.Split(tags, ",") {
			var id int64
			if _, err := fmt.Sscan(t, &id); err == nil {
				v.tags = append(v.tags, id)
			}
		}
		v.score = near[v.id]
		out = append(out, v)
	}
	if err := rows.Err(); err != nil || len(out) < knnMinVotes {
		return nil, err
	}
	slices.SortFunc(out, func(a, b voter) int {
		if a.score != b.score {
			if a.score > b.score {
				return -1
			}
			return 1
		}
		return int(a.id - b.id)
	})
	return out, nil
}

// vote picks the label most of the voters' weight agrees on.
func vote(vs []voter, label func(voter) int64, learn map[int64]bool) (int64, float64, string) {
	weight := map[int64]float64{}
	total := 0.0
	for _, v := range vs {
		weight[label(v)] += v.score
		total += v.score
	}
	var best int64
	for id, w := range weight {
		if id != 0 && (best == 0 || w > weight[best] || (w == weight[best] && id < best)) {
			best = id
		}
	}
	if best == 0 || !learn[best] || weight[best]/total < knnThreshold {
		return 0, 0, ""
	}
	n := 0
	closest := ""
	for _, v := range vs {
		if label(v) == best {
			n++
			if closest == "" {
				closest = v.title
			}
		}
	}
	return best, weight[best] / total, fmt.Sprintf(i18n.N("与“%s”等 %d 份相似文档相同"), clip(closest, 30), n)
}

// supplement fills what sug left open from the document's neighbours.
func (s *Service) supplement(ctx context.Context, scope tenant.Scope, docID int64, learn Learners, sug *Suggestion) error {
	vs, err := s.voters(ctx, scope, docID)
	if err != nil || len(vs) == 0 {
		return err
	}
	if sug.TypeID == 0 {
		if id, p, basis := vote(vs, func(v voter) int64 { return v.typeID }, learn.Types); id != 0 {
			sug.TypeID, sug.Sources.Type = id, &document.Source{Source: document.SourceSimilar, Confidence: round2(p), Basis: basis}
		}
	}
	if sug.CorrespondentID == 0 {
		if id, p, basis := vote(vs, func(v voter) int64 { return v.correspondent }, learn.Correspondents); id != 0 {
			sug.CorrespondentID, sug.Sources.Correspondent = id, &document.Source{Source: document.SourceSimilar, Confidence: round2(p), Basis: basis}
		}
	}
	if sug.PersonID == 0 {
		if id, p, basis := vote(vs, func(v voter) int64 { return v.person }, learn.Persons); id != 0 {
			sug.PersonID, sug.Sources.Person = id, &document.Source{Source: document.SourceSimilar, Confidence: round2(p), Basis: basis}
		}
	}
	total := 0.0
	weight := map[int64]float64{}
	for _, v := range vs {
		total += v.score
		for _, t := range v.tags {
			weight[t] += v.score
		}
	}
	if sug.Sources.Tags == nil {
		sug.Sources.Tags = map[int64]document.Source{}
	}
	for id, w := range weight {
		if w/total >= knnThreshold && learn.Tags[id] && !slices.Contains(sug.TagIDs, id) {
			sug.TagIDs = append(sug.TagIDs, id)
			sug.Sources.Tags[id] = document.Source{Source: document.SourceSimilar, Confidence: round2(w / total)}
		}
	}
	slices.Sort(sug.TagIDs)
	return nil
}

// FillFromNeighbors classifies documents waiting in the inbox that just got
// vectors: rules and the learned model first, then similar documents,
// filling only what is empty (semantic.Options.OnEmbedded).
func (s *Service) FillFromNeighbors(ctx context.Context, ws int64, ids []int64) {
	if s.neighbors == nil || s.opt.Documents == nil {
		return
	}
	scope := tenant.New(ws, tenant.RoleEditor)
	var inbox []int64
	for _, id := range ids {
		var in int
		if err := s.db.QueryRowContext(ctx, `SELECT inbox FROM documents WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL`, ws, id).Scan(&in); err == nil && in == 1 {
			inbox = append(inbox, id)
		}
	}
	if len(inbox) == 0 {
		return
	}
	suggest, err := s.Suggester(ctx, ws)
	if err != nil {
		s.opt.Log.Warn("classify by similar documents", "workspace", ws, "err", err)
		return
	}
	_, learn, err := s.Rules(ctx, ws)
	if err != nil {
		return
	}
	ctx = audit.WithActor(ctx, audit.Actor{Name: i18n.N("自动分类"), Via: audit.ViaSystem})
	for _, id := range inbox {
		if _, err := s.reapply(ctx, scope, id, suggest, &learn, ModeFill); err != nil && ctx.Err() == nil {
			s.opt.Log.Warn("classify by similar documents", "document", id, "err", err)
		}
	}
}
