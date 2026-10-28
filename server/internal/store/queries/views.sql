-- name: ListSavedViews :many
SELECT v.*, coalesce(u.display_name, '') AS creator_name
FROM saved_views AS v LEFT JOIN users AS u ON u.id = v.created_by
WHERE v.workspace_id = ? ORDER BY v.position, v.sort_key, v.name, v.id;

-- name: GetSavedView :one
SELECT * FROM saved_views WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: FindSavedViewByName :one
SELECT id FROM saved_views WHERE workspace_id = sqlc.arg(workspace_id) AND name = sqlc.arg(name);

-- A new view goes last once the views were ordered (v1.6.5).
-- name: InsertSavedView :one
INSERT INTO saved_views (workspace_id, name, sort_key, filter, sort, display, created_by, created_at, updated_at, position)
VALUES (sqlc.arg(workspace_id), sqlc.arg(name), sqlc.arg(sort_key), sqlc.arg(filter), sqlc.arg(sort), sqlc.arg(display), sqlc.narg(created_by),
        sqlc.arg(created_at), sqlc.arg(updated_at),
        (SELECT CASE WHEN max(o.position) > 0 THEN max(o.position) + 1 ELSE 0 END FROM saved_views AS o WHERE o.workspace_id = sqlc.arg(workspace_id))) RETURNING id;

-- name: SetSavedViewPosition :execrows
UPDATE saved_views SET position = sqlc.arg(position) WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: UpdateSavedView :exec
UPDATE saved_views SET name = sqlc.arg(name), sort_key = sqlc.arg(sort_key), filter = sqlc.arg(filter), sort = sqlc.arg(sort),
    display = sqlc.arg(display), updated_at = sqlc.arg(updated_at)
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: DeleteSavedView :exec
DELETE FROM saved_views WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);
