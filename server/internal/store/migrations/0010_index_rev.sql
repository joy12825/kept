-- Milestone 1.6: full-text index bookkeeping. index_rev goes up with every
-- change to what the search index holds for a document (text, names of its
-- classifications, notes, trash state); the indexer writes the document and
-- sets indexed_rev to the index_rev it read. indexed_rev < index_rev means
-- the index is behind. Existing documents start out behind.
ALTER TABLE documents ADD COLUMN index_rev INTEGER NOT NULL DEFAULT 1;
ALTER TABLE documents ADD COLUMN indexed_rev INTEGER NOT NULL DEFAULT 0;
CREATE INDEX documents_index_behind ON documents (id) WHERE indexed_rev < index_rev;
