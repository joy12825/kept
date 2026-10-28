package drawer

import (
	"context"
	"database/sql"
	"errors"

	"kept/server/internal/apperr"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// The 书房 (v0.4): a drawer of kind library whose rule takes every book;
// it stays first in the order so books land there.

const KindLibrary = "library"

var (
	errLibraryDelete = apperr.WithCode(apperr.KindConflict, "library", "书房不能直接删除，可以在书房的“更多”中关闭书房")
	errLibraryRule   = apperr.WithCode(apperr.KindConflict, "library", "书房收纳所有书籍，不能修改收纳规则")
)

type libraryKey struct{}

// libraryOK lets DisableLibrary delete the library drawer.
func libraryOK(ctx context.Context) bool { ok, _ := ctx.Value(libraryKey{}).(bool); return ok }

// Library returns the workspace's 书房, if it has one.
func (s *Service) Library(ctx context.Context, scope tenant.Scope) (*Drawer, error) {
	r, err := s.q.GetLibraryDrawer(ctx, scope.WorkspaceID())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d := toDrawer(r)
	return &d, nil
}

// EnableLibrary creates the 书房 (or returns it) and refiles the books.
func (s *Service) EnableLibrary(ctx context.Context, scope tenant.Scope) (Drawer, int64, error) {
	if d, err := s.Library(ctx, scope); err != nil || d != nil {
		if d != nil {
			return *d, 0, nil
		}
		return Drawer{}, 0, err
	}
	rule := fieldCond("book", "eq", true)
	ws := scope.WorkspaceID()
	var d Drawer
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		// named in the language of the request (v1.10.1)
		name := TemplateText(ctx, "书房")
		if _, err := q.FindDrawerByName(ctx, sqlc.FindDrawerByNameParams{WorkspaceID: ws, Name: name}); err == nil {
			name = TemplateText(ctx, "我的书房")
		}
		var err error
		if d, err = s.insert(ctx, q, scope, name, Input{Name: name, Icon: "book", Color: "orange", Description: TemplateText(ctx, "所有书籍，按封面排列"), Rule: &rule}); err != nil {
			return err
		}
		if err := q.SetDrawerKind(ctx, sqlc.SetDrawerKindParams{Kind: KindLibrary, WorkspaceID: ws, ID: d.ID}); err != nil {
			return err
		}
		rows, err := q.ListDrawers(ctx, ws)
		if err != nil {
			return err
		}
		order := []int64{d.ID}
		for _, r := range rows {
			if r.ID != d.ID {
				order = append(order, r.ID)
			}
		}
		for i, id := range order {
			if err := q.SetDrawerOrder(ctx, sqlc.SetDrawerOrderParams{SortOrder: int64(i + 1), WorkspaceID: ws, ID: id}); err != nil {
				return err
			}
		}
		d.Kind, d.SortOrder = KindLibrary, 1
		return nil
	})
	if err != nil {
		return Drawer{}, 0, err
	}
	job, err := s.EnqueueReassign(ctx, scope)
	s.changed(ws)
	return d, job, err
}

// DisableLibrary removes the 书房; the books themselves stay.
func (s *Service) DisableLibrary(ctx context.Context, scope tenant.Scope) (int64, error) {
	d, err := s.Library(ctx, scope)
	if err != nil || d == nil {
		return 0, err
	}
	return s.Delete(context.WithValue(ctx, libraryKey{}, true), scope, d.ID)
}
