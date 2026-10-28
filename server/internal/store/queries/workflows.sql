-- name: ListWorkflows :many
SELECT w.*,
       CAST(coalesce((SELECT max(r.created_at) FROM workflow_runs AS r WHERE r.workflow_id = w.id), 0) AS INTEGER) AS last_run_at,
       CAST(coalesce((SELECT r.status FROM workflow_runs AS r WHERE r.workflow_id = w.id ORDER BY r.id DESC LIMIT 1), '') AS TEXT) AS last_status
FROM workflows AS w
WHERE w.workspace_id = ?
ORDER BY w.sort_order, w.id;

-- name: ListEnabledWorkflows :many
SELECT * FROM workflows WHERE workspace_id = ? AND enabled = 1 ORDER BY sort_order, id;

-- name: GetWorkflow :one
SELECT * FROM workflows WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: InsertWorkflow :one
INSERT INTO workflows (workspace_id, name, enabled, sort_order, triggers, conditions, actions, created_at, updated_at)
VALUES (sqlc.arg(workspace_id), sqlc.arg(name), sqlc.arg(enabled),
        (SELECT coalesce(max(sort_order), 0) + 1 FROM workflows WHERE workspace_id = sqlc.arg(workspace_id)),
        sqlc.arg(triggers), sqlc.arg(conditions), sqlc.arg(actions), sqlc.arg(now), sqlc.arg(now))
RETURNING id;

-- name: UpdateWorkflow :execrows
UPDATE workflows SET name = sqlc.arg(name), enabled = sqlc.arg(enabled), triggers = sqlc.arg(triggers),
       conditions = sqlc.arg(conditions), actions = sqlc.arg(actions), updated_at = sqlc.arg(now)
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: DeleteWorkflow :execrows
DELETE FROM workflows WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: SetWorkflowOrder :exec
UPDATE workflows SET sort_order = sqlc.arg(sort_order) WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: InsertWorkflowRun :exec
INSERT INTO workflow_runs (workflow_id, workspace_id, document_id, trigger, status, message, created_at)
VALUES (sqlc.arg(workflow_id), sqlc.arg(workspace_id), sqlc.narg(document_id), sqlc.arg(trigger), sqlc.arg(status), sqlc.arg(message), sqlc.arg(now));

-- name: PruneWorkflowRuns :exec
DELETE FROM workflow_runs WHERE workflow_runs.workflow_id = sqlc.arg(workflow_id) AND workflow_runs.id NOT IN (
    SELECT k.id FROM workflow_runs AS k WHERE k.workflow_id = sqlc.arg(workflow_id) ORDER BY k.id DESC LIMIT 200);

-- name: ListWorkflowRuns :many
SELECT r.id, r.document_id, r.trigger, r.status, r.message, r.created_at, coalesce(d.title, '') AS document_title
FROM workflow_runs AS r
LEFT JOIN documents AS d ON d.id = r.document_id
WHERE r.workspace_id = sqlc.arg(workspace_id) AND r.workflow_id = sqlc.arg(workflow_id)
  AND NOT EXISTS (SELECT 1 FROM documents AS vx WHERE vx.id = r.document_id AND NOT (vx.private = 0 OR vx.person_id IN (SELECT pa.person_id FROM person_access AS pa WHERE pa.user_id = sqlc.arg(viewer)) OR (vx.added_by = sqlc.arg(viewer) AND vx.uploader_access = 1) OR sqlc.arg(viewer) = -1))
ORDER BY r.id DESC
LIMIT sqlc.arg(lim);

-- name: LastScheduledRuns :many
SELECT CAST(document_id AS INTEGER) AS document_id, CAST(max(created_at) AS INTEGER) AS last_at FROM workflow_runs
WHERE workflow_id = ? AND trigger = 'scheduled' AND document_id IS NOT NULL
GROUP BY document_id;
