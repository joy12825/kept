package api

import (
	"context"
	"encoding/json"
	"fmt"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"kept/server/internal/i18n"
	"strconv"
	"time"
	"unicode/utf8"

	"kept/server/internal/api/gen"
	"kept/server/internal/classify"
	"kept/server/internal/document"
	"kept/server/internal/taxonomy"
	"kept/server/internal/tenant"
)

func (s *Server) GetClassifier(ctx context.Context, _ gen.GetClassifierRequestObject) (gen.GetClassifierResponseObject, error) {
	scope := tenant.MustFrom(ctx)
	st, err := s.classifier.Status(ctx, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	out := gen.ClassifierStatus{
		TrainedAt: st.TrainedAt, DocumentCount: st.Summary.DocCount, MinDocuments: classify.MinDocs,
		Items: make([]gen.ClassifierItem, len(st.Summary.Items)), Training: st.Training,
	}
	if st.Summary.Skipped != "" {
		sk := gen.ClassifierStatusSkipped(st.Summary.Skipped)
		out.Skipped = &sk
	}
	for i, it := range st.Summary.Items {
		out.Items[i] = gen.ClassifierItem{Kind: gen.ClassifierItemKind(it.Kind), Id: it.ID, Samples: it.Samples, Learned: it.Learned}
	}
	rows, err := s.jobs.ListRecent(ctx, scope, classify.KindApply, time.Now().Add(-24*time.Hour), 1)
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		r := rows[0]
		var pl struct{ IDs []int64 }
		_ = json.Unmarshal([]byte(r.Payload), &pl)
		var res classify.ApplyResult
		if r.Result.Valid {
			_ = json.Unmarshal([]byte(r.Result.String), &res)
		}
		out.Applying = &gen.ClassifyJob{JobId: r.ID, Status: gen.ClassifyJobStatus(r.Status), Done: res.Done, Total: len(pl.IDs), Changed: res.Changed}
	}
	return gen.GetClassifier200JSONResponse(out), nil
}

func (s *Server) TrainClassifier(ctx context.Context, _ gen.TrainClassifierRequestObject) (gen.TrainClassifierResponseObject, error) {
	id, err := s.classifier.EnqueueTrain(ctx, tenant.MustFrom(ctx))
	if err != nil {
		return nil, err
	}
	return gen.TrainClassifier202JSONResponse{JobId: id}, nil
}

func (s *Server) TestMatchRule(ctx context.Context, req gen.TestMatchRuleRequestObject) (gen.TestMatchRuleResponseObject, error) {
	b := req.Body
	if utf8.RuneCountInString(b.Text) > classify.MaxText {
		return nil, invalid(fmt.Sprintf("文字最多 %d 个字", classify.MaxText))
	}
	rule, err := taxonomy.ValidateRule(taxonomy.MatchRule{Algorithm: string(b.Rule.Algorithm), Match: b.Rule.Match, CaseInsensitive: b.Rule.CaseInsensitive})
	if err != nil {
		return nil, err
	}
	m := classify.Test(rule, b.Text)
	out := gen.MatchTestResult{Matched: m.Matched}
	if m.Matched {
		basis := i18n.T(ctx, m.Basis)
		out.Basis, out.Hits = &basis, &m.Hits
	}
	return gen.TestMatchRule200JSONResponse(out), nil
}

// toAPISources translates the bases (分类依据) to the request's language
// (v1.10.1); they are stored in Chinese.
func toAPISources(ctx context.Context, fs document.FieldSources) gen.FieldSources {
	one := func(s *document.Source) *gen.FieldSource {
		if s == nil {
			return nil
		}
		return ptrSource(ctx, *s)
	}
	out := gen.FieldSources{Type: one(fs.Type), Correspondent: one(fs.Correspondent), Person: one(fs.Person), Date: one(fs.Date), Drawer: one(fs.Drawer)}
	if len(fs.Tags) > 0 {
		tags := make(map[string]gen.FieldSource, len(fs.Tags))
		for id, s := range fs.Tags {
			tags[strconv.FormatInt(id, 10)] = *ptrSource(ctx, s)
		}
		out.Tags = &tags
	}
	return out
}

func ptrSource(ctx context.Context, s document.Source) *gen.FieldSource {
	out := &gen.FieldSource{Source: gen.FieldSourceSource(s.Source)}
	if s.Basis != "" {
		b := i18n.T(ctx, s.Basis)
		out.Basis = &b
	}
	if s.Confidence > 0 {
		c := float32(s.Confidence)
		out.Confidence = &c
	}
	if len(s.Alternatives) > 0 {
		alts := make([]struct {
			Basis string             `json:"basis"`
			Date  openapi_types.Date `json:"date"`
		}, 0, len(s.Alternatives))
		for _, a := range s.Alternatives {
			var d openapi_types.Date
			if d.UnmarshalText([]byte(a.Date)) == nil {
				alts = append(alts, struct {
					Basis string             `json:"basis"`
					Date  openapi_types.Date `json:"date"`
				}{i18n.T(ctx, a.Basis), d})
			}
		}
		out.Alternatives = &alts
	}
	return out
}
