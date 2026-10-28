-- Filed documents of a workspace, the classifier's training data.
-- name: ListTrainingDocuments :many
SELECT id, title, content, document_type_id, correspondent_id, person_id, modified_at
FROM documents
WHERE workspace_id = ? AND inbox = 0 AND deleted_at IS NULL
ORDER BY id;

-- name: ListTrainingTags :many
SELECT dt.document_id, dt.tag_id
FROM document_tags AS dt JOIN documents AS d ON d.id = dt.document_id
WHERE d.workspace_id = ? AND d.inbox = 0 AND d.deleted_at IS NULL
ORDER BY dt.document_id, dt.tag_id;

-- The matching rules of a workspace's types, correspondents and tags.
-- name: ListTypeRules :many
SELECT id, name, algorithm, match, case_insensitive FROM document_types WHERE workspace_id = ? ORDER BY id;

-- name: ListCorrespondentRules :many
SELECT id, name, algorithm, match, case_insensitive FROM correspondents WHERE workspace_id = ? ORDER BY id;

-- name: ListPersonRules :many
SELECT id, name, algorithm, match, case_insensitive FROM persons WHERE workspace_id = ? ORDER BY id;

-- name: ListTagRules :many
SELECT id, name, algorithm, match, case_insensitive FROM tags WHERE workspace_id = ? ORDER BY id;

-- name: GetClassifierModel :one
SELECT * FROM classifier_models WHERE workspace_id = ?;

-- name: GetClassifierSummary :one
SELECT workspace_id, format_version, trained_at, data_hash, doc_count, summary
FROM classifier_models WHERE workspace_id = ?;

-- name: SaveClassifierModel :exec
INSERT INTO classifier_models (workspace_id, format_version, trained_at, data_hash, doc_count, summary, blob)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (workspace_id) DO UPDATE SET
    format_version = excluded.format_version, trained_at = excluded.trained_at, data_hash = excluded.data_hash,
    doc_count = excluded.doc_count, summary = excluded.summary, blob = excluded.blob;
