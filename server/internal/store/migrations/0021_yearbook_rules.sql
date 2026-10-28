-- v0.3 stage 21: a workspace's yearbook milestone rules as JSON
-- ([{"verb": ..., "filter": ...}]); NULL means the defaults.
ALTER TABLE workspaces ADD COLUMN yearbook_rules TEXT;
