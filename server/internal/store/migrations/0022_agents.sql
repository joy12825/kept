-- v0.3 stage 22: AI agents (design doc part 10). A workspace's AI settings
-- are JSON (off by default); every run and its model calls are logged; every
-- change an agent makes is a proposal that can be reverted.
ALTER TABLE workspaces ADD COLUMN ai_settings TEXT NOT NULL DEFAULT '{}';

CREATE TABLE agent_runs (
    id            INTEGER PRIMARY KEY,
    workspace_id  INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    agent         TEXT    NOT NULL,
    trigger       TEXT    NOT NULL,
    document_id   INTEGER REFERENCES documents (id) ON DELETE SET NULL,
    model         TEXT    NOT NULL DEFAULT '',
    status        TEXT    NOT NULL CHECK (status IN ('running', 'done', 'failed', 'skipped')),
    calls         INTEGER NOT NULL DEFAULT 0,
    input_tokens  INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    summary       TEXT    NOT NULL DEFAULT '',
    error         TEXT    NOT NULL DEFAULT '',
    created_at    INTEGER NOT NULL
);
CREATE INDEX agent_runs_workspace ON agent_runs (workspace_id, created_at);

CREATE TABLE agent_steps (
    id     INTEGER PRIMARY KEY,
    run_id INTEGER NOT NULL REFERENCES agent_runs (id) ON DELETE CASCADE,
    seq    INTEGER NOT NULL,
    kind   TEXT    NOT NULL,
    input  TEXT    NOT NULL,
    output TEXT    NOT NULL
);
CREATE INDEX agent_steps_run ON agent_steps (run_id, seq);

CREATE TABLE agent_proposals (
    id           INTEGER PRIMARY KEY,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    run_id       INTEGER REFERENCES agent_runs (id) ON DELETE SET NULL,
    agent        TEXT    NOT NULL,
    kind         TEXT    NOT NULL,
    document_id  INTEGER REFERENCES documents (id) ON DELETE CASCADE,
    payload      TEXT    NOT NULL,
    confidence   REAL    NOT NULL DEFAULT 0,
    reason       TEXT    NOT NULL DEFAULT '',
    status       TEXT    NOT NULL CHECK (status IN ('applied', 'pending', 'accepted', 'rejected', 'reverted', 'expired')),
    decided_by   INTEGER REFERENCES users (id) ON DELETE SET NULL,
    decided_at   INTEGER,
    created_at   INTEGER NOT NULL
);
CREATE INDEX agent_proposals_document ON agent_proposals (workspace_id, document_id, status);
