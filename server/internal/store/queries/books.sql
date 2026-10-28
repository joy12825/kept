-- name: GetBook :one
SELECT * FROM books WHERE workspace_id = sqlc.arg(workspace_id) AND document_id = sqlc.arg(document_id);

-- name: UpsertBook :exec
INSERT INTO books (document_id, workspace_id, isbn, authors, publisher, series, edition, published, pages, language, description, updated_at)
VALUES (sqlc.arg(document_id), sqlc.arg(workspace_id), sqlc.arg(isbn), sqlc.arg(authors), sqlc.arg(publisher), sqlc.arg(series), sqlc.arg(edition),
        sqlc.arg(published), sqlc.arg(pages), sqlc.arg(language), sqlc.arg(description), sqlc.arg(now))
ON CONFLICT (document_id) DO UPDATE SET isbn = excluded.isbn, authors = excluded.authors, publisher = excluded.publisher, series = excluded.series,
    edition = excluded.edition, published = excluded.published, pages = excluded.pages, language = excluded.language,
    description = excluded.description, updated_at = excluded.updated_at;

-- name: DeleteBook :execrows
DELETE FROM books WHERE workspace_id = sqlc.arg(workspace_id) AND document_id = sqlc.arg(document_id);

-- name: FindBookByISBN :one
SELECT b.document_id, d.title, d.deleted_at FROM books AS b JOIN documents AS d ON d.id = b.document_id
WHERE b.workspace_id = sqlc.arg(workspace_id) AND b.isbn = sqlc.arg(isbn);

-- name: GetLibraryDrawer :one
SELECT * FROM drawers WHERE workspace_id = ? AND kind = 'library';

-- name: SetDrawerKind :exec
UPDATE drawers SET kind = sqlc.arg(kind) WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: SetBookCover :exec
UPDATE books SET cover = sqlc.arg(cover), updated_at = sqlc.arg(now) WHERE workspace_id = sqlc.arg(workspace_id) AND document_id = sqlc.arg(document_id);

-- name: FindDrawerByName :one
SELECT id FROM drawers WHERE workspace_id = sqlc.arg(workspace_id) AND name = sqlc.arg(name);
