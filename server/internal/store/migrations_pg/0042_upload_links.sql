-- v1.6.2: links that let someone without an account (a grandparent, the
-- landlord, a builder) upload files into a workspace's inbox. The token is
-- the credential; a link stops at its expiry, when revoked, or after
-- max_files files. Uploads get the link's tags and drawer.
CREATE TABLE upload_links (
    id             BIGINT PRIMARY KEY,
    workspace_id   BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    token          TEXT COLLATE "C" NOT NULL UNIQUE,
    name           TEXT COLLATE "C" NOT NULL,
    message        TEXT COLLATE "C" NOT NULL DEFAULT '',
    tag_ids        TEXT COLLATE "C" NOT NULL DEFAULT '[]',
    drawer_id      BIGINT REFERENCES drawers (id) ON DELETE SET NULL,
    max_files      BIGINT NOT NULL,
    file_count     BIGINT NOT NULL DEFAULT 0,
    expires_at     BIGINT NOT NULL,
    created_by     BIGINT REFERENCES users (id) ON DELETE SET NULL,
    created_at     BIGINT NOT NULL,
    revoked_at     BIGINT,
    last_upload_at BIGINT
);
CREATE INDEX upload_links_workspace ON upload_links (workspace_id, expires_at);
CREATE TRIGGER upload_links_id BEFORE INSERT ON upload_links FOR EACH ROW EXECUTE FUNCTION kept_next_id();
