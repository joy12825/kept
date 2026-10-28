-- v1.9.0: scenes (templates of metrics beyond health), entries made by
-- hand, medications (package health).

CREATE TABLE scenes (
    id           BIGINT PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    key          TEXT COLLATE "C" NOT NULL DEFAULT '',
    name         TEXT COLLATE "C" NOT NULL,
    icon         TEXT COLLATE "C" NOT NULL DEFAULT '',
    by_person    BIGINT NOT NULL DEFAULT 1,
    private      BIGINT NOT NULL DEFAULT 0,
    sort_order   BIGINT NOT NULL DEFAULT 0,
    created_at   BIGINT NOT NULL
);
CREATE UNIQUE INDEX scenes_key ON scenes (workspace_id, key) WHERE key <> '';
CREATE TRIGGER scenes_id BEFORE INSERT ON scenes FOR EACH ROW EXECUTE FUNCTION kept_next_id();

CREATE TABLE scene_metrics (
    scene_id   BIGINT NOT NULL REFERENCES scenes (id) ON DELETE CASCADE,
    metric_id  BIGINT NOT NULL REFERENCES metrics (id) ON DELETE CASCADE,
    sort_order BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (scene_id, metric_id)
);
CREATE INDEX scene_metrics_metric ON scene_metrics (metric_id);

ALTER TABLE metrics ADD COLUMN direction TEXT COLLATE "C" NOT NULL DEFAULT '';

ALTER TABLE observations ALTER COLUMN document_id DROP NOT NULL;
ALTER TABLE observations ADD COLUMN note TEXT COLLATE "C" NOT NULL DEFAULT '';
ALTER TABLE observations ADD COLUMN private BIGINT NOT NULL DEFAULT 0;
ALTER TABLE observations ADD COLUMN added_by BIGINT REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE observations ADD COLUMN uploader_access BIGINT NOT NULL DEFAULT 1;

CREATE TABLE medications (
    id           BIGINT PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    person_id    BIGINT REFERENCES persons (id) ON DELETE SET NULL,
    document_id  BIGINT REFERENCES documents (id) ON DELETE CASCADE,
    name         TEXT COLLATE "C" NOT NULL,
    spec         TEXT COLLATE "C" NOT NULL DEFAULT '',
    dose         TEXT COLLATE "C" NOT NULL DEFAULT '',
    frequency    TEXT COLLATE "C" NOT NULL DEFAULT '',
    days         BIGINT NOT NULL DEFAULT 0,
    start_on     TEXT COLLATE "C" NOT NULL DEFAULT '',
    end_on       TEXT COLLATE "C" NOT NULL DEFAULT '',
    status       TEXT COLLATE "C" NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'stopped')),
    note         TEXT COLLATE "C" NOT NULL DEFAULT '',
    source       TEXT COLLATE "C" NOT NULL DEFAULT 'rule' CHECK (source IN ('rule', 'ai', 'manual')),
    sort_order   BIGINT NOT NULL DEFAULT 0,
    private      BIGINT NOT NULL DEFAULT 0,
    added_by     BIGINT REFERENCES users (id) ON DELETE SET NULL,
    uploader_access BIGINT NOT NULL DEFAULT 1,
    created_at   BIGINT NOT NULL,
    updated_at   BIGINT NOT NULL
);
CREATE INDEX medications_person ON medications (workspace_id, person_id, status);
CREATE INDEX medications_document ON medications (document_id, sort_order);
CREATE TRIGGER medications_id BEFORE INSERT ON medications FOR EACH ROW EXECUTE FUNCTION kept_next_id();

INSERT INTO scenes (workspace_id, key, name, icon, by_person, private, sort_order, created_at)
  SELECT DISTINCT workspace_id, 'health', '体检与化验', 'heart', 1, 1, 0, (extract(epoch from now()) * 1000)::bigint FROM metrics;
INSERT INTO scene_metrics (scene_id, metric_id, sort_order)
  SELECT s.id, m.id, m.sort_order FROM metrics m JOIN scenes s ON s.workspace_id = m.workspace_id AND s.key = 'health';

ALTER TABLE health_alerts ALTER COLUMN document_id DROP NOT NULL;
ALTER TABLE health_alerts ADD COLUMN observation_id BIGINT REFERENCES observations (id) ON DELETE CASCADE;
