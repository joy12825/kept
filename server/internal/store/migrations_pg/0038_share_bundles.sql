-- v1.4.2: one link for several documents (a batch of receipts for the
-- accountant). Like share_links: the token is the credential; it stops
-- working when it expires, is revoked, or none of its documents is left.
CREATE TABLE share_bundles (
    id             BIGINT PRIMARY KEY,
    workspace_id   BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    token          TEXT COLLATE "C" NOT NULL UNIQUE,
    title          TEXT COLLATE "C" NOT NULL,
    file           TEXT COLLATE "C" NOT NULL CHECK (file IN ('archive', 'original')),
    expires_at     BIGINT NOT NULL,
    created_by     BIGINT REFERENCES users (id) ON DELETE SET NULL,
    created_at     BIGINT NOT NULL,
    revoked_at     BIGINT,
    access_count   BIGINT NOT NULL DEFAULT 0,
    last_access_at BIGINT
);
CREATE INDEX share_bundles_workspace ON share_bundles (workspace_id, expires_at);
CREATE TRIGGER share_bundles_id BEFORE INSERT ON share_bundles FOR EACH ROW EXECUTE FUNCTION kept_next_id();

CREATE TABLE share_bundle_documents (
    bundle_id   BIGINT NOT NULL REFERENCES share_bundles (id) ON DELETE CASCADE,
    document_id BIGINT NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    position    BIGINT NOT NULL,
    PRIMARY KEY (bundle_id, document_id)
);
CREATE INDEX share_bundle_documents_document ON share_bundle_documents (document_id);
