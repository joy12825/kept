package privacy

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/sealbox"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// A person's keys (v1.8.1, package sealbox): the public key seals copies
// of their private documents on remote storages and in remote backups; the
// private key is kept only wrapped, by a recovery code and by the login
// password of the keeper who set it up (or made the current recovery code),
// and is needed only to restore from those copies.

var (
	errKeyExists    = apperr.New(apperr.KindConflict, "这位家人已经有钥匙了；忘了恢复码可以重新生成")
	errNoKey        = apperr.WithCode(apperr.KindNotFound, "no_key", "这位家人还没有设钥匙")
	errKeySecret    = apperr.New(apperr.KindInvalid, "请输入登录密码或现在的恢复码")
	errWrongSecret  = apperr.WithCode(apperr.KindInvalid, "wrong_secret", "密码或恢复码不对")
	errConsentOwner = apperr.New(apperr.KindForbidden, "只有这位家人本人（没有本人时是照看人）可以决定")
)

// KeyInfo is what the viewer sees of a person's keys.
type KeyInfo struct {
	Has        bool
	CreatedAt  time.Time
	RecoveryAt time.Time
	// MyPassword: the private key is also wrapped by the viewer's password.
	MyPassword bool
	// CloudAI: the person lets cloud models read their private documents.
	CloudAI bool
}

// Keys returns the key state of every person in the workspace, keyed by
// person (persons without keys are left out of Has but carry CloudAI).
func (s *Service) Keys(ctx context.Context, scope tenant.Scope) (map[int64]KeyInfo, error) {
	rows, err := s.q.ListPersonKeys(ctx, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	out := map[int64]KeyInfo{}
	for _, r := range rows {
		info := KeyInfo{Has: true, CreatedAt: time.UnixMilli(r.CreatedAt), RecoveryAt: time.UnixMilli(r.RecoveryAt)}
		if me := scope.User(); me > 0 {
			_, err := s.q.GetPersonKeyWrap(ctx, sqlc.GetPersonKeyWrapParams{PersonID: r.PersonID, UserID: me})
			info.MyPassword = err == nil
		}
		out[r.PersonID] = info
	}
	consents, err := s.db.QueryContext(ctx, `SELECT a.person_id FROM person_ai a JOIN persons p ON p.id = a.person_id
		WHERE p.workspace_id = ? AND a.cloud_ok = 1`, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	defer consents.Close()
	for consents.Next() {
		var id int64
		if err := consents.Scan(&id); err != nil {
			return nil, err
		}
		info := out[id]
		info.CloudAI = true
		out[id] = info
	}
	return out, consents.Err()
}

func (s *Service) mustHold(ctx context.Context, q *sqlc.Queries, scope tenant.Scope, personID int64) error {
	st, err := s.person(ctx, q, scope, personID)
	if err != nil {
		return err
	}
	if !st.isHolder(scope.User()) {
		return errNotHolder
	}
	return nil
}

func (s *Service) wrapFor(ctx context.Context, q *sqlc.Queries, personID, userID int64, password string, private []byte) error {
	if password == "" || userID <= 0 {
		return nil
	}
	w, err := sealbox.Wrap(password, private)
	if err != nil {
		return err
	}
	return q.UpsertPersonKeyWrap(ctx, sqlc.UpsertPersonKeyWrapParams{PersonID: personID, UserID: userID, Wrapped: w, CreatedAt: s.now().UnixMilli()})
}

// openWith opens a person's private key with the viewer's password or a
// recovery code.
func (s *Service) openWith(ctx context.Context, q *sqlc.Queries, key sqlc.PersonKey, userID int64, password, code string) ([]byte, error) {
	var private []byte
	var err error
	switch {
	case code != "":
		private, err = sealbox.Unwrap(sealbox.NormalizeCode(code), key.Recovery)
	case password != "":
		w, werr := q.GetPersonKeyWrap(ctx, sqlc.GetPersonKeyWrapParams{PersonID: key.PersonID, UserID: userID})
		if errors.Is(werr, sql.ErrNoRows) {
			return nil, errWrongSecret
		} else if werr != nil {
			return nil, werr
		}
		private, err = sealbox.Unwrap(password, w)
	default:
		return nil, errKeySecret
	}
	if errors.Is(err, sealbox.ErrWrongCode) {
		return nil, errWrongSecret
	}
	if err != nil {
		return nil, err
	}
	pub, err := sealbox.PublicOf(private)
	if err != nil || encodeKey(pub) != key.PublicKey {
		return nil, errWrongSecret
	}
	return private, nil
}

// NewRecoveryCode replaces a person's recovery code, opening the key with
// the holder's password or the current code; the holder's password wraps
// it again when given.
func (s *Service) NewRecoveryCode(ctx context.Context, scope tenant.Scope, personID int64, password, code string) (string, error) {
	var fresh string
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		if err := s.mustHold(ctx, q, scope, personID); err != nil {
			return err
		}
		key, err := q.GetPersonKey(ctx, personID)
		if errors.Is(err, sql.ErrNoRows) {
			return errNoKey
		} else if err != nil {
			return err
		}
		private, err := s.openWith(ctx, q, key, scope.User(), password, code)
		if err != nil {
			return err
		}
		if fresh, err = sealbox.NewRecoveryCode(); err != nil {
			return err
		}
		recovery, err := sealbox.Wrap(fresh, private)
		if err != nil {
			return err
		}
		if err := q.SetPersonKeyRecovery(ctx, sqlc.SetPersonKeyRecoveryParams{Recovery: recovery, RecoveryAt: s.now().UnixMilli(), PersonID: personID}); err != nil {
			return err
		}
		return s.wrapFor(ctx, q, personID, scope.User(), password, private)
	})
	return fresh, err
}

