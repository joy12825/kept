package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"

	"kept/server/internal/audit"
	"kept/server/internal/i18n"
	"kept/server/internal/store/sqlc"
)

// totpSecretEncoding is unpadded, upper-case base32 (RFC 4648): used both to
// display the secret next to the QR code and to store it.
var totpSecretEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

const (
	totpSecretBytes   = 20
	recoveryCodeChars = "abcdefghijklmnopqrstuvwxyz234567"
	recoveryCodeLen   = 10
	recoveryCodeCount = 10
)

// TwoFactorStatus is what the "me" endpoints report about the caller's
// two-factor authentication.
type TwoFactorStatus struct {
	Enabled           bool
	RecoveryCodesLeft int
}

// TwoFactorSetup is returned once, when a new (unconfirmed) secret is
// generated: the raw secret (for manual entry) and the otpauth:// URI an
// authenticator app scans as a QR code.
type TwoFactorSetup struct {
	Secret     string
	OtpauthURI string
}

// TwoFactorStatus reports whether the account has two-factor authentication
// enabled, and (when enabled) how many unused recovery codes remain.
func (s *Service) TwoFactorStatus(ctx context.Context, userID int64) (TwoFactorStatus, error) {
	row, err := s.q.GetUserTOTP(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return TwoFactorStatus{}, nil
	}
	if err != nil {
		return TwoFactorStatus{}, err
	}
	if !row.EnabledAt.Valid {
		return TwoFactorStatus{}, nil
	}
	left, err := s.q.CountUnusedRecoveryCodes(ctx, userID)
	if err != nil {
		return TwoFactorStatus{}, err
	}
	return TwoFactorStatus{Enabled: true, RecoveryCodesLeft: int(left)}, nil
}

// BeginTwoFactor generates a new secret and stores it unconfirmed
// (enabled_at NULL), overwriting any previous unconfirmed secret. It fails
// with ErrTwoFactorEnabled when two-factor authentication is already on.
func (s *Service) BeginTwoFactor(ctx context.Context, userID int64, username string) (TwoFactorSetup, error) {
	existing, err := s.q.GetUserTOTP(ctx, userID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return TwoFactorSetup{}, err
	}
	if err == nil && existing.EnabledAt.Valid {
		return TwoFactorSetup{}, ErrTwoFactorEnabled
	}

	secret := make([]byte, totpSecretBytes)
	if _, err := rand.Read(secret); err != nil {
		return TwoFactorSetup{}, err
	}
	encoded := totpSecretEncoding.EncodeToString(secret)

	if err := s.q.UpsertPendingTOTP(ctx, sqlc.UpsertPendingTOTPParams{
		UserID: userID, Secret: encoded, CreatedAt: s.now().UnixMilli(),
	}); err != nil {
		return TwoFactorSetup{}, err
	}

	issuer := strings.ReplaceAll(s.instanceName(ctx), ":", "：") // a colon would split the label
	uri := "otpauth://totp/" + url.PathEscape(issuer+":"+username) +
		"?secret=" + encoded + "&issuer=" + url.QueryEscape(issuer) + "&algorithm=SHA1&digits=6&period=30"
	return TwoFactorSetup{Secret: encoded, OtpauthURI: uri}, nil
}

// EnableTwoFactor confirms the pending secret with a TOTP code, turns
// two-factor authentication on, and returns a freshly generated set of
// recovery codes (plaintext, shown to the user only this once).
func (s *Service) EnableTwoFactor(ctx context.Context, userID int64, code string) ([]string, error) {
	// Reading the pending secret, checking the code and switching two-factor
	// on happen in one write transaction (BEGIN IMMEDIATE; see store.Open), so
	// a concurrent BeginTwoFactor cannot swap the secret between the check
	// and the switch, and two concurrent confirmations cannot both succeed.
	var codes []string
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.GetUserTOTP(ctx, userID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrTwoFactorNotStarted
		}
		if err != nil {
			return err
		}
		if row.EnabledAt.Valid {
			return ErrTwoFactorNotStarted
		}
		secret, err := totpSecretEncoding.DecodeString(row.Secret)
		if err != nil {
			return err
		}
		now := s.now()
		step, ok := verifyTOTP(secret, code, now, row.LastUsedStep)
		if !ok {
			return ErrWrongCode
		}
		if err := q.EnableUserTOTP(ctx, sqlc.EnableUserTOTPParams{
			EnabledAt:    sql.NullInt64{Int64: now.UnixMilli(), Valid: true},
			LastUsedStep: step,
			UserID:       userID,
		}); err != nil {
			return err
		}
		if err := q.DeleteRecoveryCodes(ctx, userID); err != nil {
			return err
		}
		var genErr error
		codes, genErr = generateAndStoreRecoveryCodes(ctx, q, userID)
		return genErr
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// DisableTwoFactor turns two-factor authentication off after checking the
// caller's current password, and clears the account's recovery codes.
func (s *Service) DisableTwoFactor(ctx context.Context, p Principal, password string) error {
	u, err := s.q.GetUserByID(ctx, p.UserID)
	if err != nil {
		return err
	}
	if !VerifyPassword(u.PasswordHash, password) {
		return ErrWrongPassword
	}
	if !u.TwoFactorEnabled {
		return ErrTwoFactorNotEnabled
	}
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		if err := q.DeleteUserTOTP(ctx, p.UserID); err != nil {
			return err
		}
		return q.DeleteRecoveryCodes(ctx, p.UserID)
	})
}

