-- kept:foreign_keys=off
-- Milestone 1.2b: family/team workspaces and the audit log.
CREATE TABLE workspaces_new (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL,
    kind       TEXT    NOT NULL CHECK (kind IN ('personal', 'family', 'team')),
    color      TEXT    NOT NULL DEFAULT 'blue',
    created_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL,
    deleted_at INTEGER
);
INSERT INTO workspaces_new (id, name, kind, color, created_by, created_at)
SELECT w.id, w.name, w.kind, 'blue',
       (SELECT m.user_id FROM workspace_members AS m
        WHERE m.workspace_id = w.id AND m.role = 'owner'
        ORDER BY m.created_at, m.user_id LIMIT 1),
       w.created_at
FROM workspaces AS w;
DROP TABLE workspaces;
ALTER TABLE workspaces_new RENAME TO workspaces;

-- workspace_id NULL means an instance-level event.
CREATE TABLE audit_events (
    id            INTEGER PRIMARY KEY,
    workspace_id  INTEGER REFERENCES workspaces (id) ON DELETE CASCADE,
    actor_user_id INTEGER REFERENCES users (id) ON DELETE SET NULL,
    actor_via     TEXT    NOT NULL CHECK (actor_via IN ('session', 'token', 'device', 'agent', 'system')),
    action        TEXT    NOT NULL,
    target_type   TEXT    NOT NULL DEFAULT '',
    target_id     TEXT    NOT NULL DEFAULT '',
    summary       TEXT    NOT NULL,
    changes       TEXT    NOT NULL DEFAULT '{}',
    request_id    TEXT    NOT NULL DEFAULT '',
    created_at    INTEGER NOT NULL
);
CREATE INDEX audit_events_workspace ON audit_events (workspace_id, id);
CREATE INDEX audit_events_created ON audit_events (created_at);

-- Self-registration is removed: accounts are created by administrators only.
DELETE FROM settings WHERE key = 'allow_registration';
