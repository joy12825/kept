-- v1.11.5: readings on bills and service records, and reminders to have
-- the car serviced. A car scene says how often (kilometres, months; 0 not
-- by that); a mileage that was a service is marked.
ALTER TABLE scenes ADD COLUMN service_km INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scenes ADD COLUMN service_months INTEGER NOT NULL DEFAULT 0;
UPDATE scenes SET service_km = 5000, service_months = 6 WHERE key = 'car';
ALTER TABLE observations ADD COLUMN service INTEGER NOT NULL DEFAULT 0;
-- the mileage read on a service record is a service
UPDATE observations SET service = 1 WHERE document_id IS NOT NULL AND metric_id IN (SELECT id FROM metrics WHERE code = 'mileage');
-- workspaces with the car scene recognise their service records
INSERT INTO type_metrics (document_type_id, workspace_id)
SELECT dt.id, dt.workspace_id FROM document_types dt JOIN scenes s ON s.workspace_id = dt.workspace_id AND s.key = 'car'
WHERE dt.name IN ('保养单', 'Service record')
ON CONFLICT (document_type_id) DO NOTHING;
-- reminders gain a kind: the car's service
CREATE TABLE health_alerts_new (
    id           INTEGER PRIMARY KEY,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    person_id    INTEGER REFERENCES persons (id) ON DELETE CASCADE,
    kind         TEXT    NOT NULL CHECK (kind IN ('range', 'trend', 'change', 'recheck', 'service')),
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
INSERT INTO health_alerts_new (id, workspace_id, person_id, kind, metric_id, document_id, key, data, due_date, status, emailed_at, created_at, updated_at, observation_id)
  SELECT id, workspace_id, person_id, kind, metric_id, document_id, key, data, due_date, status, emailed_at, created_at, updated_at, observation_id FROM health_alerts;
DROP TABLE health_alerts;
ALTER TABLE health_alerts_new RENAME TO health_alerts;
CREATE INDEX health_alerts_open ON health_alerts (workspace_id, status);
