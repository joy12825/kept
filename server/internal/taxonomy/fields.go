package taxonomy

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"kept/server/internal/i18n"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"kept/server/internal/apperr"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// Field data types (design doc 3.4).
var dataTypes = map[string]bool{
	"text": true, "integer": true, "float": true, "boolean": true, "date": true,
	"url": true, "monetary": true, "select": true, "document_link": true,
}

var (
	errDataType     = apperr.New(apperr.KindInvalid, "不支持这种字段类型")
	errCurrency     = apperr.New(apperr.KindInvalid, "币种需要是三位大写字母，例如 CNY")
	errOptions      = apperr.New(apperr.KindInvalid, "单选字段至少需要一个选项")
	errOptionLabel  = apperr.New(apperr.KindInvalid, "选项名称需要 1 到 64 个字符，且不能重复")
	errOptionID     = apperr.New(apperr.KindInvalid, "选项已被修改，请刷新后重试")
	errBuiltInExtra = apperr.New(apperr.KindInvalid, "内置字段只能改名")
	errBuiltInGone  = apperr.New(apperr.KindConflict, "内置字段不能删除，可以改名")
)

var currencyRE = regexp.MustCompile(`^[A-Z]{3}$`)

// SelectOption is one choice of a select field. The ID stays when the label
// changes, so stored values keep pointing at the right option.
type SelectOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// FieldExtra holds the type-specific settings, stored as JSON.
type FieldExtra struct {
	Currency string         `json:"currency,omitempty"` // monetary
	Options  []SelectOption `json:"options,omitempty"`  // select
	// Multiline shows a text field as several lines (v1.5.8): a warranty's
	// terms, how to take a medicine. Still text for search and filters.
	Multiline bool `json:"multiline,omitempty"`
}

type Field struct {
	ID       int64
	Key      string // amount or due_date for built-in fields, "" otherwise
	Name     string
	SortKey  string
	Initial  string
	DataType string
	Extra    FieldExtra
	BuiltIn  bool
}

type FieldInput struct {
	Name     string
	DataType string
	Extra    FieldExtra
}

// FieldPatch changes a field; nil fields stay. The data type never changes.
type FieldPatch struct {
	Name  *string
	Extra *FieldExtra
}

// builtIns are created in every workspace (design doc 3.4).
var builtIns = []struct {
	key, name, dataType string
	extra               FieldExtra
}{
	{"amount", i18n.N("金额"), "monetary", FieldExtra{Currency: "CNY"}},
	{"due_date", i18n.N("到期日"), "date", FieldExtra{}},
}

// SeedWorkspace creates the built-in fields of a new workspace, in the
// caller's transaction (account creation, workspace creation). They are
// named in the language of the request (v1.10.1); the code finds them by
// key, never by name.
func SeedWorkspace(ctx context.Context, q *sqlc.Queries, workspaceID int64, now time.Time) error {
	for _, b := range builtIns {
		name := i18n.T(ctx, b.name)
		sortKey, _ := sortFields(name)
		extra, _ := json.Marshal(b.extra)
		if _, err := q.InsertCustomField(ctx, sqlc.InsertCustomFieldParams{
			WorkspaceID: workspaceID, Key: sql.NullString{String: b.key, Valid: true}, Name: name, SortKey: sortKey,
			DataType: b.dataType, Extra: string(extra), CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli(),
		}); err != nil {
			return err
		}
	}
	return nil
}

func toField(r sqlc.CustomField) (Field, error) {
	_, initial := sortFields(r.Name)
	f := Field{ID: r.ID, Key: r.Key.String, Name: r.Name, SortKey: r.SortKey, Initial: initial, DataType: r.DataType, BuiltIn: r.Key.Valid}
	if err := json.Unmarshal([]byte(r.Extra), &f.Extra); err != nil {
		return Field{}, err
	}
	return f, nil
}

// normalizeExtra validates the settings of a data type; prev holds the
// current options when editing, so existing option IDs are recognised.
func normalizeExtra(dataType string, e FieldExtra, prev []SelectOption) (FieldExtra, error) {
	switch dataType {
	case "monetary":
		if e.Currency == "" {
			e.Currency = "CNY"
		}
		if !currencyRE.MatchString(e.Currency) {
			return e, errCurrency
		}
		return FieldExtra{Currency: e.Currency}, nil
	case "select":
		if len(e.Options) == 0 {
			return e, errOptions
		}
		known := map[string]bool{}
		for _, o := range prev {
			known[o.ID] = true
		}
		seen := map[string]bool{}
		out := make([]SelectOption, len(e.Options))
		for i, o := range e.Options {
			label := strings.TrimSpace(o.Label)
			if n := utf8.RuneCountInString(label); n < 1 || n > maxNameLen || seen[strings.ToLower(label)] {
				return e, errOptionLabel
			}
			seen[strings.ToLower(label)] = true
			id := o.ID
			if id == "" {
				id = newOptionID()
			} else if !known[id] {
				return e, errOptionID
			}
			out[i] = SelectOption{ID: id, Label: label}
		}
		return FieldExtra{Options: out}, nil
	case "text":
		return FieldExtra{Multiline: e.Multiline}, nil
	default:
		return FieldExtra{}, nil
	}
}

