-- v1.7.5: storages added on the page may be WebDAV folders.
ALTER TABLE storages DROP CONSTRAINT storages_kind_check;
ALTER TABLE storages ADD CONSTRAINT storages_kind_check CHECK (kind IN ('dir', 's3', 'webdav'));
