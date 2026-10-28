-- v1.6.5: how long documents of a type are worth keeping (after Mayan
-- EDMS's retention policies): 账单 2 年, 快递单 6 个月. Older ones are only
-- suggested for clean-up, never deleted by themselves. A type without a
-- row is kept for ever.
CREATE TABLE type_retention (
    document_type_id INTEGER PRIMARY KEY REFERENCES document_types (id) ON DELETE CASCADE,
    workspace_id     INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    months           INTEGER NOT NULL CHECK (months > 0)
);
CREATE INDEX type_retention_workspace ON type_retention (workspace_id);
