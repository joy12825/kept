-- v0.2: fetching attachments from mailboxes (IMAP), per workspace.
--
-- A mail account is one mailbox folder with an optional filter (sender or
-- subject contains) and presets for what it brings in. password is never
-- sent back by the API. mail_seen remembers the messages already handled
-- (by UIDVALIDITY and UID), so nothing is fetched twice.
CREATE TABLE mail_accounts (
    id               INTEGER PRIMARY KEY,
    workspace_id     INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name             TEXT    NOT NULL,
    host             TEXT    NOT NULL,
    port             INTEGER NOT NULL,
    security         TEXT    NOT NULL DEFAULT 'tls' CHECK (security IN ('tls', 'starttls', 'none')),
    username         TEXT    NOT NULL,
    password         TEXT    NOT NULL,
    folder           TEXT    NOT NULL DEFAULT 'INBOX',
    filter_from      TEXT    NOT NULL DEFAULT '',
    filter_subject   TEXT    NOT NULL DEFAULT '',
    document_type_id INTEGER REFERENCES document_types (id) ON DELETE SET NULL,
    correspondent_id INTEGER REFERENCES correspondents (id) ON DELETE SET NULL,
    tag_ids          TEXT    NOT NULL DEFAULT '[]',
    after_action     TEXT    NOT NULL DEFAULT 'read' CHECK (after_action IN ('read', 'move')),
    move_to          TEXT    NOT NULL DEFAULT '',
    enabled          INTEGER NOT NULL DEFAULT 1,
    checked_at       INTEGER,
    last_error       TEXT    NOT NULL DEFAULT '',
    last_count       INTEGER NOT NULL DEFAULT 0,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);
CREATE INDEX mail_accounts_workspace ON mail_accounts (workspace_id);

CREATE TABLE mail_seen (
    account_id   INTEGER NOT NULL REFERENCES mail_accounts (id) ON DELETE CASCADE,
    uid_validity INTEGER NOT NULL,
    uid          INTEGER NOT NULL,
    seen_at      INTEGER NOT NULL,
    PRIMARY KEY (account_id, uid_validity, uid)
);
