-- v1.6.3: where a paper original is kept (书房柜子第 2 层, 文件盒 A, the
-- bank's safe deposit box). The archive serial number says which original;
-- the location says which box to open.
CREATE TABLE locations (
    id           INTEGER PRIMARY KEY,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         TEXT    NOT NULL COLLATE NOCASE,
    sort_key     TEXT    NOT NULL,
    note         TEXT    NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    UNIQUE (workspace_id, name)
);
CREATE INDEX locations_sort ON locations (workspace_id, sort_key);
ALTER TABLE documents ADD COLUMN location_id INTEGER REFERENCES locations (id) ON DELETE SET NULL;
CREATE INDEX documents_location ON documents (location_id);
