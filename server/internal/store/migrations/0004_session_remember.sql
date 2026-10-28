-- Sessions from "remember me" survive browser restarts; others use a
-- browser-session cookie and a shorter idle lifetime.
ALTER TABLE sessions ADD COLUMN persistent INTEGER NOT NULL DEFAULT 1;
