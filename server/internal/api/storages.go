package api

import (
	"context"
	"errors"
	"time"

	"kept/server/internal/api/gen"
	"kept/server/internal/apperr"
	"kept/server/internal/i18n"
	"kept/server/internal/storage"
	"kept/server/internal/tenant"
)

// Storages (v1.7.0): where documents' files are kept, for the instance
// administrator.

func (s *Server) AdminListStorages(ctx context.Context, _ gen.AdminListStoragesRequestObject) (gen.AdminListStoragesResponseObject, error) {
	list, err := s.storage.List(ctx)
	if err != nil {
		return nil, err
	}
	out := gen.AdminListStorages200JSONResponse{Items: make([]gen.AdminStorage, len(list))}
	for i, st := range list {
		src := st.Source
		if src == "" {
			src = "missing"
		}
		item := gen.AdminStorage{Name: st.Name, Kind: gen.AdminStorageKind(st.Kind), Source: gen.AdminStorageSource(src), Location: st.Location,
			Description: st.Description, Selectable: st.Selectable, Documents: st.Documents, Workspaces: st.Workspaces, Used: st.Used, Capacity: st.Capacity,
			Encryption: toStorageEncryption(ctx, st.Encryption)}
		if st.Error != "" {
			item.Error = strPtr(i18n.T(ctx, st.Error))
		}
		if set := st.Settings; set != nil {
			ps := set.PathStyle
			item.Settings = &gen.StorageSettings{Path: strPtr(set.Path), Endpoint: strPtr(set.Endpoint), Region: strPtr(set.Region), Bucket: strPtr(set.Bucket),
				Prefix: strPtr(set.Prefix), PathStyle: &ps, AccessKey: strPtr(set.AccessKey), Url: strPtr(set.URL), Username: strPtr(set.Username)}
		}
		out.Items[i] = item
	}
	return out, nil
}

func storageInput(in gen.StorageInput) storage.Input {
	out := storage.Input{Name: deref(in.Name), Kind: string(in.Kind), Description: deref(in.Description), SecretKey: deref(in.SecretKey), Selectable: in.Selectable}
	if in.Capacity != nil {
		out.Capacity = *in.Capacity
	}
	set := in.Settings
	out.Settings = storage.Settings{Path: deref(set.Path), Endpoint: deref(set.Endpoint), Region: deref(set.Region), Bucket: deref(set.Bucket),
		Prefix: deref(set.Prefix), PathStyle: set.PathStyle != nil && *set.PathStyle, AccessKey: deref(set.AccessKey), URL: deref(set.Url), Username: deref(set.Username)}
	return out
}

func (s *Server) AdminCreateStorage(ctx context.Context, req gen.AdminCreateStorageRequestObject) (gen.AdminCreateStorageResponseObject, error) {
	if err := s.requireStorageKind(string(req.Body.Kind)); err != nil {
		return nil, err
	}
	if err := s.storage.Create(ctx, storageInput(*req.Body)); err != nil {
		return nil, err
	}
	return gen.AdminCreateStorage204Response{}, nil
}

func (s *Server) AdminUpdateStorage(ctx context.Context, req gen.AdminUpdateStorageRequestObject) (gen.AdminUpdateStorageResponseObject, error) {
	if err := s.requireStorageKind(string(req.Body.Kind)); err != nil {
		return nil, err
	}
	if err := s.storage.Update(ctx, req.Name, storageInput(*req.Body)); err != nil {
		return nil, err
	}
	return gen.AdminUpdateStorage204Response{}, nil
}

func (s *Server) AdminDeleteStorage(ctx context.Context, req gen.AdminDeleteStorageRequestObject) (gen.AdminDeleteStorageResponseObject, error) {
	if err := s.storage.Delete(ctx, req.Name); err != nil {
		return nil, err
	}
	return gen.AdminDeleteStorage204Response{}, nil
}

// testResult reports a connection test; an invalid form is an error,
// a storage that does not answer is a result.
func testResult(ctx context.Context, took time.Duration, err error) (gen.StorageTestResult, error) {
	out := gen.StorageTestResult{Ok: err == nil, TookMs: took.Milliseconds()}
	if err != nil {
		if ae := (*apperr.Error)(nil); errors.As(err, &ae) {
			return out, err
		}
		out.Error = strPtr(i18n.T(ctx, err.Error()))
	}
	return out, nil
}

