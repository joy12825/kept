-- name: ListStaleFingerprints :many
SELECT id, content, index_rev FROM documents
WHERE workspace_id = ? AND deleted_at IS NULL AND simhash_rev != index_rev
LIMIT 500;

-- name: SetFingerprint :exec
UPDATE documents SET simhash = sqlc.narg(simhash), simhash_rev = sqlc.arg(rev)
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: ListFingerprints :many
SELECT id, CAST(simhash AS INTEGER) AS simhash FROM documents
WHERE workspace_id = ? AND deleted_at IS NULL AND simhash IS NOT NULL
ORDER BY id;

-- name: ListDuplicatePairs :many
SELECT * FROM duplicate_pairs WHERE workspace_id = ?;

-- name: UpsertDuplicatePair :exec
INSERT INTO duplicate_pairs (workspace_id, doc_a, doc_b, distance, status, found_at)
VALUES (sqlc.arg(workspace_id), sqlc.arg(doc_a), sqlc.arg(doc_b), sqlc.arg(distance), 'open', sqlc.arg(now))
ON CONFLICT (doc_a, doc_b) DO UPDATE SET distance = excluded.distance;

-- name: DeleteDuplicatePair :exec
DELETE FROM duplicate_pairs WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: GetDuplicatePair :one
SELECT * FROM duplicate_pairs WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: DismissDuplicatePair :execrows
UPDATE duplicate_pairs SET status = 'dismissed' WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id) AND status = 'open';

-- name: ListOpenDuplicatePairs :many
SELECT p.* FROM duplicate_pairs AS p
JOIN documents AS a ON a.id = p.doc_a AND a.deleted_at IS NULL
JOIN documents AS b ON b.id = p.doc_b AND b.deleted_at IS NULL
WHERE p.workspace_id = ? AND p.status = 'open'
ORDER BY p.distance, p.id;

-- name: GetDocumentContent :one
SELECT content FROM documents WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);
