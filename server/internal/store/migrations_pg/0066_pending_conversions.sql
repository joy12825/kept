-- v1.9.1: documents kept without their converted PDF (see the SQLite
-- migration).
CREATE TABLE pending_conversions (
    document_id  BIGINT PRIMARY KEY REFERENCES documents (id) ON DELETE CASCADE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    kind         TEXT COLLATE "C" NOT NULL CHECK (kind IN ('office', 'email')),
    added_at     BIGINT NOT NULL,
    queued_at    BIGINT
);
CREATE INDEX pending_conversions_workspace ON pending_conversions (workspace_id, queued_at);
