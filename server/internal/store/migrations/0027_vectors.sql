-- v0.5 stage 29: documents cut into chunks, each with a vector in
-- chunk_vectors (vec1, SQLite's vector extension). workspace_id is a
-- metadata column so a search filters by workspace inside the index.
CREATE TABLE doc_chunks (
    id           INTEGER PRIMARY KEY,
    document_id  INTEGER NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    seq          INTEGER NOT NULL,
    text         TEXT NOT NULL,
    hash         TEXT NOT NULL
);
CREATE INDEX doc_chunks_document ON doc_chunks (document_id, seq);
CREATE INDEX doc_chunks_hash ON doc_chunks (workspace_id, hash);

ALTER TABLE documents ADD COLUMN embedded_rev INTEGER NOT NULL DEFAULT 0;

CREATE VIRTUAL TABLE chunk_vectors USING vec1(vector, workspace_id);
INSERT INTO chunk_vectors(cmd, arg) VALUES ('rebuild', '{index:"flat", distance:"cos"}');
