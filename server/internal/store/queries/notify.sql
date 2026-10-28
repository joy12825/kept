-- name: GetMemberNotify :one
SELECT m.notify_email, coalesce(u.email, '') AS email
FROM workspace_members AS m JOIN users AS u ON u.id = m.user_id
WHERE m.workspace_id = sqlc.arg(workspace_id) AND m.user_id = sqlc.arg(user_id);

-- name: SetMemberNotify :execrows
UPDATE workspace_members SET notify_email = sqlc.arg(notify_email)
WHERE workspace_id = sqlc.arg(workspace_id) AND user_id = sqlc.arg(user_id);

-- name: ListAlertRecipients :many
SELECT u.id, u.email FROM workspace_members AS m JOIN users AS u ON u.id = m.user_id
WHERE m.workspace_id = ? AND m.notify_email = 1 AND u.disabled_at IS NULL
  AND u.email IS NOT NULL AND u.email != ''
ORDER BY u.id;

-- name: ListUnemailedAlertIDs :many
SELECT id FROM alerts WHERE workspace_id = ? AND status = 'open' AND emailed_at IS NULL ORDER BY id;

-- name: MarkAlertEmailed :exec
UPDATE alerts SET emailed_at = sqlc.arg(now) WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: ListDrawerNames :many
SELECT id, name FROM drawers WHERE workspace_id = ?;

-- name: GetWorkspaceName :one
SELECT name FROM workspaces WHERE id = ?;

-- The workspace's administrators who take reminder emails (v1.7.4: quota warnings).
-- name: ListOwnerRecipients :many
SELECT u.email FROM workspace_members AS m JOIN users AS u ON u.id = m.user_id
WHERE m.workspace_id = ? AND m.role = 'owner' AND m.notify_email = 1 AND u.disabled_at IS NULL
  AND u.email IS NOT NULL AND u.email != ''
ORDER BY u.id;
