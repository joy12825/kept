package taxonomy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"kept/server/internal/apperr"
	"kept/server/internal/filter"
	"kept/server/internal/privacy"
	"kept/server/internal/tenant"
)

var errMerge = apperr.New(apperr.KindInvalid, "要合并的项目不正确")

// filterField is the filter field naming each kind.
var filterField = map[Kind]string{Correspondent: "correspondent", DocumentType: "type", Tag: "tag", Person: "person"}

// Merge folds items into keep (v0.5, the organizing agent's suggestion):
// their documents move to keep, a merged tag's children move under keep,
// drawer rules naming them name keep instead, and they are deleted. It
// returns how many documents changed.
func (s *Service) Merge(ctx context.Context, scope tenant.Scope, kind Kind, keep int64, merge []int64) (int, error) {
	field, ok := filterField[kind]
	if !ok || len(merge) == 0 || slices.Contains(merge, keep) {
		return 0, errMerge
	}
	ws := scope.WorkspaceID()
	tbl := map[Kind]string{Correspondent: "correspondents", DocumentType: "document_types", Tag: "tags", Person: "persons"}[kind]
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	q := s.q.WithTx(tx)
	name := func(id int64) (string, error) {
		var n string
		err := tx.QueryRowContext(ctx, `SELECT name FROM `+tbl+` WHERE workspace_id = ? AND id = ?`, ws, id).Scan(&n)
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return n, err
	}
	keepName, err := name(keep)
	if err != nil {
		return 0, err
	}
	var names []string
	in := strings.TrimSuffix(strings.Repeat("?,", len(merge)), ",")
	args := []any{ws}
	for _, id := range merge {
		n, err := name(id)
		if err != nil {
			return 0, err
		}
		names = append(names, quote(n))
		args = append(args, id)
	}
	if kind == Person {
		if err := privacy.New(s.db).CheckMergePersons(ctx, q, scope, keep, merge); err != nil {
			return 0, err
		}
	}
	var moved int64
	switch kind {
	case Correspondent, DocumentType, Person:
		col := map[Kind]string{Correspondent: "correspondent_id", DocumentType: "document_type_id", Person: "person_id"}[kind]
		r, err := tx.ExecContext(ctx, `UPDATE documents SET `+col+` = ?, index_rev = index_rev + 1 WHERE workspace_id = ? AND `+col+` IN (`+in+`)`,
			append([]any{keep}, args...)...)
		if err != nil {
			return 0, err
		}
		moved, _ = r.RowsAffected()
	case Tag:
		r, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO document_tags (document_id, tag_id)
			SELECT DISTINCT dt.document_id, CAST(? AS INTEGER) FROM document_tags dt JOIN documents d ON d.id = dt.document_id WHERE d.workspace_id = ? AND dt.tag_id IN (`+in+`)`,
			append([]any{keep}, args...)...)
		if err != nil {
			return 0, err
		}
		moved, _ = r.RowsAffected()
		if _, err := tx.ExecContext(ctx, `UPDATE documents SET index_rev = index_rev + 1 WHERE workspace_id = ? AND id IN (
			SELECT document_id FROM document_tags WHERE tag_id IN (`+in+`))`, args...); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tags SET parent_id = ? WHERE workspace_id = ? AND parent_id IN (`+in+`)`, append([]any{keep}, args...)...); err != nil {
			return 0, err
		}
	}
	// Drawer rules that name a merged item name keep.
	from := map[int64]int64{}
	for _, id := range merge {
		from[id] = keep
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, rule FROM drawers WHERE workspace_id = ? AND rule != ''`, ws)
	if err != nil {
		return 0, err
	}
	type upd struct {
		id   int64
		rule string
	}
	var updates []upd
	for rows.Next() {
		var id int64
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return 0, err
		}
		c, err := filter.ParseString(raw)
		if err != nil {
			continue
		}
		if c2, changed := filter.ReplaceIDs(c, field, from); changed {
			b, _ := json.Marshal(c2)
			updates = append(updates, upd{id, string(b)})
		}
	}
	rows.Close()
	for _, u := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE drawers SET rule = ? WHERE id = ?`, u.rule, u.id); err != nil {
			return 0, err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM `+tbl+` WHERE workspace_id = ? AND id IN (`+in+`)`, args...); err != nil {
		return 0, err
	}
	if err := s.record(ctx, q, ws, kind, "merge", keep, nil, kindSummaries[kind].merge, strings.Join(names, "、"), keepName); err != nil {
		return 0, err
	}
	return int(moved), tx.Commit()
}
