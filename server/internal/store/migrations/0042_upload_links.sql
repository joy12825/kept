-- v1.6.2: links that let someone without an account (a grandparent, the
-- landlord, a builder) upload files into a workspace's inbox. The token is
-- the credential; a link stops at its expiry, when revoked, or after
-- max_files files. Uploads get the link's tags and drawer.
CREATE TABLE upload_links (
    id             INTEGER PRIMARY KEY,
    workspace_id   INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    token          TEXT    NOT NULL UNIQUE,
    name           TEXT    NOT NULL,
    message        TEXT    NOT NULL DEFAULT '',
    tag_ids        TEXT    NOT NULL DEFAULT '[]',
    drawer_id      INTEGER REFERENCES drawers (id) ON DELETE SET NULL,
    max_files      INTEGER NOT NULL,
    file_count     INTEGER NOT NULL DEFAULT 0,
    expires_at     INTEGER NOT NULL,
    created_by     INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at     INTEGER NOT NULL,
    revoked_at     INTEGER,
    last_upload_at INTEGER
);
CREATE INDEX upload_links_workspace ON upload_links (workspace_id, expires_at);
