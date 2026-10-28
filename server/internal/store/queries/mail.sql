-- Mail accounts (v0.2), always within one workspace.

-- name: ListMailAccounts :many
SELECT * FROM mail_accounts WHERE workspace_id = ? ORDER BY id;

-- name: GetMailAccount :one
SELECT * FROM mail_accounts WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: InsertMailAccount :one
INSERT INTO mail_accounts (workspace_id, name, host, port, security, username, password, folder, filter_from, filter_subject,
                           document_type_id, correspondent_id, tag_ids, after_action, move_to, body_mode,
                           attach_include, attach_exclude, correspondent_from_sender, filter_to, filter_body, max_age_days, title_from,
                           enabled, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateMailAccount :exec
UPDATE mail_accounts
SET name = sqlc.arg(name), host = sqlc.arg(host), port = sqlc.arg(port), security = sqlc.arg(security),
    username = sqlc.arg(username), password = sqlc.arg(password), folder = sqlc.arg(folder),
    filter_from = sqlc.arg(filter_from), filter_subject = sqlc.arg(filter_subject),
    document_type_id = sqlc.narg(document_type_id), correspondent_id = sqlc.narg(correspondent_id), tag_ids = sqlc.arg(tag_ids),
    after_action = sqlc.arg(after_action), move_to = sqlc.arg(move_to), body_mode = sqlc.arg(body_mode),
    attach_include = sqlc.arg(attach_include), attach_exclude = sqlc.arg(attach_exclude), correspondent_from_sender = sqlc.arg(correspondent_from_sender),
    filter_to = sqlc.arg(filter_to), filter_body = sqlc.arg(filter_body), max_age_days = sqlc.arg(max_age_days), title_from = sqlc.arg(title_from), enabled = sqlc.arg(enabled), updated_at = sqlc.arg(updated_at)
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: DeleteMailAccount :execrows
DELETE FROM mail_accounts WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: SetMailAccountChecked :exec
UPDATE mail_accounts SET checked_at = sqlc.arg(checked_at), last_error = sqlc.arg(last_error), last_count = sqlc.arg(last_count)
WHERE id = sqlc.arg(id);

-- name: MailSeen :one
SELECT count(*) FROM mail_seen WHERE account_id = ? AND uid_validity = ? AND uid = ?;

-- name: MarkMailSeen :exec
INSERT OR IGNORE INTO mail_seen (account_id, uid_validity, uid, seen_at, sender, subject, sent_at, status, reason, taken, action)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListMailSeen :many
SELECT uid_validity, uid, seen_at, sender, subject, sent_at, status, reason, taken, action FROM mail_seen
WHERE account_id = ? AND status != '' ORDER BY seen_at DESC, uid DESC LIMIT 100;

-- name: GetMailSeen :one
SELECT status, action FROM mail_seen WHERE account_id = ? AND uid_validity = ? AND uid = ?;

-- name: ForgetMailSeen :execrows
DELETE FROM mail_seen WHERE account_id = ? AND uid_validity = ? AND uid = ?;
