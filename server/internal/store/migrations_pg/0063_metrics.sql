-- v1.8.2: metrics. A lab report becomes rows of numbers that can be
-- compared over time (package health).

-- The workspace's dictionary of metrics: built-in ones carry a code.
CREATE TABLE metrics (
    id             BIGINT PRIMARY KEY,
    workspace_id   BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    code           TEXT COLLATE "C"    NOT NULL DEFAULT '',
    name           TEXT COLLATE "C"    NOT NULL,
    aliases        TEXT COLLATE "C"    NOT NULL DEFAULT '[]',
    category       TEXT COLLATE "C"    NOT NULL DEFAULT '',
    specimen       TEXT COLLATE "C"    NOT NULL DEFAULT '',  -- blood, urine, stool, other
    unit           TEXT COLLATE "C"    NOT NULL DEFAULT '',
    conversions    TEXT COLLATE "C"    NOT NULL DEFAULT '{}', -- unit → factor to the standard unit
    ref_low        DOUBLE PRECISION,
    ref_high       DOUBLE PRECISION,
    plausible_low  DOUBLE PRECISION,
    plausible_high DOUBLE PRECISION,
    kind           TEXT COLLATE "C"    NOT NULL DEFAULT 'number' CHECK (kind IN ('number', 'qualitative')),
    sort_order     BIGINT NOT NULL DEFAULT 0,
    created_at     BIGINT NOT NULL,
    updated_at     BIGINT NOT NULL,
    UNIQUE (workspace_id, name)
);
CREATE TRIGGER metrics_id BEFORE INSERT ON metrics FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE INDEX metrics_code ON metrics (workspace_id, code);

-- One visit to a doctor: the papers of one person, one day, one hospital.
CREATE TABLE visits (
    id               BIGINT PRIMARY KEY,
    workspace_id     BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    person_id        BIGINT REFERENCES persons (id) ON DELETE SET NULL,
    visit_date       TEXT COLLATE "C"    NOT NULL,
    correspondent_id BIGINT REFERENCES correspondents (id) ON DELETE SET NULL,
    department       TEXT COLLATE "C"    NOT NULL DEFAULT '',
    note             TEXT COLLATE "C"    NOT NULL DEFAULT '',
    created_at       BIGINT NOT NULL,
    updated_at       BIGINT NOT NULL
);
CREATE TRIGGER visits_id BEFORE INSERT ON visits FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE INDEX visits_person ON visits (workspace_id, person_id, visit_date);
ALTER TABLE documents ADD COLUMN visit_id BIGINT REFERENCES visits (id) ON DELETE SET NULL;
CREATE INDEX documents_visit ON documents (visit_id);

-- What was recognised on a document; observations hang off it.
CREATE TABLE lab_reports (
    document_id  BIGINT PRIMARY KEY REFERENCES documents (id) ON DELETE CASCADE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    status       TEXT COLLATE "C"    NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('running', 'pending', 'confirmed', 'empty', 'failed')),
    source       TEXT COLLATE "C"    NOT NULL DEFAULT '', -- rule, ai, rule+ai, manual
    patient      TEXT COLLATE "C"    NOT NULL DEFAULT '',
    specimen     TEXT COLLATE "C"    NOT NULL DEFAULT '',
    sampled_on   TEXT COLLATE "C"    NOT NULL DEFAULT '',
    reported_on  TEXT COLLATE "C"    NOT NULL DEFAULT '',
    department   TEXT COLLATE "C"    NOT NULL DEFAULT '',
    error        TEXT COLLATE "C"    NOT NULL DEFAULT '',
    extracted_at BIGINT NOT NULL,
    confirmed_by BIGINT REFERENCES users (id) ON DELETE SET NULL,
    confirmed_at BIGINT
);
CREATE INDEX lab_reports_status ON lab_reports (workspace_id, status);

CREATE TABLE observations (
    id           BIGINT PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    document_id  BIGINT NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    person_id    BIGINT REFERENCES persons (id) ON DELETE SET NULL,
    metric_id    BIGINT REFERENCES metrics (id) ON DELETE SET NULL,
    raw_name     TEXT COLLATE "C"    NOT NULL DEFAULT '',
    value        DOUBLE PRECISION,
    value_text   TEXT COLLATE "C"    NOT NULL DEFAULT '',
    unit         TEXT COLLATE "C"    NOT NULL DEFAULT '',
    value_std    DOUBLE PRECISION,
    ref_low      DOUBLE PRECISION,
    ref_high     DOUBLE PRECISION,
    ref_text     TEXT COLLATE "C"    NOT NULL DEFAULT '',
    flag         TEXT COLLATE "C"    NOT NULL DEFAULT '' CHECK (flag IN ('', 'high', 'low', 'abnormal')),
    observed_on  TEXT COLLATE "C"    NOT NULL DEFAULT '',
    page         BIGINT NOT NULL DEFAULT 0,
    bbox         TEXT COLLATE "C"    NOT NULL DEFAULT '',
    confidence   DOUBLE PRECISION    NOT NULL DEFAULT 0,
    source       TEXT COLLATE "C"    NOT NULL DEFAULT 'rule' CHECK (source IN ('rule', 'ai', 'manual')),
    sort_order   BIGINT NOT NULL DEFAULT 0,
    created_at   BIGINT NOT NULL
);
CREATE TRIGGER observations_id BEFORE INSERT ON observations FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE INDEX observations_document ON observations (document_id, sort_order);
CREATE INDEX observations_series ON observations (workspace_id, person_id, metric_id, observed_on);

-- Documents of these types have their metrics recognised when added.
CREATE TABLE type_metrics (
    document_type_id BIGINT PRIMARY KEY REFERENCES document_types (id) ON DELETE CASCADE,
    workspace_id     BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE
);
CREATE INDEX type_metrics_workspace ON type_metrics (workspace_id);
