-- v1.4.2: one link for several documents (a batch of receipts for the
-- accountant). Like share_links: the token is the credential; it stops
-- working when it expires, is revoked, or none of its documents is left.
CREATE TABLE share_bundles (
    id             INTEGER PRIMARY KEY,
    workspace_id   INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    token          TEXT    NOT NULL UNIQUE,
    title          TEXT    NOT NULL,
    file           TEXT    NOT NULL CHECK (file IN ('archive', 'original')),
    expires_at     INTEGER NOT NULL,
    created_by     INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at     INTEGER NOT NULL,
    revoked_at     INTEGER,
    access_count   INTEGER NOT NULL DEFAULT 0,
    last_access_at INTEGER
);
CREATE INDEX share_bundles_workspace ON share_bundles (workspace_id, expires_at);

CREATE TABLE share_bundle_documents (
    bundle_id   INTEGER NOT NULL REFERENCES share_bundles (id) ON DELETE CASCADE,
    document_id INTEGER NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    position    INTEGER NOT NULL,
    PRIMARY KEY (bundle_id, document_id)
);
CREATE INDEX share_bundle_documents_document ON share_bundle_documents (document_id);
