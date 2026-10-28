-- v0.2: phone pairing and device tokens, idempotent uploads, document
-- source (design doc 9.6).
--
-- A device token is an api_tokens row of kind 'device', bound to the
-- workspace it was paired for; last_used_at doubles as "last seen".
ALTER TABLE api_tokens ADD COLUMN kind TEXT NOT NULL DEFAULT 'personal' CHECK (kind IN ('personal', 'device'));
ALTER TABLE api_tokens ADD COLUMN workspace_id INTEGER REFERENCES workspaces (id) ON DELETE CASCADE;
ALTER TABLE api_tokens ADD COLUMN device_name TEXT NOT NULL DEFAULT '';
ALTER TABLE api_tokens ADD COLUMN paired_at INTEGER;

-- One-time pairing codes (8 digits, stored hashed), valid for 10 minutes.
CREATE TABLE pairing_codes (
    id           INTEGER PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    code_hash    TEXT    NOT NULL UNIQUE,
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    used_at      INTEGER
);
CREATE INDEX pairing_codes_user ON pairing_codes (user_id, workspace_id);

-- web | api | mobile (later: watch, mail)
ALTER TABLE documents ADD COLUMN source TEXT NOT NULL DEFAULT 'web';

ALTER TABLE jobs ADD COLUMN client_upload_id TEXT;
CREATE UNIQUE INDEX jobs_client_upload ON jobs (workspace_id, client_upload_id) WHERE client_upload_id IS NOT NULL;
