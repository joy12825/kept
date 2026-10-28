-- Milestone 1.2: accounts, personal workspaces, sessions, API tokens.
-- Usernames are stored lower-cased by the application.
CREATE TABLE users (
    id                  INTEGER PRIMARY KEY,
    username            TEXT    NOT NULL UNIQUE,
    display_name        TEXT    NOT NULL DEFAULT '',
    email               TEXT,
    password_hash       TEXT    NOT NULL,
    is_admin            INTEGER NOT NULL DEFAULT 0,
    disabled_at         INTEGER,
    password_changed_at INTEGER NOT NULL,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
);

CREATE TABLE workspaces (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL,
    kind       TEXT    NOT NULL CHECK (kind IN ('personal', 'team')),
    created_at INTEGER NOT NULL
);

CREATE TABLE workspace_members (
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role         TEXT    NOT NULL CHECK (role IN ('owner', 'editor', 'viewer')),
    created_at   INTEGER NOT NULL,
    PRIMARY KEY (workspace_id, user_id)
);
CREATE INDEX workspace_members_user ON workspace_members (user_id);

-- id is the SHA-256 (hex) of the cookie value; the raw token is never stored.
CREATE TABLE sessions (
    id           TEXT    PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    csrf_token   TEXT    NOT NULL,
    ip           TEXT    NOT NULL DEFAULT '',
    user_agent   TEXT    NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL
);
CREATE INDEX sessions_user ON sessions (user_id);
CREATE INDEX sessions_expires ON sessions (expires_at);

-- token_hash is the SHA-256 (hex) of the bearer token; prefix is shown in the UI.
CREATE TABLE api_tokens (
    id           INTEGER PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    token_hash   TEXT    NOT NULL UNIQUE,
    prefix       TEXT    NOT NULL,
    created_at   INTEGER NOT NULL,
    last_used_at INTEGER
);
CREATE INDEX api_tokens_user ON api_tokens (user_id);

-- Instance-wide settings (e.g. allow_registration).
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Rebuild jobs so that workspace_id references workspaces.
CREATE TABLE jobs_new (
    id           INTEGER PRIMARY KEY,
    workspace_id INTEGER REFERENCES workspaces (id) ON DELETE CASCADE,
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
INSERT INTO jobs_new SELECT * FROM jobs;
DROP TABLE jobs;
ALTER TABLE jobs_new RENAME TO jobs;
CREATE INDEX jobs_ready ON jobs (status, priority, run_after);
CREATE INDEX jobs_workspace ON jobs (workspace_id, created_at);
