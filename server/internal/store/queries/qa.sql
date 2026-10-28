-- name: ListQAConversations :many
SELECT * FROM qa_conversations WHERE workspace_id = sqlc.arg(workspace_id) AND user_id = sqlc.arg(user_id) ORDER BY updated_at DESC, id DESC LIMIT 200;

-- name: GetQAConversation :one
SELECT * FROM qa_conversations WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id) AND user_id = sqlc.arg(user_id);

-- name: InsertQAConversation :one
INSERT INTO qa_conversations (workspace_id, user_id, title, created_at, updated_at)
VALUES (sqlc.arg(workspace_id), sqlc.arg(user_id), sqlc.arg(title), sqlc.arg(now), sqlc.arg(now)) RETURNING *;

-- name: TouchQAConversation :exec
UPDATE qa_conversations SET title = CASE WHEN title = '' THEN sqlc.arg(title) ELSE title END, updated_at = sqlc.arg(now) WHERE id = sqlc.arg(id);

-- name: DeleteQAConversation :execrows
DELETE FROM qa_conversations WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id) AND user_id = sqlc.arg(user_id);

-- name: ListQAMessages :many
SELECT * FROM qa_messages WHERE conversation_id = ? ORDER BY id;

-- name: InsertQAMessage :one
INSERT INTO qa_messages (conversation_id, role, content, sources, status, run_id, created_at)
VALUES (sqlc.arg(conversation_id), sqlc.arg(role), sqlc.arg(content), sqlc.arg(sources), sqlc.arg(status), sqlc.narg(run_id), sqlc.arg(now)) RETURNING *;
