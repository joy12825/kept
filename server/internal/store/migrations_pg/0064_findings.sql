-- v1.8.3: what a report concludes, health reminders, the metric fact card
-- and the summary a doctor sees with a shared bundle (package health).

ALTER TABLE lab_reports ADD COLUMN conclusion TEXT COLLATE "C" NOT NULL DEFAULT '';
ALTER TABLE lab_reports ADD COLUMN diagnosis TEXT COLLATE "C" NOT NULL DEFAULT '';
ALTER TABLE lab_reports ADD COLUMN advice TEXT COLLATE "C" NOT NULL DEFAULT '';
ALTER TABLE lab_reports ADD COLUMN recheck_on TEXT COLLATE "C" NOT NULL DEFAULT '';
ALTER TABLE lab_reports ADD COLUMN recheck_note TEXT COLLATE "C" NOT NULL DEFAULT '';

CREATE TABLE health_alerts (
    id           BIGINT PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    person_id    BIGINT REFERENCES persons (id) ON DELETE CASCADE,
    kind         TEXT COLLATE "C" NOT NULL CHECK (kind IN ('range', 'trend', 'change', 'recheck')),
    metric_id    BIGINT REFERENCES metrics (id) ON DELETE CASCADE,
    document_id  BIGINT NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    key          TEXT COLLATE "C" NOT NULL,
    data         TEXT COLLATE "C" NOT NULL DEFAULT '{}',
    due_date     TEXT COLLATE "C" NOT NULL DEFAULT '',
    status       TEXT COLLATE "C" NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'dismissed', 'resolved')),
    emailed_at   BIGINT,
    created_at   BIGINT NOT NULL,
    updated_at   BIGINT NOT NULL,
    UNIQUE (workspace_id, key)
);
CREATE INDEX health_alerts_open ON health_alerts (workspace_id, status);
CREATE TRIGGER health_alerts_id BEFORE INSERT ON health_alerts FOR EACH ROW EXECUTE FUNCTION kept_next_id();

CREATE TABLE health_settings (
    workspace_id BIGINT PRIMARY KEY REFERENCES workspaces (id) ON DELETE CASCADE,
    config       TEXT COLLATE "C" NOT NULL DEFAULT '{}'
);

CREATE TABLE share_bundle_summaries (
    bundle_id BIGINT PRIMARY KEY REFERENCES share_bundles (id) ON DELETE CASCADE,
    summary   TEXT COLLATE "C" NOT NULL
);

ALTER TABLE drawer_facts DROP CONSTRAINT drawer_facts_kind_check;
ALTER TABLE drawer_facts ADD CONSTRAINT drawer_facts_kind_check CHECK (kind IN ('latest', 'due', 'sum', 'expect', 'pinned', 'metric'));
