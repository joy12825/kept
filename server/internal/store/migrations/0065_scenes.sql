-- v1.9.0: scenes (templates of metrics beyond health), entries made by
-- hand, medications (package health).

CREATE TABLE scenes (
    id           INTEGER PRIMARY KEY,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    key          TEXT    NOT NULL DEFAULT '', -- health, growth, utility, car; '' made by a person
    name         TEXT    NOT NULL,
    icon         TEXT    NOT NULL DEFAULT '',
    by_person    INTEGER NOT NULL DEFAULT 1,
    private      INTEGER NOT NULL DEFAULT 0,
    sort_order   INTEGER NOT NULL DEFAULT 0,
    created_at   INTEGER NOT NULL
);
CREATE UNIQUE INDEX scenes_key ON scenes (workspace_id, key) WHERE key <> '';

CREATE TABLE scene_metrics (
    scene_id   INTEGER NOT NULL REFERENCES scenes (id) ON DELETE CASCADE,
    metric_id  INTEGER NOT NULL REFERENCES metrics (id) ON DELETE CASCADE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (scene_id, metric_id)
);
CREATE INDEX scene_metrics_metric ON scene_metrics (metric_id);

-- '' judges by the range (a size: bigger is worse); 'none' only records.
ALTER TABLE metrics ADD COLUMN direction TEXT NOT NULL DEFAULT '';

-- Entries made by hand have no document; they carry the same privacy
-- columns as documents so privacy.Visible applies to them.
CREATE TABLE observations_new (
    id INTEGER PRIMARY KEY, workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    document_id INTEGER REFERENCES documents (id) ON DELETE CASCADE,
    person_id INTEGER REFERENCES persons (id) ON DELETE SET NULL,
    metric_id INTEGER REFERENCES metrics (id) ON DELETE SET NULL,
    raw_name TEXT NOT NULL DEFAULT '', value REAL, value_text TEXT NOT NULL DEFAULT '', unit TEXT NOT NULL DEFAULT '',
    value_std REAL, ref_low REAL, ref_high REAL, ref_text TEXT NOT NULL DEFAULT '',
    flag TEXT NOT NULL DEFAULT '' CHECK (flag IN ('', 'high', 'low', 'abnormal')),
    observed_on TEXT NOT NULL DEFAULT '', page INTEGER NOT NULL DEFAULT 0, bbox TEXT NOT NULL DEFAULT '',
    confidence REAL NOT NULL DEFAULT 0,
    source TEXT NOT NULL DEFAULT 'rule' CHECK (source IN ('rule', 'ai', 'manual')),
    sort_order INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL,
    note TEXT NOT NULL DEFAULT '', private INTEGER NOT NULL DEFAULT 0,
    added_by INTEGER REFERENCES users (id) ON DELETE SET NULL, uploader_access INTEGER NOT NULL DEFAULT 1);
INSERT INTO observations_new (id, workspace_id, document_id, person_id, metric_id, raw_name, value, value_text, unit, value_std, ref_low, ref_high, ref_text,
    flag, observed_on, page, bbox, confidence, source, sort_order, created_at)
  SELECT id, workspace_id, document_id, person_id, metric_id, raw_name, value, value_text, unit, value_std, ref_low, ref_high, ref_text,
    flag, observed_on, page, bbox, confidence, source, sort_order, created_at FROM observations;
DROP TABLE observations;
ALTER TABLE observations_new RENAME TO observations;
CREATE INDEX observations_document ON observations (document_id, sort_order);
CREATE INDEX observations_series ON observations (workspace_id, person_id, metric_id, observed_on);

CREATE TABLE medications (
    id           INTEGER PRIMARY KEY,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    person_id    INTEGER REFERENCES persons (id) ON DELETE SET NULL,
    document_id  INTEGER REFERENCES documents (id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    spec         TEXT    NOT NULL DEFAULT '', -- 20mg×7片
    dose         TEXT    NOT NULL DEFAULT '', -- 20mg
    frequency    TEXT    NOT NULL DEFAULT '', -- 每晚一次
    days         INTEGER NOT NULL DEFAULT 0,
    start_on     TEXT    NOT NULL DEFAULT '',
    end_on       TEXT    NOT NULL DEFAULT '', -- '' while taken
    status       TEXT    NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'stopped')),
    note         TEXT    NOT NULL DEFAULT '',
    source       TEXT    NOT NULL DEFAULT 'rule' CHECK (source IN ('rule', 'ai', 'manual')),
    sort_order   INTEGER NOT NULL DEFAULT 0,
    private      INTEGER NOT NULL DEFAULT 0,
    added_by     INTEGER REFERENCES users (id) ON DELETE SET NULL,
    uploader_access INTEGER NOT NULL DEFAULT 1,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
CREATE INDEX medications_person ON medications (workspace_id, person_id, status);
CREATE INDEX medications_document ON medications (document_id, sort_order);

-- Workspaces with metrics have the health scene, holding them all.
INSERT INTO scenes (workspace_id, key, name, icon, by_person, private, sort_order, created_at)
  SELECT DISTINCT workspace_id, 'health', '体检与化验', 'heart', 1, 1, 0, CAST(strftime('%s', 'now') AS INTEGER) * 1000 FROM metrics;
INSERT INTO scene_metrics (scene_id, metric_id, sort_order)
  SELECT s.id, m.id, m.sort_order FROM metrics m JOIN scenes s ON s.workspace_id = m.workspace_id AND s.key = 'health';

-- A reminder about an entry made by hand points at it, not a document.
CREATE TABLE health_alerts_new (
    id           INTEGER PRIMARY KEY,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    person_id    INTEGER REFERENCES persons (id) ON DELETE CASCADE,
    kind         TEXT    NOT NULL CHECK (kind IN ('range', 'trend', 'change', 'recheck')),
    metric_id    INTEGER REFERENCES metrics (id) ON DELETE CASCADE,
    document_id  INTEGER REFERENCES documents (id) ON DELETE CASCADE,
    key          TEXT    NOT NULL,
    data         TEXT    NOT NULL DEFAULT '{}',
    due_date     TEXT    NOT NULL DEFAULT '',
    status       TEXT    NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'dismissed', 'resolved')),
    emailed_at   INTEGER,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    observation_id INTEGER REFERENCES observations (id) ON DELETE CASCADE,
    UNIQUE (workspace_id, key)
);
INSERT INTO health_alerts_new (id, workspace_id, person_id, kind, metric_id, document_id, key, data, due_date, status, emailed_at, created_at, updated_at)
  SELECT id, workspace_id, person_id, kind, metric_id, document_id, key, data, due_date, status, emailed_at, created_at, updated_at FROM health_alerts;
DROP TABLE health_alerts;
ALTER TABLE health_alerts_new RENAME TO health_alerts;
CREATE INDEX health_alerts_open ON health_alerts (workspace_id, status);
