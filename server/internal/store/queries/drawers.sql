-- name: ListDrawers :many
SELECT * FROM drawers WHERE workspace_id = ? ORDER BY sort_order, id;

-- name: GetDrawer :one
SELECT * FROM drawers WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: InsertDrawer :one
INSERT INTO drawers (workspace_id, name, icon, color, description, template_key, rule, sort_order, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateDrawer :exec
UPDATE drawers SET name = sqlc.arg(name), icon = sqlc.arg(icon), color = sqlc.arg(color), description = sqlc.arg(description),
    rule = sqlc.arg(rule), updated_at = sqlc.arg(updated_at)
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: SetDrawerOrder :exec
UPDATE drawers SET sort_order = sqlc.arg(sort_order) WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: DeleteDrawer :execrows
DELETE FROM drawers WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: MaxDrawerOrder :one
SELECT CAST(coalesce(max(sort_order), 0) AS INTEGER) FROM drawers WHERE workspace_id = ?;

-- Documents per drawer (outside the trash); a NULL drawer_id counts the
-- documents in no drawer.
-- name: CountDocumentsByDrawer :many
SELECT drawer_id, count(*) AS n FROM documents
WHERE documents.workspace_id = sqlc.arg(workspace_id) AND documents.deleted_at IS NULL
  -- viewer: private documents (v1.8.0), as privacy.Visible
  AND (documents.private = 0 OR documents.person_id IN (SELECT pa.person_id FROM person_access AS pa WHERE pa.user_id = sqlc.arg(viewer)) OR (documents.added_by = sqlc.arg(viewer) AND documents.uploader_access = 1) OR sqlc.arg(viewer) = -1)
GROUP BY drawer_id;

-- Releases documents a person put into a drawer that is being deleted.
-- name: ClearManualDrawer :exec
UPDATE documents SET drawer_manual = 0 WHERE workspace_id = sqlc.arg(workspace_id) AND drawer_id = sqlc.arg(drawer_id);

-- name: SetDocumentDrawer :exec
UPDATE documents SET drawer_id = sqlc.narg(drawer_id), drawer_manual = sqlc.arg(drawer_manual)
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- Documents the rules decide on (not placed by hand), with their drawer.
-- name: ListRuledDocuments :many
SELECT id, drawer_id FROM documents
WHERE workspace_id = ? AND deleted_at IS NULL AND drawer_manual = 0
ORDER BY id;

-- name: CountManualInDrawer :one
SELECT count(*) FROM documents
WHERE documents.workspace_id = sqlc.arg(workspace_id) AND documents.drawer_id = sqlc.arg(drawer_id) AND documents.drawer_manual = 1 AND documents.deleted_at IS NULL
  -- viewer: private documents (v1.9.10), as privacy.Visible
  AND (documents.private = 0 OR documents.person_id IN (SELECT pa.person_id FROM person_access AS pa WHERE pa.user_id = sqlc.arg(viewer)) OR (documents.added_by = sqlc.arg(viewer) AND documents.uploader_access = 1) OR sqlc.arg(viewer) = -1);

-- name: ListDrawerFacts :many
SELECT f.* FROM drawer_facts AS f JOIN drawers AS d ON d.id = f.drawer_id
WHERE d.workspace_id = sqlc.arg(workspace_id) AND f.drawer_id = sqlc.arg(drawer_id)
ORDER BY f.sort_order, f.id;

-- name: ListWorkspaceFacts :many
SELECT f.* FROM drawer_facts AS f JOIN drawers AS d ON d.id = f.drawer_id
WHERE d.workspace_id = ?
ORDER BY d.sort_order, d.id, f.sort_order, f.id;

-- name: GetDrawerFact :one
SELECT f.* FROM drawer_facts AS f JOIN drawers AS d ON d.id = f.drawer_id
WHERE d.workspace_id = sqlc.arg(workspace_id) AND f.id = sqlc.arg(id);

-- name: InsertDrawerFact :one
INSERT INTO drawer_facts (drawer_id, kind, label, config, sort_order, created_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateDrawerFact :exec
UPDATE drawer_facts SET label = sqlc.arg(label), config = sqlc.arg(config) WHERE id = sqlc.arg(id);

-- name: SetDrawerFactOrder :exec
UPDATE drawer_facts SET sort_order = sqlc.arg(sort_order) WHERE id = sqlc.arg(id);

-- name: DeleteDrawerFact :exec
DELETE FROM drawer_facts WHERE id = ?;

-- name: MaxDrawerFactOrder :one
SELECT CAST(coalesce(max(sort_order), 0) AS INTEGER) FROM drawer_facts WHERE drawer_id = ?;

-- name: GetDocumentDrawerState :one
SELECT drawer_id, drawer_manual, field_sources FROM documents
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: GetDocumentTitle :one
SELECT title FROM documents WHERE documents.workspace_id = sqlc.arg(workspace_id) AND documents.id = sqlc.arg(id)
  -- viewer: private documents (v1.8.0), as privacy.Visible
  AND (documents.private = 0 OR documents.person_id IN (SELECT pa.person_id FROM person_access AS pa WHERE pa.user_id = sqlc.arg(viewer)) OR (documents.added_by = sqlc.arg(viewer) AND documents.uploader_access = 1) OR sqlc.arg(viewer) = -1);

-- An alert seen again: a resolved one reopens, a dismissed one stays dismissed.
-- name: UpsertAlert :exec
INSERT INTO alerts (workspace_id, drawer_id, fact_id, kind, period_key, document_id, period_label, due_date, status, created_at, updated_at)
VALUES (sqlc.arg(workspace_id), sqlc.arg(drawer_id), sqlc.arg(fact_id), sqlc.arg(kind), sqlc.arg(period_key), sqlc.narg(document_id),
        sqlc.arg(period_label), sqlc.narg(due_date), 'open', sqlc.arg(now), sqlc.arg(now))
ON CONFLICT (fact_id, period_key) DO UPDATE SET
    status = CASE WHEN alerts.status = 'resolved' THEN 'open' ELSE alerts.status END,
    drawer_id = excluded.drawer_id, document_id = excluded.document_id, period_label = excluded.period_label,
    due_date = excluded.due_date, updated_at = excluded.updated_at;

-- name: ListUnresolvedAlertKeys :many
SELECT id, fact_id, period_key FROM alerts WHERE workspace_id = ? AND status != 'resolved';

-- name: ResolveAlert :exec
UPDATE alerts SET status = 'resolved', updated_at = sqlc.arg(now) WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: ListOpenAlerts :many
SELECT a.id, a.drawer_id, a.fact_id, a.kind, a.period_key, a.document_id, a.period_label, a.due_date, a.created_at, a.note,
       f.label AS fact_label, coalesce(doc.title, '') AS document_title, coalesce(cf.name, '') AS field_name,
       coalesce(p.name, '') AS person_name
FROM alerts AS a
JOIN drawer_facts AS f ON f.id = a.fact_id
LEFT JOIN documents AS doc ON doc.id = a.document_id
LEFT JOIN persons AS p ON p.id = doc.person_id
LEFT JOIN custom_fields AS cf ON cf.id = CAST(json_extract(f.config, '$.fieldId') AS INTEGER)
WHERE a.workspace_id = sqlc.arg(workspace_id) AND a.status = 'open'
  AND NOT EXISTS (SELECT 1 FROM documents AS vx WHERE vx.id = a.document_id AND NOT (vx.private = 0 OR vx.person_id IN (SELECT pa.person_id FROM person_access AS pa WHERE pa.user_id = sqlc.arg(viewer)) OR (vx.added_by = sqlc.arg(viewer) AND vx.uploader_access = 1) OR sqlc.arg(viewer) = -1))
ORDER BY a.id;

-- name: SetAlertStatus :execrows
UPDATE alerts SET status = sqlc.arg(status), updated_at = sqlc.arg(now)
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id) AND status != 'resolved';

-- name: ListDrawerShares :many
SELECT s.target_workspace_id, w.name, w.kind, w.color, s.created_at
FROM drawer_shares AS s JOIN workspaces AS w ON w.id = s.target_workspace_id
WHERE s.drawer_id = ? AND w.deleted_at IS NULL
ORDER BY s.created_at;

-- name: CountDrawerShares :many
SELECT s.drawer_id, count(*) AS n FROM drawer_shares AS s
JOIN drawers AS d ON d.id = s.drawer_id JOIN workspaces AS w ON w.id = s.target_workspace_id
WHERE d.workspace_id = ? AND w.deleted_at IS NULL
GROUP BY s.drawer_id;

-- name: InsertDrawerShare :exec
INSERT INTO drawer_shares (drawer_id, target_workspace_id, created_by, created_at) VALUES (?, ?, ?, ?)
ON CONFLICT (drawer_id, target_workspace_id) DO NOTHING;

-- name: DeleteDrawerShare :execrows
DELETE FROM drawer_shares WHERE drawer_id = ? AND target_workspace_id = ?;

-- Drawers shared into a workspace, from workspaces that still exist.
-- name: ListSharedDrawers :many
SELECT d.*, w.name AS source_name
FROM drawer_shares AS s
JOIN drawers AS d ON d.id = s.drawer_id
JOIN workspaces AS w ON w.id = d.workspace_id
WHERE s.target_workspace_id = ? AND w.deleted_at IS NULL
ORDER BY w.name, d.sort_order, d.id;

-- name: GetSharedDrawer :one
SELECT d.*, w.name AS source_name
FROM drawer_shares AS s
JOIN drawers AS d ON d.id = s.drawer_id
JOIN workspaces AS w ON w.id = d.workspace_id
WHERE s.target_workspace_id = sqlc.arg(target_workspace_id) AND d.id = sqlc.arg(drawer_id) AND w.deleted_at IS NULL;

-- The only way a shared workspace reaches a document (design doc 3.6): it
-- belongs to the drawer's workspace, sits in the drawer, and the drawer is
-- shared with the target.
-- name: GetSharedDocumentID :one
SELECT doc.id FROM documents AS doc
JOIN drawers AS d ON d.id = doc.drawer_id AND d.workspace_id = doc.workspace_id
JOIN drawer_shares AS s ON s.drawer_id = d.id
WHERE doc.id = sqlc.arg(document_id) AND d.id = sqlc.arg(drawer_id) AND s.target_workspace_id = sqlc.arg(target_workspace_id)
  AND doc.deleted_at IS NULL AND doc.private = 0;

-- name: CountSharedDocuments :one
-- Another workspace sees no private document (v1.8.0).
SELECT count(*) FROM documents AS doc
WHERE doc.drawer_id = ? AND doc.deleted_at IS NULL AND doc.private = 0;

-- v1.6.4: every document in a sensitive drawer is sensitive.
-- name: SetDrawerSensitive :exec
UPDATE drawers SET sensitive = sqlc.arg(sensitive), updated_at = sqlc.arg(updated_at) WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: BumpIndexRevByDrawer :exec
UPDATE documents SET index_rev = index_rev + 1 WHERE workspace_id = sqlc.arg(workspace_id) AND drawer_id = sqlc.arg(drawer_id);

-- name: SetDrawerSameNameVersions :exec
UPDATE drawers SET same_name_versions = sqlc.arg(same_name_versions), updated_at = sqlc.arg(updated_at) WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);
