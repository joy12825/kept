package document

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"kept/server/internal/filter"
	"kept/server/internal/privacy"
	"kept/server/internal/tenant"
)

// FieldValue is a custom field's value on a document, typed by the field:
// string for text, url, date and select (the option ID); int64 for integer
// and document_link; float64; bool; and for monetary the amount as a
// two-decimal string ("326.40").
type FieldValue struct {
	FieldID  int64
	Key      string // "amount" or "due_date" for the built-in fields
	DataType string
	Value    any
}

// Detail is a document with everything the details page shows.
type Detail struct {
	Summary
	Fields []FieldValue
	// LinkedFrom (v1.5.3) are the documents whose link fields point to
	// this one: a contract shows the invoices that refer to it.
	LinkedFrom []LinkedFrom
	// Privacy is what the viewer may do about the document's privacy
	// (v1.8.0).
	Privacy Privacy
}

// Privacy is the viewer's standing on a document's privacy.
type Privacy struct {
	// CanEdit: the viewer may change the document (a private one: its
	// person, a guardian or a manager).
	CanEdit bool
	// CanSetPrivate: the viewer may turn privacy on or off (the person's
	// holder; for a document without a person, anyone who may edit it,
	// once they choose one).
	CanSetPrivate bool
	// AsUploader: the viewer sees this private document only because they
	// uploaded it; the person may take that back.
	AsUploader bool
}

// LinkedFrom is a document linking to another, and through which field.
type LinkedFrom struct {
	ID          int64
	Title       string
	CreatedDate string
	Field       string
}

// GetDetail returns a document outside the trash with its recognised text,
// tags, custom field values and note count.
func (s *Service) GetDetail(ctx context.Context, scope tenant.Scope, id int64) (Detail, error) {
	d, err := s.Get(ctx, scope, id)
	if err != nil {
		return Detail{}, err
	}
	items := []Summary{{Document: d}}
	if err := s.decorate(ctx, items); err != nil {
		return Detail{}, err
	}
	det := Detail{Summary: items[0], Fields: []FieldValue{}}
	rows, err := s.db.QueryContext(ctx, `SELECT v.document_id, f.id, coalesce(f.key, ''), f.data_type, `+valueColumns+`
		FROM custom_field_values v JOIN custom_fields f ON f.id = v.field_id
		WHERE v.document_id = ? AND f.workspace_id = ? ORDER BY f.key IS NULL, f.key, f.sort_key, f.id`, id, scope.WorkspaceID())
	if err != nil {
		return Detail{}, err
	}
	defer rows.Close()
	for rows.Next() {
		_, fv, err := scanFieldValue(rows)
		if err != nil {
			return Detail{}, err
		}
		if fv.Value != nil {
			det.Fields = append(det.Fields, fv)
		}
	}
	if err := rows.Err(); err != nil {
		return Detail{}, err
	}
	rows.Close()
	det.LinkedFrom, err = s.linkedFrom(ctx, scope, id)
	if err != nil {
		return Detail{}, err
	}
	det.Privacy, err = s.privacyOf(ctx, scope, d)
	return det, err
}

func (s *Service) privacyOf(ctx context.Context, scope tenant.Scope, d Document) (Privacy, error) {
	pv := privacy.New(s.db)
	person := sql.NullInt64{Int64: d.PersonID, Valid: d.PersonID != 0}
	var out Privacy
	var err error
	if out.CanEdit, err = pv.CanEdit(ctx, s.q, scope, d.Private, person); err != nil {
		return out, err
	}
	switch {
	case !out.CanEdit:
	case d.PersonID == 0:
		out.CanSetPrivate = !d.Private
	default:
		if out.CanSetPrivate, err = pv.IsHolder(ctx, s.q, scope, d.PersonID); err != nil {
			return out, err
		}
	}
	if d.Private && scope.User() != 0 && d.AddedBy == scope.User() {
		level, err := pv.Level(ctx, s.q, d.PersonID, scope.User())
		if err != nil {
			return out, err
		}
		out.AsUploader = level == ""
	}
	return out, nil
}

// valueColumns are the value columns scanFieldValue reads, after the
// document ID, field ID, key and data type.
const valueColumns = `v.value_text, v.value_int, v.value_float, v.value_bool, v.value_date, v.value_monetary, v.value_select, v.value_doclink`

