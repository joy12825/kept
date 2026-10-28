package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"kept/server/internal/audit"
	"kept/server/internal/i18n"
	"kept/server/internal/privacy"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// Member is an account's membership of a workspace.
type Member struct {
	UserID      int64
	Username    string
	DisplayName string
	Email       string
	Role        tenant.Role
	JoinedAt    time.Time
}

// roleLabel is how a role reads in the UI and in audit summaries.
var roleLabel = map[tenant.Role]string{tenant.RoleOwner: i18n.N("管理员"), tenant.RoleEditor: i18n.N("成员"), tenant.RoleViewer: i18n.N("只看")}

// ListMembers returns who is in a workspace, in joining order. Any member
// may look.
func (s *Service) ListMembers(ctx context.Context, userID, workspaceID int64) ([]Member, error) {
	if _, err := get(ctx, s.q, userID, workspaceID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListWorkspaceMembers(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]Member, len(rows))
	for i, r := range rows {
		out[i] = Member{UserID: r.UserID, Username: r.Username, DisplayName: r.DisplayName, Email: r.Email.String,
			Role: tenant.Role(r.Role), JoinedAt: time.UnixMilli(r.CreatedAt)}
	}
	return out, nil
}

// AddMember adds an existing account, found by username or email, to a
// family or team workspace. There are no invitations: accounts are created
// by the instance administrator first.
func (s *Service) AddMember(ctx context.Context, userID, workspaceID int64, login string, role tenant.Role) (Member, error) {
	if roleLabel[role] == "" {
		return Member{}, errRole
	}
	var m Member
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		ws, err := getOwned(ctx, q, userID, workspaceID)
		if err != nil {
			return err
		}
		if ws.Kind == KindPersonal {
			return ErrPersonalMembers
		}
		target, err := findUser(ctx, q, login)
		if err != nil {
			return err
		}
		if _, err := q.GetWorkspaceMember(ctx, sqlc.GetWorkspaceMemberParams{WorkspaceID: ws.ID, UserID: target.ID}); err == nil {
			return ErrAlreadyMember
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		now := s.now()
		if err := q.InsertWorkspaceMember(ctx, sqlc.InsertWorkspaceMemberParams{
			WorkspaceID: ws.ID, UserID: target.ID, Role: string(role), CreatedAt: now.UnixMilli(),
		}); err != nil {
			return err
		}
		m = Member{UserID: target.ID, Username: target.Username, DisplayName: target.DisplayName, Email: target.Email.String, Role: role, JoinedAt: now}
		return s.recordMember(ctx, q, ws.ID, "member.add", m.UserID, fmt.Sprintf(i18n.N("%s 添加了成员 %s（%s）"), audit.ActorFrom(ctx).Name, m.DisplayName, roleLabel[role]), nil)
	})
	return m, err
}

// UpdateMemberRole changes a member's role; the workspace always keeps at
// least one administrator.
func (s *Service) UpdateMemberRole(ctx context.Context, userID, workspaceID, targetID int64, role tenant.Role) (Member, error) {
	if roleLabel[role] == "" {
		return Member{}, errRole
	}
	var m Member
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		ws, err := getOwned(ctx, q, userID, workspaceID)
		if err != nil {
			return err
		}
		if m, err = getMember(ctx, q, ws.ID, targetID); err != nil {
			return err
		}
		if m.Role == role {
			return nil
		}
		if m.Role == tenant.RoleOwner {
			if err := ensureAnotherOwner(ctx, q, ws.ID); err != nil {
				return err
			}
		}
		if err := q.UpdateWorkspaceMemberRole(ctx, sqlc.UpdateWorkspaceMemberRoleParams{Role: string(role), WorkspaceID: ws.ID, UserID: targetID}); err != nil {
			return err
		}
		old := m.Role
		m.Role = role
		return s.recordMember(ctx, q, ws.ID, "member.role_change", targetID,
			fmt.Sprintf(i18n.N("%s 将 %s 的角色从 %s 改为 %s"), audit.ActorFrom(ctx).Name, m.DisplayName, roleLabel[old], roleLabel[role]), map[string][2]any{"role": {string(old), string(role)}})
	})
	return m, err
}

