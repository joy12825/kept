package transfer

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strconv"

	"kept/server/internal/document"
	"kept/server/internal/i18n"
	"kept/server/internal/taxonomy"
	"kept/server/internal/tenant"
)

// mapping turns the source workspace's IDs into the target's.
type mapping struct {
	typeID, corrID int64             // 0: none
	personID       int64             // 0: none (v1.6.3)
	locationID     int64             // 0: none; the original stays where it is (v1.6.3)
	tags           map[int64]int64   // source tag → target tag
	fields         map[int64]int64   // source field → target field
	options        map[string]string // source select option → target option, per target field: "field:option"
}

// mapTaxonomy finds or creates in the target what the document uses.
func (s *Service) mapTaxonomy(ctx context.Context, scope tenant.Scope, d document.Detail, target Target, res *Result) (mapping, error) {
	tx := s.opt.Taxonomy
	dst := tenant.New(target.ID, tenant.RoleEditor)
	m := mapping{tags: map[int64]int64{}, fields: map[int64]int64{}, options: map[string]string{}}

	simple := func(kind taxonomy.Kind, id int64, format string) (int64, error) {
		if id == 0 {
			return 0, nil
		}
		from, err := tx.List(ctx, scope, kind)
		if err != nil {
			return 0, err
		}
		i := slices.IndexFunc(from, func(x taxonomy.Item) bool { return x.ID == id })
		if i < 0 {
			return 0, nil
		}
		to, err := tx.List(ctx, dst, kind)
		if err != nil {
			return 0, err
		}
		if j := slices.IndexFunc(to, func(x taxonomy.Item) bool { return x.Name == from[i].Name }); j >= 0 {
			return to[j].ID, nil
		}
		rule := from[i].Rule
		made, err := tx.Create(ctx, dst, kind, taxonomy.Input{Name: from[i].Name, Rule: &rule})
		if err != nil {
			return 0, err
		}
		res.Created = append(res.Created, fmt.Sprintf(format, made.Name))
		return made.ID, nil
	}
	var err error
	if m.typeID, err = simple(taxonomy.DocumentType, d.DocumentTypeID, i18n.N("类型“%s”")); err != nil {
		return m, err
	}
	if m.corrID, err = simple(taxonomy.Correspondent, d.CorrespondentID, i18n.N("往来方“%s”")); err != nil {
		return m, err
	}
	if m.personID, err = simple(taxonomy.Person, d.PersonID, i18n.N("家人“%s”")); err != nil {
		return m, err
	}
	if d.LocationID != 0 {
		from, err := tx.ListLocations(ctx, scope)
		if err != nil {
			return m, err
		}
		if i := slices.IndexFunc(from, func(x taxonomy.Location) bool { return x.ID == d.LocationID }); i >= 0 {
			to, err := tx.ListLocations(ctx, dst)
			if err != nil {
				return m, err
			}
			if j := slices.IndexFunc(to, func(x taxonomy.Location) bool { return x.Name == from[i].Name }); j >= 0 {
				m.locationID = to[j].ID
			} else {
				made, err := tx.CreateLocation(ctx, dst, taxonomy.LocationInput{Name: from[i].Name, Note: from[i].Note})
				if err != nil {
					return m, err
				}
				m.locationID = made.ID
				res.Created = append(res.Created, fmt.Sprintf(i18n.N("存放位置“%s”"), made.Name))
			}
		}
	}

	if len(d.TagIDs) > 0 {
		from, err := tx.ListTags(ctx, scope)
		if err != nil {
			return m, err
		}
		to, err := tx.ListTags(ctx, dst)
		if err != nil {
			return m, err
		}
		for _, id := range d.TagIDs {
			i := slices.IndexFunc(from, func(x taxonomy.TagItem) bool { return x.ID == id })
			if i < 0 || from[i].IsInbox {
				continue // the inbox tag is the target's own business
			}
			t := from[i]
			if j := slices.IndexFunc(to, func(x taxonomy.TagItem) bool { return x.Name == t.Name && !x.IsInbox }); j >= 0 {
				m.tags[id] = to[j].ID
				continue
			}
			rule := t.Rule
			made, err := tx.CreateTag(ctx, dst, taxonomy.TagInput{Name: t.Name, Color: t.Color, Rule: &rule, ShowInYearbook: t.ShowInYearbook})
			if err != nil {
				return m, err
			}
			to = append(to, made)
			m.tags[id] = made.ID
			res.Created = append(res.Created, fmt.Sprintf(i18n.N("标签“%s”"), made.Name))
		}
	}

	if len(d.Fields) > 0 {
		from, err := tx.ListFields(ctx, scope)
		if err != nil {
			return m, err
		}
		to, err := tx.ListFields(ctx, dst)
		if err != nil {
			return m, err
		}
		for _, v := range d.Fields {
			i := slices.IndexFunc(from, func(x taxonomy.Field) bool { return x.ID == v.FieldID })
			if i < 0 || from[i].DataType == "document_link" {
				continue // a link points into the source workspace
			}
			f := from[i]
			j := slices.IndexFunc(to, func(x taxonomy.Field) bool {
				return x.DataType == f.DataType && ((f.Key != "" && x.Key == f.Key) || (f.Key == "" && x.Key == "" && x.Name == f.Name))
			})
			if j < 0 {
				extra := taxonomy.FieldExtra{Currency: f.Extra.Currency}
				for _, o := range f.Extra.Options {
					extra.Options = append(extra.Options, taxonomy.SelectOption{Label: o.Label}) // the target gives the IDs
				}
				made, err := tx.CreateField(ctx, dst, taxonomy.FieldInput{Name: f.Name, DataType: f.DataType, Extra: extra})
				if err != nil {
					return m, err
				}
				to = append(to, made)
				j = len(to) - 1
				res.Created = append(res.Created, fmt.Sprintf(i18n.N("字段“%s”"), made.Name))
			}
			m.fields[f.ID] = to[j].ID
			if f.DataType == "select" {
				if err := s.mapOptions(ctx, dst, f, &to[j], m.options); err != nil {
					return m, err
				}
			}
		}
	}
	return m, nil
}

