-- name: InsertJob :one
INSERT INTO jobs (workspace_id, kind, payload, priority, max_attempts, run_after, created_at, client_upload_id, request_id, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- A phone upload's job, found by the ID the phone chose (design doc 9.4).
-- name: GetJobByClientUploadID :one
SELECT * FROM jobs WHERE workspace_id = sqlc.arg(workspace_id) AND client_upload_id = sqlc.arg(client_upload_id);

-- name: ReleaseClientUploadID :exec
UPDATE jobs SET client_upload_id = NULL WHERE id = ? AND status = 'failed';

-- name: GetJob :one
SELECT * FROM jobs WHERE id = ?;

-- ClaimNextJob atomically picks one runnable job and marks it running.
-- name: ClaimNextJob :one
UPDATE jobs
SET status = 'running',
    attempts = attempts + 1,
    started_at = sqlc.arg(now),
    heartbeat_at = sqlc.arg(now)
WHERE id = (
    SELECT j.id FROM jobs AS j
    WHERE j.status = 'queued' AND j.run_after <= sqlc.arg(now)
    ORDER BY j.priority DESC, j.run_after, j.id
    LIMIT 1
) AND status = 'queued' -- on Postgres a concurrent claim may have taken it
RETURNING *;

-- name: HeartbeatJob :exec
UPDATE jobs SET heartbeat_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND status = 'running';

-- name: CompleteJob :exec
UPDATE jobs
SET status = 'succeeded', result = sqlc.narg(result), error = NULL,
    finished_at = sqlc.arg(now), heartbeat_at = NULL
WHERE id = sqlc.arg(id);

-- name: RetryJob :exec
UPDATE jobs
SET status = 'queued', error = sqlc.arg(error), run_after = sqlc.arg(run_after), heartbeat_at = NULL
WHERE id = sqlc.arg(id);

-- name: FailJob :exec
UPDATE jobs
SET status = 'failed', error = sqlc.arg(error), finished_at = sqlc.arg(now), heartbeat_at = NULL
WHERE id = sqlc.arg(id);

-- Running jobs whose heartbeat stopped (e.g. the process crashed):
-- requeue those with attempts left, fail the rest.
-- name: RequeueStaleJobs :execrows
UPDATE jobs
SET status = 'queued', run_after = sqlc.arg(now), heartbeat_at = NULL, error = sqlc.arg(error)
WHERE status = 'running' AND heartbeat_at < sqlc.arg(cutoff) AND attempts < max_attempts;

-- name: FailStaleJobs :execrows
UPDATE jobs
SET status = 'failed', finished_at = sqlc.arg(now), heartbeat_at = NULL, error = sqlc.arg(error)
WHERE status = 'running' AND heartbeat_at < sqlc.arg(cutoff) AND attempts >= max_attempts;

-- Progress of a running job ({stage, progress} JSON), replaced by the final
-- result when the job completes.
-- name: SetJobProgress :exec
UPDATE jobs SET result = sqlc.arg(result) WHERE id = sqlc.arg(id) AND status = 'running';

-- name: SetJobDocument :exec
UPDATE jobs SET document_id = sqlc.arg(document_id) WHERE id = sqlc.arg(id);

-- Recent jobs of a workspace, newest first (the upload tray).
-- name: ListRecentJobs :many
SELECT * FROM jobs
WHERE jobs.workspace_id = sqlc.arg(workspace_id) AND substr(jobs.kind, 1, length(sqlc.arg(kind_prefix))) = sqlc.arg(kind_prefix)
  AND jobs.created_at >= sqlc.arg(since)
  AND NOT EXISTS (SELECT 1 FROM documents AS vx WHERE vx.id = jobs.document_id AND NOT (vx.private = 0 OR vx.person_id IN (SELECT pa.person_id FROM person_access AS pa WHERE pa.user_id = sqlc.arg(viewer)) OR (vx.added_by = sqlc.arg(viewer) AND vx.uploader_access = 1) OR sqlc.arg(viewer) = -1))
ORDER BY id DESC
LIMIT sqlc.arg(max_rows);

-- Working directories still needed: payloads of jobs that have not finished.
-- name: ListUnfinishedJobPayloads :many
SELECT payload FROM jobs WHERE status IN ('queued', 'running');

-- A workspace's jobs of every kind since a time, newest first (the task list).
-- name: ListWorkspaceJobs :many
SELECT j.*, coalesce(d.title, '') AS document_title, coalesce(nullif(u.display_name, ''), u.username, '') AS creator_name
FROM jobs AS j LEFT JOIN documents AS d ON d.id = j.document_id LEFT JOIN users AS u ON u.id = j.created_by
WHERE j.workspace_id = sqlc.arg(workspace_id) AND j.created_at >= sqlc.arg(since)
  AND (sqlc.arg(status) = '' OR j.status = sqlc.arg(status)
       OR (sqlc.arg(status) = 'active' AND j.status IN ('queued', 'running')))
  -- kinds: ',kind,kind,' ('' all); created_by 0: anyone (v1.7.14)
  AND (sqlc.arg(kinds) = '' OR instr(sqlc.arg(kinds), ',' || j.kind || ',') > 0)
  AND (sqlc.arg(created_by) = 0 OR j.created_by = sqlc.arg(created_by))
  -- people 1: only jobs a person started (v1.7.16)
  AND (sqlc.arg(people) = 0 OR j.created_by IS NOT NULL)
  -- viewer: no task about a private document the viewer may not see (v1.8.0)
  AND NOT EXISTS (SELECT 1 FROM documents AS vx WHERE vx.id = j.document_id AND NOT (vx.private = 0 OR vx.person_id IN (SELECT pa.person_id FROM person_access AS pa WHERE pa.user_id = sqlc.arg(viewer)) OR (vx.added_by = sqlc.arg(viewer) AND vx.uploader_access = 1) OR sqlc.arg(viewer) = -1))
ORDER BY j.id DESC
LIMIT sqlc.arg(max_rows);

-- An unfinished job of a kind (the manual backup reuses it).
-- name: FindActiveJob :one
SELECT id FROM jobs WHERE kind = ? AND status IN ('queued', 'running') ORDER BY id LIMIT 1;

-- Finished jobs older than the task list reaches back (v1.7.19).
-- name: PurgeFinishedJobs :execrows
DELETE FROM jobs WHERE status IN ('succeeded', 'failed') AND finished_at < sqlc.arg(before);

-- A failed upload processed again from its kept file (v1.9.12): the same
-- job goes back to the queue, as if new.
-- name: RequeueFailedJob :execrows
UPDATE jobs
SET status = 'queued', attempts = 0, error = NULL, result = NULL, document_id = NULL,
    run_after = sqlc.arg(now), started_at = NULL, finished_at = NULL, heartbeat_at = NULL
WHERE id = sqlc.arg(id) AND status = 'failed';

-- Failed uploads of a kind that finished since a time (their files are
-- kept for a retry, v1.9.12), or before it (their files can go).
-- name: ListFailedPayloadsSince :many
SELECT payload FROM jobs WHERE status = 'failed' AND kind = sqlc.arg(kind) AND finished_at >= sqlc.arg(since);

-- name: ListFailedPayloadsBefore :many
SELECT payload FROM jobs WHERE status = 'failed' AND kind = sqlc.arg(kind) AND finished_at < sqlc.arg(before);
