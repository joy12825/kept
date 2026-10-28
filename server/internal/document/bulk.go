package document

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"kept/server/internal/audit"
	"kept/server/internal/i18n"
	"slices"
	"strings"

	"kept/server/internal/apperr"
	"kept/server/internal/filter"
	"kept/server/internal/privacy"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// MaxBulk is the most documents one bulk operation may touch.
const MaxBulk = 10000

var (
	errSelection = apperr.New(apperr.KindInvalid, "请选择要处理的文档")
	errTooMany   = apperr.New(apperr.KindInvalid, "一次最多处理 10000 份文档，请缩小范围")
)

// Selection names documents by ID or by filter (exactly one).
type Selection struct {
	IDs    []int64
	Filter *filter.Cond
}

// Resolve returns the IDs of the selected documents outside the trash, in
// ascending order. IDs that do not exist (or are elsewhere) are dropped.
func (s *Service) Resolve(ctx context.Context, scope tenant.Scope, sel Selection) ([]int64, error) {
	if (sel.Filter == nil) == (len(sel.IDs) == 0) {
		return nil, errSelection
	}
	cond := filter.Cond{}
	if sel.Filter != nil {
		cond = *sel.Filter
	} else if len(sel.IDs) > MaxBulk {
		return nil, errTooMany
	}
	cat, _, err := s.catalog(ctx, scope)
	if err != nil {
		return nil, err
	}
	comp, err := filter.Compile(cat, scope, cond)
	if err != nil {
		return nil, err
	}
	where, args := comp.Where, comp.Args
	if sel.Filter == nil {
		where += " AND d.id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(sel.IDs)), ",") + ")"
		for _, id := range sel.IDs {
			args = append(args, id)
		}
	}
	rows, err := s.db.QueryContext(ctx, "SELECT d.id FROM documents d WHERE "+where+" ORDER BY d.id LIMIT ?", append(args, MaxBulk+1)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if len(ids) > MaxBulk {
		return nil, errTooMany
	}
	return ids, rows.Err()
}

// BulkEdit changes the same fields on many documents in one transaction.
// TypeID / CorrespondentID / PersonID / LocationID 0 clears; tags are added and removed rather than
// replaced.
type BulkEdit struct {
	TypeID          *int64
	CorrespondentID *int64
	PersonID        *int64 // v1.6.3
	LocationID      *int64 // v1.6.3
	AddTagIDs       []int64
	RemoveTagIDs    []int64
	CustomFields    []FieldInput
	Inbox           *bool  // false = complete (完成整理)
	DrawerID        *int64 // put into a drawer by hand (0: into none)
}

// Bulk applies e to the selected documents and returns how many changed.
func (s *Service) Bulk(ctx context.Context, scope tenant.Scope, sel Selection, e BulkEdit) (int, error) {
	ids, err := s.Resolve(ctx, scope, sel)
	if err != nil {
		return 0, err
	}
	summary := i18n.N("%s 批量修改了“%s”")
	if e.Inbox != nil && !*e.Inbox && e.TypeID == nil && e.CorrespondentID == nil && e.PersonID == nil && e.LocationID == nil && e.DrawerID == nil && len(e.AddTagIDs)+len(e.RemoveTagIDs)+len(e.CustomFields) == 0 {
		summary = i18n.N("%s 整理了“%s”")
	}
	var changed []int64
	inbox, moved := false, false
	err = s.inTxDB(ctx, func(tx *sql.Tx, q *sqlc.Queries) error {
		nm := newNamesTx(tx, q, scope.WorkspaceID())
		for _, id := range ids {
			row, err := q.GetDocument(ctx, sqlc.GetDocumentParams{WorkspaceID: scope.WorkspaceID(), ID: id, Viewer: scope.Viewer()})
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			p := Patch{TypeID: e.TypeID, CorrespondentID: e.CorrespondentID, PersonID: e.PersonID, LocationID: e.LocationID, CustomFields: e.CustomFields, Inbox: e.Inbox, DrawerID: e.DrawerID}
			if len(e.AddTagIDs)+len(e.RemoveTagIDs) > 0 {
				have, err := q.ListDocumentTagIDs(ctx, id)
				if err != nil {
					return err
				}
				tags := slices.DeleteFunc(append(have, e.AddTagIDs...), func(t int64) bool { return slices.Contains(e.RemoveTagIDs, t) })
				p.TagIDs = &tags
			}
			if ok, err := privacy.New(s.db).CanEdit(ctx, q, scope, row.Private == 1, row.PersonID); err != nil {
				return err
			} else if !ok {
				continue // a family member's private document this member may only look at
			}
			res, err := s.apply(ctx, q, scope, nm, row, p, fmt.Sprintf(summary, audit.ActorFrom(ctx).Name, row.Title))
			if err != nil {
				return err
			}
			if res.changed {
				changed = append(changed, id)
				inbox = inbox || res.inbox
				moved = moved || res.privacy
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if moved {
		s.privacyChanged(scope.WorkspaceID())
	}
	s.notify(scope.WorkspaceID(), changed, inbox)
	if len(changed) > 0 && s.opt.OnUserUpdate != nil {
		s.opt.OnUserUpdate(scope.WorkspaceID(), changed)
	}
	return len(changed), nil
}
