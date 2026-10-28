-- v1.8.0: private documents. A document can be private to a family member
-- (documents.private, belonging to documents.person_id): only the person
-- themself, their guardians, the members they granted access, and the
-- uploader (until the person takes it back) see it. Everyone else in the
-- workspace, the owner included, does not see it nor count it.
--
-- person_access: who may see a person's private documents.
--   self     the person's own account (at most one per person, and an
--            account is at most one person per workspace)
--   guardian looks after a person without an account (a child)
--   manage   granted by the person: sees and edits
--   read     granted by the person: only sees
-- via: '' granted by hand, 'emergency' granted by emergency access.
-- reviewed (self rows): 0 when the account was linked to a person who
-- already had guardians; the person is asked whether to keep them.
CREATE TABLE person_access (
    person_id    INTEGER NOT NULL REFERENCES persons (id) ON DELETE CASCADE,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    level        TEXT    NOT NULL CHECK (level IN ('self', 'guardian', 'manage', 'read')),
    via          TEXT    NOT NULL DEFAULT '',
    created_by   INTEGER REFERENCES users (id) ON DELETE SET NULL,
    reviewed     INTEGER NOT NULL DEFAULT 1,
    created_at   INTEGER NOT NULL,
    PRIMARY KEY (person_id, user_id)
);
CREATE INDEX person_access_user ON person_access (user_id, person_id);
CREATE UNIQUE INDEX person_access_self ON person_access (person_id) WHERE level = 'self';
CREATE UNIQUE INDEX person_access_self_user ON person_access (workspace_id, user_id) WHERE level = 'self';

-- Emergency contacts for a person's private documents: after the wait, a
-- request the person did not deny grants read access (person_access, via
-- 'emergency').
CREATE TABLE person_emergency (
    id           INTEGER PRIMARY KEY,
    person_id    INTEGER NOT NULL REFERENCES persons (id) ON DELETE CASCADE,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    wait_hours   INTEGER NOT NULL,
    status       TEXT    NOT NULL DEFAULT 'idle' CHECK (status IN ('idle', 'pending', 'granted')),
    reason       TEXT    NOT NULL DEFAULT '',
    requested_at INTEGER,
    grant_at     INTEGER,
    granted_at   INTEGER,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    UNIQUE (person_id, user_id)
);
CREATE INDEX person_emergency_due ON person_emergency (status, grant_at);

ALTER TABLE documents ADD COLUMN private INTEGER NOT NULL DEFAULT 0;
-- 0: the person took back the uploader's (added_by) access.
ALTER TABLE documents ADD COLUMN uploader_access INTEGER NOT NULL DEFAULT 1;
-- 1: private was turned off by hand; automatic privacy leaves the document
-- alone until its person or type changes again.
ALTER TABLE documents ADD COLUMN private_manual INTEGER NOT NULL DEFAULT 0;
CREATE INDEX documents_private ON documents (workspace_id, private, person_id);

-- Documents of these types become private when they get a person (a
-- separate table like type_retention: document_types rows keep the shape
-- of correspondents).
CREATE TABLE type_private (
    document_type_id INTEGER PRIMARY KEY REFERENCES document_types (id) ON DELETE CASCADE,
    workspace_id     INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE
);
CREATE INDEX type_private_workspace ON type_private (workspace_id);