// CheckRecoveryCode reports whether a recovery code opens a person's key.
func (s *Service) CheckRecoveryCode(ctx context.Context, scope tenant.Scope, personID int64, code string) (bool, error) {
	if err := s.mustHold(ctx, s.q, scope, personID); err != nil {
		return false, err
	}
	key, err := s.q.GetPersonKey(ctx, personID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, errNoKey
	} else if err != nil {
		return false, err
	}
	if code == "" {
		return false, errKeySecret
	}
	_, err = s.openWith(ctx, s.q, key, 0, "", code)
	if errors.Is(err, errWrongSecret) {
		return false, nil
	}
	return err == nil, err
}

// PublicKey is a person's public key; ok is false without one.
func PublicKey(ctx context.Context, q *sqlc.Queries, personID int64) ([]byte, bool, error) {
	key, err := q.GetPersonKey(ctx, personID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	b, err := decodeKey(key.PublicKey)
	return b, err == nil, err
}

// OpenWithRecoveryCode opens a person's private key by its recovery code
// (restoring from sealed copies).
func OpenWithRecoveryCode(ctx context.Context, q *sqlc.Queries, personID int64, code string) ([]byte, error) {
	key, err := q.GetPersonKey(ctx, personID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNoKey
	} else if err != nil {
		return nil, err
	}
	private, err := sealbox.Unwrap(sealbox.NormalizeCode(code), key.Recovery)
	if errors.Is(err, sealbox.ErrWrongCode) {
		return nil, errWrongSecret
	}
	if err != nil {
		return nil, err
	}
	if pub, _ := sealbox.PublicOf(private); encodeKey(pub) != key.PublicKey {
		return nil, errWrongSecret
	}
	return private, nil
}

// PasswordChanged wraps a user's keys again with their new password; ones
// the old password does not open (should not happen) are dropped.
func PasswordChanged(ctx context.Context, q *sqlc.Queries, userID int64, old, next string, now time.Time) error {
	wraps, err := q.ListUserKeyWraps(ctx, userID)
	if err != nil {
		return err
	}
	for _, w := range wraps {
		private, err := sealbox.Unwrap(old, w.Wrapped)
		if err != nil {
			continue
		}
		again, err := sealbox.Wrap(next, private)
		if err != nil {
			return err
		}
		if err := q.UpsertPersonKeyWrap(ctx, sqlc.UpsertPersonKeyWrapParams{PersonID: w.PersonID, UserID: userID, Wrapped: again, CreatedAt: now.UnixMilli()}); err != nil {
			return err
		}
	}
	return nil
}

// PasswordReset drops a user's password-wrapped keys: an administrator
// set a password the old wraps do not know. The recovery codes still work.
func PasswordReset(ctx context.Context, q *sqlc.Queries, userID int64) error {
	return q.DeleteUserKeyWraps(ctx, userID)
}

// PersonCloudAI reports whether a person lets cloud models read their
// private documents.
func PersonCloudAI(ctx context.Context, q *sqlc.Queries, personID int64) (bool, error) {
	v, err := q.GetPersonAI(ctx, personID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return v == 1, err
}

func encodeKey(b []byte) string { return base64.RawStdEncoding.EncodeToString(b) }

func decodeKey(s string) ([]byte, error) { return base64.RawStdEncoding.DecodeString(s) }

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
