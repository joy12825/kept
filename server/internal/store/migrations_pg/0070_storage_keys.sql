-- v1.11.0: encrypted storages (see the SQLite migration).
CREATE TABLE storage_keys (
    storage    TEXT COLLATE "C" PRIMARY KEY,
    key_id     TEXT COLLATE "C" NOT NULL,
    sealed     TEXT COLLATE "C" NOT NULL,
    recovery   TEXT COLLATE "C" NOT NULL,
    enabled    BIGINT NOT NULL DEFAULT 1,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL
);
