-- name: ListSchedules :many
SELECT * FROM schedules ORDER BY name;

-- name: InsertSchedule :exec
INSERT INTO schedules (name, next_run_at) VALUES (?, ?) ON CONFLICT (name) DO NOTHING;

-- Advance an entry that is due; no row changes when another check already
-- advanced it.
-- name: AdvanceSchedule :execrows
UPDATE schedules SET next_run_at = sqlc.arg(next_run_at), last_run_at = sqlc.arg(now)
WHERE name = sqlc.arg(name) AND next_run_at = sqlc.arg(due_at);

-- name: CountUnfinishedJobs :one
SELECT count(*) FROM jobs
WHERE kind = sqlc.arg(kind) AND workspace_id IS sqlc.narg(workspace_id) AND status IN ('queued', 'running');

-- name: ListActiveWorkspaceIDs :many
SELECT id FROM workspaces WHERE deleted_at IS NULL ORDER BY id;
