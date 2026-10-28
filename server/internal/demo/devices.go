package demo

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"time"

	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// demoPhone is the paired phone shown in 已连接的设备 (prototype: 小王的 iPhone).
const demoPhone = "小王的 iPhone"

// loadDevices pairs a phone with the workspace and marks the newest inbox
// documents as scanned with it. The phone's token is random and thrown
// away: it is there to be seen and disconnected, not used.
func loadDevices(ctx context.Context, d Deps, userID int64, scope tenant.Scope) (int, error) {
	q := sqlc.New(d.DB)
	ds, err := q.ListDevices(ctx, userID)
	if err != nil {
		return 0, err
	}
	added := 0
	exists := false
	for _, dv := range ds {
		if dv.DeviceName == demoPhone && dv.WorkspaceID.Int64 == scope.WorkspaceID() {
			exists = true
		}
	}
	if !exists {
		b := make([]byte, 20)
		if _, err := rand.Read(b); err != nil {
			return 0, err
		}
		sum := sha256.Sum256(b)
		now := time.Now()
		id, err := q.InsertDeviceToken(ctx, sqlc.InsertDeviceTokenParams{
			UserID: userID, DeviceName: demoPhone, TokenHash: hex.EncodeToString(sum[:]), Prefix: "kept_demo",
			Now: now.Add(-20 * 24 * time.Hour).UnixMilli(), WorkspaceID: sql.NullInt64{Int64: scope.WorkspaceID(), Valid: true},
		})
		if err != nil {
			return 0, err
		}
		if err := q.TouchAPIToken(ctx, sqlc.TouchAPITokenParams{Now: sql.NullInt64{Int64: now.Add(-2 * time.Hour).UnixMilli(), Valid: true}, ID: id}); err != nil {
			return 0, err
		}
		added = 1
	}
	_, err = d.DB.ExecContext(ctx, `UPDATE documents SET source = 'mobile'
		WHERE id IN (SELECT id FROM documents WHERE workspace_id = ? AND inbox = 1 AND deleted_at IS NULL ORDER BY added_at DESC, id DESC LIMIT 3)`,
		scope.WorkspaceID())
	return added, err
}
