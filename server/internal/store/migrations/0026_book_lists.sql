-- v0.4 stage 28: book lists, shared in the workspace, with books in order
-- and a line about each.
CREATE TABLE book_lists (
    id           INTEGER PRIMARY KEY,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    created_by   INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
CREATE UNIQUE INDEX book_lists_name ON book_lists (workspace_id, name);

CREATE TABLE book_list_items (
    list_id     INTEGER NOT NULL REFERENCES book_lists (id) ON DELETE CASCADE,
    document_id INTEGER NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    position    INTEGER NOT NULL,
    note        TEXT NOT NULL DEFAULT '',
    added_at    INTEGER NOT NULL,
    PRIMARY KEY (list_id, document_id)
);
CREATE INDEX book_list_items_document ON book_list_items (document_id);
