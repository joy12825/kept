package api

import (
	"context"

	"kept/server/internal/api/gen"
	"kept/server/internal/tenant"
	"kept/server/internal/workspace"
)

func (s *Server) ListMembers(ctx context.Context, req gen.ListMembersRequestObject) (gen.ListMembersResponseObject, error) {
	list, err := s.workspaces.ListMembers(ctx, mustPrincipal(ctx).UserID, req.Id)
	if err != nil {
		return nil, err
	}
	out := make(gen.ListMembers200JSONResponse, len(list))
	for i, m := range list {
		out[i] = toAPIMember(m)
	}
	return out, nil
}

func (s *Server) AddMember(ctx context.Context, req gen.AddMemberRequestObject) (gen.AddMemberResponseObject, error) {
	m, err := s.workspaces.AddMember(ctx, mustPrincipal(ctx).UserID, req.Id, req.Body.Login, tenant.Role(req.Body.Role))
	if err != nil {
		return nil, err
	}
	return gen.AddMember201JSONResponse(toAPIMember(m)), nil
}

func (s *Server) UpdateMember(ctx context.Context, req gen.UpdateMemberRequestObject) (gen.UpdateMemberResponseObject, error) {
	m, err := s.workspaces.UpdateMemberRole(ctx, mustPrincipal(ctx).UserID, req.Id, req.UserId, tenant.Role(req.Body.Role))
	if err != nil {
		return nil, err
	}
	return gen.UpdateMember200JSONResponse(toAPIMember(m)), nil
}

func (s *Server) RemoveMember(ctx context.Context, req gen.RemoveMemberRequestObject) (gen.RemoveMemberResponseObject, error) {
	if err := s.workspaces.RemoveMember(ctx, mustPrincipal(ctx).UserID, req.Id, req.UserId); err != nil {
		return nil, err
	}
	return gen.RemoveMember204Response{}, nil
}

func toAPIMember(m workspace.Member) gen.Member {
	out := gen.Member{
		UserId: m.UserID, Username: m.Username, DisplayName: m.DisplayName,
		Role: gen.WorkspaceRole(m.Role), JoinedAt: m.JoinedAt,
	}
	if m.Email != "" {
		out.Email = &m.Email
	}
	return out
}
