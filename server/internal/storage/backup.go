package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/files"
)

// Backing up into a storage (v1.7.6): the instance administrator picks one
// storage as the target of the daily backup. It must hold no documents and
// no workspace may write to it: a backup kept where the documents are
// does not survive losing that storage.

const backupSetting = "backup_storage"

// KindBackup is the backup job's kind (backup.KindStorage), read here
// for the latest run.
const KindBackup = "system.backup-storage"

// BackupTarget is the storage backups go to; "" is none.
type BackupTarget struct {
	Storage string `json:"storage"`
	Keep    int    `json:"keep"` // database snapshots kept
}

// DefaultKeep is how many snapshots a target keeps unless set.
const DefaultKeep = 7

// GetBackupTarget reads the setting.
func (s *Service) GetBackupTarget(ctx context.Context) (BackupTarget, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, backupSetting).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return BackupTarget{Keep: DefaultKeep}, nil
	}
	if err != nil {
		return BackupTarget{}, err
	}
	var t BackupTarget
	if err := json.Unmarshal([]byte(raw), &t); err != nil {
		return BackupTarget{}, err
	}
	if t.Keep <= 0 {
		t.Keep = DefaultKeep
	}
	return t, nil
}

// isBackupTarget: name is where backups go.
func (s *Service) isBackupTarget(ctx context.Context, name string) bool {
	t, err := s.GetBackupTarget(ctx)
	return err == nil && t.Storage != "" && t.Storage == name
}

// BackupEligible lists the storages that may be the target: configured,
// not the data directory, holding no documents, written to by no workspace.
func (s *Service) BackupEligible(ctx context.Context) ([]string, error) {
	list, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, st := range list {
		if st.Name == files.Local || st.Kind == "missing" || st.Error != "" || st.Documents > 0 || len(st.Workspaces) > 0 {
			continue
		}
		out = append(out, st.Name)
	}
	return out, nil
}

// BackupRun is the latest backup into the storage.
type BackupRun struct {
	ID     int64
	Status string // queued | running | succeeded | failed
	Error  string
	At     time.Time // finished, or queued while not
	Result json.RawMessage
}

// LastBackup is the latest backup job, nil when there was none.
func (s *Service) LastBackup(ctx context.Context) (*BackupRun, error) {
	var r BackupRun
	var errText, result sql.NullString
	var created int64
	var finished sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id, status, error, result, created_at, finished_at FROM jobs WHERE kind = ? ORDER BY id DESC LIMIT 1`, KindBackup).
		Scan(&r.ID, &r.Status, &errText, &result, &created, &finished)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.Error = errText.String
	if result.Valid {
		r.Result = json.RawMessage(result.String)
	}
	r.At = time.UnixMilli(created)
	if finished.Valid {
		r.At = time.UnixMilli(finished.Int64)
	}
	return &r, nil
}

// errBackupTarget: documents never go to the backup target.
func errBackupTarget(name string) error {
	return apperr.New(apperr.KindInvalid, fmt.Sprintf("存储“%s”是备份目标，不能存放文档；请先在“备份”里换一个备份目标", name))
}

// Versioning of the backup target (v1.7.12): only S3-compatible storages
// have it in a standard way; kept can read and turn it on.
type Versioning struct {
	Supported bool // the target is S3-compatible
	Enabled   bool
	Error     string // reading it failed (keys without the right, …)
}

func (s *Service) backupS3(ctx context.Context) (*files.S3Backend, error) {
	t, err := s.GetBackupTarget(ctx)
	if err != nil || t.Storage == "" {
		return nil, err
	}
	b, err := s.files.Backend(t.Storage)
	if err != nil {
		return nil, nil
	}
	s3, _ := files.Unwrap(b).(*files.S3Backend)
	return s3, nil
}

// BackupVersioning reads the backup target's versioning.
func (s *Service) BackupVersioning(ctx context.Context) (Versioning, error) {
	b, err := s.backupS3(ctx)
	if err != nil || b == nil {
		return Versioning{}, err
	}
	on, err := b.Versioning(ctx)
	if err != nil {
		return Versioning{Supported: true, Error: err.Error()}, nil
	}
	return Versioning{Supported: true, Enabled: on}, nil
}
