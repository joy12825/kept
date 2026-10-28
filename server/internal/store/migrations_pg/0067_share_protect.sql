-- v1.9.2: share watermarks and view-only shares (see the SQLite migration).
ALTER TABLE share_links ADD COLUMN watermark TEXT COLLATE "C" NOT NULL DEFAULT '';
ALTER TABLE share_links ADD COLUMN allow_download BIGINT NOT NULL DEFAULT 1;
ALTER TABLE share_bundles ADD COLUMN watermark TEXT COLLATE "C" NOT NULL DEFAULT '';
ALTER TABLE share_bundles ADD COLUMN allow_download BIGINT NOT NULL DEFAULT 1;
