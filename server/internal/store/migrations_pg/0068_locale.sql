-- v1.10.0: an account's web language (see the SQLite migration).
ALTER TABLE users ADD COLUMN locale TEXT COLLATE "C" NOT NULL DEFAULT '';
