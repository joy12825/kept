package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// A readable copy (v1.7.9): every day each workspace is exported into a
// folder of a storage, as the export to a directory does: folders by
// drawer and year, files named by date and title, a list beside them.
// Unlike a backup it can be read without kept; it is not restored from.

const exportSetting = "export_storage"

// KindExport is the export job's kind (export.KindStorage).
const KindExport = "export.storage"

// ExportTarget is where the copy goes; Storage "" is none.
type ExportTarget struct {
	Storage   string `json:"storage"`
	Folder    string `json:"folder"`
	Sensitive bool   `json:"sensitive"` // include sensitive documents
}

// GetExportTarget reads the setting.
func (s *Service) GetExportTarget(ctx context.Context) (ExportTarget, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, exportSetting).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ExportTarget{Folder: "收好导出"}, nil
	}
	if err != nil {
		return ExportTarget{}, err
	}
	var t ExportTarget
	return t, json.Unmarshal([]byte(raw), &t)
}

// ExportRun is the latest export of one workspace.
type ExportRun struct {
	WorkspaceID int64
	Workspace   string
	Status      string
	Error       string
	At          time.Time
	Result      json.RawMessage
}

// LastExports lists each workspace's latest export into the storage.
func (s *Service) LastExports(ctx context.Context) ([]ExportRun, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT j.workspace_id, w.name, j.status, j.error, j.result, j.created_at, j.finished_at
		FROM jobs j JOIN workspaces w ON w.id = j.workspace_id
		WHERE j.kind = ? AND w.deleted_at IS NULL AND j.id = (SELECT max(k.id) FROM jobs k WHERE k.kind = j.kind AND k.workspace_id = j.workspace_id)
		ORDER BY w.id`, KindExport)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExportRun
	for rows.Next() {
		var r ExportRun
		var errText, result sql.NullString
		var created int64
		var finished sql.NullInt64
		if err := rows.Scan(&r.WorkspaceID, &r.Workspace, &r.Status, &errText, &result, &created, &finished); err != nil {
			return nil, err
		}
		r.Error, r.At = errText.String, time.UnixMilli(created)
		if finished.Valid {
			r.At = time.UnixMilli(finished.Int64)
		}
		if result.Valid {
			r.Result = json.RawMessage(result.String)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
