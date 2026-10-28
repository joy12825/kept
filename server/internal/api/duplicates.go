package api

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"kept/server/internal/api/gen"
	"kept/server/internal/dedupe"
	"kept/server/internal/tenant"
)

// snippet is the start of a document's text, spaces folded.
func snippet(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > 160 {
		s = string([]rune(s)[:160]) + "…"
	}
	return s
}

func (s *Server) ListDuplicates(ctx context.Context, req gen.ListDuplicatesRequestObject) (gen.ListDuplicatesResponseObject, error) {
	scope := tenant.MustFrom(ctx)
	var doc int64
	if req.Params.Document != nil {
		doc = *req.Params.Document
	}
	pairs, err := s.dedupe.ListFor(ctx, scope, doc)
	if err != nil {
		return nil, err
	}
	out := gen.ListDuplicates200JSONResponse{Pairs: []gen.DuplicatePair{}}
	for _, p := range pairs {
		out.Pairs = append(out.Pairs, gen.DuplicatePair{Id: p.ID, Similarity: p.Similarity, A: toAPISummary(p.A.Summary), B: toAPISummary(p.B.Summary),
			ASnippet: snippet(p.A.Content), BSnippet: snippet(p.B.Content), FoundAt: p.FoundAt})
	}
	jobs, err := s.jobs.ListRecent(ctx, scope, dedupe.Kind, time.Now().AddDate(0, 0, -60), 20)
	if err != nil {
		return nil, err
	}
	for _, j := range jobs {
		switch {
		case j.Status == "queued" || j.Status == "running":
			out.Scanning = true
		case j.Status == "succeeded" && j.FinishedAt.Valid && out.LastScanAt == nil:
			t := time.UnixMilli(j.FinishedAt.Int64)
			out.LastScanAt = &t
		}
	}
	return out, nil
}

func (s *Server) ScanDuplicates(ctx context.Context, _ gen.ScanDuplicatesRequestObject) (gen.ScanDuplicatesResponseObject, error) {
	id, err := s.dedupe.QueueScan(ctx, tenant.MustFrom(ctx))
	if err != nil {
		return nil, err
	}
	return gen.ScanDuplicates202JSONResponse{JobId: id}, nil
}

func (s *Server) KeepDuplicate(ctx context.Context, req gen.KeepDuplicateRequestObject) (gen.KeepDuplicateResponseObject, error) {
	if err := s.dedupe.Keep(ctx, tenant.MustFrom(ctx), req.Id, req.Body.DocumentId); err != nil {
		return nil, err
	}
	return gen.KeepDuplicate204Response{}, nil
}

func (s *Server) DismissDuplicate(ctx context.Context, req gen.DismissDuplicateRequestObject) (gen.DismissDuplicateResponseObject, error) {
	if err := s.dedupe.Dismiss(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	return gen.DismissDuplicate204Response{}, nil
}
