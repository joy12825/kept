package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"kept/server/internal/audit"
	"kept/server/internal/i18n"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// PairingCodeTTL is how long a pairing code shown on the web stays valid.
const PairingCodeTTL = 10 * time.Minute

var (
	ErrPairingNotAllowed = newErr(KindForbidden, "只看的成员不能用这个版本的 App 连接：它只能上传文档。请更新 App 后重新扫码，就能在手机上查看文档")
	ErrPairingCode       = newErr(KindInvalid, "配对码不正确或已过期，请在网页端重新生成")
	ErrPairingLimited    = newErr(KindRateLimited, "配对码错误次数过多，请稍后再试")
	ErrDeviceNotFound    = newErr(KindNotFound, "找不到这台设备，它可能已经断开")
)

// PairingCode is a freshly generated code; only its hash is stored.
type PairingCode struct {
	Code      string // 8 digits
	ExpiresAt time.Time
}

// Device is a paired phone.
type Device struct {
	ID            int64
	Name          string
	WorkspaceID   int64
	WorkspaceName string
	PairedAt      time.Time
	LastSeenAt    *time.Time
	Access        string // AccessUpload or AccessFull
}

// What a paired phone may do (App 2.0): AccessUpload only uploads (App
// 1.x); AccessFull acts with the user's role in the workspace, short of
// account security and administration (enforced by the API).
const (
	AccessUpload = "upload"
	AccessFull   = "full"
)

// Paired is what a phone receives when pairing succeeds.
type Paired struct {
	Token         string // shown to the phone once
	DeviceID      int64
	WorkspaceID   int64
	WorkspaceName string
	UserName      string
	Access        string
}

// CreatePairingCode issues a code that pairs a phone with the caller's
// current workspace. Earlier unused codes of the same user and workspace
// stop working. Any member can pair; a viewer's phone needs full access
// (App 2.0) to be of use, since it cannot upload (Pair).
func (s *Service) CreatePairingCode(ctx context.Context, userID int64, scope tenant.Scope) (PairingCode, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(100_000_000))
	if err != nil {
		return PairingCode{}, err
	}
	code := fmt.Sprintf("%08d", n.Int64())
	now := s.now()
	expires := now.Add(PairingCodeTTL)
	err = s.inTx(ctx, func(q *sqlc.Queries) error {
		if err := q.DeleteStalePairingCodes(ctx, sqlc.DeleteStalePairingCodesParams{
			Before: now.Add(-24 * time.Hour).UnixMilli(), UserID: userID, WorkspaceID: scope.WorkspaceID(),
		}); err != nil {
			return err
		}
		return q.InsertPairingCode(ctx, sqlc.InsertPairingCodeParams{
			UserID: userID, WorkspaceID: scope.WorkspaceID(), CodeHash: hashToken(code),
			CreatedAt: now.UnixMilli(), ExpiresAt: expires.UnixMilli(),
		})
	})
	if err != nil {
		return PairingCode{}, err
	}
	return PairingCode{Code: code, ExpiresAt: expires}, nil
}

// NormalizePairingCode drops the spaces and dashes people type ("1234 5678").
func NormalizePairingCode(code string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' {
			return -1
		}
		return r
	}, strings.TrimSpace(code))
}

