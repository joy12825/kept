-- kept:foreign_keys=off
-- v1.8.3: what a report concludes, health reminders, the metric fact card
-- and the summary a doctor sees with a shared bundle (package health).

-- A report's conclusion, diagnosis, advice and when to come back: reviewed
-- together with its results.
ALTER TABLE lab_reports ADD COLUMN conclusion TEXT NOT NULL DEFAULT '';
ALTER TABLE lab_reports ADD COLUMN diagnosis TEXT NOT NULL DEFAULT '';
ALTER TABLE lab_reports ADD COLUMN advice TEXT NOT NULL DEFAULT '';
ALTER TABLE lab_reports ADD COLUMN recheck_on TEXT NOT NULL DEFAULT '';
ALTER TABLE lab_reports ADD COLUMN recheck_note TEXT NOT NULL DEFAULT '';

-- One reminder per condition (key, e.g. range:<person>:<metric>:<document>).
-- A dismissed one stays dismissed; one whose condition no longer holds is
-- resolved.
CREATE TABLE health_alerts (
    id           INTEGER PRIMARY KEY,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    person_id    INTEGER REFERENCES persons (id) ON DELETE CASCADE,
    kind         TEXT    NOT NULL CHECK (kind IN ('range', 'trend', 'change', 'recheck')),
    metric_id    INTEGER REFERENCES metrics (id) ON DELETE CASCADE,
    document_id  INTEGER NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    key          TEXT    NOT NULL,
    data         TEXT    NOT NULL DEFAULT '{}',
    due_date     TEXT    NOT NULL DEFAULT '',
    status       TEXT    NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'dismissed', 'resolved')),
    emailed_at   INTEGER,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    UNIQUE (workspace_id, key)
);
CREATE INDEX health_alerts_open ON health_alerts (workspace_id, status);

CREATE TABLE health_settings (
    workspace_id INTEGER PRIMARY KEY REFERENCES workspaces (id) ON DELETE CASCADE,
    config       TEXT    NOT NULL DEFAULT '{}'
);

-- The summary a doctor sees first when opening a shared bundle.
CREATE TABLE share_bundle_summaries (
    bundle_id INTEGER PRIMARY KEY REFERENCES share_bundles (id) ON DELETE CASCADE,
    summary   TEXT    NOT NULL
);

-- Drawer facts may show a metric.
CREATE TABLE drawer_facts_new (
    id         INTEGER PRIMARY KEY,
    drawer_id  INTEGER NOT NULL REFERENCES drawers (id) ON DELETE CASCADE,
    kind       TEXT    NOT NULL CHECK (kind IN ('latest', 'due', 'sum', 'expect', 'pinned', 'metric')),
    label      TEXT    NOT NULL,
    config     TEXT    NOT NULL DEFAULT '{}',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);
INSERT INTO drawer_facts_new SELECT id, drawer_id, kind, label, config, sort_order, created_at FROM drawer_facts;
DROP TABLE drawer_facts;
ALTER TABLE drawer_facts_new RENAME TO drawer_facts;
CREATE INDEX drawer_facts_drawer ON drawer_facts (drawer_id, sort_order);
