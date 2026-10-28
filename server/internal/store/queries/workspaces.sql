-- Workspaces a user belongs to: the personal one first, then by creation time.
-- name: ListWorkspacesForUser :many
SELECT w.id, w.name, w.kind, w.color, m.role,
       (SELECT count(*) FROM workspace_members AS c WHERE c.workspace_id = w.id) AS member_count
FROM workspaces AS w
JOIN workspace_members AS m ON m.workspace_id = w.id
WHERE m.user_id = ? AND w.deleted_at IS NULL
ORDER BY CASE WHEN w.kind = 'personal' THEN 0 ELSE 1 END, w.created_at, w.id;

-- One workspace as seen by one of its members; no row for non-members and
-- for deleted workspaces.
-- name: GetWorkspaceForUser :one
SELECT w.id, w.name, w.kind, w.color, m.role,
       (SELECT count(*) FROM workspace_members AS c WHERE c.workspace_id = w.id) AS member_count
FROM workspaces AS w
JOIN workspace_members AS m ON m.workspace_id = w.id
WHERE m.user_id = sqlc.arg(user_id) AND w.id = sqlc.arg(id) AND w.deleted_at IS NULL;

-- name: UpdateWorkspace :exec
UPDATE workspaces SET name = sqlc.arg(name), color = sqlc.arg(color) WHERE id = sqlc.arg(id);

-- name: SoftDeleteWorkspace :exec
UPDATE workspaces SET deleted_at = sqlc.arg(deleted_at) WHERE id = sqlc.arg(id);

-- name: ListWorkspaceMembers :many
SELECT u.id AS user_id, u.username, u.display_name, u.email, m.role, m.created_at
FROM workspace_members AS m
JOIN users AS u ON u.id = m.user_id
WHERE m.workspace_id = ?
ORDER BY m.created_at, u.id;

-- name: GetWorkspaceMember :one
SELECT u.id AS user_id, u.username, u.display_name, u.email, m.role, m.created_at
FROM workspace_members AS m
JOIN users AS u ON u.id = m.user_id
WHERE m.workspace_id = sqlc.arg(workspace_id) AND m.user_id = sqlc.arg(user_id);

-- name: UpdateWorkspaceMemberRole :exec
UPDATE workspace_members SET role = sqlc.arg(role)
WHERE workspace_id = sqlc.arg(workspace_id) AND user_id = sqlc.arg(user_id);

-- name: DeleteWorkspaceMember :exec
DELETE FROM workspace_members
WHERE workspace_id = sqlc.arg(workspace_id) AND user_id = sqlc.arg(user_id);

-- name: CountWorkspaceOwners :one
SELECT count(*) FROM workspace_members WHERE workspace_id = ? AND role = 'owner';

-- Members, jobs and audit events go with the workspace (ON DELETE CASCADE).
-- name: DeleteWorkspacesDeletedBefore :execrows
DELETE FROM workspaces WHERE deleted_at IS NOT NULL AND deleted_at < ?;

-- name: ListWorkspacesDeletedBefore :many
SELECT id FROM workspaces WHERE deleted_at IS NOT NULL AND deleted_at < ?;

-- The storage new documents of a workspace go to (v1.7.0).
-- name: GetWorkspaceStorage :one
SELECT storage FROM workspaces WHERE id = ?;

-- name: SetWorkspaceStorage :execrows
UPDATE workspaces SET storage = sqlc.arg(storage) WHERE id = sqlc.arg(id) AND deleted_at IS NULL;
