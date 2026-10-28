-- v1.7.4: the sizes of a document's stored files, so a workspace's usage
-- adds up in the database instead of asking every file's storage (slow and
-- billed per request on S3). original_size is the original; derived_size
-- the searchable PDF and the page-edit base. NULL is not known yet: set
-- when files change, counted from the storage once by the next recount.
ALTER TABLE documents ADD COLUMN original_size BIGINT;
ALTER TABLE documents ADD COLUMN derived_size BIGINT;
-- The quota warning last emailed to the workspace's administrators: 90
-- (nearly full) or 100 (full); 0 again once usage falls below 85%.
ALTER TABLE workspaces ADD COLUMN quota_notified INTEGER NOT NULL DEFAULT 0;
