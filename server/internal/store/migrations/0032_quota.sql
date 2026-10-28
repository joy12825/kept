-- v1.0 stage 36: how much a workspace may store. NULL follows the
-- instance's KEPT_DEFAULT_QUOTA, 0 is no limit, otherwise bytes.
ALTER TABLE workspaces ADD COLUMN quota_bytes INTEGER;
