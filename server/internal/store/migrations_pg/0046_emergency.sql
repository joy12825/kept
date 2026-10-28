-- v1.6.4: emergency access (after Bitwarden's): a workspace owner names an
-- existing account as emergency contact for some drawers, with a waiting
-- period. When the contact asks, the owners are told and may deny within
-- the wait; if nobody does, the drawers are shared read-only into the
-- contact's personal workspace (shared_drawer_ids: the shares this made,
-- removed again when access is revoked).
CREATE TABLE emergency_contacts (
    id                BIGINT PRIMARY KEY,
    workspace_id      BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    user_id           BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    drawer_ids        TEXT COLLATE "C" NOT NULL DEFAULT '[]',
    wait_hours        BIGINT NOT NULL,
    status            TEXT COLLATE "C" NOT NULL DEFAULT 'idle' CHECK (status IN ('idle', 'pending', 'granted')),
    reason            TEXT COLLATE "C" NOT NULL DEFAULT '',
    requested_at      BIGINT,
    grant_at          BIGINT,
    granted_at        BIGINT,
    shared_drawer_ids TEXT COLLATE "C" NOT NULL DEFAULT '[]',
    created_by        BIGINT REFERENCES users (id) ON DELETE SET NULL,
    created_at        BIGINT NOT NULL,
    updated_at        BIGINT NOT NULL,
    UNIQUE (workspace_id, user_id)
);
CREATE INDEX emergency_contacts_user ON emergency_contacts (user_id);
CREATE TRIGGER emergency_contacts_id BEFORE INSERT ON emergency_contacts FOR EACH ROW EXECUTE FUNCTION kept_next_id();