// mapOptions matches a select field's options by label, adding the
// missing ones to the target field (the server gives them IDs).
func (s *Service) mapOptions(ctx context.Context, dst tenant.Scope, from taxonomy.Field, to *taxonomy.Field, out map[string]string) error {
	has := func(f taxonomy.Field, label string) int {
		return slices.IndexFunc(f.Extra.Options, func(x taxonomy.SelectOption) bool { return x.Label == label })
	}
	extra := to.Extra
	extra.Options = slices.Clone(extra.Options)
	for _, o := range from.Extra.Options {
		if has(*to, o.Label) < 0 && !slices.ContainsFunc(extra.Options, func(x taxonomy.SelectOption) bool { return x.Label == o.Label }) {
			extra.Options = append(extra.Options, taxonomy.SelectOption{Label: o.Label})
		}
	}
	if len(extra.Options) > len(to.Extra.Options) {
		f, err := s.opt.Taxonomy.UpdateField(ctx, dst, to.ID, taxonomy.FieldPatch{Extra: &extra})
		if err != nil {
			return err
		}
		*to = f
	}
	for _, o := range from.Extra.Options {
		if i := has(*to, o.Label); i >= 0 {
			out[strconv.FormatInt(to.ID, 10)+":"+o.ID] = to.Extra.Options[i].ID
		}
	}
	return nil
}

