-- v0.4 stage 26: where each person stopped reading a document. The
-- position is "page:12" for PDFs and an EPUB CFI for e-books.
CREATE TABLE reading_progress (
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    document_id  INTEGER NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    position     TEXT NOT NULL DEFAULT '',
    percent      REAL NOT NULL DEFAULT 0,
    updated_at   INTEGER NOT NULL,
    PRIMARY KEY (user_id, document_id)
);
CREATE INDEX reading_progress_document ON reading_progress (document_id);
