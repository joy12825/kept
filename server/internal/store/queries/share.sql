-- name: InsertShareLink :one
INSERT INTO share_links (workspace_id, document_id, token, file, expires_at, created_by, created_at, watermark, allow_download)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id;

-- name: ListActiveShareLinks :many
SELECT s.*, coalesce(u.display_name, u.username, '') AS creator_name
FROM share_links AS s LEFT JOIN users AS u ON u.id = s.created_by
WHERE s.workspace_id = sqlc.arg(workspace_id) AND s.document_id = sqlc.arg(document_id)
  AND s.revoked_at IS NULL AND s.expires_at > sqlc.arg(now)
ORDER BY s.id DESC;

-- name: CountActiveShareLinks :one
SELECT count(*) FROM share_links
WHERE workspace_id = sqlc.arg(workspace_id) AND document_id = sqlc.arg(document_id) AND revoked_at IS NULL AND expires_at > sqlc.arg(now);

-- name: GetShareLink :one
SELECT * FROM share_links WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: RevokeShareLink :execrows
UPDATE share_links SET revoked_at = sqlc.arg(now)
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id) AND revoked_at IS NULL;

-- name: FindShareByToken :one
SELECT s.*, d.title, d.mime_type, d.original_filename, d.original_ext, d.page_count, d.has_archive, d.storage, d.deleted_at AS document_deleted_at,
  d.checksum AS document_checksum, coalesce(d.archive_checksum, '') AS archive_checksum
FROM share_links AS s JOIN documents AS d ON d.id = s.document_id
WHERE s.token = ?;

-- name: TouchShareLink :exec
UPDATE share_links SET access_count = access_count + 1, last_access_at = sqlc.arg(now) WHERE id = sqlc.arg(id);
