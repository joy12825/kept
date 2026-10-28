package document

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/filter"
	"kept/server/internal/search"
	"kept/server/internal/tenant"
)

// Page sizes of the document list (design doc 11.9).
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

// Sort orders of the list. The correspondent order puts documents without
// one last when ascending.
const (
	SortCreated       = "created"
	SortAdded         = "added"
	SortTitle         = "title"
	SortCorrespondent = "correspondent"
	// SortRelevance ranks by the keyword search's score, best first; without
	// a keyword it falls back to SortCreated.
	SortRelevance = "relevance"
	// v1.4.3: by the built-in fields, the archive serial number and the
	// last change. Documents without the value come last either way.
	SortAmount   = "amount"
	SortDue      = "due"
	SortASN      = "asn"
	SortModified = "modified"
	// SortFieldPrefix + a field ID sorts by that custom field (v1.5.6).
	SortFieldPrefix = "field:"
)

// MaxListFields bounds the custom fields a list returns per row.
const MaxListFields = 20

var errSortField = apperr.New(apperr.KindInvalid, "排序用的字段不存在，请重新选择排序")

// fieldSort is the lastSorts entry for sorting by custom field id: its own
// value column, select options by their order, and a stand-in for missing
// values that sorts last.
func (s *Service) fieldSort(ctx context.Context, ws int64, id int64) (lastSort, error) {
	var none lastSort
	var dataType, extra string
	err := s.db.QueryRowContext(ctx, `SELECT data_type, extra FROM custom_fields WHERE id = ? AND workspace_id = ?`, id, ws).Scan(&dataType, &extra)
	if errors.Is(err, sql.ErrNoRows) {
		return none, errSortField
	} else if err != nil {
		return none, err
	}
	value := func(col string) string {
		return fmt.Sprintf(`(SELECT v.%s FROM custom_field_values v WHERE v.document_id = d.id AND v.field_id = %d)`, col, id)
	}
	switch dataType {
	case "text", "url":
		return lastSort{value("value_text"), "char(1114111)", "''"}, nil
	case "integer":
		return lastSort{value("value_int"), "9223372036854775807", "-9223372036854775808"}, nil
	case "monetary":
		return lastSort{value("value_monetary"), "9223372036854775807", "-9223372036854775808"}, nil
	case "float":
		return lastSort{value("value_float"), "1e308", "-1e308"}, nil
	case "boolean":
		return lastSort{value("value_bool"), "2", "-1"}, nil
	case "date":
		return lastSort{value("value_date"), "'9999-12-31'", "''"}, nil
	case "select":
		var x struct {
			Options []struct{ ID string } `json:"options"`
		}
		_ = json.Unmarshal([]byte(extra), &x)
		if len(x.Options) == 0 {
			return lastSort{"NULL", "0", "0"}, nil
		}
		// by the options' order; the IDs are short generated strings, quoted
		var b strings.Builder
		b.WriteString("CASE ")
		b.WriteString(value("value_select"))
		for i, o := range x.Options {
			fmt.Fprintf(&b, " WHEN '%s' THEN %d", strings.ReplaceAll(o.ID, "'", "''"), i)
		}
		b.WriteString(" END")
		return lastSort{"(" + b.String() + ")", "1000000", "-1"}, nil
	}
	return none, errSortField // document links have no order
}

// lastSorts are the orders whose missing values need a stand-in that sorts
// last in the chosen direction (the cursor compares plain values).
type lastSort struct{ expr, asc, desc string }

var lastSorts = map[string]lastSort{
	SortAmount: {`(SELECT v.value_monetary FROM custom_field_values v JOIN custom_fields f ON f.id = v.field_id WHERE v.document_id = d.id AND f.key = 'amount')`,
		"9223372036854775807", "-9223372036854775808"},
	SortDue: {`(SELECT v.value_date FROM custom_field_values v JOIN custom_fields f ON f.id = v.field_id WHERE v.document_id = d.id AND f.key = 'due_date')`,
		"'9999-12-31'", "''"},
	SortASN: {"d.asn", "9223372036854775807", "-1"},
}

var sortExpr = map[string]string{
	SortCreated:       "d.created_date",
	SortAdded:         "d.added_at",
	SortTitle:         "d.title_sort",
	SortCorrespondent: "coalesce(c.sort_key, char(1114111))", // U+10FFFF sorts after every name
	SortModified:      "d.modified_at",
}