func (s *Server) AdminTryStorage(ctx context.Context, req gen.AdminTryStorageRequestObject) (gen.AdminTryStorageResponseObject, error) {
	if err := s.requireStorageKind(string(req.Body.Storage.Kind)); err != nil {
		return nil, err
	}
	took, err := s.storage.TestInput(ctx, deref(req.Body.Existing), storageInput(req.Body.Storage))
	r, err := testResult(ctx, took, err)
	if err != nil {
		return nil, err
	}
	return gen.AdminTryStorage200JSONResponse(r), nil
}

func (s *Server) GetStorageLocation(ctx context.Context, _ gen.GetStorageLocationRequestObject) (gen.GetStorageLocationResponseObject, error) {
	ws := tenant.MustFrom(ctx).WorkspaceID()
	all, err := s.storage.Workspaces(ctx)
	if err != nil {
		return nil, err
	}
	choices, err := s.storage.Choices(ctx)
	if err != nil {
		return nil, err
	}
	out := gen.GetStorageLocation200JSONResponse{Current: toWorkspaceStorage(all[ws]), Choices: make([]gen.StorageChoice, len(choices))}
	if out.Current.Error != nil {
		msg := i18n.T(ctx, *out.Current.Error)
		out.Current.Error = &msg
	}
	// family members' private documents: the owner sees how many, not which (v1.8.0)
	if n, size, err := s.privacy.Usage(ctx, ws); err != nil {
		return nil, err
	} else if n > 0 {
		c := int(n)
		out.Current.PrivateDocuments, out.Current.PrivateBytes = &c, &size
	}
	for i, c := range choices {
		out.Choices[i] = gen.StorageChoice{Name: c.Name, Kind: c.Kind, Description: c.Description}
	}
	return out, nil
}

func (s *Server) SetStorageLocation(ctx context.Context, req gen.SetStorageLocationRequestObject) (gen.SetStorageLocationResponseObject, error) {
	if err := s.requireStorageTarget(ctx, req.Body.Storage); err != nil {
		return nil, err
	}
	id, err := s.storage.SetOwn(ctx, tenant.MustFrom(ctx).WorkspaceID(), req.Body.Storage, req.Body.Move)
	if err != nil {
		return nil, err
	}
	out := gen.SetStorageLocation200JSONResponse{}
	if id != 0 {
		out.JobId = &id
	}
	return out, nil
}

func (s *Server) AdminTestStorage(ctx context.Context, req gen.AdminTestStorageRequestObject) (gen.AdminTestStorageResponseObject, error) {
	took, err := s.storage.Test(ctx, req.Name)
	r, err := testResult(ctx, took, err)
	if err != nil {
		return nil, err
	}
	return gen.AdminTestStorage200JSONResponse(r), nil
}

func (s *Server) AdminSetWorkspaceStorage(ctx context.Context, req gen.AdminSetWorkspaceStorageRequestObject) (gen.AdminSetWorkspaceStorageResponseObject, error) {
	if err := s.requireStorageTarget(ctx, req.Body.Storage); err != nil {
		return nil, err
	}
	id, err := s.storage.SetWorkspace(ctx, req.Id, req.Body.Storage, req.Body.Move)
	if err != nil {
		return nil, err
	}
	out := gen.AdminSetWorkspaceStorage200JSONResponse{}
	if id != 0 {
		out.JobId = &id
	}
	return out, nil
}

func toWorkspaceStorage(w storage.Workspace) gen.WorkspaceStorage {
	out := gen.WorkspaceStorage{Name: w.Storage, Pending: w.Pending, Moving: w.Migrating, Cold: strPtr(w.Cold)}
	if w.Cold != "" {
		out.ColdAfterMonths = &w.ColdAfter
	}
	if w.LastError != "" {
		out.Error = &w.LastError
	}
	if p := w.Progress; p != nil {
		out.Progress = &struct {
			Failed  int `json:"failed"`
			Moved   int `json:"moved"`
			Skipped int `json:"skipped"`
			Total   int `json:"total"`
		}{Failed: p.Failed, Moved: p.Moved, Skipped: p.Skipped, Total: p.Total}
	}
	if d := w.Last; d != nil {
		out.Last = &struct {
			Failed     int       `json:"failed"`
			FinishedAt time.Time `json:"finishedAt"`
			JobId      int64     `json:"jobId"`
			Moved      int       `json:"moved"`
			Skipped    int       `json:"skipped"`
			Total      int       `json:"total"`
		}{Failed: d.Failed, FinishedAt: d.FinishedAt, JobId: d.JobID, Moved: d.Moved.Moved, Skipped: d.Skipped, Total: d.Total}
	}
	return out
}
