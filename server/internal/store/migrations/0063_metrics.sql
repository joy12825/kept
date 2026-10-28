-- v1.8.2: metrics. A lab report becomes rows of numbers that can be
-- compared over time (package health).

-- The workspace's dictionary of metrics: built-in ones carry a code.
CREATE TABLE metrics (
    id             INTEGER PRIMARY KEY,
    workspace_id   INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    code           TEXT    NOT NULL DEFAULT '',
    name           TEXT    NOT NULL,
    aliases        TEXT    NOT NULL DEFAULT '[]',
    category       TEXT    NOT NULL DEFAULT '',
    specimen       TEXT    NOT NULL DEFAULT '',  -- blood, urine, stool, other
    unit           TEXT    NOT NULL DEFAULT '',
    conversions    TEXT    NOT NULL DEFAULT '{}', -- unit → factor to the standard unit
    ref_low        REAL,
    ref_high       REAL,
    plausible_low  REAL,
    plausible_high REAL,
    kind           TEXT    NOT NULL DEFAULT 'number' CHECK (kind IN ('number', 'qualitative')),
    sort_order     INTEGER NOT NULL DEFAULT 0,
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL,
    UNIQUE (workspace_id, name)
);
CREATE INDEX metrics_code ON metrics (workspace_id, code);

-- One visit to a doctor: the papers of one person, one day, one hospital.
CREATE TABLE visits (
    id               INTEGER PRIMARY KEY,
    workspace_id     INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    person_id        INTEGER REFERENCES persons (id) ON DELETE SET NULL,
    visit_date       TEXT    NOT NULL,
    correspondent_id INTEGER REFERENCES correspondents (id) ON DELETE SET NULL,
    department       TEXT    NOT NULL DEFAULT '',
    note             TEXT    NOT NULL DEFAULT '',
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);
CREATE INDEX visits_person ON visits (workspace_id, person_id, visit_date);
ALTER TABLE documents ADD COLUMN visit_id INTEGER REFERENCES visits (id) ON DELETE SET NULL;
CREATE INDEX documents_visit ON documents (visit_id);

-- What was recognised on a document; observations hang off it.
CREATE TABLE lab_reports (
    document_id  INTEGER PRIMARY KEY REFERENCES documents (id) ON DELETE CASCADE,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    status       TEXT    NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('running', 'pending', 'confirmed', 'empty', 'failed')),
    source       TEXT    NOT NULL DEFAULT '', -- rule, ai, rule+ai, manual
    patient      TEXT    NOT NULL DEFAULT '',
    specimen     TEXT    NOT NULL DEFAULT '',
    sampled_on   TEXT    NOT NULL DEFAULT '',
    reported_on  TEXT    NOT NULL DEFAULT '',
    department   TEXT    NOT NULL DEFAULT '',
    error        TEXT    NOT NULL DEFAULT '',
    extracted_at INTEGER NOT NULL,
    confirmed_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
    confirmed_at INTEGER
);
CREATE INDEX lab_reports_status ON lab_reports (workspace_id, status);

CREATE TABLE observations (
    id           INTEGER PRIMARY KEY,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    document_id  INTEGER NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    person_id    INTEGER REFERENCES persons (id) ON DELETE SET NULL,
    metric_id    INTEGER REFERENCES metrics (id) ON DELETE SET NULL,
    raw_name     TEXT    NOT NULL DEFAULT '',
    value        REAL,
    value_text   TEXT    NOT NULL DEFAULT '',
    unit         TEXT    NOT NULL DEFAULT '',
    value_std    REAL,
    ref_low      REAL,
    ref_high     REAL,
    ref_text     TEXT    NOT NULL DEFAULT '',
    flag         TEXT    NOT NULL DEFAULT '' CHECK (flag IN ('', 'high', 'low', 'abnormal')),
    observed_on  TEXT    NOT NULL DEFAULT '',
    page         INTEGER NOT NULL DEFAULT 0,
    bbox         TEXT    NOT NULL DEFAULT '',
    confidence   REAL    NOT NULL DEFAULT 0,
    source       TEXT    NOT NULL DEFAULT 'rule' CHECK (source IN ('rule', 'ai', 'manual')),
    sort_order   INTEGER NOT NULL DEFAULT 0,
    created_at   INTEGER NOT NULL
);
CREATE INDEX observations_document ON observations (document_id, sort_order);
CREATE INDEX observations_series ON observations (workspace_id, person_id, metric_id, observed_on);

-- Documents of these types have their metrics recognised when added.
CREATE TABLE type_metrics (
    document_type_id INTEGER PRIMARY KEY REFERENCES document_types (id) ON DELETE CASCADE,
    workspace_id     INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE
);
CREATE INDEX type_metrics_workspace ON type_metrics (workspace_id);
