-- v1.6.4: emergency access (after Bitwarden's): a workspace owner names an
-- existing account as emergency contact for some drawers, with a waiting
-- period. When the contact asks, the owners are told and may deny within
-- the wait; if nobody does, the drawers are shared read-only into the
-- contact's personal workspace (shared_drawer_ids: the shares this made,
-- removed again when access is revoked).
CREATE TABLE emergency_contacts (
    id                INTEGER PRIMARY KEY,
    workspace_id      INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    user_id           INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    drawer_ids        TEXT NOT NULL DEFAULT '[]',
    wait_hours        INTEGER NOT NULL,
    status            TEXT NOT NULL DEFAULT 'idle' CHECK (status IN ('idle', 'pending', 'granted')),
    reason            TEXT NOT NULL DEFAULT '',
    requested_at      INTEGER,
    grant_at          INTEGER,
    granted_at        INTEGER,
    shared_drawer_ids TEXT NOT NULL DEFAULT '[]',
    created_by        INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL,
    UNIQUE (workspace_id, user_id)
);
CREATE INDEX emergency_contacts_user ON emergency_contacts (user_id);
