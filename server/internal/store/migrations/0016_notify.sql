-- v0.3 stage 16: reminder emails. A member opts in per workspace; an alert
-- is emailed once (emailed_at), in the next morning's digest.
ALTER TABLE workspace_members ADD COLUMN notify_email INTEGER NOT NULL DEFAULT 0;
ALTER TABLE alerts ADD COLUMN emailed_at INTEGER;
