package auth

// Single sign-on (v1.0 stage 35): a person the OpenID Connect provider
// vouched for is let in as an existing account; accounts are never created
// this way (decision 29: accounts are given out by an administrator).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"kept/server/internal/audit"
	"kept/server/internal/i18n"
	"kept/server/internal/store/sqlc"
)

// SSOIdentity is who the provider says signed in.
type SSOIdentity struct {
	Issuer, Subject, Email string
	EmailVerified          bool
}

// SSORefusal says why a single sign-on was refused; Code is shown to the
// login page.
type SSORefusal struct{ Code string }

func (r *SSORefusal) Error() string { return "sso refused: " + r.Code }

// Reasons a single sign-on is refused.
const (
	SSONoAccount  = "no_account" // no account has this email
	SSODisabled   = "disabled"   // the account is disabled
	SSOUnverified = "unverified" // the provider did not vouch for the email
	SSOTaken      = "taken"      // the account is bound to someone else there
)

// LoginSSO starts a session for the account the provider's person is
// bound to; the first time, it binds the account with the same (verified)
// email. Kept's own second factor is not asked: the provider signed them in.
func (s *Service) LoginSSO(ctx context.Context, id SSOIdentity, remember bool, ci ClientInfo) (string, Principal, error) {
	u, err := s.q.GetUserBySSO(ctx, sqlc.GetUserBySSOParams{SsoIssuer: nullString(id.Issuer), SsoSubject: nullString(id.Subject)})
	bind := false
	if errors.Is(err, sql.ErrNoRows) {
		email := strings.ToLower(strings.TrimSpace(id.Email))
		if email == "" {
			return "", Principal{}, &SSORefusal{SSONoAccount}
		}
		if !id.EmailVerified {
			return "", Principal{}, &SSORefusal{SSOUnverified}
		}
		r, err := s.q.GetUserByEmail(ctx, nullString(email))
		if errors.Is(err, sql.ErrNoRows) {
			return "", Principal{}, &SSORefusal{SSONoAccount}
		}
		if err != nil {
			return "", Principal{}, err
		}
		if r.SsoSubject.Valid {
			return "", Principal{}, &SSORefusal{SSOTaken}
		}
		u, bind = sqlc.GetUserBySSORow(r), true
	} else if err != nil {
		return "", Principal{}, err
	}
	if u.DisabledAt.Valid {
		return "", Principal{}, &SSORefusal{SSODisabled}
	}
	name := u.DisplayName
	if name == "" {
		name = u.Username
	}
	actx := audit.WithActor(ctx, audit.Actor{UserID: u.ID, Name: name, Via: audit.ViaSession, RequestID: audit.ActorFrom(ctx).RequestID})
	err = s.inTx(actx, func(q *sqlc.Queries) error {
		if bind {
			if err := q.SetUserSSO(actx, sqlc.SetUserSSOParams{Issuer: nullString(id.Issuer), Subject: nullString(id.Subject), Now: s.now().UnixMilli(), ID: u.ID}); err != nil {
				return err
			}
			if err := s.recordUser(actx, q, "user.sso_link", u.ID, fmt.Sprintf(i18n.N("%s 绑定了单点登录"), name), nil); err != nil {
				return err
			}
		}
		return s.recordUser(actx, q, "user.sso_login", u.ID, fmt.Sprintf(i18n.N("%s 通过单点登录登录"), name), nil)
	})
	if err != nil {
		return "", Principal{}, err
	}
	return s.createSession(ctx, u.ID, remember, ci)
}

// UnlinkSSO removes an account's binding (an instance administrator); the
// next single sign-on binds it again by email.
func (s *Service) UnlinkSSO(ctx context.Context, userID int64) error {
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		u, err := q.GetUserByID(ctx, userID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUserNotFound
		}
		if err != nil {
			return err
		}
		if !u.SsoSubject.Valid {
			return nil
		}
		if err := q.SetUserSSO(ctx, sqlc.SetUserSSOParams{Now: s.now().UnixMilli(), ID: userID}); err != nil {
			return err
		}
		return s.recordUser(ctx, q, "user.sso_unlink", userID, fmt.Sprintf(i18n.N("%s 解除了 %s 的单点登录绑定"), audit.ActorFrom(ctx).Name, u.Username), nil)
	})
}
