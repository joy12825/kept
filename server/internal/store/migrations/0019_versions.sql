-- v0.3 stage 19: document versions. The current version's file stays at
-- the original's path; the others live in media/versions. A document
-- without rows has one version, described by the document itself.
CREATE TABLE document_versions (
    id                INTEGER PRIMARY KEY,
    workspace_id      INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    document_id       INTEGER NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    number            INTEGER NOT NULL,
    label             TEXT    NOT NULL DEFAULT '',
    original_filename TEXT    NOT NULL,
    original_ext      TEXT    NOT NULL,
    mime_type         TEXT    NOT NULL,
    checksum          TEXT    NOT NULL,
    size              INTEGER NOT NULL,
    encrypted         INTEGER NOT NULL DEFAULT 0,
    current           INTEGER NOT NULL DEFAULT 0,
    created_by        INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at        INTEGER NOT NULL,
    UNIQUE (document_id, number)
);
CREATE INDEX document_versions_document ON document_versions (workspace_id, document_id);
