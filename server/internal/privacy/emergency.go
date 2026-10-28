package privacy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"kept/server/internal/i18n"
	"strings"
	"time"
	"unicode/utf8"

	"kept/server/internal/apperr"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// Emergency contacts for private documents (after the drawers' emergency
// access, v1.6.4): a person names members who may ask to see their private
// documents. The person (or a guardian) is told and may deny within the
// wait; if nobody does, the contact gets read access, which the person may
// take back at any time.

const (
	MinWaitHours        = 24
	MaxWaitHours        = 30 * 24
	maxEmergencyContact = 5
	maxReason           = 500
)

// Emergency states.
const (
	EmergencyIdle    = "idle"
	EmergencyPending = "pending"
	EmergencyGranted = "granted"
)

var (
	errEmergencyWait  = apperr.New(apperr.KindInvalid, "等待期应在 1 到 30 天之间")
	errEmergencySelf  = apperr.New(apperr.KindInvalid, "已经能看这位家人私密文档的人不需要设为紧急联系人")
	errEmergencyMany  = apperr.New(apperr.KindInvalid, "一位家人最多 5 位紧急联系人")
	errEmergencyNone  = apperr.New(apperr.KindNotFound, "你不是这位家人的紧急联系人")
	errEmergencyState = apperr.WithCode(apperr.KindConflict, "emergency_state", "状态已经变了，请刷新后再试")
	errEmergencyWhy   = apperr.New(apperr.KindInvalid, "说明最多 500 个字")
)

// Emergency is an emergency contact of a person.
type Emergency struct {
	UserID      int64
	DisplayName string
	WaitHours   int
	Status      string
	Reason      string
	RequestedAt *time.Time
	GrantAt     *time.Time
	GrantedAt   *time.Time
}

// Mailer sends a notice to some members (nil: none; the app shows it).
type Mailer func(ctx context.Context, ws int64, userIDs []int64, subject, text string)

// SetMailer sets how emergency notices are sent.
func (s *Service) SetMailer(m Mailer) { s.mail = m }

func (s *Service) emergencies(ctx context.Context, q *sqlc.Queries, ws int64) (map[int64][]Emergency, error) {
	rows, err := q.ListPersonEmergency(ctx, ws)
	if err != nil {
		return nil, err
	}
	out := map[int64][]Emergency{}
	for _, r := range rows {
		out[r.PersonID] = append(out[r.PersonID], Emergency{
			UserID: r.UserID, DisplayName: r.DisplayName, WaitHours: int(r.WaitHours), Status: r.Status, Reason: r.Reason,
			RequestedAt: ms(r.RequestedAt), GrantAt: ms(r.GrantAt), GrantedAt: ms(r.GrantedAt),
		})
	}
	return out, nil
}

func ms(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := time.UnixMilli(v.Int64)
	return &t
}

// RemoveEmergency removes a contact (the person's holders, or the contact
// themself); access the contact got through it goes too.
func (s *Service) RemoveEmergency(ctx context.Context, scope tenant.Scope, personID, userID int64) error {
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		st, err := s.person(ctx, q, scope, personID)
		if err != nil {
			return err
		}
		if userID != scope.User() && !st.isHolder(scope.User()) {
			return errNotHolder
		}
		e, err := q.GetPersonEmergency(ctx, sqlc.GetPersonEmergencyParams{PersonID: personID, UserID: userID})
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if e.Status == EmergencyGranted {
			if err := s.dropEmergencyAccess(ctx, q, personID, userID); err != nil {
				return err
			}
		}
		_, err = q.DeletePersonEmergency(ctx, sqlc.DeletePersonEmergencyParams{PersonID: personID, UserID: userID})
		return err
	})
}

func (s *Service) dropEmergencyAccess(ctx context.Context, q *sqlc.Queries, personID, userID int64) error {
	l, err := q.GetPersonAccessVia(ctx, sqlc.GetPersonAccessViaParams{PersonID: personID, UserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil || l != ViaEmergency {
		return err
	}
	_, err = q.DeletePersonAccess(ctx, sqlc.DeletePersonAccessParams{PersonID: personID, UserID: userID})
	return err
}

// RequestEmergency is the contact asking to see a person's private
// documents; the holders are told.
func (s *Service) RequestEmergency(ctx context.Context, scope tenant.Scope, personID int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > maxReason {
		return errEmergencyWhy
	}
	var holders []int64
	var grantAt time.Time
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		st, err := s.person(ctx, q, scope, personID)
		if err != nil {
			return err
		}
		e, err := q.GetPersonEmergency(ctx, sqlc.GetPersonEmergencyParams{PersonID: personID, UserID: scope.User()})
		if errors.Is(err, sql.ErrNoRows) {
			return errEmergencyNone
		}
		if err != nil {
			return err
		}
		if e.Status != EmergencyIdle {
			return errEmergencyState
		}
		now := s.now()
		grantAt = now.Add(time.Duration(e.WaitHours) * time.Hour)
		holders = st.holders()
		return q.SetPersonEmergencyState(ctx, sqlc.SetPersonEmergencyStateParams{
			Status: EmergencyPending, Reason: reason, ID: e.ID, UpdatedAt: now.UnixMilli(),
			RequestedAt: sql.NullInt64{Int64: now.UnixMilli(), Valid: true}, GrantAt: sql.NullInt64{Int64: grantAt.UnixMilli(), Valid: true},
		})
	})
	if err == nil && s.mail != nil && len(holders) > 0 {
		// one sentence a line: each recipient gets it in their language (v1.10.2)
		text := fmt.Sprintf(i18n.N("有人申请紧急查看家人的私密文档。如果不同意，请在 %s 之前登录，在“设置 → 我的私密”里拒绝；到时没有拒绝，对方就能只读查看。"), grantAt.Format("2006-01-02 15:04"))
		if reason != "" {
			text += "\n\n" + fmt.Sprintf(i18n.N("对方的说明：%s"), reason)
		}
		s.mail(ctx, scope.WorkspaceID(), holders, i18n.N("有人申请紧急查看私密文档"), text)
	}
	return err
}