// RemoveMember removes targetID from a workspace. Administrators may remove
// anyone; any member may remove themselves (leave).
func (s *Service) RemoveMember(ctx context.Context, userID, workspaceID, targetID int64) error {
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		ws, err := get(ctx, q, userID, workspaceID)
		if err != nil {
			return err
		}
		leaving := targetID == userID
		if !leaving && ws.Role != tenant.RoleOwner {
			return ErrNotOwner
		}
		if leaving && ws.Kind == KindPersonal {
			return ErrPersonalLeave
		}
		m, err := getMember(ctx, q, ws.ID, targetID)
		if err != nil {
			return err
		}
		if m.Role == tenant.RoleOwner {
			if err := ensureAnotherOwner(ctx, q, ws.ID); err != nil {
				return err
			}
		}
		if err := q.DeleteWorkspaceMember(ctx, sqlc.DeleteWorkspaceMemberParams{WorkspaceID: ws.ID, UserID: targetID}); err != nil {
			return err
		}
		// access to family members' private documents ends with membership
		if err := privacy.MemberLeft(ctx, q, ws.ID, targetID); err != nil {
			return err
		}
		if leaving {
			return s.recordMember(ctx, q, ws.ID, "member.leave", targetID, fmt.Sprintf(i18n.N("%s 退出了空间"), audit.ActorFrom(ctx).Name), nil)
		}
		return s.recordMember(ctx, q, ws.ID, "member.remove", targetID, fmt.Sprintf(i18n.N("%s 移除了成员 %s"), audit.ActorFrom(ctx).Name, m.DisplayName), nil)
	})
}

// findUser looks an active account up by username or email.
func findUser(ctx context.Context, q *sqlc.Queries, login string) (sqlc.GetUserByIDRow, error) {
	login = strings.ToLower(strings.TrimSpace(login))
	var (
		u   sqlc.GetUserByIDRow
		err error
	)
	if strings.Contains(login, "@") {
		var row sqlc.GetUserByEmailRow
		row, err = q.GetUserByEmail(ctx, sql.NullString{String: login, Valid: true})
		u = sqlc.GetUserByIDRow(row)
	} else {
		var row sqlc.GetUserByUsernameRow
		row, err = q.GetUserByUsername(ctx, login)
		u = sqlc.GetUserByIDRow(row)
	}
	if errors.Is(err, sql.ErrNoRows) || (err == nil && u.DisabledAt.Valid) {
		return sqlc.GetUserByIDRow{}, ErrUserNotFound
	}
	return u, err
}

func getMember(ctx context.Context, q *sqlc.Queries, workspaceID, userID int64) (Member, error) {
	r, err := q.GetWorkspaceMember(ctx, sqlc.GetWorkspaceMemberParams{WorkspaceID: workspaceID, UserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return Member{}, ErrMemberNotFound
	}
	if err != nil {
		return Member{}, err
	}
	return Member{UserID: r.UserID, Username: r.Username, DisplayName: r.DisplayName, Email: r.Email.String,
		Role: tenant.Role(r.Role), JoinedAt: time.UnixMilli(r.CreatedAt)}, nil
}

// ensureAnotherOwner fails when the workspace has a single administrator.
// It runs in the caller's write transaction, so the count cannot change
// before the caller's own update.
func ensureAnotherOwner(ctx context.Context, q *sqlc.Queries, workspaceID int64) error {
	n, err := q.CountWorkspaceOwners(ctx, workspaceID)
	if err != nil {
		return err
	}
	if n <= 1 {
		return ErrLastOwner
	}
	return nil
}

func (s *Service) recordMember(ctx context.Context, q *sqlc.Queries, workspaceID int64, action string, targetID int64, summary string, changes map[string][2]any) error {
	return audit.Record(ctx, q, audit.Event{
		WorkspaceID: workspaceID, Action: action, TargetType: "user", TargetID: strconv.FormatInt(targetID, 10),
		Summary: summary, Changes: changes, At: s.now(),
	})
}