// Pair exchanges a pairing code for a device token. The code works once;
// wrong codes count against the client IP.
func (s *Service) Pair(ctx context.Context, code, deviceName, access string, client ClientInfo) (Paired, error) {
	if access != AccessFull {
		access = AccessUpload
	}
	key := client.IP
	if !s.pairLimit.Allow(key) {
		return Paired{}, ErrPairingLimited.With(map[string]any{"retryAfterSeconds": int(s.pairLimit.RetryAfter(key) / time.Second)})
	}
	deviceName = strings.TrimSpace(deviceName)
	if deviceName == "" {
		deviceName = i18n.T(ctx, i18n.N("手机"))
	}
	if r := []rune(deviceName); len(r) > 64 {
		deviceName = string(r[:64])
	}
	now := s.now()
	var out Paired
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		pc, err := q.FindPairingCode(ctx, sqlc.FindPairingCodeParams{CodeHash: hashToken(NormalizePairingCode(code)), Now: now.UnixMilli()})
		if errors.Is(err, sql.ErrNoRows) {
			return ErrPairingCode
		}
		if err != nil {
			return err
		}
		if access == AccessUpload && !tenant.New(pc.WorkspaceID, tenant.Role(pc.Role)).CanWrite() {
			return ErrPairingNotAllowed // an upload-only phone of a viewer could do nothing
		}
		if n, err := q.UsePairingCode(ctx, sqlc.UsePairingCodeParams{Now: sql.NullInt64{Int64: now.UnixMilli(), Valid: true}, ID: pc.ID}); err != nil {
			return err
		} else if n == 0 {
			return ErrPairingCode
		}
		plain, err := newTokenString()
		if err != nil {
			return err
		}
		id, err := q.InsertDeviceToken(ctx, sqlc.InsertDeviceTokenParams{
			UserID: pc.UserID, DeviceName: deviceName, TokenHash: hashToken(plain), Prefix: plain[:12],
			Now: now.UnixMilli(), WorkspaceID: sql.NullInt64{Int64: pc.WorkspaceID, Valid: true}, Access: access,
		})
		if err != nil {
			return err
		}
		out = Paired{Token: plain, DeviceID: id, WorkspaceID: pc.WorkspaceID, WorkspaceName: pc.WorkspaceName, UserName: pc.DisplayName, Access: access}
		return audit.Record(ctx, q, audit.Event{
			WorkspaceID: pc.WorkspaceID, Action: "device.pair", TargetType: "device", TargetID: strconv.FormatInt(id, 10),
			Summary: fmt.Sprintf(i18n.N("%s 连接了手机“%s”"), pc.DisplayName, deviceName),
			Actor:   &audit.Actor{UserID: pc.UserID, Name: pc.DisplayName, Via: audit.ViaDevice, RequestID: audit.ActorFrom(ctx).RequestID},
			At:      now,
		})
	})
	if errors.Is(err, ErrPairingCode) {
		s.pairLimit.Fail(key)
	}
	if err != nil {
		return Paired{}, err
	}
	s.pairLimit.Reset(key)
	return out, nil
}

// ListDevices returns the user's paired phones, newest first.
func (s *Service) ListDevices(ctx context.Context, userID int64) ([]Device, error) {
	rows, err := s.q.ListDevices(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Device, len(rows))
	for i, r := range rows {
		d := Device{ID: r.ID, Name: r.DeviceName, WorkspaceID: r.WorkspaceID.Int64, WorkspaceName: r.WorkspaceName, PairedAt: time.UnixMilli(r.PairedAt.Int64), Access: r.Access}
		if r.LastUsedAt.Valid {
			at := time.UnixMilli(r.LastUsedAt.Int64)
			d.LastSeenAt = &at
		}
		out[i] = d
	}
	return out, nil
}

// RevokeDevice disconnects one of the user's phones; its token stops
// working at once. Another user's device is reported as not found.
func (s *Service) RevokeDevice(ctx context.Context, userID, deviceID int64) error {
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		d, err := q.GetDevice(ctx, sqlc.GetDeviceParams{ID: deviceID, UserID: userID})
		if errors.Is(err, sql.ErrNoRows) {
			return ErrDeviceNotFound
		}
		if err != nil {
			return err
		}
		if _, err := q.DeleteDevice(ctx, sqlc.DeleteDeviceParams{ID: deviceID, UserID: userID}); err != nil {
			return err
		}
		actor := audit.ActorFrom(ctx)
		return audit.Record(ctx, q, audit.Event{
			WorkspaceID: d.WorkspaceID.Int64, Action: "device.revoke", TargetType: "device", TargetID: strconv.FormatInt(deviceID, 10),
			Summary: fmt.Sprintf(i18n.N("%s 断开了手机“%s”"), actor.Name, d.DeviceName), At: s.now(),
		})
	})
}

// newTokenString makes a token in the same format as personal API tokens.
func newTokenString() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "kept_" + strings.ToLower(tokenEncoding.EncodeToString(b)), nil
}
