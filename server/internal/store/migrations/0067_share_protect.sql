-- v1.9.2: a share can carry a watermark (text stamped on every PDF page it
-- serves) and can be view-only (no download buttons, downloads refused).
ALTER TABLE share_links ADD COLUMN watermark TEXT NOT NULL DEFAULT '';
ALTER TABLE share_links ADD COLUMN allow_download INTEGER NOT NULL DEFAULT 1;
ALTER TABLE share_bundles ADD COLUMN watermark TEXT NOT NULL DEFAULT '';
ALTER TABLE share_bundles ADD COLUMN allow_download INTEGER NOT NULL DEFAULT 1;
