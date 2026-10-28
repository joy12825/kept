package api

import (
	"context"
	"slices"

	"kept/server/internal/api/gen"
	"kept/server/internal/quota"
	"kept/server/internal/tenant"
)

// Storage and quotas (v1.0 stage 36).

func toStorage(u quota.Usage, l quota.Limit) gen.StorageUsage {
	return gen.StorageUsage{Originals: u.Originals, Searchable: u.Searchable, Thumbnails: u.Thumbnails, Versions: u.Versions, Trash: u.Trash,
		Total: u.Total, Documents: u.Documents, Quota: l.Bytes, QuotaDefault: l.Default, CountedAt: u.CountedAt}
}

func (s *Server) GetStorage(ctx context.Context, _ gen.GetStorageRequestObject) (gen.GetStorageResponseObject, error) {
	ws := tenant.MustFrom(ctx).WorkspaceID()
	u, err := s.quota.Usage(ctx, ws)
	if err != nil {
		return nil, err
	}
	l, err := s.quota.Limit(ctx, ws)
	if err != nil {
		return nil, err
	}
	out := toStorage(u, l)
	keep, err := s.ingest.KeepVersions(ctx, ws)
	if err != nil {
		return nil, err
	}
	out.KeepVersions = &keep
	return gen.GetStorage200JSONResponse(out), nil
}

func (s *Server) SetKeepVersions(ctx context.Context, req gen.SetKeepVersionsRequestObject) (gen.SetKeepVersionsResponseObject, error) {
	if err := s.ingest.SetKeepVersions(ctx, tenant.MustFrom(ctx), req.Body.Keep); err != nil {
		return nil, err
	}
	return gen.SetKeepVersions204Response{}, nil
}

func (s *Server) AdminListWorkspaces(ctx context.Context, _ gen.AdminListWorkspacesRequestObject) (gen.AdminListWorkspacesResponseObject, error) {
	rows, err := s.quota.Workspaces(ctx)
	if err != nil {
		return nil, err
	}
	stores, err := s.storage.Workspaces(ctx)
	if err != nil {
		return nil, err
	}
	out := gen.AdminListWorkspaces200JSONResponse{Items: make([]gen.AdminWorkspace, len(rows)), DefaultQuota: s.quota.DefaultBytes()}
	for i, r := range rows {
		out.Items[i] = gen.AdminWorkspace{Id: r.ID, Name: r.Name, Kind: r.Kind, Color: r.Color, Members: r.Members, Owners: r.Owners, Usage: toStorage(r.Usage, r.Limit),
			Storage: toWorkspaceStorage(stores[r.ID])}
	}
	return out, nil
}

// Member quotas (v1.7.11), for the workspace administrator.

func (s *Server) ListMemberUploads(ctx context.Context, _ gen.ListMemberUploadsRequestObject) (gen.ListMemberUploadsResponseObject, error) {
	all, err := s.quota.Members(ctx, tenant.MustFrom(ctx).WorkspaceID())
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(all))
	for id := range all {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	out := make(gen.ListMemberUploads200JSONResponse, len(ids))
	for i, id := range ids {
		out[i].UserId, out[i].Uploaded, out[i].Quota = id, all[id].Used, all[id].Limit
	}
	return out, nil
}
