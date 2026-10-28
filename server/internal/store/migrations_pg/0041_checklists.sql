-- v1.6.2: checklists of the papers an errand needs (a visa, school
-- enrolment, an expense claim). items is a JSON array of
-- {id, label, filter, count, note}: each item is a filter (design doc 11.9)
-- and how many of the newest matching documents it wants.
CREATE TABLE checklists (
    id           BIGINT PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         TEXT COLLATE "C" NOT NULL,
    note         TEXT COLLATE "C" NOT NULL DEFAULT '',
    items        TEXT COLLATE "C" NOT NULL DEFAULT '[]',
    created_by   BIGINT REFERENCES users (id) ON DELETE SET NULL,
    created_at   BIGINT NOT NULL,
    updated_at   BIGINT NOT NULL
);
CREATE INDEX checklists_workspace ON checklists (workspace_id);
CREATE TRIGGER checklists_id BEFORE INSERT ON checklists FOR EACH ROW EXECUTE FUNCTION kept_next_id();