// Query selects a page of the list.
type Query struct {
	Filter filter.Cond
	Sort   string // one of the Sort constants; "" means created
	Desc   bool   // ignored for SortRelevance
	Cursor string // from the previous page's NextCursor
	Limit  int
	// Fields are custom fields whose values each row carries (v1.5.6).
	Fields []int64
}

// Summary is a row of the list.
type Summary struct {
	Document
	TagIDs    []int64
	Amount    *int64 // minor units (fen), from the built-in field "amount"
	DueDate   string // built-in field "due_date"; "" when unset
	NoteCount int
	IsBook    bool
	// Sensitive: marked itself or in a sensitive drawer (v1.6.4). Lists
	// carry no search snippet for it.
	Sensitive bool
	HasCover  bool             // a book with its own cover image
	Reading   *Reading         // the caller's own reading (v0.4); nil when none
	Highlight []search.Segment // with a keyword: where it was found
	// FieldValues of Query.Fields that are set (v1.5.6)
	FieldValues []FieldValue
}

// Reading is the caller's status and progress in a document.
type Reading struct {
	Status  string
	Percent float64
}

// Page is one page of the list.
type Page struct {
	Items      []Summary
	NextCursor string // "" on the last page
	Total      int64
	// Truncated: the keyword matched more documents than the search recalls
	// (KEPT_SEARCH_LIMIT), only the best are in the list.
	Truncated bool
}

// fewToSort: up to this many matches, a page sorts the matches instead of
// walking the sort's index (see List).
const fewToSort = 5000

var errCursor = apperr.New(apperr.KindInvalid, "分页参数不正确，请刷新列表")

// summaryColumns are the columns of a row, without the recognised text.
const summaryColumns = `d.id, d.workspace_id, d.title, d.created_date, d.added_at, d.modified_at, d.correspondent_id, d.document_type_id,
	d.asn, d.mime_type, d.original_filename, d.original_ext, d.page_count, d.checksum, d.has_archive, d.has_thumbnail, d.inbox,
	d.drawer_id, d.drawer_manual, d.is_milestone, d.person_id, d.location_id, d.sensitive, d.private`

