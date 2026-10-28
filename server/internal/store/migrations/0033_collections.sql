-- v1.0 stage 37: collections, shared read-only with other workspaces like
-- drawers (0012): a chosen set of documents, or a filter whose matching
-- documents are shared as they come.
CREATE TABLE collections (
    id           INTEGER PRIMARY KEY,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    kind         TEXT    NOT NULL CHECK (kind IN ('documents', 'filter')),
    filter       TEXT    NOT NULL DEFAULT '',
    created_by   INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
CREATE INDEX collections_workspace ON collections (workspace_id);

CREATE TABLE collection_documents (
    collection_id INTEGER NOT NULL REFERENCES collections (id) ON DELETE CASCADE,
    document_id   INTEGER NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    PRIMARY KEY (collection_id, document_id)
);
CREATE INDEX collection_documents_document ON collection_documents (document_id);

CREATE TABLE collection_shares (
    collection_id       INTEGER NOT NULL REFERENCES collections (id) ON DELETE CASCADE,
    target_workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    created_by          INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at          INTEGER NOT NULL,
    PRIMARY KEY (collection_id, target_workspace_id)
);
CREATE INDEX collection_shares_target ON collection_shares (target_workspace_id);
