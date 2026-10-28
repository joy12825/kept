-- name: GetReadingProgress :one
SELECT * FROM reading_progress WHERE user_id = sqlc.arg(user_id) AND document_id = sqlc.arg(document_id) AND workspace_id = sqlc.arg(workspace_id);

-- name: UpsertReadingProgress :exec
INSERT INTO reading_progress (user_id, document_id, workspace_id, position, percent, updated_at)
VALUES (sqlc.arg(user_id), sqlc.arg(document_id), sqlc.arg(workspace_id), sqlc.arg(position), sqlc.arg(percent), sqlc.arg(now))
ON CONFLICT (user_id, document_id) DO UPDATE SET position = excluded.position, percent = excluded.percent, updated_at = excluded.updated_at;

-- name: UpsertReadingState :exec
INSERT INTO reading_progress (user_id, document_id, workspace_id, status, rating, started_on, finished_on, updated_at)
VALUES (sqlc.arg(user_id), sqlc.arg(document_id), sqlc.arg(workspace_id), sqlc.arg(status), sqlc.arg(rating), sqlc.arg(started_on), sqlc.arg(finished_on), sqlc.arg(now))
ON CONFLICT (user_id, document_id) DO UPDATE SET status = excluded.status, rating = excluded.rating, started_on = excluded.started_on,
    finished_on = excluded.finished_on, updated_at = excluded.updated_at;

-- name: CountHighlights :one
SELECT count(*) FROM highlights WHERE user_id = sqlc.arg(user_id) AND document_id = sqlc.arg(document_id) AND workspace_id = sqlc.arg(workspace_id);

-- name: ListDocumentHighlights :many
SELECT * FROM highlights WHERE user_id = sqlc.arg(user_id) AND document_id = sqlc.arg(document_id) AND workspace_id = sqlc.arg(workspace_id) ORDER BY id;

-- name: GetHighlight :one
SELECT * FROM highlights WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) AND workspace_id = sqlc.arg(workspace_id);

-- name: InsertHighlight :one
INSERT INTO highlights (user_id, document_id, workspace_id, locator, quote, note, color, created_at, updated_at)
VALUES (sqlc.arg(user_id), sqlc.arg(document_id), sqlc.arg(workspace_id), sqlc.arg(locator), sqlc.arg(quote), sqlc.arg(note), sqlc.arg(color), sqlc.arg(now), sqlc.arg(now))
RETURNING *;

-- name: UpdateHighlight :exec
UPDATE highlights SET note = sqlc.arg(note), color = sqlc.arg(color), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) AND workspace_id = sqlc.arg(workspace_id);

-- name: DeleteHighlight :execrows
DELETE FROM highlights WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) AND workspace_id = sqlc.arg(workspace_id);
