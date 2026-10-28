-- v0.4 stage 27: each person's reading status, rating and dates, and
-- their highlights and notes. All of it is private to the person.
ALTER TABLE reading_progress ADD COLUMN status TEXT NOT NULL DEFAULT '' CHECK (status IN ('', 'want', 'reading', 'finished'));
ALTER TABLE reading_progress ADD COLUMN rating INTEGER NOT NULL DEFAULT 0 CHECK (rating BETWEEN 0 AND 5);
ALTER TABLE reading_progress ADD COLUMN started_on TEXT NOT NULL DEFAULT '';
ALTER TABLE reading_progress ADD COLUMN finished_on TEXT NOT NULL DEFAULT '';
CREATE INDEX reading_progress_user ON reading_progress (user_id, workspace_id, status);

CREATE TABLE highlights (
    id           INTEGER PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    document_id  INTEGER NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    locator      TEXT NOT NULL,
    quote        TEXT NOT NULL DEFAULT '',
    note         TEXT NOT NULL DEFAULT '',
    color        TEXT NOT NULL DEFAULT 'yellow' CHECK (color IN ('yellow', 'green', 'blue', 'pink')),
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
CREATE INDEX highlights_user_document ON highlights (user_id, document_id);
CREATE INDEX highlights_user_workspace ON highlights (user_id, workspace_id, id);
