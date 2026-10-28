-- v1.6.2: checklists of the papers an errand needs (a visa, school
-- enrolment, an expense claim). items is a JSON array of
-- {id, label, filter, count, note}: each item is a filter (design doc 11.9)
-- and how many of the newest matching documents it wants.
CREATE TABLE checklists (
    id           INTEGER PRIMARY KEY,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    note         TEXT    NOT NULL DEFAULT '',
    items        TEXT    NOT NULL DEFAULT '[]',
    created_by   INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
CREATE INDEX checklists_workspace ON checklists (workspace_id);
