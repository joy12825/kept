-- name: ListDocumentVersions :many
SELECT v.*, coalesce(u.display_name, u.username, '') AS creator_name
FROM document_versions AS v LEFT JOIN users AS u ON u.id = v.created_by
WHERE v.workspace_id = sqlc.arg(workspace_id) AND v.document_id = sqlc.arg(document_id)
ORDER BY v.number DESC;

-- name: GetDocumentVersion :one
SELECT * FROM document_versions WHERE workspace_id = sqlc.arg(workspace_id) AND document_id = sqlc.arg(document_id) AND id = sqlc.arg(id);

-- name: InsertDocumentVersion :one
INSERT INTO document_versions (workspace_id, document_id, number, label, original_filename, original_ext, mime_type, checksum, size, encrypted, current, created_by, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id;

-- name: ClearCurrentVersion :exec
UPDATE document_versions SET current = 0 WHERE workspace_id = sqlc.arg(workspace_id) AND document_id = sqlc.arg(document_id);

-- name: SetCurrentVersion :exec
UPDATE document_versions SET current = 1 WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: DeleteDocumentVersion :exec
DELETE FROM document_versions WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: SetDocumentOriginal :exec
UPDATE documents SET original_filename = sqlc.arg(original_filename), original_ext = sqlc.arg(original_ext), mime_type = sqlc.arg(mime_type),
    checksum = sqlc.arg(checksum), original_encrypted = sqlc.arg(original_encrypted), has_archive = 0, archive_checksum = NULL,
    has_thumbnail = 0, page_edits = '', modified_at = sqlc.arg(modified_at), original_size = NULL, derived_size = NULL
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: FindVersionByChecksum :one
SELECT number FROM document_versions WHERE workspace_id = sqlc.arg(workspace_id) AND document_id = sqlc.arg(document_id) AND checksum = sqlc.arg(checksum)
LIMIT 1;
