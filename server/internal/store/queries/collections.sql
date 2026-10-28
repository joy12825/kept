-- name: InsertCollection :one
INSERT INTO collections (workspace_id, name, kind, filter, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id;

-- name: GetCollection :one
SELECT * FROM collections WHERE workspace_id = ? AND id = ?;

-- name: ListCollections :many
SELECT * FROM collections WHERE workspace_id = ? ORDER BY id DESC;

-- name: UpdateCollection :exec
UPDATE collections SET name = ?, filter = ?, updated_at = ? WHERE workspace_id = ? AND id = ?;

-- name: DeleteCollection :execrows
DELETE FROM collections WHERE workspace_id = ? AND id = ?;

-- name: AddCollectionDocument :exec
INSERT INTO collection_documents (collection_id, document_id)
SELECT sqlc.arg(collection_id), d.id FROM documents d WHERE d.id = sqlc.arg(document_id) AND d.workspace_id = sqlc.arg(workspace_id)
ON CONFLICT DO NOTHING;

-- name: RemoveCollectionDocument :exec
DELETE FROM collection_documents WHERE collection_id = ? AND document_id = ?;

-- name: ListCollectionShares :many
SELECT s.collection_id, s.target_workspace_id, w.name, w.kind, w.color, s.created_at
FROM collection_shares s JOIN workspaces w ON w.id = s.target_workspace_id
WHERE s.collection_id IN (SELECT c.id FROM collections c WHERE c.workspace_id = ?) AND w.deleted_at IS NULL
ORDER BY s.created_at;

-- name: InsertCollectionShare :exec
INSERT INTO collection_shares (collection_id, target_workspace_id, created_by, created_at) VALUES (?, ?, ?, ?)
ON CONFLICT DO NOTHING;

-- name: DeleteCollectionShare :execrows
DELETE FROM collection_shares WHERE collection_id = ? AND target_workspace_id = ?;

-- name: ListSharedCollections :many
SELECT c.*, w.name AS source_name
FROM collection_shares s JOIN collections c ON c.id = s.collection_id JOIN workspaces w ON w.id = c.workspace_id
WHERE s.target_workspace_id = ? AND w.deleted_at IS NULL
ORDER BY c.name;

-- name: GetSharedCollection :one
SELECT c.*, w.name AS source_name
FROM collection_shares s JOIN collections c ON c.id = s.collection_id JOIN workspaces w ON w.id = c.workspace_id
WHERE s.target_workspace_id = ? AND c.id = ? AND w.deleted_at IS NULL;

-- name: GetLiveWorkspaceName :one
SELECT name FROM workspaces WHERE id = ? AND deleted_at IS NULL;
