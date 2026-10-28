-- v1.6.3: family members (家人), a fourth way to classify a document beside
-- correspondent, type and tags: the child's passport, father's health check.
-- Shaped like correspondents (with a match rule the classifier can learn);
-- a document belongs to at most one person.
CREATE TABLE persons (
    id               INTEGER PRIMARY KEY,
    workspace_id     INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name             TEXT    NOT NULL COLLATE NOCASE,
    sort_key         TEXT    NOT NULL,
    match            TEXT    NOT NULL DEFAULT '',
    algorithm        TEXT    NOT NULL DEFAULT 'auto'
                     CHECK (algorithm IN ('none', 'any', 'all', 'literal', 'regex', 'fuzzy', 'auto')),
    case_insensitive INTEGER NOT NULL DEFAULT 1,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL,
    UNIQUE (workspace_id, name)
);
CREATE INDEX persons_sort ON persons (workspace_id, sort_key);
ALTER TABLE documents ADD COLUMN person_id INTEGER REFERENCES persons (id) ON DELETE SET NULL;
CREATE INDEX documents_person ON documents (person_id);
