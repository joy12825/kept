package api

import (
	"context"
	"strconv"

	"kept/server/internal/api/gen"
	"kept/server/internal/auth"
	"kept/server/internal/ingest"
	"kept/server/internal/pages"
	"kept/server/internal/tenant"
)

func (s *Server) SetDocumentPages(ctx context.Context, req gen.SetDocumentPagesRequestObject) (gen.SetDocumentPagesResponseObject, error) {
	scope := tenant.MustFrom(ctx)
	e := pages.Edits{Rotate: map[int]int{}}
	if req.Body.Rotate != nil {
		for k, deg := range *req.Body.Rotate {
			p, err := strconv.Atoi(k)
			if err != nil {
				return nil, invalid("页码不正确")
			}
			e.Rotate[p] = deg
		}
	}
	if req.Body.Delete != nil {
		e.Delete = *req.Body.Delete
	}
	if req.Body.Order != nil {
		e.Order = *req.Body.Order
	}
	if err := s.pages.Set(ctx, scope, req.Id, e); err != nil {
		return nil, err
	}
	d, err := s.documents.GetDetail(ctx, scope, req.Id)
	if err != nil {
		return nil, err
	}
	return gen.SetDocumentPages200JSONResponse(s.lockSensitive(ctx, s.withRuleDrawer(ctx, s.toAPIDocument(ctx, d)))), nil
}

func (s *Server) SplitDocument(ctx context.Context, req gen.SplitDocumentRequestObject) (gen.SplitDocumentResponseObject, error) {
	trash := req.Body.TrashOriginal != nil && *req.Body.TrashOriginal
	id, err := s.pages.Split(ctx, tenant.MustFrom(ctx), req.Id, req.Body.After, trash, uploadSource(ctx))
	if err != nil {
		return nil, err
	}
	return gen.SplitDocument202JSONResponse{JobId: id}, nil
}

func (s *Server) MergeDocuments(ctx context.Context, req gen.MergeDocumentsRequestObject) (gen.MergeDocumentsResponseObject, error) {
	trash := req.Body.TrashOriginals != nil && *req.Body.TrashOriginals
	id, err := s.pages.Merge(ctx, tenant.MustFrom(ctx), req.Body.Ids, trash, uploadSource(ctx))
	if err != nil {
		return nil, err
	}
	return gen.MergeDocuments202JSONResponse{JobId: id}, nil
}

// uploadSource is the document source for what the caller adds.
func uploadSource(ctx context.Context) string {
	switch mustPrincipal(ctx).Via {
	case auth.ViaToken:
		return ingest.SourceAPI
	case auth.ViaDevice:
		return ingest.SourceMobile
	}
	return ingest.SourceWeb
}
