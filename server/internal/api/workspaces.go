package api

import (
	"context"

	"kept/server/internal/api/gen"
	"kept/server/internal/workspace"
)

// These operations name their workspace in the path; the service checks
// the caller's membership and role there, independent of X-Workspace-ID.

func (s *Server) ListWorkspaces(ctx context.Context, _ gen.ListWorkspacesRequestObject) (gen.ListWorkspacesResponseObject, error) {
	list, err := s.workspaces.List(ctx, mustPrincipal(ctx).UserID)
	if err != nil {
		return nil, err
	}
	out := make(gen.ListWorkspaces200JSONResponse, len(list))
	for i, ws := range list {
		out[i] = toAPIWorkspace(ws)
	}
	return out, nil
}

func (s *Server) CreateWorkspace(ctx context.Context, req gen.CreateWorkspaceRequestObject) (gen.CreateWorkspaceResponseObject, error) {
	color := ""
	if req.Body.Color != nil {
		color = string(*req.Body.Color)
	}
	if req.Body.Kind != "personal" { // v1.12
		if err := s.canAddSharedWorkspace(ctx); err != nil {
			return nil, err
		}
	}
	ws, err := s.workspaces.Create(ctx, mustPrincipal(ctx).UserID, req.Body.Name, string(req.Body.Kind), color)
	if err != nil {
		return nil, err
	}
	return gen.CreateWorkspace201JSONResponse(toAPIWorkspace(ws)), nil
}

func (s *Server) UpdateWorkspace(ctx context.Context, req gen.UpdateWorkspaceRequestObject) (gen.UpdateWorkspaceResponseObject, error) {
	var color *string
	if req.Body.Color != nil {
		c := string(*req.Body.Color)
		color = &c
	}
	ws, err := s.workspaces.Update(ctx, mustPrincipal(ctx).UserID, req.Id, req.Body.Name, color)
	if err != nil {
		return nil, err
	}
	return gen.UpdateWorkspace200JSONResponse(toAPIWorkspace(ws)), nil
}

func (s *Server) DeleteWorkspace(ctx context.Context, req gen.DeleteWorkspaceRequestObject) (gen.DeleteWorkspaceResponseObject, error) {
	if err := s.workspaces.Delete(ctx, mustPrincipal(ctx).UserID, req.Id, req.Body.Name); err != nil {
		return nil, err
	}
	return gen.DeleteWorkspace204Response{}, nil
}

func toAPIWorkspace(ws workspace.Workspace) gen.Workspace {
	return gen.Workspace{
		Id: ws.ID, Name: ws.Name, Kind: gen.WorkspaceKind(ws.Kind), Color: gen.WorkspaceColor(ws.Color),
		Role: gen.WorkspaceRole(ws.Role), MemberCount: ws.MemberCount,
	}
}
