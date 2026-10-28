-- name: CountUsers :one
SELECT count(*) FROM users;

-- name: InsertUser :one
INSERT INTO users (username, display_name, email, password_hash, is_admin,
                   password_changed_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetUserByID :one
SELECT u.*,
       EXISTS (SELECT 1 FROM user_totp t WHERE t.user_id = u.id AND t.enabled_at IS NOT NULL) AS two_factor_enabled
FROM users u WHERE u.id = ?;

-- name: GetUserByUsername :one
SELECT u.*,
       EXISTS (SELECT 1 FROM user_totp t WHERE t.user_id = u.id AND t.enabled_at IS NOT NULL) AS two_factor_enabled
FROM users u WHERE u.username = ?;

-- name: GetUserByEmail :one
SELECT u.*,
       EXISTS (SELECT 1 FROM user_totp t WHERE t.user_id = u.id AND t.enabled_at IS NOT NULL) AS two_factor_enabled
FROM users u WHERE u.email = ?;

-- name: ListUsers :many
SELECT u.*,
       EXISTS (SELECT 1 FROM user_totp t WHERE t.user_id = u.id AND t.enabled_at IS NOT NULL) AS two_factor_enabled
FROM users u ORDER BY u.id;

-- name: UpdateUserProfile :exec
UPDATE users SET display_name = sqlc.arg(display_name), email = sqlc.narg(email), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id);

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = sqlc.arg(password_hash), password_changed_at = sqlc.arg(now), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id);

-- name: SetUserDisabled :exec
UPDATE users SET disabled_at = sqlc.narg(disabled_at), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id);

-- name: SetUserAdmin :exec
UPDATE users SET is_admin = sqlc.arg(is_admin), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id);

-- name: CountActiveAdmins :one
SELECT count(*) FROM users WHERE is_admin = 1 AND disabled_at IS NULL;

-- name: InsertWorkspace :one
INSERT INTO workspaces (name, kind, color, created_by, created_at) VALUES (?, ?, ?, ?, ?)
RETURNING id;

-- name: InsertWorkspaceMember :exec
INSERT INTO workspace_members (workspace_id, user_id, role, created_at) VALUES (?, ?, ?, ?);

-- name: GetPersonalWorkspace :one
SELECT w.id, w.name, w.kind, w.color, m.role
FROM workspaces AS w
JOIN workspace_members AS m ON m.workspace_id = w.id
WHERE m.user_id = ? AND w.kind = 'personal' AND w.deleted_at IS NULL
LIMIT 1;

-- Signing in counts as confirming the password (verified_at, v1.6.4).
-- name: InsertSession :exec
INSERT INTO sessions (id, user_id, csrf_token, ip, user_agent, created_at, last_seen_at, expires_at, persistent, verified_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- GetSessionPrincipal resolves a live session to its user and personal workspace.
-- name: GetSessionPrincipal :one
SELECT s.id AS session_id, s.csrf_token, s.last_seen_at, s.expires_at, s.persistent, s.verified_at,
       u.id AS user_id, u.username, u.display_name, u.is_admin,
       w.id AS workspace_id, w.name AS workspace_name, w.kind AS workspace_kind, w.color AS workspace_color, m.role
FROM sessions AS s
JOIN users AS u ON u.id = s.user_id
JOIN workspace_members AS m ON m.user_id = u.id
JOIN workspaces AS w ON w.id = m.workspace_id AND w.kind = 'personal' AND w.deleted_at IS NULL
WHERE s.id = sqlc.arg(id) AND s.expires_at > sqlc.arg(now) AND u.disabled_at IS NULL;

-- name: SetSessionVerified :exec
UPDATE sessions SET verified_at = sqlc.arg(now) WHERE id = sqlc.arg(id);

-- name: TouchSession :exec
UPDATE sessions SET last_seen_at = sqlc.arg(now), expires_at = sqlc.arg(expires_at)
WHERE id = sqlc.arg(id);

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = ?;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = ?;

-- name: DeleteUserSessionsExcept :exec
DELETE FROM sessions WHERE user_id = sqlc.arg(user_id) AND id != sqlc.arg(keep_id);

-- Revokes a user's API tokens and paired phones, except keep_id (0: none).
-- name: DeleteUserTokensExcept :execrows
DELETE FROM api_tokens WHERE user_id = sqlc.arg(user_id) AND id != sqlc.arg(keep_id);

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at <= ?;

-- name: InsertAPIToken :one
INSERT INTO api_tokens (user_id, name, token_hash, prefix, created_at) VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: ListAPITokens :many
SELECT * FROM api_tokens WHERE user_id = ? AND kind = 'personal' ORDER BY id DESC;

-- name: DeleteAPIToken :execrows
DELETE FROM api_tokens WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) AND kind = 'personal';

-- GetTokenPrincipal resolves an API token: a personal token acts in the
-- user's personal workspace, a device token in the workspace it was paired
-- for, with the user's current role there (no membership, no principal).
-- name: GetTokenPrincipal :one
SELECT t.id AS token_id, t.last_used_at, t.kind AS token_kind, t.access AS token_access, t.verified_at AS token_verified_at,
       u.id AS user_id, u.username, u.display_name, u.is_admin,
       w.id AS workspace_id, w.name AS workspace_name, w.kind AS workspace_kind, w.color AS workspace_color, m.role
FROM api_tokens AS t
JOIN users AS u ON u.id = t.user_id
JOIN workspace_members AS m ON m.user_id = u.id
JOIN workspaces AS w ON w.id = m.workspace_id AND w.deleted_at IS NULL
 AND ((t.kind = 'personal' AND w.kind = 'personal') OR (t.kind = 'device' AND w.id = t.workspace_id))
WHERE t.token_hash = ? AND u.disabled_at IS NULL;

-- A paired phone confirmed the password or a two-factor code (v1.7.20).
-- name: SetTokenVerified :exec
UPDATE api_tokens SET verified_at = sqlc.arg(now) WHERE id = sqlc.arg(id) AND kind = 'device';

-- name: TouchAPIToken :exec
UPDATE api_tokens SET last_used_at = sqlc.arg(now) WHERE id = sqlc.arg(id);

-- name: GetUserBySSO :one
SELECT u.*,
       EXISTS (SELECT 1 FROM user_totp t WHERE t.user_id = u.id AND t.enabled_at IS NOT NULL) AS two_factor_enabled
FROM users u WHERE u.sso_issuer = ? AND u.sso_subject = ?;

-- name: SetUserSSO :exec
UPDATE users SET sso_issuer = sqlc.narg(issuer), sso_subject = sqlc.narg(subject), updated_at = sqlc.arg(now) WHERE id = sqlc.arg(id);

-- name: SetUserLocale :exec
UPDATE users SET locale = sqlc.arg(locale), updated_at = sqlc.arg(now) WHERE id = sqlc.arg(id);

-- name: SetUserSeenLang :exec
UPDATE users SET seen_lang = sqlc.arg(lang) WHERE id = sqlc.arg(id) AND seen_lang <> sqlc.arg(lang);

-- The language to write to a person in (v1.10.2), by their email address.
-- name: GetUserLangByEmail :one
SELECT locale, seen_lang FROM users WHERE lower(email) = lower(sqlc.arg(email)) AND disabled_at IS NULL ORDER BY id LIMIT 1;
