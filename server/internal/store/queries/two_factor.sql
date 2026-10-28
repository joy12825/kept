-- name: GetUserTOTP :one
SELECT * FROM user_totp WHERE user_id = ?;

-- name: UpsertPendingTOTP :exec
INSERT INTO user_totp (user_id, secret, enabled_at, last_used_step, created_at)
VALUES (sqlc.arg(user_id), sqlc.arg(secret), NULL, 0, sqlc.arg(created_at))
ON CONFLICT (user_id) DO UPDATE SET
    secret = excluded.secret,
    enabled_at = NULL,
    last_used_step = 0,
    created_at = excluded.created_at;

-- name: EnableUserTOTP :exec
UPDATE user_totp SET enabled_at = sqlc.arg(enabled_at), last_used_step = sqlc.arg(last_used_step)
WHERE user_id = sqlc.arg(user_id);

-- name: UpdateTOTPLastUsedStep :exec
UPDATE user_totp SET last_used_step = sqlc.arg(last_used_step) WHERE user_id = sqlc.arg(user_id);

-- name: DeleteUserTOTP :exec
DELETE FROM user_totp WHERE user_id = ?;

-- name: InsertRecoveryCode :exec
INSERT INTO user_recovery_codes (user_id, code_hash, used_at) VALUES (?, ?, NULL);

-- name: DeleteRecoveryCodes :exec
DELETE FROM user_recovery_codes WHERE user_id = ?;

-- name: CountUnusedRecoveryCodes :one
SELECT count(*) FROM user_recovery_codes WHERE user_id = ? AND used_at IS NULL;

-- name: GetUnusedRecoveryCode :one
SELECT * FROM user_recovery_codes WHERE user_id = sqlc.arg(user_id) AND code_hash = sqlc.arg(code_hash) AND used_at IS NULL;

-- name: MarkRecoveryCodeUsed :exec
UPDATE user_recovery_codes SET used_at = sqlc.arg(used_at) WHERE id = sqlc.arg(id);
