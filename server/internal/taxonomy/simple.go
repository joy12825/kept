package taxonomy

import (
	"context"
	"database/sql"
	"errors"

	"kept/server/internal/apperr"

	"kept/server/internal/privacy"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// Item is a correspondent, a document type or a person: they have the same shape.
type Item struct {
	ID      int64
	Name    string
	SortKey string
	Initial string // directory group: A-Z or #
	Rule    MatchRule
	// RetentionMonths: document types only (v1.6.5); 0 keeps for ever.
	RetentionMonths int
}

// Input creates an item; a nil Rule means DefaultRule.
type Input struct {
	Name string
	Rule *MatchRule
}

// Patch changes an item; nil fields stay as they are.
type Patch struct {
	Name *string
	Rule *MatchRule
	// RetentionMonths sets a document type's retention; 0 clears it.
	RetentionMonths *int
}

// simpleTable adapts the generated queries of one table: correspondents and
// document_types have identical columns, so their rows convert to one type.
type simpleTable struct {
	list   func(context.Context, *sqlc.Queries, int64) ([]sqlc.Correspondent, error)
	get    func(context.Context, *sqlc.Queries, int64, int64) (sqlc.Correspondent, error)
	find   func(context.Context, *sqlc.Queries, int64, string) (int64, error)
	insert func(context.Context, *sqlc.Queries, sqlc.InsertCorrespondentParams) (sqlc.Correspondent, error)
	update func(context.Context, *sqlc.Queries, sqlc.UpdateCorrespondentParams) error
	delete func(context.Context, *sqlc.Queries, int64, int64) error
	// bump marks the documents that use the item for re-indexing (its
	// name is part of what the search index holds).
	bump func(context.Context, *sqlc.Queries, int64, int64) error
}

var simpleTables = map[Kind]simpleTable{
	Correspondent: {
		list: func(ctx context.Context, q *sqlc.Queries, ws int64) ([]sqlc.Correspondent, error) {
			return q.ListCorrespondents(ctx, ws)
		},
		get: func(ctx context.Context, q *sqlc.Queries, ws, id int64) (sqlc.Correspondent, error) {
			return q.GetCorrespondent(ctx, sqlc.GetCorrespondentParams{WorkspaceID: ws, ID: id})
		},
		find: func(ctx context.Context, q *sqlc.Queries, ws int64, name string) (int64, error) {
			return q.FindCorrespondentByName(ctx, sqlc.FindCorrespondentByNameParams{WorkspaceID: ws, Name: name})
		},
		insert: func(ctx context.Context, q *sqlc.Queries, p sqlc.InsertCorrespondentParams) (sqlc.Correspondent, error) {
			return q.InsertCorrespondent(ctx, p)
		},
		update: func(ctx context.Context, q *sqlc.Queries, p sqlc.UpdateCorrespondentParams) error {
			return q.UpdateCorrespondent(ctx, p)
		},
		delete: func(ctx context.Context, q *sqlc.Queries, ws, id int64) error {
			return q.DeleteCorrespondent(ctx, sqlc.DeleteCorrespondentParams{WorkspaceID: ws, ID: id})
		},
		bump: func(ctx context.Context, q *sqlc.Queries, ws, id int64) error {
			return q.BumpIndexRevByCorrespondent(ctx, sqlc.BumpIndexRevByCorrespondentParams{WorkspaceID: ws, ID: sql.NullInt64{Int64: id, Valid: true}})
		},
	},
	DocumentType: {
		list: func(ctx context.Context, q *sqlc.Queries, ws int64) ([]sqlc.Correspondent, error) {
			rows, err := q.ListDocumentTypes(ctx, ws)
			out := make([]sqlc.Correspondent, len(rows))
			for i, r := range rows {
				out[i] = sqlc.Correspondent(r)
			}
			return out, err
		},
		get: func(ctx context.Context, q *sqlc.Queries, ws, id int64) (sqlc.Correspondent, error) {
			r, err := q.GetDocumentType(ctx, sqlc.GetDocumentTypeParams{WorkspaceID: ws, ID: id})
			return sqlc.Correspondent(r), err
		},
		find: func(ctx context.Context, q *sqlc.Queries, ws int64, name string) (int64, error) {
			return q.FindDocumentTypeByName(ctx, sqlc.FindDocumentTypeByNameParams{WorkspaceID: ws, Name: name})
		},
		insert: func(ctx context.Context, q *sqlc.Queries, p sqlc.InsertCorrespondentParams) (sqlc.Correspondent, error) {
			r, err := q.InsertDocumentType(ctx, sqlc.InsertDocumentTypeParams(p))
			return sqlc.Correspondent(r), err
		},
		update: func(ctx context.Context, q *sqlc.Queries, p sqlc.UpdateCorrespondentParams) error {
			return q.UpdateDocumentType(ctx, sqlc.UpdateDocumentTypeParams(p))
		},
		delete: func(ctx context.Context, q *sqlc.Queries, ws, id int64) error {
			return q.DeleteDocumentType(ctx, sqlc.DeleteDocumentTypeParams{WorkspaceID: ws, ID: id})
		},
		bump: func(ctx context.Context, q *sqlc.Queries, ws, id int64) error {
			return q.BumpIndexRevByDocumentType(ctx, sqlc.BumpIndexRevByDocumentTypeParams{WorkspaceID: ws, ID: sql.NullInt64{Int64: id, Valid: true}})
		},
	},
	Person: {
		list: func(ctx context.Context, q *sqlc.Queries, ws int64) ([]sqlc.Correspondent, error) {
			rows, err := q.ListPersons(ctx, ws)
			out := make([]sqlc.Correspondent, len(rows))
			for i, r := range rows {
				out[i] = sqlc.Correspondent(r)
			}
			return out, err
		},
		get: func(ctx context.Context, q *sqlc.Queries, ws, id int64) (sqlc.Correspondent, error) {
			r, err := q.GetPerson(ctx, sqlc.GetPersonParams{WorkspaceID: ws, ID: id})
			return sqlc.Correspondent(r), err
		},
		find: func(ctx context.Context, q *sqlc.Queries, ws int64, name string) (int64, error) {
			return q.FindPersonByName(ctx, sqlc.FindPersonByNameParams{WorkspaceID: ws, Name: name})
		},
		insert: func(ctx context.Context, q *sqlc.Queries, p sqlc.InsertCorrespondentParams) (sqlc.Correspondent, error) {
			r, err := q.InsertPerson(ctx, sqlc.InsertPersonParams(p))
			return sqlc.Correspondent(r), err
		},
		update: func(ctx context.Context, q *sqlc.Queries, p sqlc.UpdateCorrespondentParams) error {
			return q.UpdatePerson(ctx, sqlc.UpdatePersonParams(p))
		},
		delete: func(ctx context.Context, q *sqlc.Queries, ws, id int64) error {
			return q.DeletePerson(ctx, sqlc.DeletePersonParams{WorkspaceID: ws, ID: id})
		},
		bump: func(ctx context.Context, q *sqlc.Queries, ws, id int64) error {
			return q.BumpIndexRevByPerson(ctx, sqlc.BumpIndexRevByPersonParams{WorkspaceID: ws, ID: sql.NullInt64{Int64: id, Valid: true}})
		},
	},
}

func toItem(r sqlc.Correspondent) Item {
	_, initial := sortFields(r.Name)
	return Item{ID: r.ID, Name: r.Name, SortKey: r.SortKey, Initial: initial,
		Rule: MatchRule{Algorithm: r.Algorithm, Match: r.Match, CaseInsensitive: r.CaseInsensitive == 1}}
}

func table(kind Kind) simpleTable {
	t, ok := simpleTables[kind]
	if !ok {
		panic("taxonomy: not a correspondent, document type or person kind: " + string(kind))
	}
	return t
}

// List returns a workspace's correspondents or document types in pinyin order.
func (s *Service) List(ctx context.Context, scope tenant.Scope, kind Kind) ([]Item, error) {
	rows, err := table(kind).list(ctx, s.q, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	out := make([]Item, len(rows))
	for i, r := range rows {
		out[i] = toItem(r)
	}
	if kind == DocumentType {
		ret, err := s.q.ListTypeRetention(ctx, scope.WorkspaceID())
		if err != nil {
			return nil, err
		}
		months := map[int64]int{}
		for _, r := range ret {
			months[r.DocumentTypeID] = int(r.Months)
		}
		for i := range out {
			out[i].RetentionMonths = months[out[i].ID]
		}
	}
	return out, nil
}

func (s *Service) Create(ctx context.Context, scope tenant.Scope, kind Kind, in Input) (Item, error) {
	t := table(kind)
	name, err := normalizeName(in.Name)
	if err != nil {
		return Item{}, err
	}
	rule := DefaultRule
	if in.Rule != nil {
		if rule, err = in.Rule.normalized(); err != nil {
			return Item{}, err
		}
	}
	var item Item
	err = s.inTx(ctx, func(q *sqlc.Queries) error {
		ws := scope.WorkspaceID()
		if err := ensureFree(ctx, t.find, q, ws, name, 0, kind); err != nil {
			return err
		}
		sortKey, _ := sortFields(name)
		now := s.now().UnixMilli()
		row, err := t.insert(ctx, q, sqlc.InsertCorrespondentParams{
			WorkspaceID: ws, Name: name, SortKey: sortKey, Match: rule.Match, Algorithm: rule.Algorithm,
			CaseInsensitive: boolInt(rule.CaseInsensitive), CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			return err
		}
		item = toItem(row)
		return s.record(ctx, q, ws, kind, "create", item.ID, nil, kindSummaries[kind].create, name)
	})
	return item, err
}

func (s *Service) Update(ctx context.Context, scope tenant.Scope, kind Kind, id int64, p Patch) (Item, error) {
	t := table(kind)
	var item Item
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		ws := scope.WorkspaceID()
		row, err := t.get(ctx, q, ws, id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		before := toItem(row)
		item = before
		if p.Name != nil {
			if item.Name, err = normalizeName(*p.Name); err != nil {
				return err
			}
		}
		if p.Rule != nil {
			if item.Rule, err = p.Rule.normalized(); err != nil {
				return err
			}
		}
		changes := map[string][2]any{}
		if item.Name != before.Name {
			changes["name"] = [2]any{before.Name, item.Name}
			if err := ensureFree(ctx, t.find, q, ws, item.Name, id, kind); err != nil {
				return err
			}
		}
		before.Rule.changes(item.Rule, changes)
		if p.RetentionMonths != nil && kind == DocumentType {
			if err := s.setRetention(ctx, q, ws, id, *p.RetentionMonths, changes); err != nil {
				return err
			}
			item.RetentionMonths = *p.RetentionMonths
		}
		if len(changes) == 0 {
			return nil
		}
		item.SortKey, item.Initial = sortFields(item.Name)
		if item.Name != before.Name {
			if err := t.bump(ctx, q, ws, id); err != nil {
				return err
			}
		}
		if err := t.update(ctx, q, sqlc.UpdateCorrespondentParams{
			Name: item.Name, SortKey: item.SortKey, Match: item.Rule.Match, Algorithm: item.Rule.Algorithm,
			CaseInsensitive: boolInt(item.Rule.CaseInsensitive), UpdatedAt: s.now().UnixMilli(), WorkspaceID: ws, ID: id,
		}); err != nil {
			return err
		}
		sf, sa := renameSummary(kind, before.Name, item.Name)
		return s.record(ctx, q, ws, kind, "update", id, changes, sf, sa...)
	})
	return item, err
}

func (s *Service) Delete(ctx context.Context, scope tenant.Scope, kind Kind, id int64) error {
	t := table(kind)
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		ws := scope.WorkspaceID()
		row, err := t.get(ctx, q, ws, id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if kind == Person {
			// private documents would show to everyone (v1.8.0)
			if err := privacy.New(s.db).CheckDeletePerson(ctx, q, id); err != nil {
				return err
			}
		}
		if err := t.bump(ctx, q, ws, id); err != nil {
			return err
		}
		if err := t.delete(ctx, q, ws, id); err != nil {
			return err
		}
		return s.record(ctx, q, ws, kind, "delete", id, nil, kindSummaries[kind].delete, row.Name)
	})
}

// ensureFree fails when another item (not except) already has the name.
func ensureFree(ctx context.Context, find func(context.Context, *sqlc.Queries, int64, string) (int64, error), q *sqlc.Queries, ws int64, name string, except int64, kind Kind) error {
	id, err := find(ctx, q, ws, name)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && id == except) {
		return nil
	}
	if err != nil {
		return err
	}
	return errTaken[kind]
}

// MaxRetentionMonths: 50 years is as good as for ever.
const MaxRetentionMonths = 600

var errRetention = apperr.New(apperr.KindInvalid, "保留期限应在 1 到 600 个月之间（0 表示永久保留）")

// setRetention sets (or with 0 clears) a document type's retention and
// notes the change.
func (s *Service) setRetention(ctx context.Context, q *sqlc.Queries, ws, typeID int64, months int, changes map[string][2]any) error {
	if months < 0 || months > MaxRetentionMonths {
		return errRetention
	}
	before := 0
	ret, err := q.ListTypeRetention(ctx, ws)
	if err != nil {
		return err
	}
	for _, r := range ret {
		if r.DocumentTypeID == typeID {
			before = int(r.Months)
		}
	}
	if before == months {
		return nil
	}
	changes["保留期限"] = [2]any{before, months}
	if months == 0 {
		return q.ClearTypeRetention(ctx, sqlc.ClearTypeRetentionParams{WorkspaceID: ws, DocumentTypeID: typeID})
	}
	return q.SetTypeRetention(ctx, sqlc.SetTypeRetentionParams{DocumentTypeID: typeID, WorkspaceID: ws, Months: int64(months)})
}
