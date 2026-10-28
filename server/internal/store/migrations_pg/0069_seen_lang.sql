-- v1.10.2: the language a person last used (see the SQLite migration).
ALTER TABLE users ADD COLUMN seen_lang TEXT COLLATE "C" NOT NULL DEFAULT '';
