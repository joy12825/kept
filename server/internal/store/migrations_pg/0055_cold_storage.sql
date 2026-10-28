-- v1.7.8: hot and cold. Documents older than cold_after_months (by their
-- date) belong in cold_storage; the rest in the workspace's storage. The
-- move job puts each document where it belongs; a daily check queues it.
ALTER TABLE workspaces ADD COLUMN cold_storage TEXT COLLATE "C" NOT NULL DEFAULT '';
ALTER TABLE workspaces ADD COLUMN cold_after_months INTEGER NOT NULL DEFAULT 0;
