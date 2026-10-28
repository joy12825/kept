package classify

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"kept/server/internal/audit"
	"kept/server/internal/document"
	"kept/server/internal/jobs"
	"kept/server/internal/tenant"
)

// KindApply re-applies classification to existing documents.
const KindApply = "classify.apply"

// Suggestion is what classification would set, with the source of each
// value. IDs are 0 (and TagIDs empty) when nothing is suggested.
type Suggestion struct {
	TypeID          int64
	CorrespondentID int64
	PersonID        int64 // v1.6.3
	TagIDs          []int64
	Sources         document.FieldSources
}

// Suggest classifies one text in the workspace.
func (s *Service) Suggest(ctx context.Context, ws int64, title, content string) (Suggestion, error) {
	f, err := s.Suggester(ctx, ws)
	if err != nil {
		return Suggestion{}, err
	}
	return f(title, content), nil
}

// Suggester loads the workspace's rules and model once and returns a
// function that classifies texts with them (design doc 5.6): a matching
// rule decides type and correspondent before the model; tags are the union
// of matching rules and the model.
func (s *Service) Suggester(ctx context.Context, ws int64) (func(title, content string) Suggestion, error) {
	rules, learn, err := s.Rules(ctx, ws)
	if err != nil {
		return nil, err
	}
	model, err := s.Model(ctx, ws)
	if err != nil {
		return nil, err
	}
	return func(title, content string) Suggestion {
		text := Text(title, content)
		sug := Suggestion{Sources: document.FieldSources{Tags: map[int64]document.Source{}}}
		for _, r := range rules {
			m := Test(r.Rule, text)
			if !m.Matched {
				continue
			}
			src := document.Source{Source: document.SourceRule, Basis: m.Basis}
			switch r.Kind {
			case "type":
				if sug.TypeID == 0 {
					sug.TypeID, sug.Sources.Type = r.ID, &src
				}
			case "correspondent":
				if sug.CorrespondentID == 0 {
					sug.CorrespondentID, sug.Sources.Correspondent = r.ID, &src
				}
			case "person":
				if sug.PersonID == 0 {
					sug.PersonID, sug.Sources.Person = r.ID, &src
				}
			case "tag":
				sug.TagIDs = append(sug.TagIDs, r.ID)
				sug.Sources.Tags[r.ID] = src
			}
		}
		if model != nil {
			p := model.Predict(Features(text))
			// Items that stopped learning (or were deleted) since training are ignored.
			if g := p.Type; sug.TypeID == 0 && g != nil && g.P >= TypeThreshold && learn.Types[g.ID] {
				sug.TypeID, sug.Sources.Type = g.ID, &document.Source{Source: document.SourceModel, Confidence: round2(g.P)}
			}
			if g := p.Correspondent; sug.CorrespondentID == 0 && g != nil && g.P >= CorrespondentThreshold && learn.Correspondents[g.ID] {
				sug.CorrespondentID, sug.Sources.Correspondent = g.ID, &document.Source{Source: document.SourceModel, Confidence: round2(g.P)}
			}
			if g := p.Person; sug.PersonID == 0 && g != nil && g.P >= PersonThreshold && learn.Persons[g.ID] {
				sug.PersonID, sug.Sources.Person = g.ID, &document.Source{Source: document.SourceModel, Confidence: round2(g.P)}
			}
			for _, g := range p.Tags {
				if g.P >= TagThreshold && learn.Tags[g.ID] && !slices.Contains(sug.TagIDs, g.ID) {
					sug.TagIDs = append(sug.TagIDs, g.ID)
					sug.Sources.Tags[g.ID] = document.Source{Source: document.SourceModel, Confidence: round2(g.P)}
				}
			}
		}
		slices.Sort(sug.TagIDs)
		return sug
	}, nil
}

func round2(p float64) float64 { return float64(int(p*100+0.5)) / 100 }

// Mode of re-applying classification to existing documents.
const (
	ModeFill      = "fill"      // only empty type/correspondent, only add tags (default)
	ModeOverwrite = "overwrite" // replace what classification set before
)

