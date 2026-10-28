-- v1.7.2: storages added on the page by the instance administrator, besides
-- those of KEPT_STORAGES. settings is JSON without secrets (directory, or
-- endpoint, region, bucket, prefix, path style, access key); secret is the
-- S3 secret key sealed with the master key (secret.key / KEPT_SECRET_KEY).
-- selectable: workspace administrators may pick it for their workspace.
CREATE TABLE storages (
    name        TEXT COLLATE "C" PRIMARY KEY,
    kind        TEXT COLLATE "C" NOT NULL CHECK (kind IN ('dir', 's3')),
    description TEXT COLLATE "C" NOT NULL DEFAULT '',
    settings    TEXT COLLATE "C" NOT NULL DEFAULT '{}',
    secret      TEXT COLLATE "C" NOT NULL DEFAULT '',
    selectable  BIGINT NOT NULL DEFAULT 1,
    created_by  BIGINT REFERENCES users (id) ON DELETE SET NULL,
    created_at  BIGINT NOT NULL,
    updated_at  BIGINT NOT NULL
);
