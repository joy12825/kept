package document

import (
	"context"
	"fmt"
	"kept/server/internal/i18n"
	"strconv"

	"kept/server/internal/audit"
	"kept/server/internal/tenant"
)

// Absorb folds document from into keep after its original became keep's
// new version (v1.5.8): keep gets its tags, notes and the custom field
// values keep has none of, links to it point to keep, and from is removed
// for good (its file lives on as the version).
func (s *Service) Absorb(ctx context.Context, scope tenant.Scope, keep, from int64) error {
	ws := scope.WorkspaceID()
	d, err := s.Get(ctx, scope, from)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`INSERT OR IGNORE INTO document_tags (document_id, tag_id) SELECT ?, tag_id FROM document_tags WHERE document_id = ?`,
		`UPDATE notes SET document_id = ? WHERE document_id = ?`,
		`INSERT OR IGNORE INTO custom_field_values (document_id, field_id, value_text, value_int, value_float, value_bool, value_date, value_monetary, value_select, value_doclink)
			SELECT ?, field_id, value_text, value_int, value_float, value_bool, value_date, value_monetary, value_select, value_doclink
			FROM custom_field_values WHERE document_id = ? AND (value_doclink IS NULL OR value_doclink <> ` + strconv.FormatInt(keep, 10) + `)`,
	} {
		if _, err := tx.ExecContext(ctx, q, keep, from); err != nil {
			return err
		}
	}
	// links: to from now go to keep; keep's own link to from goes away
	if _, err := tx.ExecContext(ctx, `DELETE FROM custom_field_values WHERE document_id = ? AND value_doclink = ?`, keep, from); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE custom_field_values SET value_doclink = ? WHERE value_doclink = ? AND document_id <> ?`, keep, from, keep); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE documents SET index_rev = index_rev + 1 WHERE workspace_id = ? AND id = ?`, ws, keep); err != nil {
		return err
	}
	if err := audit.Record(ctx, s.q.WithTx(tx), audit.Event{WorkspaceID: ws, Action: "document.absorb", TargetType: "document", TargetID: strconv.FormatInt(keep, 10),
		Summary: fmt.Sprintf(i18n.N("%s 把《%s》的标签、备注和字段并了过来"), audit.ActorFrom(ctx).Name, d.Title), At: s.opt.Now()}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if _, err := s.Delete(ctx, scope, Selection{IDs: []int64{from}}); err != nil {
		return err
	}
	return s.Purge(ctx, scope, []int64{from})
}
