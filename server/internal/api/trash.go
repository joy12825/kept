package api

import (
	"context"

	"kept/server/internal/api/gen"
	"kept/server/internal/tenant"
)

// The trash lives in the current workspace. Anyone who may edit deletes and
// restores; only owners purge (x-kept-min-role: owner).

func (s *Server) ListTrash(ctx context.Context, _ gen.ListTrashRequestObject) (gen.ListTrashResponseObject, error) {
	list, err := s.documents.ListTrash(ctx, tenant.MustFrom(ctx))
	if err != nil {
		return nil, err
	}
	out := make(gen.ListTrash200JSONResponse, len(list))
	for i, d := range list {
		out[i] = gen.TrashedDocument{
			Id: d.ID, Title: d.Title, CorrespondentId: idPtr(d.CorrespondentID), DocumentTypeId: idPtr(d.DocumentTypeID),
			DeletedAt: d.DeletedAt, DaysLeft: d.DaysLeft,
		}
	}
	return out, nil
}

func (s *Server) PurgeDocument(ctx context.Context, req gen.PurgeDocumentRequestObject) (gen.PurgeDocumentResponseObject, error) {
	if err := s.documents.Purge(ctx, tenant.MustFrom(ctx), []int64{req.Id}); err != nil {
		return nil, err
	}
	return gen.PurgeDocument204Response{}, nil
}

func (s *Server) EmptyTrash(ctx context.Context, _ gen.EmptyTrashRequestObject) (gen.EmptyTrashResponseObject, error) {
	n, err := s.documents.EmptyTrash(ctx, tenant.MustFrom(ctx))
	if err != nil {
		return nil, err
	}
	return gen.EmptyTrash200JSONResponse{Count: n}, nil
}