// List returns a page of the documents matching q.Filter, outside the trash.
func (s *Service) List(ctx context.Context, scope tenant.Scope, q Query) (Page, error) {
	cat, sr, err := s.catalog(ctx, scope)
	if err != nil {
		return Page{}, err
	}
	comp, err := filter.Compile(cat, scope, q.Filter)
	if err != nil {
		return Page{}, err
	}
	key, hasText := primary(q.Filter)
	if q.Sort == "" {
		q.Sort = SortCreated
	}
	if q.Sort == SortRelevance && !hasText {
		q.Sort, q.Desc = SortCreated, true // newest first
	}
	head, rel, expr := " FROM documents d", "", sortExpr[q.Sort]
	l, ok := lastSorts[q.Sort]
	if n, isField := strings.CutPrefix(q.Sort, SortFieldPrefix); isField {
		id, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			return Page{}, errSortField
		}
		if l, err = s.fieldSort(ctx, scope.WorkspaceID(), id); err != nil {
			return Page{}, err
		}
		ok = true
	}
	if ok {
		missing := l.asc
		if q.Desc {
			missing = l.desc
		}
		expr = "coalesce(" + l.expr + ", " + missing + ")"
	}
	args := []any{}
	if q.Sort == SortRelevance {
		// the ranked hits drive the join, each finding its document by id: with
		// documents first, SQLite walked the whole list for every document
		// (5 seconds at 50,000 documents, v1.13.5). CROSS JOIN keeps the order.
		head, rel, expr, q.Desc = " FROM json_each(?) r CROSS JOIN documents d", "d.id = CAST(r.value AS INTEGER) AND ", "r.key", false
		args = append(args, sr.ranks(key))
	} else if expr == "" {
		return Page{}, apperr.New(apperr.KindInvalid, "不支持这种排序")
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultPageSize
	}
	limit = min(limit, MaxPageSize)

	from := head + " LEFT JOIN correspondents c ON c.id = d.correspondent_id WHERE " + rel + comp.Where
	args = append(args, comp.Args...)
	page := Page{Truncated: sr.truncated}
	if err := s.db.QueryRowContext(ctx, "SELECT count(*)"+from, args...).Scan(&page.Total); err != nil {
		return Page{}, err
	}

	where := from
	cmp, dir := "<", "DESC"
	if !q.Desc {
		cmp, dir = ">", "ASC"
	}
	if q.Cursor != "" {
		val, id, err := decodeCursor(q.Cursor)
		if err != nil {
			return Page{}, err
		}
		where += fmt.Sprintf(" AND (%s %s ? OR (%s = ? AND d.id %s ?))", expr, cmp, expr, cmp)
		args = append(args, val, val, id)
	}
	// When few documents match, find them first and sort them: walking the
	// whole space in the sort's order to fill a page took 65 ms for a
	// drawer of 7 among 50,000 documents (v1.13.5). coalesce keeps the
	// planner off the sort's index, in SQLite and Postgres alike.
	order := expr
	if page.Total <= fewToSort {
		order = "coalesce(" + expr + ", " + expr + ")"
	}
	query := "SELECT " + summaryColumns + ", " + expr + where + fmt.Sprintf(" ORDER BY %s %s, d.id %s LIMIT ?", order, dir, dir)
	rows, err := s.db.QueryContext(ctx, query, append(args, limit+1)...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	var sortVals []any
	for rows.Next() {
		var sm Summary
		var sortVal any
		var corr, typ, asn, drawer, person, location sql.NullInt64
		var added, modified, pages, hasArchive, hasThumb, inbox, manual, milestone, sensitive, private int64
		if err := rows.Scan(&sm.ID, &sm.WorkspaceID, &sm.Title, &sm.CreatedDate, &added, &modified, &corr, &typ,
			&asn, &sm.MimeType, &sm.OriginalFilename, &sm.OriginalExt, &pages, &sm.Checksum, &hasArchive, &hasThumb, &inbox,
			&drawer, &manual, &milestone, &person, &location, &sensitive, &private, &sortVal); err != nil {
			return Page{}, err
		}
		sm.AddedAt, sm.ModifiedAt = time.UnixMilli(added), time.UnixMilli(modified)
		sm.CorrespondentID, sm.DocumentTypeID, sm.ASN = corr.Int64, typ.Int64, nullInt(asn)
		sm.PageCount, sm.HasArchive, sm.HasThumbnail, sm.Inbox = int(pages), hasArchive == 1, hasThumb == 1, inbox == 1
		sm.DrawerID, sm.DrawerManual, sm.IsMilestone = drawer.Int64, manual == 1, milestone == 1
		sm.PersonID, sm.LocationID, sm.SensitiveOwn, sm.Private = person.Int64, location.Int64, sensitive == 1, private == 1
		page.Items = append(page.Items, sm)
		sortVals = append(sortVals, sortVal)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextCursor = encodeCursor(sortVals[limit-1], page.Items[limit-1].ID)
	}
	if err := s.decorate(ctx, page.Items); err != nil {
		return Page{}, err
	}
	if err := s.listFields(ctx, scope.WorkspaceID(), page.Items, q.Fields); err != nil {
		return Page{}, err
	}
	if hasText {
		if err := sr.highlight(key, page.Items); err != nil {
			return Page{}, err
		}
		for i := range page.Items {
			if page.Items[i].Sensitive {
				page.Items[i].Highlight = nil // the snippet would show the text
			}
		}
	}
	return page, nil
}

// listFields fills in the values of the given custom fields.
func (s *Service) listFields(ctx context.Context, ws int64, items []Summary, fields []int64) error {
	if len(items) == 0 || len(fields) == 0 {
		return nil
	}
	if len(fields) > MaxListFields {
		fields = fields[:MaxListFields]
	}
	idx := make(map[int64]*Summary, len(items))
	args := []any{ws}
	for _, f := range fields {
		args = append(args, f)
	}
	for i := range items {
		idx[items[i].ID] = &items[i]
		args = append(args, items[i].ID)
	}
	q := `SELECT v.document_id, f.id, coalesce(f.key, ''), f.data_type, ` + valueColumns + ` FROM custom_field_values v
		JOIN custom_fields f ON f.id = v.field_id WHERE f.workspace_id = ? AND f.id IN (` + placeholders(len(fields)) + `)
		AND v.document_id IN (` + placeholders(len(items)) + `) ORDER BY f.id`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		doc, fv, err := scanFieldValue(rows)
		if err != nil {
			return err
		}
		if fv.Value != nil {
			idx[doc].FieldValues = append(idx[doc].FieldValues, fv)
		}
	}
	return rows.Err()
}

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

