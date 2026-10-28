-- v1.6.3: where a paper original is kept (书房柜子第 2 层, 文件盒 A, the
-- bank's safe deposit box). The archive serial number says which original;
-- the location says which box to open.
CREATE TABLE locations (
    id           BIGINT PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         CITEXT NOT NULL,
    sort_key     TEXT COLLATE "C" NOT NULL,
    note         TEXT COLLATE "C" NOT NULL DEFAULT '',
    created_at   BIGINT NOT NULL,
    updated_at   BIGINT NOT NULL,
    UNIQUE (workspace_id, name)
);
CREATE INDEX locations_sort ON locations (workspace_id, sort_key);
CREATE TRIGGER locations_id BEFORE INSERT ON locations FOR EACH ROW EXECUTE FUNCTION kept_next_id();
ALTER TABLE documents ADD COLUMN location_id BIGINT REFERENCES locations (id) ON DELETE SET NULL;
CREATE INDEX documents_location ON documents (location_id);
