-- v1.11.5: readings and service reminders (see the SQLite migration).
ALTER TABLE scenes ADD COLUMN service_km BIGINT NOT NULL DEFAULT 0;
ALTER TABLE scenes ADD COLUMN service_months BIGINT NOT NULL DEFAULT 0;
UPDATE scenes SET service_km = 5000, service_months = 6 WHERE key = 'car';
ALTER TABLE observations ADD COLUMN service BIGINT NOT NULL DEFAULT 0;
-- the mileage read on a service record is a service
UPDATE observations SET service = 1 WHERE document_id IS NOT NULL AND metric_id IN (SELECT id FROM metrics WHERE code = 'mileage');
-- workspaces with the car scene recognise their service records
INSERT INTO type_metrics (document_type_id, workspace_id)
SELECT dt.id, dt.workspace_id FROM document_types dt JOIN scenes s ON s.workspace_id = dt.workspace_id AND s.key = 'car'
WHERE dt.name IN ('保养单', 'Service record')
ON CONFLICT (document_type_id) DO NOTHING;
-- reminders gain a kind: the car's service
ALTER TABLE health_alerts DROP CONSTRAINT health_alerts_kind_check;
ALTER TABLE health_alerts ADD CONSTRAINT health_alerts_kind_check CHECK (kind IN ('range', 'trend', 'change', 'recheck', 'service'));
