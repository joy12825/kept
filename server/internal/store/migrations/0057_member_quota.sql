-- v1.7.11: member quotas. documents.added_by is who uploaded a document
-- (none for mail, the consume directory, remote inboxes, upload links);
-- older documents take it from the activity log while it still has it.
-- workspace_members.upload_quota is the most a member may upload into the
-- workspace, bytes; NULL is no limit of their own.
ALTER TABLE documents ADD COLUMN added_by INTEGER REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE workspace_members ADD COLUMN upload_quota INTEGER;
UPDATE documents SET added_by = (
    SELECT a.actor_user_id FROM audit_events AS a
    WHERE a.workspace_id = documents.workspace_id AND a.action = 'document.create' AND a.target_type = 'document'
      AND a.target_id = CAST(documents.id AS TEXT) AND a.actor_via <> 'system'
    ORDER BY a.id LIMIT 1);
