-- Taxonomy queries. Every query is scoped by workspace_id.

-- name: ListCorrespondents :many
SELECT * FROM correspondents WHERE workspace_id = ? ORDER BY sort_key, name, id;

-- name: GetCorrespondent :one
SELECT * FROM correspondents WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- Name comparison follows the column's NOCASE collation.
-- name: FindCorrespondentByName :one
SELECT id FROM correspondents WHERE workspace_id = sqlc.arg(workspace_id) AND name = sqlc.arg(name);

-- name: InsertCorrespondent :one
INSERT INTO correspondents (workspace_id, name, sort_key, match, algorithm, case_insensitive, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateCorrespondent :exec
UPDATE correspondents SET name = sqlc.arg(name), sort_key = sqlc.arg(sort_key), match = sqlc.arg(match),
       algorithm = sqlc.arg(algorithm), case_insensitive = sqlc.arg(case_insensitive), updated_at = sqlc.arg(updated_at)
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: DeleteCorrespondent :exec
DELETE FROM correspondents WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: ListDocumentTypes :many
SELECT * FROM document_types WHERE workspace_id = ? ORDER BY sort_key, name, id;

-- name: GetDocumentType :one
SELECT * FROM document_types WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- Name comparison follows the column's NOCASE collation.
-- name: FindDocumentTypeByName :one
SELECT id FROM document_types WHERE workspace_id = sqlc.arg(workspace_id) AND name = sqlc.arg(name);

-- name: InsertDocumentType :one
INSERT INTO document_types (workspace_id, name, sort_key, match, algorithm, case_insensitive, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateDocumentType :exec
UPDATE document_types SET name = sqlc.arg(name), sort_key = sqlc.arg(sort_key), match = sqlc.arg(match),
       algorithm = sqlc.arg(algorithm), case_insensitive = sqlc.arg(case_insensitive), updated_at = sqlc.arg(updated_at)
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: DeleteDocumentType :exec
DELETE FROM document_types WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: ListPersons :many
SELECT * FROM persons WHERE workspace_id = ? ORDER BY sort_key, name, id;

-- name: GetPerson :one
SELECT * FROM persons WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- Name comparison follows the column's NOCASE collation.
-- name: FindPersonByName :one
SELECT id FROM persons WHERE workspace_id = sqlc.arg(workspace_id) AND name = sqlc.arg(name);

-- name: InsertPerson :one
INSERT INTO persons (workspace_id, name, sort_key, match, algorithm, case_insensitive, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdatePerson :exec
UPDATE persons SET name = sqlc.arg(name), sort_key = sqlc.arg(sort_key), match = sqlc.arg(match),
       algorithm = sqlc.arg(algorithm), case_insensitive = sqlc.arg(case_insensitive), updated_at = sqlc.arg(updated_at)
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: DeletePerson :exec
DELETE FROM persons WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: ListLocations :many
SELECT * FROM locations WHERE workspace_id = ? ORDER BY sort_key, name, id;

-- name: GetLocation :one
SELECT * FROM locations WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: FindLocationByName :one
SELECT id FROM locations WHERE workspace_id = sqlc.arg(workspace_id) AND name = sqlc.arg(name);

-- name: InsertLocation :one
INSERT INTO locations (workspace_id, name, sort_key, note, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateLocation :exec
UPDATE locations SET name = sqlc.arg(name), sort_key = sqlc.arg(sort_key), note = sqlc.arg(note), updated_at = sqlc.arg(updated_at)
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: DeleteLocation :exec
DELETE FROM locations WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- How many documents each location holds (non-deleted).
-- name: CountDocumentsByLocation :many
SELECT location_id, count(*) AS n FROM documents
WHERE documents.workspace_id = sqlc.arg(workspace_id) AND documents.deleted_at IS NULL AND documents.location_id IS NOT NULL
  -- viewer: private documents (v1.8.0), as privacy.Visible
  AND (documents.private = 0 OR documents.person_id IN (SELECT pa.person_id FROM person_access AS pa WHERE pa.user_id = sqlc.arg(viewer)) OR (documents.added_by = sqlc.arg(viewer) AND documents.uploader_access = 1) OR sqlc.arg(viewer) = -1)
GROUP BY location_id;

-- Puts the originals numbered from..to in a location (or none).
-- name: AssignLocationByASN :execrows
UPDATE documents SET location_id = sqlc.narg(location_id), modified_at = sqlc.arg(modified_at)
WHERE documents.workspace_id = sqlc.arg(workspace_id) AND documents.deleted_at IS NULL AND documents.asn >= sqlc.arg(asn_from) AND documents.asn <= sqlc.arg(asn_to)
  -- viewer: private documents (v1.8.0), as privacy.Visible
  AND (documents.private = 0 OR documents.person_id IN (SELECT pa.person_id FROM person_access AS pa WHERE pa.user_id = sqlc.arg(viewer)) OR (documents.added_by = sqlc.arg(viewer) AND documents.uploader_access = 1) OR sqlc.arg(viewer) = -1);

-- name: ListTags :many
SELECT * FROM tags WHERE workspace_id = ? ORDER BY sort_key, name, id;

-- name: GetTag :one
SELECT * FROM tags WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: FindTagByName :one
SELECT id FROM tags WHERE workspace_id = sqlc.arg(workspace_id) AND name = sqlc.arg(name);

-- name: InsertTag :one
INSERT INTO tags (workspace_id, name, sort_key, color, parent_id, match, algorithm, case_insensitive, show_in_yearbook, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateTag :exec
UPDATE tags SET name = sqlc.arg(name), sort_key = sqlc.arg(sort_key), color = sqlc.arg(color),
       parent_id = sqlc.narg(parent_id), match = sqlc.arg(match), algorithm = sqlc.arg(algorithm),
       case_insensitive = sqlc.arg(case_insensitive), show_in_yearbook = sqlc.arg(show_in_yearbook), updated_at = sqlc.arg(updated_at)
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- Moves the children of a tag that is about to be deleted up one level.
-- name: ReparentTagChildren :execrows
UPDATE tags SET parent_id = sqlc.narg(new_parent_id), updated_at = sqlc.arg(updated_at)
WHERE workspace_id = sqlc.arg(workspace_id) AND parent_id = sqlc.arg(old_parent_id);

-- name: DeleteTag :exec
DELETE FROM tags WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: ListCustomFields :many
SELECT * FROM custom_fields WHERE workspace_id = ? ORDER BY sort_key, name, id;

-- name: GetCustomField :one
SELECT * FROM custom_fields WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: FindCustomFieldByName :one
SELECT id FROM custom_fields WHERE workspace_id = sqlc.arg(workspace_id) AND name = sqlc.arg(name);

-- name: InsertCustomField :one
INSERT INTO custom_fields (workspace_id, key, name, sort_key, data_type, extra, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateCustomField :exec
UPDATE custom_fields SET name = sqlc.arg(name), sort_key = sqlc.arg(sort_key), extra = sqlc.arg(extra), updated_at = sqlc.arg(updated_at)
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: DeleteCustomField :exec
DELETE FROM custom_fields WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- v1.6.5
-- name: ListTypeRetention :many
SELECT document_type_id, months FROM type_retention WHERE workspace_id = ?;

-- name: SetTypeRetention :exec
INSERT INTO type_retention (document_type_id, workspace_id, months) VALUES (?, ?, ?)
ON CONFLICT (document_type_id) DO UPDATE SET months = excluded.months;

-- name: ClearTypeRetention :exec
DELETE FROM type_retention WHERE workspace_id = ? AND document_type_id = ?;

-- Documents pinned in drawer facts: never suggested for clean-up.
-- name: ListPinnedFactConfigs :many
SELECT f.config FROM drawer_facts AS f JOIN drawers AS dr ON dr.id = f.drawer_id WHERE dr.workspace_id = ? AND f.kind = 'pinned';