// RegenerateRecoveryCodes checks the caller's current password, then
// replaces its recovery codes with a fresh set.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, p Principal, password string) ([]string, error) {
	u, err := s.q.GetUserByID(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	if !VerifyPassword(u.PasswordHash, password) {
		return nil, ErrWrongPassword
	}
	if !u.TwoFactorEnabled {
		return nil, ErrTwoFactorNotEnabled
	}
	var codes []string
	err = s.inTx(ctx, func(q *sqlc.Queries) error {
		if err := q.DeleteRecoveryCodes(ctx, p.UserID); err != nil {
			return err
		}
		var genErr error
		codes, genErr = generateAndStoreRecoveryCodes(ctx, q, p.UserID)
		return genErr
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// AdminDisableTwoFactor is an administrator turning two-factor
// authentication off for another account (e.g. a lost phone). It is
// idempotent: calling it on an account without two-factor authentication is
// not an error.
func (s *Service) AdminDisableTwoFactor(ctx context.Context, userID int64) error {
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		u, err := q.GetUserByID(ctx, userID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUserNotFound
		} else if err != nil {
			return err
		}
		if err := q.DeleteUserTOTP(ctx, userID); err != nil {
			return err
		}
		if err := q.DeleteRecoveryCodes(ctx, userID); err != nil {
			return err
		}
		if !u.TwoFactorEnabled {
			return nil // nothing was switched off
		}
		return s.recordUser(ctx, q, "user.2fa_disable", userID, fmt.Sprintf(i18n.N("%s 关闭了 %s 的两步验证"), audit.ActorFrom(ctx).Name, u.Username), nil)
	})
}

// checkSecondFactor verifies a TOTP code or a recovery code for userID,
// recording the used time step or recovery code on success so it cannot be
// replayed. Exactly one of code and recoveryCode is expected to be
// non-empty; an incorrect or already-used value is reported as (false, nil),
// not an error. It runs against q so a caller can use it inside its own
// transaction (the two-step login flow, task 4).
func (s *Service) checkSecondFactor(ctx context.Context, q *sqlc.Queries, userID int64, code, recoveryCode string, now time.Time) (bool, error) {
	if code != "" {
		row, err := q.GetUserTOTP(ctx, userID)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !row.EnabledAt.Valid {
			return false, nil
		}
		secret, err := totpSecretEncoding.DecodeString(row.Secret)
		if err != nil {
			return false, err
		}
		step, ok := verifyTOTP(secret, code, now, row.LastUsedStep)
		if !ok {
			return false, nil
		}
		if err := q.UpdateTOTPLastUsedStep(ctx, sqlc.UpdateTOTPLastUsedStepParams{
			LastUsedStep: step, UserID: userID,
		}); err != nil {
			return false, err
		}
		return true, nil
	}
	if recoveryCode != "" {
		row, err := q.GetUnusedRecoveryCode(ctx, sqlc.GetUnusedRecoveryCodeParams{
			UserID: userID, CodeHash: hashRecoveryCode(recoveryCode),
		})
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if err := q.MarkRecoveryCodeUsed(ctx, sqlc.MarkRecoveryCodeUsedParams{
			UsedAt: sql.NullInt64{Int64: now.UnixMilli(), Valid: true}, ID: row.ID,
		}); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

// ---------- recovery codes ----------

// generateAndStoreRecoveryCodes creates recoveryCodeCount fresh codes,
// stores their hashes and returns the plaintext (shown to the user once).
func generateAndStoreRecoveryCodes(ctx context.Context, q *sqlc.Queries, userID int64) ([]string, error) {
	codes := make([]string, recoveryCodeCount)
	for i := range codes {
		code, err := generateRecoveryCode()
		if err != nil {
			return nil, err
		}
		codes[i] = code
		if err := q.InsertRecoveryCode(ctx, sqlc.InsertRecoveryCodeParams{
			UserID: userID, CodeHash: hashRecoveryCode(code),
		}); err != nil {
			return nil, err
		}
	}
	return codes, nil
}

// generateRecoveryCode returns 10 random [a-z2-7] characters, displayed as
// "xxxxx-xxxxx".
func generateRecoveryCode() (string, error) {
	b := make([]byte, recoveryCodeLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, recoveryCodeLen)
	for i, v := range b {
		out[i] = recoveryCodeChars[int(v)%len(recoveryCodeChars)]
	}
	half := recoveryCodeLen / 2
	return string(out[:half]) + "-" + string(out[half:]), nil
}

// normalizeRecoveryCode lower-cases a recovery code and strips hyphens and
// whitespace, so it can be entered with or without the display formatting.
func normalizeRecoveryCode(code string) string {
	code = strings.ToLower(code)
	return strings.Map(func(r rune) rune {
		if r == '-' || unicode.IsSpace(r) {
			return -1
		}
		return r
	}, code)
}

func hashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(normalizeRecoveryCode(code)))
	return hex.EncodeToString(sum[:])
}
