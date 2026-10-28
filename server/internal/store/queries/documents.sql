-- name: InsertDocument :one
INSERT INTO documents (workspace_id, title, title_sort, content, created_date, added_at, modified_at,
                       correspondent_id, document_type_id, mime_type, original_filename, original_ext,
                       page_count, checksum, archive_checksum, has_archive, has_thumbnail, inbox, field_sources, source, original_encrypted, person_id, storage, original_size, derived_size, added_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: GetDocument :one
-- viewer: the visibility condition of private documents (v1.8.0); keep it
-- in step with privacy.Visible.
SELECT * FROM documents
WHERE documents.workspace_id = sqlc.arg(workspace_id) AND documents.id = sqlc.arg(id) AND documents.deleted_at IS NULL
  AND (private = 0
       OR person_id IN (SELECT pa.person_id FROM person_access AS pa WHERE pa.user_id = sqlc.arg(viewer))
       OR (added_by = sqlc.arg(viewer) AND uploader_access = 1)
       OR sqlc.arg(viewer) = -1);

-- Duplicate check: includes the trash, so a deleted file is not uploaded twice.
-- name: FindDocumentByChecksum :one
SELECT id, deleted_at FROM documents WHERE workspace_id = sqlc.arg(workspace_id) AND checksum = sqlc.arg(checksum);

-- name: ListInboxDocuments :many
SELECT * FROM documents
WHERE workspace_id = ? AND inbox = 1 AND deleted_at IS NULL
ORDER BY added_at, id;

-- name: UpdateDocumentFiles :exec
UPDATE documents
SET content = sqlc.arg(content), page_count = sqlc.arg(page_count), has_archive = sqlc.arg(has_archive),
    archive_checksum = sqlc.narg(archive_checksum), has_thumbnail = sqlc.arg(has_thumbnail), modified_at = sqlc.arg(modified_at),
    index_rev = index_rev + 1, content_edited_at = NULL, derived_size = NULL
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- Metadata set by the demo loader (milestone 1.5 brings general editing).
-- name: SetDocumentMeta :exec
UPDATE documents
SET title = sqlc.arg(title), title_sort = sqlc.arg(title_sort), created_date = sqlc.arg(created_date), correspondent_id = sqlc.narg(correspondent_id),
    document_type_id = sqlc.narg(document_type_id), asn = sqlc.narg(asn), inbox = sqlc.arg(inbox), modified_at = sqlc.arg(modified_at),
    index_rev = index_rev + 1
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: AddDocumentTag :exec
INSERT OR IGNORE INTO document_tags (document_id, tag_id) VALUES (?, ?);

-- name: SetMonetaryValue :exec
INSERT INTO custom_field_values (document_id, field_id, value_monetary) VALUES (?, ?, ?)
ON CONFLICT (document_id, field_id) DO UPDATE SET value_monetary = excluded.value_monetary;

-- name: FindDocumentByTitle :one
SELECT id FROM documents WHERE workspace_id = sqlc.arg(workspace_id) AND title = sqlc.arg(title) LIMIT 1;

-- name: GetBuiltInField :one
SELECT id FROM custom_fields WHERE workspace_id = sqlc.arg(workspace_id) AND key = sqlc.arg(key);

-- Rows written before migration 0009 have no title sort key yet.
-- name: ListDocumentsWithoutTitleSort :many
SELECT id, title FROM documents WHERE title_sort = '' AND title <> '' LIMIT 500;

-- name: SetDocumentTitleSort :exec
UPDATE documents SET title_sort = ? WHERE id = ?;

-- name: GetDocumentAnyState :one
SELECT * FROM documents WHERE documents.workspace_id = sqlc.arg(workspace_id) AND documents.id = sqlc.arg(id)
  -- viewer: private documents (v1.8.0), as privacy.Visible
  AND (documents.private = 0 OR documents.person_id IN (SELECT pa.person_id FROM person_access AS pa WHERE pa.user_id = sqlc.arg(viewer)) OR (documents.added_by = sqlc.arg(viewer) AND documents.uploader_access = 1) OR sqlc.arg(viewer) = -1);

-- name: UpdateDocumentFields :exec
UPDATE documents
SET title = sqlc.arg(title), title_sort = sqlc.arg(title_sort), created_date = sqlc.arg(created_date),
    document_type_id = sqlc.narg(document_type_id), correspondent_id = sqlc.narg(correspondent_id),
    person_id = sqlc.narg(person_id), location_id = sqlc.narg(location_id),
    asn = sqlc.narg(asn), inbox = sqlc.arg(inbox), modified_at = sqlc.arg(modified_at),
    index_rev = index_rev + 1
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- The ASN is unique per workspace, the trash included.
-- name: FindDocumentByASN :one
SELECT id, title, deleted_at FROM documents WHERE workspace_id = sqlc.arg(workspace_id) AND asn = sqlc.arg(asn);

-- name: ListDocumentTagIDs :many
SELECT tag_id FROM document_tags WHERE document_id = ? ORDER BY tag_id;

-- name: RemoveDocumentTag :exec
DELETE FROM document_tags WHERE document_id = ? AND tag_id = ?;

-- name: ListDocumentFieldValues :many
SELECT * FROM custom_field_values WHERE document_id = ?;

-- name: DeleteFieldValue :exec
DELETE FROM custom_field_values WHERE document_id = ? AND field_id = ?;

-- name: UpsertFieldValue :exec
INSERT INTO custom_field_values (document_id, field_id, value_text, value_int, value_float, value_bool, value_date,
                                 value_monetary, value_select, value_doclink)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (document_id, field_id) DO UPDATE SET
    value_text = excluded.value_text, value_int = excluded.value_int, value_float = excluded.value_float,
    value_bool = excluded.value_bool, value_date = excluded.value_date, value_monetary = excluded.value_monetary,
    value_select = excluded.value_select, value_doclink = excluded.value_doclink;

-- name: SetDocumentDeleted :exec
UPDATE documents SET deleted_at = sqlc.narg(deleted_at), modified_at = sqlc.arg(modified_at), index_rev = index_rev + 1
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: ListTrash :many
SELECT * FROM documents WHERE documents.workspace_id = sqlc.arg(workspace_id) AND documents.deleted_at IS NOT NULL
  -- viewer: private documents (v1.8.0), as privacy.Visible
  AND (documents.private = 0 OR documents.person_id IN (SELECT pa.person_id FROM person_access AS pa WHERE pa.user_id = sqlc.arg(viewer)) OR (documents.added_by = sqlc.arg(viewer) AND documents.uploader_access = 1) OR sqlc.arg(viewer) = -1)
ORDER BY deleted_at DESC, id DESC;

-- name: ListTrashIDs :many
SELECT id FROM documents WHERE documents.workspace_id = sqlc.arg(workspace_id) AND documents.deleted_at IS NOT NULL
  -- viewer: private documents (v1.8.0), as privacy.Visible
  AND (documents.private = 0 OR documents.person_id IN (SELECT pa.person_id FROM person_access AS pa WHERE pa.user_id = sqlc.arg(viewer)) OR (documents.added_by = sqlc.arg(viewer) AND documents.uploader_access = 1) OR sqlc.arg(viewer) = -1);

-- name: ListExpiredTrash :many
SELECT workspace_id, id FROM documents WHERE deleted_at IS NOT NULL AND deleted_at < ? ORDER BY workspace_id, id;

-- Links from other documents to one being purged would keep an empty row.
-- name: DeleteLinksTo :exec
DELETE FROM custom_field_values WHERE value_doclink = ?;

-- name: DeleteDocument :exec
DELETE FROM documents WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- Something the index holds changed outside the documents row (notes,
-- tags added after the row was written).
-- name: BumpIndexRev :exec
UPDATE documents SET index_rev = index_rev + 1 WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- A correspondent or document type was renamed or deleted.
-- name: BumpIndexRevByCorrespondent :exec
UPDATE documents SET index_rev = index_rev + 1 WHERE workspace_id = sqlc.arg(workspace_id) AND correspondent_id = sqlc.arg(id);

-- name: BumpIndexRevByDocumentType :exec
UPDATE documents SET index_rev = index_rev + 1 WHERE workspace_id = sqlc.arg(workspace_id) AND document_type_id = sqlc.arg(id);

-- name: BumpIndexRevByPerson :exec
UPDATE documents SET index_rev = index_rev + 1 WHERE workspace_id = sqlc.arg(workspace_id) AND person_id = sqlc.arg(id);

-- A tag was renamed, moved or deleted: its path changes for every document
-- on it (callers bump each tag of the subtree).
-- name: BumpIndexRevByTag :exec
UPDATE documents SET index_rev = index_rev + 1
WHERE workspace_id = sqlc.arg(workspace_id) AND id IN (SELECT dt.document_id FROM document_tags dt WHERE dt.tag_id = sqlc.arg(id));

-- A custom field is deleted: its values leave the index.
-- name: BumpIndexRevByField :exec
UPDATE documents SET index_rev = index_rev + 1
WHERE workspace_id = sqlc.arg(workspace_id) AND id IN (SELECT v.document_id FROM custom_field_values v WHERE v.field_id = sqlc.arg(id));

-- name: SetDocumentFieldSources :exec
UPDATE documents SET field_sources = ? WHERE workspace_id = ? AND id = ?;

-- Recognised text corrected by hand (v1.3.7); reindexed like any change.
-- name: SetDocumentContent :exec
UPDATE documents SET content = ?, content_edited_at = ?, index_rev = index_rev + 1 WHERE workspace_id = ? AND id = ?;

-- name: SetDocumentMilestone :exec
UPDATE documents SET is_milestone = ? WHERE workspace_id = ? AND id = ?;

-- name: SetDocumentPages :exec
UPDATE documents
SET content = sqlc.arg(content), page_count = sqlc.arg(page_count), has_archive = sqlc.arg(has_archive),
    archive_checksum = sqlc.narg(archive_checksum), has_thumbnail = sqlc.arg(has_thumbnail), page_edits = sqlc.arg(page_edits),
    modified_at = sqlc.arg(modified_at), index_rev = index_rev + 1, content_edited_at = NULL, derived_size = NULL
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- An archive serial number from an ASN barcode, only if no document of the
-- workspace has it yet.
-- name: SetDocumentASNIfFree :execrows
UPDATE documents SET asn = sqlc.arg(asn)
WHERE documents.workspace_id = sqlc.arg(workspace_id) AND documents.id = sqlc.arg(id)
  AND NOT EXISTS (SELECT 1 FROM documents AS o WHERE o.workspace_id = sqlc.arg(workspace_id) AND o.asn = sqlc.arg(asn));

-- v1.6.4
-- name: SetDocumentSensitive :exec
UPDATE documents SET sensitive = sqlc.arg(sensitive), index_rev = index_rev + 1 WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- v1.8.0
-- name: SetDocumentPrivate :exec
UPDATE documents SET private = sqlc.arg(private), private_manual = sqlc.arg(private_manual)
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);