// CancelEmergency withdraws the contact's own request.
func (s *Service) CancelEmergency(ctx context.Context, scope tenant.Scope, personID int64) error {
	return s.setIdle(ctx, scope, personID, scope.User(), func(st state) bool { return true })
}

// DenyEmergency is a holder refusing a pending request (or ending a
// granted one).
func (s *Service) DenyEmergency(ctx context.Context, scope tenant.Scope, personID, userID int64) error {
	err := s.setIdle(ctx, scope, personID, userID, func(st state) bool { return st.isHolder(scope.User()) })
	if err == nil && s.mail != nil {
		s.mail(ctx, scope.WorkspaceID(), []int64{userID}, i18n.N("紧急查看的申请被拒绝了"), i18n.N("你申请紧急查看家人私密文档，对方拒绝了。"))
	}
	return err
}

func (s *Service) setIdle(ctx context.Context, scope tenant.Scope, personID, userID int64, allowed func(state) bool) error {
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		st, err := s.person(ctx, q, scope, personID)
		if err != nil {
			return err
		}
		if !allowed(st) {
			return errNotHolder
		}
		e, err := q.GetPersonEmergency(ctx, sqlc.GetPersonEmergencyParams{PersonID: personID, UserID: userID})
		if errors.Is(err, sql.ErrNoRows) {
			return errEmergencyNone
		}
		if err != nil {
			return err
		}
		if e.Status == EmergencyIdle {
			return errEmergencyState
		}
		if e.Status == EmergencyGranted {
			if err := s.dropEmergencyAccess(ctx, q, personID, userID); err != nil {
				return err
			}
		}
		return q.SetPersonEmergencyState(ctx, sqlc.SetPersonEmergencyStateParams{Status: EmergencyIdle, ID: e.ID, UpdatedAt: s.now().UnixMilli()})
	})
}

// GrantDue grants every request whose wait is over. It runs when privacy
// is looked at, and on a schedule.
func (s *Service) GrantDue(ctx context.Context) error {
	due, err := s.q.DuePersonEmergency(ctx, sql.NullInt64{Int64: s.now().UnixMilli(), Valid: true})
	if err != nil || len(due) == 0 {
		return err
	}
	for _, e := range due {
		err := s.inTx(ctx, func(q *sqlc.Queries) error {
			cur, err := q.GetPersonEmergency(ctx, sqlc.GetPersonEmergencyParams{PersonID: e.PersonID, UserID: e.UserID})
			if err != nil || cur.Status != EmergencyPending {
				return err
			}
			now := s.now().UnixMilli()
			// a contact who has access already keeps it as it is
			if _, err := q.GetPersonAccessVia(ctx, sqlc.GetPersonAccessViaParams{PersonID: e.PersonID, UserID: e.UserID}); errors.Is(err, sql.ErrNoRows) {
				if err := q.UpsertPersonAccess(ctx, sqlc.UpsertPersonAccessParams{
					PersonID: e.PersonID, UserID: e.UserID, WorkspaceID: e.WorkspaceID, Level: LevelRead, Via: ViaEmergency,
					Reviewed: 1, CreatedAt: now,
				}); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
			return q.SetPersonEmergencyState(ctx, sqlc.SetPersonEmergencyStateParams{
				Status: EmergencyGranted, Reason: cur.Reason, RequestedAt: cur.RequestedAt, GrantAt: cur.GrantAt,
				GrantedAt: sql.NullInt64{Int64: now, Valid: true}, UpdatedAt: now, ID: cur.ID,
			})
		})
		if err != nil {
			return err
		}
		if s.mail != nil {
			s.mail(ctx, e.WorkspaceID, []int64{e.UserID}, i18n.N("紧急查看已生效"), i18n.N("等待期已过，你现在可以只读查看家人的私密文档了。"))
		}
	}
	return nil
}
