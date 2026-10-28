-- name: GetBookSummary :one
SELECT * FROM book_summaries WHERE workspace_id = ? AND document_id = ?;

-- name: UpsertBookSummary :exec
INSERT INTO book_summaries (document_id, workspace_id, summary, chapters, basis, run_id, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (document_id) DO UPDATE SET summary = excluded.summary, chapters = excluded.chapters,
    basis = excluded.basis, run_id = excluded.run_id, created_at = excluded.created_at;

-- name: DeleteBookSummary :execrows
DELETE FROM book_summaries WHERE workspace_id = ? AND document_id = ?;

-- name: InsertReviewCard :one
INSERT INTO review_cards (user_id, workspace_id, document_id, highlight_id, front, back, due_on, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id;

-- name: ListReviewCards :many
SELECT c.*, d.title AS document_title, coalesce(h.quote, '') AS quote
FROM review_cards c
JOIN documents d ON d.id = c.document_id
LEFT JOIN highlights h ON h.id = c.highlight_id
WHERE c.user_id = sqlc.arg(user_id) AND c.workspace_id = sqlc.arg(workspace_id) AND d.deleted_at IS NULL
  AND (c.document_id = sqlc.arg(document_id) OR sqlc.arg(document_id) = 0)
  -- viewer: private documents (v1.8.0), as privacy.Visible
  AND (d.private = 0 OR d.person_id IN (SELECT pa.person_id FROM person_access AS pa WHERE pa.user_id = sqlc.arg(viewer)) OR (d.added_by = sqlc.arg(viewer) AND d.uploader_access = 1) OR sqlc.arg(viewer) = -1)
  AND (c.due_on <= sqlc.arg(due_by) OR sqlc.arg(due_by) = '')
ORDER BY c.due_on, c.id
LIMIT sqlc.arg(lim);

-- name: CountReviewCards :one
SELECT count(*) AS total, CAST(coalesce(sum(CASE WHEN c.due_on <= sqlc.arg(today) THEN 1 ELSE 0 END), 0) AS INTEGER) AS due
FROM review_cards c JOIN documents d ON d.id = c.document_id
WHERE c.user_id = sqlc.arg(user_id) AND c.workspace_id = sqlc.arg(workspace_id) AND d.deleted_at IS NULL
  AND (c.document_id = sqlc.arg(document_id) OR sqlc.arg(document_id) = 0)
  -- viewer: private documents (v1.8.0), as privacy.Visible
  AND (d.private = 0 OR d.person_id IN (SELECT pa.person_id FROM person_access AS pa WHERE pa.user_id = sqlc.arg(viewer)) OR (d.added_by = sqlc.arg(viewer) AND d.uploader_access = 1) OR sqlc.arg(viewer) = -1);

-- name: GetReviewCard :one
SELECT * FROM review_cards WHERE id = ? AND user_id = ? AND workspace_id = ?;

-- name: UpdateReviewCard :exec
UPDATE review_cards SET due_on = ?, interval_days = ?, reps = ?, lapses = ?, updated_at = ? WHERE id = ?;

-- name: DeleteReviewCard :execrows
DELETE FROM review_cards WHERE id = ? AND user_id = ? AND workspace_id = ?;

-- name: HighlightsWithoutCards :many
SELECT h.id, h.quote, h.note FROM highlights h
WHERE h.user_id = ? AND h.workspace_id = ? AND h.document_id = ? AND (h.quote != '' OR h.note != '')
  AND NOT EXISTS (SELECT 1 FROM review_cards c WHERE c.highlight_id = h.id)
ORDER BY h.id
LIMIT ?;
