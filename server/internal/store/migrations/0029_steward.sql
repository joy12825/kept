-- v0.5 stage 32: the drawer steward. A plain-language note on each alert
-- (written once, by the agent run note_run), and the AI paragraph of a
-- year in review; basis is a hash of what it was written from.
ALTER TABLE alerts ADD COLUMN note TEXT NOT NULL DEFAULT '';
ALTER TABLE alerts ADD COLUMN note_run INTEGER;

CREATE TABLE yearbook_notes (
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    year         INTEGER NOT NULL,
    text         TEXT    NOT NULL,
    basis        TEXT    NOT NULL,
    run_id       INTEGER,
    created_at   INTEGER NOT NULL,
    PRIMARY KEY (workspace_id, year)
);
