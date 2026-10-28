package auth

import (
	"context"
	"strconv"
	"time"

	"kept/server/internal/store/sqlc"
)

// VerifyWindow is how long a confirmed password unlocks sensitive
// documents (v1.6.4), after 1Password's “require re-authentication”.
const VerifyWindow = 15 * time.Minute

var (
	ErrVerifyNothing = newErr(KindInvalid, "请输入密码或两步验证码")
	// ErrVerifyNoMeans: an account signed in by single sign-on with no
	// password and no two-factor authentication confirms by signing in again.
	ErrVerifyNoMeans = newErr(KindInvalid, "这个账号没有密码，请退出后重新登录")
	// ErrVerifyToken: API tokens and phones cannot confirm a password.
	ErrVerifyToken = newErr(KindForbidden, "API Token 不能确认密码，请在网页或手机 App 上确认")
	// ErrVerifyPassword: not ErrWrongPassword, which speaks of the
	// “current” password (changing it).
	ErrVerifyPassword = newErr(KindInvalid, "密码不正确")
)

// Verified reports whether the caller confirmed its password recently
// enough to open sensitive documents. Tokens (API, phones) never have.
func (p Principal) Verified(now time.Time) bool {
	return (p.Via == ViaSession || p.Via == ViaDevice && p.DeviceAccess == AccessFull) && !p.VerifiedAt.IsZero() && now.Sub(p.VerifiedAt) < VerifyWindow
}

// VerifySession confirms the caller's password, or a two-factor code
// (TOTP) when the account has two-factor authentication, and marks the
// session verified. Wrong attempts are limited per account like logins,
// on a counter of their own, so a stolen session cannot lock the owner out
// of signing in.
func (s *Service) VerifySession(ctx context.Context, p Principal, password, code string) (time.Time, error) {
	// A web session, or a phone with full access (v1.7.20); not API tokens.
	if p.Via != ViaSession && !(p.Via == ViaDevice && p.DeviceAccess == AccessFull) {
		return time.Time{}, ErrVerifyToken
	}
	if password == "" && code == "" {
		return time.Time{}, ErrVerifyNothing
	}
	u, err := s.q.GetUserByID(ctx, p.UserID)
	if err != nil {
		return time.Time{}, err
	}
	if u.PasswordHash == "" && !u.TwoFactorEnabled {
		return time.Time{}, ErrVerifyNoMeans
	}
	key := "v:" + strconv.FormatInt(p.UserID, 10)
	gen, ok := s.userLimit.Reserve(key)
	if !ok {
		return time.Time{}, s.rateLimitedErr(s.userLimit.RetryAfter(key))
	}
	now := s.now()
	good := false
	if code != "" {
		good, err = s.checkSecondFactor(ctx, s.q, p.UserID, code, "", now)
		if err != nil {
			s.userLimit.Release(key, gen)
			return time.Time{}, err
		}
	} else {
		good = s.verifyPassword(u.PasswordHash, password)
	}
	if !good {
		s.userLimit.Settle(key, gen)
		if s.userLimit.Remaining(key) == 0 {
			return time.Time{}, s.rateLimitedErr(s.userLimit.RetryAfter(key))
		}
		if code != "" {
			return time.Time{}, ErrWrongCode
		}
		return time.Time{}, ErrVerifyPassword
	}
	s.userLimit.Reset(key)
	if p.Via == ViaDevice {
		err = s.q.SetTokenVerified(ctx, sqlc.SetTokenVerifiedParams{Now: now.UnixMilli(), ID: p.TokenID})
	} else {
		err = s.q.SetSessionVerified(ctx, sqlc.SetSessionVerifiedParams{Now: now.UnixMilli(), ID: p.SessionID})
	}
	if err != nil {
		return time.Time{}, err
	}
	return now.Add(VerifyWindow), nil
}

// Verified reports whether p may open sensitive documents now (by the
// service's clock).
func (s *Service) Verified(p Principal) bool { return p.Verified(s.now()) }

// VerifiedUntil is when p's confirmation runs out; zero when it has.
func (s *Service) VerifiedUntil(p Principal) time.Time {
	if !p.Verified(s.now()) {
		return time.Time{}
	}
	return p.VerifiedAt.Add(VerifyWindow)
}

// CheckPassword checks a signed-in user's own password, counted like a
// confirmation (v1.8.1: setting up a family member's key wraps it with the
// password). An account without a password (signing in through SSO)
// reports hasPassword false, and then only a recovery code protects keys.
func (s *Service) CheckPassword(ctx context.Context, p Principal, password string) (hasPassword bool, err error) {
	u, err := s.q.GetUserByID(ctx, p.UserID)
	if err != nil {
		return false, err
	}
	if u.PasswordHash == "" {
		return false, nil
	}
	if password == "" {
		return true, ErrVerifyNothing
	}
	key := "v:" + strconv.FormatInt(p.UserID, 10)
	gen, ok := s.userLimit.Reserve(key)
	if !ok {
		return true, s.rateLimitedErr(s.userLimit.RetryAfter(key))
	}
	if !s.verifyPassword(u.PasswordHash, password) {
		s.userLimit.Settle(key, gen)
		if s.userLimit.Remaining(key) == 0 {
			return true, s.rateLimitedErr(s.userLimit.RetryAfter(key))
		}
		return true, ErrVerifyPassword
	}
	s.userLimit.Reset(key)
	return true, nil
}
