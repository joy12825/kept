-- v0.3 stage 20: near-duplicate documents. simhash is the fingerprint of
-- the text, computed for index_rev simhash_rev; a pair is open until a
-- person keeps one document or says they are not duplicates.
ALTER TABLE documents ADD COLUMN simhash INTEGER;
ALTER TABLE documents ADD COLUMN simhash_rev INTEGER NOT NULL DEFAULT 0;

CREATE TABLE duplicate_pairs (
    id           INTEGER PRIMARY KEY,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    doc_a        INTEGER NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    doc_b        INTEGER NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    distance     INTEGER NOT NULL,
    status       TEXT    NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'dismissed')),
    found_at     INTEGER NOT NULL,
    CHECK (doc_a < doc_b),
    UNIQUE (doc_a, doc_b)
);
CREATE INDEX duplicate_pairs_workspace ON duplicate_pairs (workspace_id, status);
