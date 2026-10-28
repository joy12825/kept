-- Milestone 1.2 addendum: TOTP two-factor authentication.
CREATE TABLE user_totp (
    user_id        INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    secret         TEXT    NOT NULL,
    enabled_at     INTEGER,
    last_used_step INTEGER NOT NULL DEFAULT 0,
    created_at     INTEGER NOT NULL
);
CREATE TABLE user_recovery_codes (
    id        INTEGER PRIMARY KEY,
    user_id   INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash TEXT    NOT NULL,
    used_at   INTEGER
);
CREATE INDEX user_recovery_codes_user ON user_recovery_codes (user_id);