func newOptionID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Service) ListFields(ctx context.Context, scope tenant.Scope) ([]Field, error) {
	rows, err := s.q.ListCustomFields(ctx, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	out := make([]Field, len(rows))
	for i, r := range rows {
		if out[i], err = toField(r); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Service) CreateField(ctx context.Context, scope tenant.Scope, in FieldInput) (Field, error) {
	name, err := normalizeName(in.Name)
	if err != nil {
		return Field{}, err
	}
	if !dataTypes[in.DataType] {
		return Field{}, errDataType
	}
	extra, err := normalizeExtra(in.DataType, in.Extra, nil)
	if err != nil {
		return Field{}, err
	}
	var f Field
	err = s.inTx(ctx, func(q *sqlc.Queries) error {
		ws := scope.WorkspaceID()
		if err := ensureFree(ctx, findField, q, ws, name, 0, CustomField); err != nil {
			return err
		}
		sortKey, _ := sortFields(name)
		raw, _ := json.Marshal(extra)
		now := s.now().UnixMilli()
		row, err := q.InsertCustomField(ctx, sqlc.InsertCustomFieldParams{
			WorkspaceID: ws, Name: name, SortKey: sortKey, DataType: in.DataType, Extra: string(raw), CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			return err
		}
		if f, err = toField(row); err != nil {
			return err
		}
		return s.record(ctx, q, ws, CustomField, "create", f.ID, nil, kindSummaries[CustomField].create, name)
	})
	return f, err
}

func (s *Service) UpdateField(ctx context.Context, scope tenant.Scope, id int64, p FieldPatch) (Field, error) {
	var f Field
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		ws := scope.WorkspaceID()
		row, err := q.GetCustomField(ctx, sqlc.GetCustomFieldParams{WorkspaceID: ws, ID: id})
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		before, err := toField(row)
		if err != nil {
			return err
		}
		f = before
		if p.Name != nil {
			if f.Name, err = normalizeName(*p.Name); err != nil {
				return err
			}
		}
		if p.Extra != nil {
			if f.Extra, err = normalizeExtra(f.DataType, *p.Extra, before.Extra.Options); err != nil {
				return err
			}
		}
		changes := map[string][2]any{}
		if f.Name != before.Name {
			changes["name"] = [2]any{before.Name, f.Name}
			if err := ensureFree(ctx, findField, q, ws, f.Name, id, CustomField); err != nil {
				return err
			}
		}
		oldExtra, _ := json.Marshal(before.Extra)
		newExtra, _ := json.Marshal(f.Extra)
		if string(oldExtra) != string(newExtra) {
			if f.BuiltIn {
				return errBuiltInExtra
			}
			changes["extra"] = [2]any{before.Extra, f.Extra}
		}
		if len(changes) == 0 {
			return nil
		}
		f.SortKey, f.Initial = sortFields(f.Name)
		if err := q.UpdateCustomField(ctx, sqlc.UpdateCustomFieldParams{
			Name: f.Name, SortKey: f.SortKey, Extra: string(newExtra), UpdatedAt: s.now().UnixMilli(), WorkspaceID: ws, ID: id,
		}); err != nil {
			return err
		}
		sf, sa := renameSummary(CustomField, before.Name, f.Name)
		return s.record(ctx, q, ws, CustomField, "update", id, changes, sf, sa...)
	})
	return f, err
}

// DeleteField deletes a custom field; built-in fields cannot be deleted.
func (s *Service) DeleteField(ctx context.Context, scope tenant.Scope, id int64) error {
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		ws := scope.WorkspaceID()
		row, err := q.GetCustomField(ctx, sqlc.GetCustomFieldParams{WorkspaceID: ws, ID: id})
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if row.Key.Valid {
			return errBuiltInGone
		}
		if err := q.BumpIndexRevByField(ctx, sqlc.BumpIndexRevByFieldParams{WorkspaceID: ws, ID: id}); err != nil {
			return err
		}
		if err := q.DeleteCustomField(ctx, sqlc.DeleteCustomFieldParams{WorkspaceID: ws, ID: id}); err != nil {
			return err
		}
		return s.record(ctx, q, ws, CustomField, "delete", id, nil, kindSummaries[CustomField].delete, row.Name)
	})
}

func findField(ctx context.Context, q *sqlc.Queries, ws int64, name string) (int64, error) {
	return q.FindCustomFieldByName(ctx, sqlc.FindCustomFieldByNameParams{WorkspaceID: ws, Name: name})
}
