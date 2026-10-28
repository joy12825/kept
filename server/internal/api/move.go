package api

import (
	"context"

	"kept/server/internal/api/gen"
	"kept/server/internal/i18n"
	"kept/server/internal/tenant"
)

// MoveDocuments moves documents to another workspace the caller can write
// in (v1.3.8), most often their personal space.
func (s *Server) MoveDocuments(ctx context.Context, req gen.MoveDocumentsRequestObject) (gen.MoveDocumentsResponseObject, error) {
	scope := tenant.MustFrom(ctx)
	user := mustPrincipal(ctx).UserID
	target, err := s.transfer.CheckTarget(ctx, scope, user, req.Body.WorkspaceId)
	if err != nil {
		return nil, err
	}
	results, err := s.transfer.Move(ctx, scope, user, req.Body.Ids, target)
	if err != nil {
		return nil, err
	}
	out := gen.MoveDocuments200JSONResponse{Moved: len(results), WorkspaceId: target.ID, WorkspaceName: target.Name, AsnCleared: []int64{}, Created: []string{}}
	seen := map[string]bool{}
	for _, r := range results {
		if r.ASNCleared {
			out.AsnCleared = append(out.AsnCleared, r.DocumentID)
		}
		for _, c := range r.Created {
			if !seen[c] {
				seen[c] = true
				out.Created = append(out.Created, i18n.T(ctx, c))
			}
		}
	}
	return out, nil
}
