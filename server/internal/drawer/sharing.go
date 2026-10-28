package drawer

import (
	"context"
	"database/sql"
	"errors"
	"kept/server/internal/i18n"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// Read-only sharing (design doc 3.6): an owner of the drawer's workspace
// shares it with another workspace they own. Members of the target can
// browse, preview and download the drawer's documents; the documents stay
// in the source workspace. The target only ever reaches them through
// SharedDocument, which checks the share on every access.

var (
	errShareTarget = apperr.New(apperr.KindInvalid, "只能共享给你也是管理员的另一个空间")
	errShareSelf   = apperr.New(apperr.KindInvalid, "不能共享给抽屉所在的空间")
)

// Share is a workspace a drawer is shared with.
type Share struct {
	WorkspaceID int64
	Name, Kind  string
	Color       string
	CreatedAt   time.Time
}

func (s *Service) Shares(ctx context.Context, scope tenant.Scope, drawerID int64) ([]Share, error) {
	if _, err := s.Get(ctx, scope, drawerID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListDrawerShares(ctx, drawerID)
	if err != nil {
		return nil, err
	}
	out := make([]Share, len(rows))
	for i, r := range rows {
		out[i] = Share{WorkspaceID: r.TargetWorkspaceID, Name: r.Name, Kind: r.Kind, Color: r.Color, CreatedAt: time.UnixMilli(r.CreatedAt)}
	}
	return out, nil
}

// ShareCounts is how many workspaces each drawer of the scope is shared with.
func (s *Service) ShareCounts(ctx context.Context, scope tenant.Scope) (map[int64]int, error) {
	rows, err := s.q.CountDrawerShares(ctx, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	out := map[int64]int{}
	for _, r := range rows {
		out[r.DrawerID] = int(r.N)
	}
	return out, nil
}

// Unshare stops sharing a drawer of the scope with target.
func (s *Service) Unshare(ctx context.Context, scope tenant.Scope, drawerID, target int64) error {
	ws := scope.WorkspaceID()
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		d, err := q.GetDrawer(ctx, sqlc.GetDrawerParams{WorkspaceID: ws, ID: drawerID})
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if n, err := q.DeleteDrawerShare(ctx, sqlc.DeleteDrawerShareParams{DrawerID: drawerID, TargetWorkspaceID: target}); err != nil || n == 0 {
			if err != nil {
				return err
			}
			return ErrNotFound
		}
		var name string
		_ = s.db.QueryRowContext(ctx, `SELECT name FROM workspaces WHERE id = ?`, target).Scan(&name)
		return s.record(ctx, q, ws, "unshare", drawerID, nil, i18n.N("%s 取消了抽屉“%s”对空间“%s”的共享"), d.Name, name)
	})
	if err == nil {
		s.changed(target)
	}
	return err
}

// SharedDrawer is a drawer another workspace shares with the scope.
type SharedDrawer struct {
	Drawer
	SourceName string
	Count      int
}

func (s *Service) SharedWithMe(ctx context.Context, scope tenant.Scope) ([]SharedDrawer, error) {
	rows, err := s.q.ListSharedDrawers(ctx, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	out := make([]SharedDrawer, len(rows))
	for i, r := range rows {
		d := toDrawer(sqlc.Drawer{ID: r.ID, WorkspaceID: r.WorkspaceID, Name: r.Name, Icon: r.Icon, Color: r.Color, Description: r.Description,
			TemplateKey: r.TemplateKey, Rule: r.Rule, SortOrder: r.SortOrder, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt})
		n, err := s.q.CountSharedDocuments(ctx, sql.NullInt64{Int64: r.ID, Valid: true})
		if err != nil {
			return nil, err
		}
		out[i] = SharedDrawer{Drawer: d, SourceName: r.SourceName, Count: int(n)}
	}
	return out, nil
}

// Shared opens a drawer shared with the scope: it returns the drawer and a
// read-only scope of its own workspace, or ErrNotFound.
func (s *Service) Shared(ctx context.Context, scope tenant.Scope, drawerID int64) (SharedDrawer, tenant.Scope, error) {
	r, err := s.q.GetSharedDrawer(ctx, sqlc.GetSharedDrawerParams{TargetWorkspaceID: scope.WorkspaceID(), DrawerID: drawerID})
	if errors.Is(err, sql.ErrNoRows) {
		return SharedDrawer{}, tenant.Scope{}, ErrNotFound
	}
	if err != nil {
		return SharedDrawer{}, tenant.Scope{}, err
	}
	d := toDrawer(sqlc.Drawer{ID: r.ID, WorkspaceID: r.WorkspaceID, Name: r.Name, Icon: r.Icon, Color: r.Color, Description: r.Description,
		TemplateKey: r.TemplateKey, Rule: r.Rule, SortOrder: r.SortOrder, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt})
	n, err := s.q.CountSharedDocuments(ctx, sql.NullInt64{Int64: r.ID, Valid: true})
	if err != nil {
		return SharedDrawer{}, tenant.Scope{}, err
	}
	return SharedDrawer{Drawer: d, SourceName: r.SourceName, Count: int(n)}, tenant.New(r.WorkspaceID, tenant.RoleViewer).Outside(), nil
}

// ErrSharedDocument: the document is not in a drawer shared with the scope.
var ErrSharedDocument = apperr.New(apperr.KindNotFound, "找不到这份文档")

// SharedDocument checks that a document is in a drawer shared with the
// scope and returns the read-only scope of its workspace.
func (s *Service) SharedDocument(ctx context.Context, scope tenant.Scope, drawerID, docID int64) (tenant.Scope, error) {
	_, src, err := s.Shared(ctx, scope, drawerID)
	if err != nil {
		return tenant.Scope{}, err
	}
	if _, err := s.q.GetSharedDocumentID(ctx, sqlc.GetSharedDocumentIDParams{DocumentID: docID, DrawerID: drawerID, TargetWorkspaceID: scope.WorkspaceID()}); errors.Is(err, sql.ErrNoRows) {
		return tenant.Scope{}, ErrSharedDocument
	} else if err != nil {
		return tenant.Scope{}, err
	}
	return src, nil
}
