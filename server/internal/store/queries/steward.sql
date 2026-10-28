-- name: ListAlertsWithoutNote :many
SELECT a.id, a.drawer_id, a.fact_id, a.kind, a.period_key, a.document_id, a.period_label, a.due_date,
       d.name AS drawer_name, f.kind AS fact_kind, f.label AS fact_label, f.config AS fact_config,
       coalesce(cf.name, '') AS field_name
FROM alerts AS a
JOIN drawers AS d ON d.id = a.drawer_id
JOIN drawer_facts AS f ON f.id = a.fact_id
LEFT JOIN custom_fields AS cf ON cf.id = CAST(json_extract(f.config, '$.fieldId') AS INTEGER)
WHERE a.workspace_id = ? AND a.status = 'open' AND a.note = ''
ORDER BY a.id
LIMIT ?;

-- name: SetAlertNote :exec
UPDATE alerts SET note = sqlc.arg(note), note_run = sqlc.arg(run_id)
WHERE workspace_id = sqlc.arg(workspace_id) AND id = sqlc.arg(id);

-- name: GetYearbookNote :one
SELECT text, basis, created_at FROM yearbook_notes WHERE workspace_id = ? AND year = ?;

-- name: UpsertYearbookNote :exec
INSERT INTO yearbook_notes (workspace_id, year, text, basis, run_id, created_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (workspace_id, year) DO UPDATE SET text = excluded.text, basis = excluded.basis,
    run_id = excluded.run_id, created_at = excluded.created_at;
