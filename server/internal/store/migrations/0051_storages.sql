-- v1.7.2: storages added on the page by the instance administrator, besides
-- those of KEPT_STORAGES. settings is JSON without secrets (directory, or
-- endpoint, region, bucket, prefix, path style, access key); secret is the
-- S3 secret key sealed with the master key (secret.key / KEPT_SECRET_KEY).
-- selectable: workspace administrators may pick it for their workspace.
CREATE TABLE storages (
    name        TEXT    PRIMARY KEY,
    kind        TEXT    NOT NULL CHECK (kind IN ('dir', 's3')),
    description TEXT    NOT NULL DEFAULT '',
    settings    TEXT    NOT NULL DEFAULT '{}',
    secret      TEXT    NOT NULL DEFAULT '',
    selectable  INTEGER NOT NULL DEFAULT 1,
    created_by  INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);
