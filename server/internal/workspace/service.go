// Package workspace manages workspaces (personal, family, team) and who
// belongs to them. Every operation on a workspace first resolves the
// caller's membership; a workspace the caller is not a member of looks
// exactly like one that does not exist.
package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"kept/server/internal/audit"
	"kept/server/internal/i18n"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/taxonomy"
	"kept/server/internal/tenant"
)

const (
	KindPersonal = "personal"
	KindFamily   = "family"
	KindTeam     = "team"

	defaultColor = "blue"
	maxNameLen   = 32
)

// colors are the avatar colours a workspace can take (DrawerColor in the
// web app's theme).
var colors = map[string]bool{
	"blue": true, "cyan": true, "green": true, "orange": true,
	"red": true, "purple": true, "pink": true, "gray": true,
}

// created is the audit summary of a new workspace, by kind.
var created = map[string]string{
	KindPersonal: i18n.N("%s 新建了个人空间“%s”"), KindFamily: i18n.N("%s 新建了家庭空间“%s”"), KindTeam: i18n.N("%s 新建了团队空间“%s”"),
}

type Options struct {
	Now func() time.Time // default time.Now
}

type Service struct {
	db  *sql.DB
	q   *sqlc.Queries
	now func() time.Time
}

func NewService(db *sql.DB, opt Options) *Service {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	return &Service{db: db, q: sqlc.New(db), now: opt.Now}
}

// Workspace is a workspace as seen by one of its members.
type Workspace struct {
	ID          int64
	Name        string
	Kind        string
	Color       string
	Role        tenant.Role // the caller's role
	MemberCount int64
}

// List returns the workspaces userID belongs to, the personal one first.
func (s *Service) List(ctx context.Context, userID int64) ([]Workspace, error) {
	rows, err := s.q.ListWorkspacesForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Workspace, len(rows))
	for i, r := range rows {
		out[i] = Workspace{ID: r.ID, Name: r.Name, Kind: r.Kind, Color: r.Color, Role: tenant.Role(r.Role), MemberCount: r.MemberCount}
	}
	return out, nil
}

// Resolve checks that userID is a member of workspaceID and returns the
// scope a request acting in that workspace runs with.
func (s *Service) Resolve(ctx context.Context, userID, workspaceID int64) (tenant.Scope, Workspace, error) {
	ws, err := get(ctx, s.q, userID, workspaceID)
	if err != nil {
		return tenant.Scope{}, Workspace{}, err
	}
	return tenant.ForUser(ws.ID, ws.Role, userID), ws, nil
}

// Create makes a family or team workspace owned by userID.
func (s *Service) Create(ctx context.Context, userID int64, name, kind, color string) (Workspace, error) {
	name, err := normalizeName(name)
	if err != nil {
		return Workspace{}, err
	}
	if kind != KindFamily && kind != KindTeam {
		return Workspace{}, errKind
	}
	if color == "" {
		color = defaultColor
	}
	if !colors[color] {
		return Workspace{}, errColor
	}
	var ws Workspace
	err = s.inTx(ctx, func(q *sqlc.Queries) error {
		now := s.now()
		id, err := q.InsertWorkspace(ctx, sqlc.InsertWorkspaceParams{
			Name: name, Kind: kind, Color: color, CreatedBy: sql.NullInt64{Int64: userID, Valid: true}, CreatedAt: now.UnixMilli(),
		})
		if err != nil {
			return err
		}
		if err := q.InsertWorkspaceMember(ctx, sqlc.InsertWorkspaceMemberParams{
			WorkspaceID: id, UserID: userID, Role: string(tenant.RoleOwner), CreatedAt: now.UnixMilli(),
		}); err != nil {
			return err
		}
		if err := taxonomy.SeedWorkspace(ctx, q, id, now); err != nil {
			return err
		}
		ws = Workspace{ID: id, Name: name, Kind: kind, Color: color, Role: tenant.RoleOwner, MemberCount: 1}
		return s.record(ctx, q, ws.ID, "workspace.create", fmt.Sprintf(created[kind], audit.ActorFrom(ctx).Name, name), nil)
	})
	return ws, err
}

