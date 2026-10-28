package api

import (
	"context"
	"encoding/json"

	"kept/server/internal/api/gen"
	"kept/server/internal/files"
)

// The readable copy in a storage (v1.7.9), for the instance administrator.

func (s *Server) AdminGetExportTarget(ctx context.Context, _ gen.AdminGetExportTargetRequestObject) (gen.AdminGetExportTargetResponseObject, error) {
	t, err := s.storage.GetExportTarget(ctx)
	if err != nil {
		return nil, err
	}
	out := gen.AdminGetExportTarget200JSONResponse{Storage: t.Storage, Folder: t.Folder, Sensitive: t.Sensitive, Dir: strPtr(s.exportDir),
		Choices: []string{}, Runs: []gen.ExportRun{}}
	for _, n := range s.files.Storages() {
		if n != files.Local {
			out.Choices = append(out.Choices, n)
		}
	}
	runs, err := s.storage.LastExports(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range runs {
		run := gen.ExportRun{WorkspaceId: r.WorkspaceID, Workspace: r.Workspace, Status: gen.ExportRunStatus(r.Status), At: r.At, Error: strPtr(r.Error)}
		var res struct {
			Folder                      string `json:"folder"`
			Written, Unchanged, Removed int
			Skipped                     string `json:"skipped"`
		}
		if r.Status == "succeeded" && json.Unmarshal(r.Result, &res) == nil {
			run.Folder, run.Skipped = strPtr(res.Folder), strPtr(res.Skipped)
			if res.Skipped == "" {
				run.Written, run.Unchanged, run.Removed = &res.Written, &res.Unchanged, &res.Removed
			}
		}
		out.Runs = append(out.Runs, run)
	}
	return out, nil
}
