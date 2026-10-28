-- name: InsertAuditEvent :exec
INSERT INTO audit_events (workspace_id, actor_user_id, actor_via, action, target_type, target_id,
                          summary, changes, request_id, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- Filters are always bound; pass '' / 0 / the int64 bounds to leave one open.
-- The action filter is a literal prefix (no LIKE wildcards).
-- name: ListWorkspaceAuditEvents :many
SELECT e.id, e.action, e.actor_user_id, e.actor_via, e.target_type, e.target_id, e.summary, e.changes, e.created_at,
       coalesce(u.display_name, '') AS actor_name
FROM audit_events AS e
LEFT JOIN users AS u ON u.id = e.actor_user_id
WHERE e.workspace_id = sqlc.arg(workspace_id)
  AND substr(e.action, 1, length(sqlc.arg(action_prefix))) = sqlc.arg(action_prefix)
  AND (sqlc.arg(actor_id) = 0 OR e.actor_user_id = sqlc.arg(actor_id))
  AND e.created_at >= sqlc.arg(from_ms) AND e.created_at < sqlc.arg(to_ms)
  AND e.id < sqlc.arg(before_id)
  -- viewer: not what was done to a private document the viewer may not see (v1.8.0)
  AND NOT EXISTS (SELECT 1 FROM documents AS vx WHERE e.target_type = 'document' AND vx.id = CAST(e.target_id AS INTEGER)
                  AND NOT (vx.private = 0 OR vx.person_id IN (SELECT pa.person_id FROM person_access AS pa WHERE pa.user_id = sqlc.arg(viewer)) OR (vx.added_by = sqlc.arg(viewer) AND vx.uploader_access = 1) OR sqlc.arg(viewer) = -1))
ORDER BY e.id DESC
LIMIT sqlc.arg(max_rows);

-- name: ListInstanceAuditEvents :many
SELECT e.id, e.action, e.actor_user_id, e.actor_via, e.target_type, e.target_id, e.summary, e.changes, e.created_at,
       coalesce(u.display_name, '') AS actor_name
FROM audit_events AS e
LEFT JOIN users AS u ON u.id = e.actor_user_id
WHERE e.workspace_id IS NULL
  AND substr(e.action, 1, length(sqlc.arg(action_prefix))) = sqlc.arg(action_prefix)
  AND (sqlc.arg(actor_id) = 0 OR e.actor_user_id = sqlc.arg(actor_id))
  AND e.created_at >= sqlc.arg(from_ms) AND e.created_at < sqlc.arg(to_ms)
  AND e.id < sqlc.arg(before_id)
ORDER BY e.id DESC
LIMIT sqlc.arg(max_rows);

-- name: DeleteAuditEventsBefore :execrows
DELETE FROM audit_events WHERE created_at < ?;

-- name: ListTargetAuditEvents :many
SELECT e.id, e.action, e.actor_user_id, e.actor_via, e.target_type, e.target_id, e.summary, e.changes, e.created_at,
       coalesce(u.display_name, '') AS actor_name
FROM audit_events AS e
LEFT JOIN users AS u ON u.id = e.actor_user_id
WHERE e.workspace_id = sqlc.arg(workspace_id) AND e.target_type = sqlc.arg(target_type) AND e.target_id = sqlc.arg(target_id)
  AND e.id < sqlc.arg(before_id)
ORDER BY e.id DESC
LIMIT sqlc.arg(max_rows);
