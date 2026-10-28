-- name: ListNoteBodies :many
SELECT body FROM notes WHERE document_id = ? ORDER BY created_at DESC, id DESC;

-- name: ListNotes :many
SELECT n.id, n.document_id, n.user_id, n.body, n.created_at, coalesce(u.display_name, '') AS author_name
FROM notes AS n LEFT JOIN users AS u ON u.id = n.user_id
WHERE n.workspace_id = ? AND n.document_id = ?
ORDER BY n.created_at DESC, n.id DESC;

-- name: InsertNote :one
INSERT INTO notes (workspace_id, document_id, user_id, body, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id;

-- name: GetNote :one
SELECT * FROM notes WHERE workspace_id = sqlc.arg(workspace_id) AND document_id = sqlc.arg(document_id) AND id = sqlc.arg(id);

-- name: DeleteNote :exec
DELETE FROM notes WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);
