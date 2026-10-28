-- v1.7.5: storages added on the page may be WebDAV folders.
CREATE TABLE storages_new (
    name        TEXT    PRIMARY KEY,
    kind        TEXT    NOT NULL CHECK (kind IN ('dir', 's3', 'webdav')),
    description TEXT    NOT NULL DEFAULT '',
    settings    TEXT    NOT NULL DEFAULT '{}',
    secret      TEXT    NOT NULL DEFAULT '',
    selectable  INTEGER NOT NULL DEFAULT 1,
    created_by  INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);
INSERT INTO storages_new SELECT name, kind, description, settings, secret, selectable, created_by, created_at, updated_at FROM storages;
DROP TABLE storages;
ALTER TABLE storages_new RENAME TO storages;
