-- Job queue: ingestion, indexing, classifier training and other background work.
-- Timestamps are Unix milliseconds. The workspace_id foreign key is added once
-- the workspaces table exists (milestone 1.2). Keep migrations ASCII-only (sqlc).
CREATE TABLE jobs (
    id           INTEGER PRIMARY KEY,
    workspace_id INTEGER,
    kind         TEXT    NOT NULL,
    payload      TEXT    NOT NULL DEFAULT '{}',
    status       TEXT    NOT NULL DEFAULT 'queued'
                 CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    priority     INTEGER NOT NULL DEFAULT 0,
    attempts     INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    run_after    INTEGER NOT NULL,
    heartbeat_at INTEGER,
    error        TEXT,
    result       TEXT,
    created_at   INTEGER NOT NULL,
    started_at   INTEGER,
    finished_at  INTEGER
);

CREATE INDEX jobs_ready ON jobs (status, priority, run_after);
CREATE INDEX jobs_workspace ON jobs (workspace_id, created_at);
