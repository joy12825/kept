-- v1.9.1: documents kept without their converted PDF. An Office document or
-- an e-mail taken while no conversion service (Gotenberg) was set up keeps
-- its original and the text read from it; once one is, they are converted
-- in batches and the row goes.
CREATE TABLE pending_conversions (
    document_id  INTEGER PRIMARY KEY REFERENCES documents (id) ON DELETE CASCADE,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    kind         TEXT    NOT NULL CHECK (kind IN ('office', 'email')),
    added_at     INTEGER NOT NULL,
    queued_at    INTEGER
);
CREATE INDEX pending_conversions_workspace ON pending_conversions (workspace_id, queued_at);