// moveRows moves the document's rows to the target and lets go of what
// stays in the source. It reports whether the archive serial number had
// to be dropped.
func (s *Service) moveRows(ctx context.Context, tx *sql.Tx, d document.Detail, dst int64, m mapping) (bool, error) {
	id, src := d.ID, d.WorkspaceID
	exec := func(query string, args ...any) error {
		_, err := tx.ExecContext(ctx, query, args...)
		return err
	}

	// archive serial numbers are unique within a workspace
	asnCleared := false
	asn := sql.NullInt64{}
	if d.ASN != nil {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM documents WHERE workspace_id = ? AND asn = ?`, dst, *d.ASN).Scan(&n); err != nil {
			return false, err
		}
		if n == 0 {
			asn = sql.NullInt64{Int64: *d.ASN, Valid: true}
		} else {
			asnCleared = true
		}
	}
	fs := d.FieldSources
	fs.Drawer = nil
	if len(fs.Tags) > 0 {
		tags := map[int64]document.Source{}
		for from, v := range fs.Tags {
			if to, ok := m.tags[from]; ok {
				tags[to] = v
			}
		}
		fs.Tags = tags
	}
	nullID := func(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: v != 0} }
	if err := exec(`UPDATE documents SET workspace_id = ?, document_type_id = ?, correspondent_id = ?, person_id = ?, location_id = ?, asn = ?, drawer_id = NULL, drawer_manual = 0, origin_path = NULL, private = 0, uploader_access = 1, private_manual = 0,
		field_sources = ?, modified_at = ?, index_rev = index_rev + 1, embedded_rev = 0, simhash_rev = 0 WHERE id = ? AND workspace_id = ?`,
		dst, nullID(m.typeID), nullID(m.corrID), nullID(m.personID), nullID(m.locationID), asn, fs.String(), s.opt.Now().UnixMilli(), id, src); err != nil {
		return false, err
	}

	// tags and custom fields: the target's
	if err := exec(`DELETE FROM document_tags WHERE document_id = ?`, id); err != nil {
		return false, err
	}
	for _, to := range m.tags {
		if err := exec(`INSERT INTO document_tags (document_id, tag_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, id, to); err != nil {
			return false, err
		}
	}
	if err := s.moveFieldValues(ctx, tx, id, m); err != nil {
		return false, err
	}
	// links from the source's documents to this one no longer resolve
	if err := exec(`DELETE FROM custom_field_values WHERE value_doclink = ?`, id); err != nil {
		return false, err
	}

	// what goes along
	for _, t := range []string{"notes", "document_versions", "books", "book_summaries"} {
		if err := exec(`UPDATE `+t+` SET workspace_id = ? WHERE document_id = ?`, dst, id); err != nil {
			return false, err
		}
	}
	// reading state is personal: the members of the target keep theirs
	for _, t := range []string{"reading_progress", "highlights", "review_cards"} {
		if err := exec(`DELETE FROM `+t+` WHERE document_id = ? AND user_id NOT IN (SELECT user_id FROM workspace_members WHERE workspace_id = ?)`, id, dst); err != nil {
			return false, err
		}
		if err := exec(`UPDATE `+t+` SET workspace_id = ? WHERE document_id = ?`, dst, id); err != nil {
			return false, err
		}
	}

	// what stays in the source lets go of the document
	for _, stmt := range []string{
		`DELETE FROM share_links WHERE document_id = ?`,
		`DELETE FROM share_bundle_documents WHERE document_id = ?`,
		`DELETE FROM collection_documents WHERE document_id = ?`,
		`DELETE FROM book_list_items WHERE document_id = ?`,
		`DELETE FROM alerts WHERE document_id = ?`,
		`DELETE FROM duplicate_pairs WHERE doc_a = ? OR doc_b = ?`,
		`DELETE FROM agent_proposals WHERE document_id = ?`,
		`UPDATE agent_runs SET document_id = NULL WHERE document_id = ?`,
		`UPDATE workflow_runs SET document_id = NULL WHERE document_id = ?`,
		`DELETE FROM doc_chunks WHERE document_id = ?`, // embedded again in the target
	} {
		args := []any{id}
		if stmt == `DELETE FROM duplicate_pairs WHERE doc_a = ? OR doc_b = ?` {
			args = append(args, id)
		}
		if err := exec(stmt, args...); err != nil {
			return false, err
		}
	}

	// filed by the target's rules
	if s.opt.Placer != nil {
		if err := s.opt.Placer.PlaceDocument(ctx, tx, dst, id); err != nil {
			return false, err
		}
	}
	return asnCleared, nil
}

// moveFieldValues re-keys the document's custom field values to the
// target's fields; values without a target field are dropped.
func (s *Service) moveFieldValues(ctx context.Context, tx *sql.Tx, id int64, m mapping) error {
	rows, err := tx.QueryContext(ctx, `SELECT field_id, value_select FROM custom_field_values WHERE document_id = ?`, id)
	if err != nil {
		return err
	}
	type val struct {
		field int64
		sel   sql.NullString
	}
	var vals []val
	for rows.Next() {
		var v val
		if err := rows.Scan(&v.field, &v.sel); err != nil {
			rows.Close()
			return err
		}
		vals = append(vals, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, v := range vals {
		to, ok := m.fields[v.field]
		if !ok {
			if _, err := tx.ExecContext(ctx, `DELETE FROM custom_field_values WHERE document_id = ? AND field_id = ?`, id, v.field); err != nil {
				return err
			}
			continue
		}
		sel := v.sel
		if sel.Valid {
			opt, ok := m.options[strconv.FormatInt(to, 10)+":"+sel.String]
			sel = sql.NullString{String: opt, Valid: ok}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE custom_field_values SET field_id = ?, value_select = ?, value_doclink = NULL WHERE document_id = ? AND field_id = ?`,
			to, sel, id, v.field); err != nil {
			return err
		}
	}
	return nil
}
