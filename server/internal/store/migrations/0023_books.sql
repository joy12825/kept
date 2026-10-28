-- v0.4 stage 23: books. A document is a book when it has a books row;
-- the title is the book's title. The library is a drawer of kind library
-- that files every book.
CREATE TABLE books (
    document_id  INTEGER PRIMARY KEY REFERENCES documents (id) ON DELETE CASCADE,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    isbn         TEXT    NOT NULL DEFAULT '',
    authors      TEXT    NOT NULL DEFAULT '[]',
    publisher    TEXT    NOT NULL DEFAULT '',
    series       TEXT    NOT NULL DEFAULT '',
    edition      TEXT    NOT NULL DEFAULT '',
    published    TEXT    NOT NULL DEFAULT '',
    pages        INTEGER NOT NULL DEFAULT 0,
    language     TEXT    NOT NULL DEFAULT '',
    description  TEXT    NOT NULL DEFAULT '',
    cover        INTEGER NOT NULL DEFAULT 0,
    updated_at   INTEGER NOT NULL
);
CREATE UNIQUE INDEX books_isbn ON books (workspace_id, isbn) WHERE isbn != '';

ALTER TABLE drawers ADD COLUMN kind TEXT NOT NULL DEFAULT 'normal' CHECK (kind IN ('normal', 'library'));
