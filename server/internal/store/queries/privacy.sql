-- v1.8.0 private documents: who may see a person's private documents.

-- name: ListPersonAccess :many
SELECT a.person_id, a.user_id, a.level, a.via, a.reviewed, a.created_at, u.display_name, u.username
FROM person_access AS a
JOIN users AS u ON u.id = a.user_id
JOIN workspace_members AS m ON m.workspace_id = a.workspace_id AND m.user_id = a.user_id
WHERE a.workspace_id = sqlc.arg(workspace_id)
ORDER BY a.person_id, CASE a.level WHEN 'self' THEN 0 WHEN 'guardian' THEN 1 WHEN 'manage' THEN 2 ELSE 3 END, u.display_name;

-- name: GetPersonAccess :one
SELECT a.level FROM person_access AS a
JOIN workspace_members AS m ON m.workspace_id = a.workspace_id AND m.user_id = a.user_id
WHERE a.person_id = sqlc.arg(person_id) AND a.user_id = sqlc.arg(user_id);

-- name: UpsertPersonAccess :exec
INSERT INTO person_access (person_id, user_id, workspace_id, level, via, created_by, reviewed, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (person_id, user_id) DO UPDATE SET level = excluded.level, via = excluded.via,
    created_by = excluded.created_by, reviewed = excluded.reviewed, created_at = excluded.created_at;

-- name: DeletePersonAccess :execrows
DELETE FROM person_access WHERE person_id = sqlc.arg(person_id) AND user_id = sqlc.arg(user_id);

-- name: DeleteMemberPersonAccess :exec
DELETE FROM person_access WHERE workspace_id = sqlc.arg(workspace_id) AND user_id = sqlc.arg(user_id);

-- name: DeleteMemberPersonEmergency :exec
DELETE FROM person_emergency WHERE workspace_id = sqlc.arg(workspace_id) AND user_id = sqlc.arg(user_id);

-- name: SetPersonAccessReviewed :exec
UPDATE person_access SET reviewed = sqlc.arg(reviewed) WHERE person_id = sqlc.arg(person_id) AND level = 'self';

-- name: CountPrivateDocuments :one
SELECT COUNT(*) FROM documents WHERE person_id = sqlc.arg(person_id) AND private = 1;

-- name: PrivateUsage :one
SELECT COUNT(*) AS n, CAST(COALESCE(SUM(COALESCE(original_size, 0) + COALESCE(derived_size, 0)), 0) AS INTEGER) AS bytes
FROM documents WHERE workspace_id = sqlc.arg(workspace_id) AND private = 1 AND deleted_at IS NULL;

-- name: ListPersonEmergency :many
SELECT e.*, u.display_name, u.username FROM person_emergency AS e
JOIN users AS u ON u.id = e.user_id
JOIN workspace_members AS m ON m.workspace_id = e.workspace_id AND m.user_id = e.user_id
WHERE e.workspace_id = sqlc.arg(workspace_id)
ORDER BY e.person_id, u.display_name;

-- name: GetPersonEmergency :one
SELECT * FROM person_emergency WHERE person_id = sqlc.arg(person_id) AND user_id = sqlc.arg(user_id);

-- name: UpsertPersonEmergency :exec
INSERT INTO person_emergency (person_id, user_id, workspace_id, wait_hours, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (person_id, user_id) DO UPDATE SET wait_hours = excluded.wait_hours, updated_at = excluded.updated_at;

-- name: DeletePersonEmergency :execrows
DELETE FROM person_emergency WHERE person_id = sqlc.arg(person_id) AND user_id = sqlc.arg(user_id);

-- name: SetPersonEmergencyState :exec
UPDATE person_emergency SET status = sqlc.arg(status), reason = sqlc.arg(reason), requested_at = sqlc.arg(requested_at),
    grant_at = sqlc.arg(grant_at), granted_at = sqlc.arg(granted_at), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: DuePersonEmergency :many
SELECT * FROM person_emergency WHERE status = 'pending' AND grant_at <= sqlc.arg(now);

-- name: ListUploaderAccess :many
SELECT d.person_id, d.added_by, u.display_name, COUNT(*) AS n
FROM documents AS d
JOIN users AS u ON u.id = d.added_by
WHERE d.workspace_id = sqlc.arg(workspace_id) AND d.private = 1 AND d.uploader_access = 1 AND d.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM person_access AS a WHERE a.person_id = d.person_id AND a.user_id = d.added_by)
GROUP BY d.person_id, d.added_by, u.display_name
ORDER BY d.person_id, u.display_name;

-- name: RevokeUploaderAccess :execrows
UPDATE documents SET uploader_access = 0
WHERE workspace_id = sqlc.arg(workspace_id) AND person_id = sqlc.arg(person_id) AND added_by = sqlc.arg(added_by) AND private = 1;

-- name: ListPrivateTypes :many
SELECT document_type_id FROM type_private WHERE workspace_id = sqlc.arg(workspace_id);

-- name: IsPrivateType :one
SELECT COUNT(*) FROM type_private WHERE document_type_id = sqlc.arg(document_type_id);

-- name: SetPrivateType :exec
INSERT INTO type_private (document_type_id, workspace_id) VALUES (?, ?) ON CONFLICT (document_type_id) DO NOTHING;

-- name: UnsetPrivateType :exec
DELETE FROM type_private WHERE document_type_id = sqlc.arg(document_type_id);

-- name: GetSelfPerson :one
SELECT person_id FROM person_access WHERE workspace_id = sqlc.arg(workspace_id) AND user_id = sqlc.arg(user_id) AND level = 'self';

-- name: GetPersonAccessVia :one
SELECT via FROM person_access WHERE person_id = sqlc.arg(person_id) AND user_id = sqlc.arg(user_id);

-- v1.8.1: a person's keys.

-- name: GetPersonKey :one
SELECT * FROM person_keys WHERE person_id = sqlc.arg(person_id);

-- name: InsertPersonKey :exec
INSERT INTO person_keys (person_id, workspace_id, public_key, recovery, created_by, created_at, recovery_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: SetPersonKeyRecovery :exec
UPDATE person_keys SET recovery = sqlc.arg(recovery), recovery_at = sqlc.arg(recovery_at) WHERE person_id = sqlc.arg(person_id);

-- name: ListPersonKeys :many
SELECT person_id, public_key, created_at, recovery_at FROM person_keys WHERE workspace_id = sqlc.arg(workspace_id);

-- name: GetPersonKeyWrap :one
SELECT wrapped FROM person_key_wraps WHERE person_id = sqlc.arg(person_id) AND user_id = sqlc.arg(user_id);

-- name: UpsertPersonKeyWrap :exec
INSERT INTO person_key_wraps (person_id, user_id, wrapped, created_at) VALUES (?, ?, ?, ?)
ON CONFLICT (person_id, user_id) DO UPDATE SET wrapped = excluded.wrapped, created_at = excluded.created_at;

-- name: ListUserKeyWraps :many
SELECT person_id, wrapped FROM person_key_wraps WHERE user_id = sqlc.arg(user_id);

-- name: DeleteUserKeyWraps :exec
DELETE FROM person_key_wraps WHERE user_id = sqlc.arg(user_id);

-- name: GetPersonAI :one
SELECT cloud_ok FROM person_ai WHERE person_id = sqlc.arg(person_id);

-- name: SetPersonAI :exec
INSERT INTO person_ai (person_id, cloud_ok, decided_by, decided_at) VALUES (?, ?, ?, ?)
ON CONFLICT (person_id) DO UPDATE SET cloud_ok = excluded.cloud_ok, decided_by = excluded.decided_by, decided_at = excluded.decided_at;

-- name: BumpPrivateIndexRev :execrows
UPDATE documents SET index_rev = index_rev + 1 WHERE person_id = sqlc.arg(person_id) AND private = 1;
