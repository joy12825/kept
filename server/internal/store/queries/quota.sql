-- name: GetWorkspaceQuota :one
SELECT quota_bytes FROM workspaces WHERE id = ? AND deleted_at IS NULL;

-- name: SetWorkspaceQuota :execrows
UPDATE workspaces SET quota_bytes = sqlc.narg(quota_bytes) WHERE id = sqlc.arg(id) AND deleted_at IS NULL;

-- name: ListDocumentFiles :many
SELECT id, original_ext, storage, deleted_at IS NOT NULL AS in_trash, original_size, derived_size FROM documents WHERE workspace_id = ?;

-- name: SetDocumentSizes :exec
UPDATE documents SET original_size = sqlc.narg(original_size), derived_size = sqlc.narg(derived_size) WHERE id = sqlc.arg(id);

-- Earlier versions keep their size; split by whether the document is in the trash.
-- name: VersionSizes :one
SELECT CAST(COALESCE(SUM(CASE WHEN d.deleted_at IS NULL THEN v.size ELSE 0 END), 0) AS BIGINT) AS live,
       CAST(COALESCE(SUM(CASE WHEN d.deleted_at IS NOT NULL THEN v.size ELSE 0 END), 0) AS BIGINT) AS trash
FROM document_versions AS v JOIN documents AS d ON d.id = v.document_id
WHERE v.workspace_id = ? AND v.current = 0;

-- name: PendingUploadPayloads :many
SELECT payload FROM jobs WHERE workspace_id = ? AND kind = 'ingest.document' AND status IN ('queued', 'running');

-- name: ListWorkspacesForAdmin :many
SELECT w.id, w.name, w.kind, w.color, w.quota_bytes,
       (SELECT count(*) FROM workspace_members m WHERE m.workspace_id = w.id) AS members,
       CAST(coalesce((SELECT group_concat(coalesce(nullif(u.display_name, ''), u.username), char(12289)) FROM workspace_members m JOIN users u ON u.id = m.user_id
                 WHERE m.workspace_id = w.id AND m.role = 'owner'), '') AS TEXT) AS owners
FROM workspaces w WHERE w.deleted_at IS NULL ORDER BY w.id;