// decorate fills in tags, the built-in amount and due date, note counts and
// whether each document is a book.
func (s *Service) decorate(ctx context.Context, items []Summary) error {
	if len(items) == 0 {
		return nil
	}
	idx := make(map[int64]*Summary, len(items))
	ids := make([]any, len(items))
	for i := range items {
		idx[items[i].ID] = &items[i]
		ids[i] = items[i].ID
	}
	in := "(" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"

	// sensitive: marked itself, or in a sensitive drawer (v1.6.4)
	for i := range items {
		items[i].Sensitive = items[i].SensitiveOwn
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d.id FROM documents d JOIN drawers dr ON dr.id = d.drawer_id WHERE dr.sensitive = 1 AND d.id IN `+in, ids...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		idx[id].Sensitive = true
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx, `SELECT dt.document_id, dt.tag_id FROM document_tags dt JOIN tags t ON t.id = dt.tag_id
		WHERE dt.document_id IN `+in+` ORDER BY t.sort_key, t.id`, ids...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var doc, tag int64
		if err := rows.Scan(&doc, &tag); err != nil {
			rows.Close()
			return err
		}
		idx[doc].TagIDs = append(idx[doc].TagIDs, tag)
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx, `SELECT v.document_id, f.key, v.value_monetary, v.value_date FROM custom_field_values v
		JOIN custom_fields f ON f.id = v.field_id WHERE f.key IN ('amount', 'due_date') AND v.document_id IN `+in, ids...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var doc int64
		var key string
		var money sql.NullInt64
		var date sql.NullString
		if err := rows.Scan(&doc, &key, &money, &date); err != nil {
			rows.Close()
			return err
		}
		if key == "amount" && money.Valid {
			idx[doc].Amount = &money.Int64
		} else if key == "due_date" {
			idx[doc].DueDate = date.String
		}
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx, `SELECT document_id, cover FROM books WHERE document_id IN `+in, ids...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var doc, cover int64
		if err := rows.Scan(&doc, &cover); err != nil {
			rows.Close()
			return err
		}
		idx[doc].IsBook, idx[doc].HasCover = true, cover == 1
	}
	rows.Close()

	if user := audit.ActorFrom(ctx).UserID; user != 0 {
		rows, err = s.db.QueryContext(ctx, `SELECT document_id, status, percent FROM reading_progress WHERE user_id = ? AND document_id IN `+in, append([]any{user}, ids...)...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var doc int64
			var r Reading
			if err := rows.Scan(&doc, &r.Status, &r.Percent); err != nil {
				rows.Close()
				return err
			}
			idx[doc].Reading = &r
		}
		rows.Close()
	}

	rows, err = s.db.QueryContext(ctx, `SELECT document_id, count(*) FROM notes WHERE document_id IN `+in+` GROUP BY document_id`, ids...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var doc int64
		var n int
		if err := rows.Scan(&doc, &n); err != nil {
			return err
		}
		idx[doc].NoteCount = n
	}
	return rows.Err()
}

// The cursor is the last row's sort value and ID, opaque to clients.
func encodeCursor(val any, id int64) string {
	b, _ := json.Marshal([]any{val, id})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (any, int64, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, 0, errCursor
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.UseNumber()
	var parts []any
	if dec.Decode(&parts) != nil || len(parts) != 2 {
		return nil, 0, errCursor
	}
	num, ok := parts[1].(json.Number)
	if !ok {
		return nil, 0, errCursor
	}
	id, err := num.Int64()
	if err != nil {
		return nil, 0, errCursor
	}
	switch v := parts[0].(type) {
	case string:
		return v, id, nil
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n, id, nil
		}
		f, err := v.Float64() // a float field (v1.5.6)
		if err != nil {
			return nil, 0, errCursor
		}
		return f, id, nil
	}
	return nil, 0, errCursor
}
