-- v1.7.13: a file uploaded under the name of a document already in the
-- same drawer can become that document's new version.
-- drawers.same_name_versions: do so without asking.
-- workspaces.keep_versions: the most earlier versions a document keeps;
-- 0 keeps them all.
-- documents.origin_path: where in its workspace's consume folder the file
-- came from, so a file put there again under the same path can become a
-- new version (when the instance turns that on).
ALTER TABLE drawers ADD COLUMN same_name_versions INTEGER NOT NULL DEFAULT 0;
ALTER TABLE workspaces ADD COLUMN keep_versions INTEGER NOT NULL DEFAULT 0;
ALTER TABLE documents ADD COLUMN origin_path TEXT;
CREATE INDEX documents_origin_path ON documents (workspace_id, origin_path) WHERE origin_path IS NOT NULL;
CREATE INDEX documents_drawer_filename ON documents (workspace_id, drawer_id, original_filename);
