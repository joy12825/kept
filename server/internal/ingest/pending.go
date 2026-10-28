package ingest

import (
	"context"
	"database/sql"
	"time"

	"kept/server/internal/audit"
	"kept/server/internal/jobs"
)

// Conversions that wait (v1.9.1): an Office document or an e-mail taken
// without Gotenberg is kept with the text read from it, and noted in
// pending_conversions. Once Gotenberg is set up, the scheduled job below
// reprocesses a batch every hour; a reprocess that produces the PDF
// removes the note.

// KindConvertPending is the scheduled job that queues them.
const KindConvertPending = "ingest.convert-pending"

// convertBatch bounds the batch queued at a time, and requeueAfter is how
// long a queued document waits before it is tried again (its reprocess
// failed, or the server stopped).
const (
	convertBatch = 20
	requeueAfter = 24 * time.Hour
)

// notePending records or clears what a document waits for, in tx.
func notePending(ctx context.Context, tx *sql.Tx, ws, id int64, pending string, now time.Time) error {
	if pending == "" {
		_, err := tx.ExecContext(ctx, `DELETE FROM pending_conversions WHERE document_id = ?`, id)
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO pending_conversions (document_id, workspace_id, kind, added_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (document_id) DO NOTHING`, id, ws, pending, now.UnixMilli())
	return err
}

// CanConvert reports whether Office conversion is set up.
func (s *Service) CanConvert() bool { return s.opt.Parser.GotenbergURL != "" }

// ConversionPending reports whether a document waits for its PDF.
func (s *Service) ConversionPending(ctx context.Context, ws, id int64) bool {
	var yes bool
	_ = s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pending_conversions WHERE workspace_id = ? AND document_id = ?)`, ws, id).Scan(&yes)
	return yes
}

// handleConvertPending queues the reprocessing of a workspace's waiting
// documents, a batch at a time.
func (s *Service) handleConvertPending(ctx context.Context, job jobs.Job) (any, error) {
	if !s.CanConvert() || job.WorkspaceID == nil {
		return map[string]int{"queued": 0}, nil
	}
	ws := *job.WorkspaceID
	now := s.opt.Now()
	rows, err := s.db.QueryContext(ctx, `SELECT p.document_id FROM pending_conversions AS p
		JOIN documents AS d ON d.id = p.document_id AND d.deleted_at IS NULL
		WHERE p.workspace_id = ? AND (p.queued_at IS NULL OR p.queued_at < ?)
		ORDER BY p.added_at, p.document_id LIMIT ?`, ws, now.Add(-requeueAfter).UnixMilli(), convertBatch)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	actor := audit.ActorFrom(context.Background()) // the system
	for _, id := range ids {
		jid, err := s.queue.Enqueue(ctx, KindReprocess, payload{DocumentID: id, Actor: actor}, jobs.EnqueueOptions{WorkspaceID: &ws})
		if err != nil {
			return nil, err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE pending_conversions SET queued_at = ? WHERE document_id = ?`, now.UnixMilli(), id); err != nil {
			return nil, err
		}
		s.publishJob(ws, jid, "queued", Progress{Stage: "queued"}, "")
	}
	return map[string]int{"queued": len(ids)}, nil
}