// decide turns a suggestion into a patch for a document with tags have.
// Fields a person set are never touched. Fill sets empty fields and adds
// tags; overwrite also replaces values (when there is a suggestion) and
// removes tags that classification added and no longer suggests.
func decide(doc document.Document, have []int64, sug Suggestion, mode string) (document.Patch, bool) {
	cur := doc.FieldSources
	overwrite := mode == ModeOverwrite
	p := document.Patch{Origin: &sug.Sources, ExpectedModifiedAt: &doc.ModifiedAt}
	changed := false
	if sug.TypeID != 0 && sug.TypeID != doc.DocumentTypeID && !cur.Type.UserSet() && (doc.DocumentTypeID == 0 || overwrite) {
		p.TypeID, changed = &sug.TypeID, true
	}
	if sug.CorrespondentID != 0 && sug.CorrespondentID != doc.CorrespondentID && !cur.Correspondent.UserSet() && (doc.CorrespondentID == 0 || overwrite) {
		p.CorrespondentID, changed = &sug.CorrespondentID, true
	}
	if sug.PersonID != 0 && sug.PersonID != doc.PersonID && !cur.Person.UserSet() && (doc.PersonID == 0 || overwrite) {
		p.PersonID, changed = &sug.PersonID, true
	}
	tags := slices.Clone(have)
	for _, id := range sug.TagIDs {
		if src, ok := cur.Tags[id]; (!ok || src.Source != document.SourceUser) && !slices.Contains(tags, id) {
			tags = append(tags, id)
		}
	}
	if overwrite {
		tags = slices.DeleteFunc(tags, func(id int64) bool {
			src, ok := cur.Tags[id]
			automatic := ok && src.Automatic()
			return automatic && !slices.Contains(sug.TagIDs, id)
		})
	}
	if len(tags) != len(have) || !slices.Equal(slices.Sorted(slices.Values(tags)), have) {
		p.TagIDs, changed = &tags, true
	}
	return p, changed
}

type applyPayload struct {
	IDs   []int64     `json:"ids"`
	Mode  string      `json:"mode"`
	Actor audit.Actor `json:"actor"`
}

// ApplyResult is the re-apply job's progress and result.
type ApplyResult struct {
	Stage    string `json:"stage"` // classify, done
	Progress int    `json:"progress"`
	Done     int    `json:"done"`
	Total    int    `json:"total"`
	Changed  int    `json:"changed"`
}

// EnqueueApply starts re-applying classification to the given documents
// (already resolved within the scope) and returns the job ID.
func (s *Service) EnqueueApply(ctx context.Context, scope tenant.Scope, ids []int64, mode string) (int64, error) {
	if mode != ModeOverwrite {
		mode = ModeFill
	}
	ws := scope.WorkspaceID()
	return s.opt.Queue.Enqueue(ctx, KindApply, applyPayload{IDs: ids, Mode: mode, Actor: audit.ActorFrom(ctx)},
		jobs.EnqueueOptions{WorkspaceID: &ws, MaxAttempts: 1})
}

// EnqueueTrain queues a forced retraining of the workspace's model.
func (s *Service) EnqueueTrain(ctx context.Context, scope tenant.Scope) (int64, error) {
	ws := scope.WorkspaceID()
	return s.opt.Queue.Enqueue(ctx, KindTrain, map[string]bool{"force": true}, jobs.EnqueueOptions{WorkspaceID: &ws, MaxAttempts: 1, Priority: 1})
}

func (s *Service) handleApply(ctx context.Context, job jobs.Job) (any, error) {
	if job.WorkspaceID == nil {
		return nil, jobs.Permanent(errors.New("classify.apply needs a workspace"))
	}
	var pl applyPayload
	if err := json.Unmarshal(job.Payload, &pl); err != nil {
		return nil, jobs.Permanent(err)
	}
	ctx = audit.WithActor(ctx, pl.Actor)
	scope := tenant.New(*job.WorkspaceID, tenant.RoleEditor)
	suggest, err := s.Suggester(ctx, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	_, learn, err := s.Rules(ctx, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	res := ApplyResult{Stage: "classify", Total: len(pl.IDs)}
	for i, id := range pl.IDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		changed, err := s.reapply(ctx, scope, id, suggest, &learn, pl.Mode)
		if err != nil {
			return nil, err
		}
		if changed {
			res.Changed++
		}
		res.Done = i + 1
		if res.Done%20 == 0 || res.Done == res.Total {
			res.Progress = res.Done * 100 / max(res.Total, 1)
			_ = s.opt.Queue.SetProgress(ctx, job.ID, res)
		}
	}
	res.Stage, res.Progress = "done", 100
	return res, nil
}

// reapply classifies one document outside any transaction and writes the
// outcome unless the document changed meanwhile. With learn, similar
// documents fill what the rules and the model left open.
func (s *Service) reapply(ctx context.Context, scope tenant.Scope, id int64, suggest func(string, string) Suggestion, learn *Learners, mode string) (bool, error) {
	doc, err := s.opt.Documents.Get(ctx, scope, id)
	if errors.Is(err, document.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	have, err := s.q.ListDocumentTagIDs(ctx, id)
	if err != nil {
		return false, err
	}
	sug := suggest(doc.Title, doc.Content)
	if learn != nil {
		if err := s.supplement(ctx, scope, id, *learn, &sug); err != nil {
			return false, err
		}
	}
	p, ok := decide(doc, have, sug, mode)
	if !ok {
		return false, nil
	}
	return s.opt.Documents.ApplyAutomatic(ctx, scope, id, p)
}
