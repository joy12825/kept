package document

import (
	"context"
	"database/sql"

	"kept/server/internal/filter"
	"kept/server/internal/privacy"
	"kept/server/internal/tenant"
)

// Stat is how many documents (outside the trash) a classification has, and
// the date of the latest one.
type Stat struct {
	Count    int
	LastDate string // YYYY-MM-DD
}

// Stats are the directory's per-item numbers, keyed by ID. A tag counts the
// documents on its child tags too, once each.
type Stats struct {
	Types, Correspondents, Persons, Tags map[int64]Stat
}

func (s *Service) Stats(ctx context.Context, scope tenant.Scope) (Stats, error) {
	out := Stats{Types: map[int64]Stat{}, Correspondents: map[int64]Stat{}, Persons: map[int64]Stat{}, Tags: map[int64]Stat{}}
	vis, vargs := privacy.Visible("d", scope)
	for col, m := range map[string]map[int64]Stat{"document_type_id": out.Types, "correspondent_id": out.Correspondents, "person_id": out.Persons} {
		rows, err := s.db.QueryContext(ctx, "SELECT d."+col+", count(*), max(d.created_date) FROM documents d WHERE d.workspace_id = ? AND d.deleted_at IS NULL AND d."+col+" IS NOT NULL AND "+vis+" GROUP BY 1",
			append([]any{scope.WorkspaceID()}, vargs...)...)
		if err != nil {
			return Stats{}, err
		}
		for rows.Next() {
			var id int64
			var st Stat
			if err := rows.Scan(&id, &st.Count, &st.LastDate); err != nil {
				rows.Close()
				return Stats{}, err
			}
			m[id] = st
		}
		rows.Close()
	}

	cat, err := filter.LoadCatalog(ctx, s.q, scope)
	if err != nil {
		return Stats{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT dt.document_id, dt.tag_id, d.created_date FROM document_tags dt JOIN documents d ON d.id = dt.document_id
		WHERE d.workspace_id = ? AND d.deleted_at IS NULL AND `+vis, append([]any{scope.WorkspaceID()}, vargs...)...)
	if err != nil {
		return Stats{}, err
	}
	defer rows.Close()
	type key struct{ doc, tag int64 }
	seen := map[key]bool{}
	for rows.Next() {
		var doc, tag int64
		var date sql.NullString
		if err := rows.Scan(&doc, &tag, &date); err != nil {
			return Stats{}, err
		}
		for _, t := range cat.Ancestors(tag) {
			if seen[key{doc, t}] {
				continue
			}
			seen[key{doc, t}] = true
			st := out.Tags[t]
			st.Count++
			if date.String > st.LastDate {
				st.LastDate = date.String
			}
			out.Tags[t] = st
		}
	}
	return out, rows.Err()
}
