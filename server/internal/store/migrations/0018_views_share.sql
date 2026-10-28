-- v0.3 stage 18: how a saved view shows its documents (list or grid, the
-- columns), and share links: a token that lets anyone holding it read one
-- document until it expires or is revoked. The token is the credential and
-- kept as is, so the link can be copied again later.
ALTER TABLE saved_views ADD COLUMN display TEXT NOT NULL DEFAULT '{}';

CREATE TABLE share_links (
    id             INTEGER PRIMARY KEY,
    workspace_id   INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    document_id    INTEGER NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    token          TEXT    NOT NULL UNIQUE,
    file           TEXT    NOT NULL CHECK (file IN ('archive', 'original')),
    expires_at     INTEGER NOT NULL,
    created_by     INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at     INTEGER NOT NULL,
    revoked_at     INTEGER,
    access_count   INTEGER NOT NULL DEFAULT 0,
    last_access_at INTEGER
);
CREATE INDEX share_links_document ON share_links (workspace_id, document_id);
