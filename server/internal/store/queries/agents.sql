-- name: GetAISettings :one
SELECT ai_settings FROM workspaces WHERE id = ?;

-- name: SetAISettings :exec
UPDATE workspaces SET ai_settings = sqlc.arg(ai_settings) WHERE id = sqlc.arg(id);

-- name: InsertAgentRun :one
INSERT INTO agent_runs (workspace_id, agent, trigger, document_id, model, status, created_at)
VALUES (?, ?, ?, ?, ?, 'running', ?) RETURNING id;

-- name: FinishAgentRun :exec
UPDATE agent_runs SET status = sqlc.arg(status), calls = sqlc.arg(calls), input_tokens = sqlc.arg(input_tokens), output_tokens = sqlc.arg(output_tokens),
    duration_ms = sqlc.arg(duration_ms), summary = sqlc.arg(summary), error = sqlc.arg(error)
WHERE id = sqlc.arg(id);

-- name: InsertAgentStep :exec
INSERT INTO agent_steps (run_id, seq, kind, input, output) VALUES (?, ?, ?, ?, ?);

-- name: CountModelCallsSince :one
SELECT CAST(coalesce(sum(calls), 0) AS INTEGER) FROM agent_runs WHERE workspace_id = sqlc.arg(workspace_id) AND created_at >= sqlc.arg(since);

-- name: ListAgentRuns :many
SELECT r.*, coalesce(d.title, '') AS document_title FROM agent_runs AS r
LEFT JOIN documents AS d ON d.id = r.document_id
WHERE r.workspace_id = sqlc.arg(workspace_id)
  AND NOT EXISTS (SELECT 1 FROM documents AS vx WHERE vx.id = r.document_id AND NOT (vx.private = 0 OR vx.person_id IN (SELECT pa.person_id FROM person_access AS pa WHERE pa.user_id = sqlc.arg(viewer)) OR (vx.added_by = sqlc.arg(viewer) AND vx.uploader_access = 1) OR sqlc.arg(viewer) = -1))
ORDER BY r.id DESC LIMIT sqlc.arg(lim);

-- name: InsertProposal :one
INSERT INTO agent_proposals (workspace_id, run_id, agent, kind, document_id, payload, confidence, reason, status, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id;

-- name: GetProposal :one
SELECT * FROM agent_proposals WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: SetProposalStatus :exec
UPDATE agent_proposals SET status = sqlc.arg(status), decided_by = sqlc.narg(decided_by), decided_at = sqlc.arg(now)
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: ExpireProposals :exec
UPDATE agent_proposals SET status = 'expired' WHERE workspace_id = sqlc.arg(workspace_id) AND status = 'pending' AND created_at < sqlc.arg(before);

-- name: ListDocumentProposals :many
SELECT * FROM agent_proposals WHERE workspace_id = sqlc.arg(workspace_id) AND document_id = sqlc.arg(document_id) ORDER BY id DESC;

-- name: ListPendingProposals :many
SELECT * FROM agent_proposals WHERE workspace_id = ? AND status = 'pending' ORDER BY id DESC LIMIT 200;
