-- v1.8.1: a person's key pair for their private documents' copies on
-- remote storages and in remote backups (package sealbox). The public key
-- encrypts; the private key is kept only wrapped: by the recovery code the
-- person wrote down, and by the login password of each keeper (the person,
-- or their guardians) who set it up or confirmed it since.
CREATE TABLE person_keys (
    person_id    BIGINT PRIMARY KEY REFERENCES persons (id) ON DELETE CASCADE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    public_key   TEXT COLLATE "C" NOT NULL,
    recovery     TEXT COLLATE "C" NOT NULL, -- the private key wrapped by the recovery code
    created_by   BIGINT REFERENCES users (id) ON DELETE SET NULL,
    created_at   BIGINT NOT NULL,
    recovery_at  BIGINT NOT NULL  -- when the current recovery code was made
);
CREATE INDEX person_keys_workspace ON person_keys (workspace_id);

CREATE TABLE person_key_wraps (
    person_id  BIGINT NOT NULL REFERENCES person_keys (person_id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    wrapped    TEXT COLLATE "C" NOT NULL, -- the private key wrapped by the user's password
    created_at BIGINT NOT NULL,
    PRIMARY KEY (person_id, user_id)
);
CREATE INDEX person_key_wraps_user ON person_key_wraps (user_id);

-- Encrypted copies of private documents' files on remote storages: which
-- file of which document is on which storage, sealed for which key.
CREATE TABLE sealed_copies (
    document_id BIGINT NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    storage     TEXT COLLATE "C" NOT NULL,
    file        TEXT COLLATE "C" NOT NULL, -- original, archive, version:<id>
    checksum    TEXT COLLATE "C" NOT NULL, -- of the plaintext
    public_key  TEXT COLLATE "C" NOT NULL,
    created_at  BIGINT NOT NULL,
    PRIMARY KEY (document_id, storage, file)
);

-- Whether a person lets cloud AI models read their private documents
-- (v1.8.1); the workspace has to allow health data in the cloud as well.
CREATE TABLE person_ai (
    person_id  BIGINT PRIMARY KEY REFERENCES persons (id) ON DELETE CASCADE,
    cloud_ok   BIGINT NOT NULL DEFAULT 0,
    decided_by BIGINT REFERENCES users (id) ON DELETE SET NULL,
    decided_at BIGINT NOT NULL
);