// Update renames and/or recolours a workspace; nil leaves a field as it is.
func (s *Service) Update(ctx context.Context, userID, workspaceID int64, name, color *string) (Workspace, error) {
	var ws Workspace
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		var err error
		if ws, err = getOwned(ctx, q, userID, workspaceID); err != nil {
			return err
		}
		changes := map[string][2]any{}
		what := ""
		if name != nil {
			n, err := normalizeName(*name)
			if err != nil {
				return err
			}
			if n != ws.Name {
				changes["name"] = [2]any{ws.Name, n}
				what = fmt.Sprintf(i18n.N("%s 将空间“%s”改名为“%s”"), audit.ActorFrom(ctx).Name, ws.Name, n)
				ws.Name = n
			}
		}
		if color != nil {
			if !colors[*color] {
				return errColor
			}
			if *color != ws.Color {
				changes["color"] = [2]any{ws.Color, *color}
				if what == "" {
					what = fmt.Sprintf(i18n.N("%s 修改了空间“%s”的颜色"), audit.ActorFrom(ctx).Name, ws.Name)
				}
				ws.Color = *color
			}
		}
		if len(changes) == 0 {
			return nil
		}
		if err := q.UpdateWorkspace(ctx, sqlc.UpdateWorkspaceParams{Name: ws.Name, Color: ws.Color, ID: ws.ID}); err != nil {
			return err
		}
		return s.record(ctx, q, ws.ID, "workspace.update", what, changes)
	})
	return ws, err
}

// Delete soft-deletes a family or team workspace: members lose access at
// once, the data is purged 30 days later. confirmName must be the
// workspace's exact name.
func (s *Service) Delete(ctx context.Context, userID, workspaceID int64, confirmName string) error {
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		ws, err := getOwned(ctx, q, userID, workspaceID)
		if err != nil {
			return err
		}
		if ws.Kind == KindPersonal {
			return ErrPersonalDelete
		}
		if confirmName != ws.Name {
			return ErrNameMismatch
		}
		now := s.now()
		if err := q.SoftDeleteWorkspace(ctx, sqlc.SoftDeleteWorkspaceParams{
			DeletedAt: sql.NullInt64{Int64: now.UnixMilli(), Valid: true}, ID: ws.ID,
		}); err != nil {
			return err
		}
		return s.record(ctx, q, ws.ID, "workspace.delete", fmt.Sprintf(i18n.N("%s 删除了空间“%s”"), audit.ActorFrom(ctx).Name, ws.Name), nil)
	})
}

// get loads a workspace as one of its members; anything else is ErrNotFound.
func get(ctx context.Context, q *sqlc.Queries, userID, workspaceID int64) (Workspace, error) {
	r, err := q.GetWorkspaceForUser(ctx, sqlc.GetWorkspaceForUserParams{UserID: userID, ID: workspaceID})
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	if err != nil {
		return Workspace{}, err
	}
	return Workspace{ID: r.ID, Name: r.Name, Kind: r.Kind, Color: r.Color, Role: tenant.Role(r.Role), MemberCount: r.MemberCount}, nil
}

// getOwned is get for operations only a workspace administrator may perform.
func getOwned(ctx context.Context, q *sqlc.Queries, userID, workspaceID int64) (Workspace, error) {
	ws, err := get(ctx, q, userID, workspaceID)
	if err != nil {
		return Workspace{}, err
	}
	if ws.Role != tenant.RoleOwner {
		return Workspace{}, ErrNotOwner
	}
	return ws, nil
}

// record writes a workspace-level audit event; what reads as
// "<caller's name><what>".
func (s *Service) record(ctx context.Context, q *sqlc.Queries, workspaceID int64, action, summary string, changes map[string][2]any) error {
	return audit.Record(ctx, q, audit.Event{
		WorkspaceID: workspaceID, Action: action, TargetType: "workspace", TargetID: strconv.FormatInt(workspaceID, 10),
		Summary: summary, Changes: changes, At: s.now(),
	})
}

func (s *Service) inTx(ctx context.Context, fn func(*sqlc.Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil) // BEGIN IMMEDIATE (see store.Open)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit()
}

func normalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > maxNameLen {
		return "", errName
	}
	return name, nil
}

// PurgeRetention is how long a deleted workspace's data is kept.
const PurgeRetention = 30 * 24 * time.Hour

// PurgeDeleted permanently removes workspaces deleted before the cutoff,
// together with their memberships, jobs and audit events.
func (s *Service) PurgeDeleted(ctx context.Context, before time.Time) (int64, error) {
	return s.q.DeleteWorkspacesDeletedBefore(ctx, sql.NullInt64{Int64: before.UnixMilli(), Valid: true})
}

// DeletedBefore lists the workspaces PurgeDeleted(before) will remove, so
// their files can go with them.
func (s *Service) DeletedBefore(ctx context.Context, before time.Time) ([]int64, error) {
	return s.q.ListWorkspacesDeletedBefore(ctx, sql.NullInt64{Int64: before.UnixMilli(), Valid: true})
}