// scanFieldValue reads a row of document ID, field ID, key, data type and
// valueColumns; Value is nil when the field's own column is empty.
func scanFieldValue(rows *sql.Rows) (int64, FieldValue, error) {
	var doc int64
	var fv FieldValue
	var text, date, sel sql.NullString
	var integer, boolean, money, link sql.NullInt64
	var float sql.NullFloat64
	if err := rows.Scan(&doc, &fv.FieldID, &fv.Key, &fv.DataType, &text, &integer, &float, &boolean, &date, &money, &sel, &link); err != nil {
		return 0, fv, err
	}
	switch fv.DataType {
	case "text", "url":
		fv.Value = nullable(text.Valid, text.String)
	case "integer":
		fv.Value = nullable(integer.Valid, integer.Int64)
	case "float":
		fv.Value = nullable(float.Valid, float.Float64)
	case "boolean":
		fv.Value = nullable(boolean.Valid, boolean.Int64 == 1)
	case "date":
		fv.Value = nullable(date.Valid, date.String)
	case "monetary":
		fv.Value = nullable(money.Valid, filter.FormatMoney(money.Int64))
	case "select":
		fv.Value = nullable(sel.Valid, sel.String)
	case "document_link":
		fv.Value = nullable(link.Valid, link.Int64)
	}
	return doc, fv, nil
}

// linkedFrom lists the documents of the workspace (outside the trash)
// whose document-link fields point to id, newest first.
func (s *Service) linkedFrom(ctx context.Context, scope tenant.Scope, id int64) ([]LinkedFrom, error) {
	vis, vargs := privacy.Visible("d", scope)
	rows, err := s.db.QueryContext(ctx, `SELECT d.id, d.title, d.created_date, f.name FROM custom_field_values v
		JOIN custom_fields f ON f.id = v.field_id JOIN documents d ON d.id = v.document_id
		WHERE v.value_doclink = ? AND d.workspace_id = ? AND d.deleted_at IS NULL AND d.id <> ? AND `+vis+`
		ORDER BY d.created_date DESC, d.id DESC LIMIT 100`, append([]any{id, scope.WorkspaceID(), id}, vargs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LinkedFrom{}
	for rows.Next() {
		var l LinkedFrom
		if err := rows.Scan(&l.ID, &l.Title, &l.CreatedDate, &l.Field); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// NextASN is the archive serial number after the workspace's largest one
// (trash included: numbers are not reused).
func (s *Service) NextASN(ctx context.Context, scope tenant.Scope) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT coalesce(max(asn), 0) + 1 FROM documents WHERE workspace_id = ?`, scope.WorkspaceID()).Scan(&n)
	return n, err
}

func nullable[T any](valid bool, v T) any {
	if !valid {
		return nil
	}
	return v
}

// Sensitive reports whether documents of the workspace are sensitive
// (v1.6.4: marked, or in a sensitive drawer); documents not found are
// left out of the result.
func (s *Service) Sensitive(ctx context.Context, scope tenant.Scope, ids ...int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	args := []any{scope.WorkspaceID()}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d.id, `+filter.SensitiveSQL+` FROM documents d WHERE d.workspace_id = ? AND d.id IN (`+
		strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var sens bool
		if err := rows.Scan(&id, &sens); err != nil {
			return nil, err
		}
		out[id] = sens
	}
	return out, rows.Err()
}

// CanEdit reports whether the scope may change a document it can see
// (private documents: v1.8.0).
func (s *Service) CanEdit(ctx context.Context, scope tenant.Scope, id int64) (bool, error) {
	d, err := s.Get(ctx, scope, id)
	if err != nil {
		return false, err
	}
	return privacy.New(s.db).CanEdit(ctx, s.q, scope, d.Private, sql.NullInt64{Int64: d.PersonID, Valid: d.PersonID != 0})
}

// SetPersonDate gives a document its person and date as a person's change
// (v1.8.2: confirming a lab report); a change that makes it someone else's
// private document is still made.
func (s *Service) SetPersonDate(ctx context.Context, scope tenant.Scope, id, personID int64, date string) error {
	p := Patch{PersonID: &personID}
	if date != "" {
		p.CreatedDate = &date
	}
	_, err := s.Update(ctx, scope, id, p)
	if errors.Is(err, ErrBecamePrivate) {
		return nil
	}
	return err
}
