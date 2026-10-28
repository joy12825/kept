-- Milestone 1.5: notes on documents, saved views (shared within a
-- workspace) and the pinyin sort key of document titles.

CREATE TABLE notes (
    id           INTEGER PRIMARY KEY,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    document_id  INTEGER NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    user_id      INTEGER REFERENCES users (id) ON DELETE SET NULL,
    body         TEXT    NOT NULL,
    created_at   INTEGER NOT NULL
);
CREATE INDEX notes_document ON notes (document_id, created_at);

-- filter is the filter JSON of design doc 11.9; sort is "field:dir".
CREATE TABLE saved_views (
    id           INTEGER PRIMARY KEY,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         TEXT    NOT NULL COLLATE NOCASE,
    sort_key     TEXT    NOT NULL,
    filter       TEXT    NOT NULL,
    sort         TEXT    NOT NULL DEFAULT '',
    created_by   INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    UNIQUE (workspace_id, name)
);

-- Filled in by Go (store.backfillTitleSort) for existing rows: SQL cannot
-- compute pinyin.
ALTER TABLE documents ADD COLUMN title_sort TEXT NOT NULL DEFAULT '';
CREATE INDEX documents_title ON documents (workspace_id, title_sort);
CREATE INDEX documents_added ON documents (workspace_id, added_at);
CREATE INDEX documents_deleted ON documents (workspace_id, deleted_at);

-- A document's history reads its audit events.
CREATE INDEX audit_events_target ON audit_events (workspace_id, target_type, target_id, id);
