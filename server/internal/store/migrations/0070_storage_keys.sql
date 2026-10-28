-- v1.11.0: encrypted storages. Each storage that was ever encrypted has a
-- key: sealed with the master key (secret.key / KEPT_SECRET_KEY), and
-- sealed to the instance's recovery key (settings storage_recovery_*), so
-- the recovery code opens it when the master key is lost. enabled says
-- whether new files are encrypted; files written before stay readable.
CREATE TABLE storage_keys (
    storage    TEXT    PRIMARY KEY,
    key_id     TEXT    NOT NULL,
    sealed     TEXT    NOT NULL,
    recovery   TEXT    NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
