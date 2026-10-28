-- name: ListBookLists :many
SELECT l.*, (SELECT count(*) FROM book_list_items i JOIN documents d ON d.id = i.document_id WHERE i.list_id = l.id AND d.deleted_at IS NULL
    AND (d.private = 0 OR d.person_id IN (SELECT pa.person_id FROM person_access AS pa WHERE pa.user_id = sqlc.arg(viewer)) OR (d.added_by = sqlc.arg(viewer) AND d.uploader_access = 1) OR sqlc.arg(viewer) = -1)) AS item_count
FROM book_lists l WHERE l.workspace_id = sqlc.arg(workspace_id) ORDER BY l.created_at, l.id;

-- name: GetBookList :one
SELECT * FROM book_lists WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: FindBookListByName :one
SELECT id FROM book_lists WHERE workspace_id = sqlc.arg(workspace_id) AND name = sqlc.arg(name);

-- name: InsertBookList :one
INSERT INTO book_lists (workspace_id, name, description, created_by, created_at, updated_at)
VALUES (sqlc.arg(workspace_id), sqlc.arg(name), sqlc.arg(description), sqlc.narg(created_by), sqlc.arg(now), sqlc.arg(now))
RETURNING *;

-- name: UpdateBookList :exec
UPDATE book_lists SET name = sqlc.arg(name), description = sqlc.arg(description), updated_at = sqlc.arg(now)
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: TouchBookList :exec
UPDATE book_lists SET updated_at = sqlc.arg(now) WHERE id = sqlc.arg(id);

-- name: DeleteBookList :execrows
DELETE FROM book_lists WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: ListBookListItems :many
SELECT i.document_id, i.position, i.note, i.added_at, d.title, d.has_thumbnail, b.authors, b.cover
FROM book_list_items i JOIN documents d ON d.id = i.document_id JOIN books b ON b.document_id = i.document_id
WHERE i.list_id = sqlc.arg(list_id) AND d.deleted_at IS NULL
  -- viewer: private documents (v1.8.0), as privacy.Visible
  AND (d.private = 0 OR d.person_id IN (SELECT pa.person_id FROM person_access AS pa WHERE pa.user_id = sqlc.arg(viewer)) OR (d.added_by = sqlc.arg(viewer) AND d.uploader_access = 1) OR sqlc.arg(viewer) = -1)
ORDER BY i.position, i.added_at;

-- name: GetBookListItem :one
SELECT * FROM book_list_items WHERE list_id = sqlc.arg(list_id) AND document_id = sqlc.arg(document_id);

-- name: MaxBookListPosition :one
SELECT CAST(coalesce(max(position), 0) AS INTEGER) FROM book_list_items WHERE list_id = ?;

-- name: InsertBookListItem :exec
INSERT INTO book_list_items (list_id, document_id, position, note, added_at)
VALUES (sqlc.arg(list_id), sqlc.arg(document_id), sqlc.arg(position), sqlc.arg(note), sqlc.arg(now));

-- name: SetBookListItemNote :execrows
UPDATE book_list_items SET note = sqlc.arg(note) WHERE list_id = sqlc.arg(list_id) AND document_id = sqlc.arg(document_id);

-- name: SetBookListItemPosition :exec
UPDATE book_list_items SET position = sqlc.arg(position) WHERE list_id = sqlc.arg(list_id) AND document_id = sqlc.arg(document_id);

-- name: DeleteBookListItem :execrows
DELETE FROM book_list_items WHERE list_id = sqlc.arg(list_id) AND document_id = sqlc.arg(document_id);

-- name: ListsOfDocument :many
SELECT i.list_id FROM book_list_items i JOIN book_lists l ON l.id = i.list_id WHERE l.workspace_id = sqlc.arg(workspace_id) AND i.document_id = sqlc.arg(document_id);

-- name: FinishedBooks :many
SELECT d.id, d.title, d.has_thumbnail, b.cover, r.finished_on, r.rating
FROM reading_progress r JOIN documents d ON d.id = r.document_id JOIN books b ON b.document_id = r.document_id
WHERE r.user_id = sqlc.arg(user_id) AND r.workspace_id = sqlc.arg(workspace_id) AND r.status = 'finished'
  AND r.finished_on >= sqlc.arg(from_date) AND r.finished_on < sqlc.arg(to_date) AND d.deleted_at IS NULL
ORDER BY r.finished_on, d.id;
