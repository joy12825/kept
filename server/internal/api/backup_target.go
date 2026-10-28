package api

import (
	"context"
	"encoding/json"
	"net/http"

	"kept/server/internal/api/gen"
	"kept/server/internal/backup"
	"kept/server/internal/i18n"
	"kept/server/internal/storage"
)

// Backing up into a storage (v1.7.6), for the instance administrator.

func (s *Server) AdminGetBackupTarget(ctx context.Context, _ gen.AdminGetBackupTargetRequestObject) (gen.AdminGetBackupTargetResponseObject, error) {
	t, err := s.storage.GetBackupTarget(ctx)
	if err != nil {
		return nil, err
	}
	eligible, err := s.storage.BackupEligible(ctx)
	if err != nil {
		return nil, err
	}
	if eligible == nil {
		eligible = []string{}
	}
	out := gen.AdminGetBackupTarget200JSONResponse{Storage: t.Storage, Keep: t.Keep, Eligible: eligible, Dir: strPtr(s.backupDir.Get(ctx))}
	if s.backupDir.FromEnv() {
		out.DirFromEnv = &[]bool{true}[0]
	}
	if t.Storage != "" {
		v, err := s.storage.BackupVersioning(ctx)
		if err != nil {
			return nil, err
		}
		out.Versioning = &struct {
			Enabled   bool    `json:"enabled"`
			Error     *string `json:"error,omitempty"`
			Supported bool    `json:"supported"`
		}{Enabled: v.Enabled, Error: strPtr(i18n.T(ctx, v.Error)), Supported: v.Supported}
	}
	last, err := s.storage.LastBackup(ctx)
	if err != nil {
		return nil, err
	}
	if last != nil {
		out.Last = backupRun(ctx, last)
	}
	return out, nil
}

func backupRun(ctx context.Context, r *storage.BackupRun) *gen.BackupRun {
	out := &gen.BackupRun{At: r.At, Status: gen.BackupRunStatus(r.Status), Error: strPtr(i18n.T(ctx, r.Error)), JobId: &r.ID}
	if r.Status != "succeeded" || len(r.Result) == 0 {
		return out
	}
	var res struct {
		backup.Result
		Skipped string `json:"skipped"`
	}
	if json.Unmarshal(r.Result, &res) != nil {
		return out
	}
	if res.Skipped != "" {
		skipped := i18n.T(ctx, res.Skipped)
		out.Skipped = &skipped
		return out
	}
	missing := len(res.Missing)
	out.Snapshot, out.Documents, out.Files, out.Copied, out.Bytes = strPtr(res.Snapshot), &res.Documents, &res.Files, &res.Copied, &res.Bytes
	out.Missing, out.External, out.Pruned = &missing, &res.External, &res.Pruned
	return out
}

var errNoBackupTarget = &apiError{http.StatusConflict, "backup_target_not_set", "还没有设置备份目标"}
