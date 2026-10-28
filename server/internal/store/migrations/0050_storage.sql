-- v1.7.0: storages. A document's files (original, searchable PDF, page-edit
-- base, earlier versions) live in one named storage; "local" is the data
-- directory, as before. A workspace's new documents go to its storage.
-- Storages themselves are configured (KEPT_STORAGES), not stored: they
-- carry secrets.
ALTER TABLE documents ADD COLUMN storage TEXT NOT NULL DEFAULT 'local';
ALTER TABLE workspaces ADD COLUMN storage TEXT NOT NULL DEFAULT 'local';
