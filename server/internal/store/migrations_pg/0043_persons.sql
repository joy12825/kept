-- v1.6.3: family members (家人), a fourth way to classify a document beside
-- correspondent, type and tags: the child's passport, father's health check.
-- Shaped like correspondents (with a match rule the classifier can learn);
-- a document belongs to at most one person.
CREATE TABLE persons (
    id               BIGINT PRIMARY KEY,
    workspace_id     BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name             CITEXT NOT NULL,
    sort_key         TEXT COLLATE "C" NOT NULL,
    match            TEXT COLLATE "C" NOT NULL DEFAULT '',
    algorithm        TEXT COLLATE "C" NOT NULL DEFAULT 'auto'
                     CHECK (algorithm IN ('none', 'any', 'all', 'literal', 'regex', 'fuzzy', 'auto')),
    case_insensitive BIGINT NOT NULL DEFAULT 1,
    created_at       BIGINT NOT NULL,
    updated_at       BIGINT NOT NULL,
    UNIQUE (workspace_id, name)
);
CREATE INDEX persons_sort ON persons (workspace_id, sort_key);
CREATE TRIGGER persons_id BEFORE INSERT ON persons FOR EACH ROW EXECUTE FUNCTION kept_next_id();
ALTER TABLE documents ADD COLUMN person_id BIGINT REFERENCES persons (id) ON DELETE SET NULL;
CREATE INDEX documents_person ON documents (person_id);
