-- v0.5 stage 33: the library assistant. A book's summary and chapter key
-- points (shared in the workspace; basis is a hash of the text it was
-- written from), and each person's review cards made from their own
-- highlights.
CREATE TABLE book_summaries (
    document_id  INTEGER PRIMARY KEY REFERENCES documents (id) ON DELETE CASCADE,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    summary      TEXT    NOT NULL,
    chapters     TEXT    NOT NULL DEFAULT '[]',
    basis        TEXT    NOT NULL,
    run_id       INTEGER,
    created_at   INTEGER NOT NULL
);

CREATE TABLE review_cards (
    id            INTEGER PRIMARY KEY,
    user_id       INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    workspace_id  INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    document_id   INTEGER NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    highlight_id  INTEGER REFERENCES highlights (id) ON DELETE SET NULL,
    front         TEXT    NOT NULL,
    back          TEXT    NOT NULL,
    due_on        TEXT    NOT NULL,
    interval_days INTEGER NOT NULL DEFAULT 0,
    reps          INTEGER NOT NULL DEFAULT 0,
    lapses        INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);
CREATE INDEX review_cards_user ON review_cards (user_id, workspace_id, due_on);
CREATE UNIQUE INDEX review_cards_highlight ON review_cards (highlight_id) WHERE highlight_id IS NOT NULL;
