-- Phone pairing and device tokens (design doc 9.3, 9.6).

-- name: DeleteStalePairingCodes :exec
DELETE FROM pairing_codes
WHERE expires_at < sqlc.arg(before)
   OR (user_id = sqlc.arg(user_id) AND workspace_id = sqlc.arg(workspace_id) AND used_at IS NULL);

-- name: InsertPairingCode :exec
INSERT INTO pairing_codes (user_id, workspace_id, code_hash, created_at, expires_at)
VALUES (?, ?, ?, ?, ?);

-- FindPairingCode returns a live code with what pairing needs: the user's
-- current role in the workspace (no row once removed or disabled).
-- name: FindPairingCode :one
SELECT p.id, p.user_id, p.workspace_id, w.name AS workspace_name, m.role, u.display_name
FROM pairing_codes AS p
JOIN users AS u ON u.id = p.user_id AND u.disabled_at IS NULL
JOIN workspaces AS w ON w.id = p.workspace_id AND w.deleted_at IS NULL
JOIN workspace_members AS m ON m.workspace_id = p.workspace_id AND m.user_id = p.user_id
WHERE p.code_hash = sqlc.arg(code_hash) AND p.used_at IS NULL AND p.expires_at > sqlc.arg(now);

-- name: UsePairingCode :execrows
UPDATE pairing_codes SET used_at = sqlc.arg(now) WHERE id = sqlc.arg(id) AND used_at IS NULL;

-- name: InsertDeviceToken :one
INSERT INTO api_tokens (user_id, name, token_hash, prefix, created_at, kind, workspace_id, device_name, paired_at, access)
VALUES (sqlc.arg(user_id), sqlc.arg(device_name), sqlc.arg(token_hash), sqlc.arg(prefix), sqlc.arg(now), 'device',
        sqlc.arg(workspace_id), sqlc.arg(device_name), sqlc.arg(now), sqlc.arg(access))
RETURNING id;

-- name: ListDevices :many
SELECT t.id, t.device_name, t.paired_at, t.last_used_at, t.workspace_id, w.name AS workspace_name, t.access
FROM api_tokens AS t
JOIN workspaces AS w ON w.id = t.workspace_id
WHERE t.user_id = ? AND t.kind = 'device' AND w.deleted_at IS NULL
ORDER BY t.paired_at DESC, t.id DESC;

-- name: GetDevice :one
SELECT t.id, t.device_name, t.workspace_id
FROM api_tokens AS t
WHERE t.id = sqlc.arg(id) AND t.user_id = sqlc.arg(user_id) AND t.kind = 'device';

-- name: DeleteDevice :execrows
DELETE FROM api_tokens WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) AND kind = 'device';
