-- v0.3 stage 17: workflows. triggers, conditions and actions are JSON
-- (package workflow validates them). Runs keep the last 200 per workflow;
-- scheduled triggers look up a document's last scheduled run.
CREATE TABLE workflows (
    id           INTEGER PRIMARY KEY,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    enabled      INTEGER NOT NULL DEFAULT 1,
    sort_order   INTEGER NOT NULL DEFAULT 0,
    triggers     TEXT    NOT NULL DEFAULT '[]',
    conditions   TEXT    NOT NULL DEFAULT '{}',
    actions      TEXT    NOT NULL DEFAULT '[]',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    UNIQUE (workspace_id, name)
);
CREATE INDEX workflows_workspace ON workflows (workspace_id, sort_order);

CREATE TABLE workflow_runs (
    id           INTEGER PRIMARY KEY,
    workflow_id  INTEGER NOT NULL REFERENCES workflows (id) ON DELETE CASCADE,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    document_id  INTEGER REFERENCES documents (id) ON DELETE SET NULL,
    trigger      TEXT    NOT NULL CHECK (trigger IN ('consumption', 'added', 'updated', 'scheduled')),
    status       TEXT    NOT NULL CHECK (status IN ('ok', 'failed')),
    message      TEXT    NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL
);
CREATE INDEX workflow_runs_workflow ON workflow_runs (workflow_id, id);
CREATE INDEX workflow_runs_document ON workflow_runs (workflow_id, document_id, trigger);
