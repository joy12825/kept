package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// Retrying a failed upload (v1.9.12). The received file of an upload whose
// processing failed is kept in its working directory for FailedRetention,
// so the web can process it again without choosing the file again (the
// phone keeps its own copy, v1.9.9). A duplicate is not kept: processing it
// again ends the same way.

// FailedRetention is how long a failed upload's file is kept.
const FailedRetention = 7 * 24 * time.Hour

var (
	errNotFailed     = apperr.WithCode(apperr.KindConflict, "not_failed", "只有失败的上传可以重试")
	errNotRetryable  = apperr.WithCode(apperr.KindConflict, "not_retryable", "这份文件已经存在，不用重试")
	errUploadExpired = apperr.WithCode(apperr.KindConflict, "upload_expired", "文件已经清理（失败的上传保留 7 天），请重新上传")
)

// KeptForRetry reports, from a failed upload's stored error, whether its
// file was kept for a retry (not a duplicate).
func KeptForRetry(stored string) bool {
	for _, e := range []string{errDuplicate.Message, errDuplicateInTrash.Message, errUploadGone.Message} {
		if strings.HasPrefix(stored, e) {
			return false
		}
	}
	return true
}

// keepFailed reports whether a failed upload's file is kept for a retry.
func keepFailed(err error) bool {
	return err != nil && !errors.Is(err, errDuplicate) && !errors.Is(err, errDuplicateInTrash) && !errors.Is(err, errUploadGone)
}

// MayRetry reports whether the viewer may process a failed upload again:
// the person who uploaded it; for one nobody uploaded in person (an upload
// link, a mailbox, the consume directory), the workspace's owners.
func MayRetry(scope tenant.Scope, createdBy sql.NullInt64) bool {
	if createdBy.Valid {
		return createdBy.Int64 == scope.Viewer()
	}
	return scope.Role() == tenant.RoleOwner
}

// Retryable reports whether a job is a failed upload whose file is still
// kept. It does not check who asks (MayRetry).
func (s *Service) Retryable(kind, status, payloadJSON string, finishedAt sql.NullInt64, now time.Time) bool {
	if kind != KindDocument || status != "failed" || !finishedAt.Valid || now.Sub(time.UnixMilli(finishedAt.Int64)) > FailedRetention {
		return false
	}
	_, err := os.Stat(filepath.Join(s.workDir(payloadJSON), "original"))
	return err == nil
}

// workDir is a job's working directory ("" when its payload has none).
func (s *Service) workDir(payloadJSON string) string {
	var pl payload
	if json.Unmarshal([]byte(payloadJSON), &pl) != nil || pl.Dir == "" {
		return ""
	}
	return filepath.Join(s.files.TmpRoot(), filepath.Base(pl.Dir))
}

// Retry puts a failed upload back in the queue: the same job, the same file
// and presets.
func (s *Service) Retry(ctx context.Context, scope tenant.Scope, jobID int64) error {
	j, err := s.q.GetJob(ctx, jobID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (!j.WorkspaceID.Valid || j.WorkspaceID.Int64 != scope.WorkspaceID() || j.Kind != KindDocument) {
		return apperr.New(apperr.KindNotFound, "没有这个上传")
	}
	if err != nil {
		return err
	}
	if !MayRetry(scope, j.CreatedBy) { // not even told it exists: it may have been going to be private
		return apperr.New(apperr.KindNotFound, "没有这个上传")
	}
	if j.Status != "failed" {
		return errNotFailed
	}
	if !KeptForRetry(j.Error.String) {
		return errNotRetryable
	}
	if !s.Retryable(j.Kind, j.Status, j.Payload, j.FinishedAt, time.Now()) {
		return errUploadExpired
	}
	n, err := s.q.RequeueFailedJob(ctx, sqlc.RequeueFailedJobParams{ID: jobID, Now: time.Now().UnixMilli()})
	if err != nil {
		return err
	}
	if n == 0 { // someone else retried it at the same moment
		return errNotFailed
	}
	s.queue.Wake()
	s.publishJob(scope.WorkspaceID(), jobID, "queued", Progress{Stage: "queued"}, "")
	return nil
}

// PurgeFailed removes the files of uploads that failed more than
// FailedRetention ago; it returns how many it removed.
func (s *Service) PurgeFailed(ctx context.Context, now time.Time) (int64, error) {
	payloads, err := s.q.ListFailedPayloadsBefore(ctx, sqlc.ListFailedPayloadsBeforeParams{Kind: KindDocument, Before: sql.NullInt64{Int64: now.Add(-FailedRetention).UnixMilli(), Valid: true}})
	if err != nil {
		return 0, err
	}
	var n int64
	for _, p := range payloads {
		dir := s.workDir(p)
		if dir == "" {
			continue
		}
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
