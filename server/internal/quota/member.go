package quota

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"kept/server/internal/apperr"
)

// Member quotas (v1.7.11): a workspace administrator may limit how much one
// member uploads into the workspace, within the workspace's own quota.
// What a member uploaded is the recorded size of the documents they added
// (documents.added_by) and of the earlier versions they uploaded; files
// that came by mail, the consume directory, a remote inbox or an upload
// link belong to nobody and count only for the workspace.

// MemberUsage is a member's uploads and limit.
type MemberUsage struct {
	Used  int64
	Limit int64 // 0: none of their own
}

// Members returns the usage of every member of a workspace.
func (s *Service) Members(ctx context.Context, ws int64) (map[int64]MemberUsage, error) {
	out := map[int64]MemberUsage{}
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, upload_quota FROM workspace_members WHERE workspace_id = ?`, ws)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var q sql.NullInt64
		if err := rows.Scan(&id, &q); err != nil {
			rows.Close()
			return nil, err
		}
		out[id] = MemberUsage{Limit: q.Int64}
	}
	rows.Close()
	used, err := s.memberUsed(ctx, ws, 0)
	if err != nil {
		return nil, err
	}
	for id, n := range used {
		if m, ok := out[id]; ok {
			m.Used = n
			out[id] = m
		}
	}
	return out, nil
}

// memberUsed adds up the uploads by member (user 0: every member).
func (s *Service) memberUsed(ctx context.Context, ws, user int64) (map[int64]int64, error) {
	out := map[int64]int64{}
	q := `SELECT added_by, CAST(COALESCE(SUM(COALESCE(original_size, 0) + COALESCE(derived_size, 0)), 0) AS BIGINT)
		FROM documents WHERE workspace_id = ? AND added_by IS NOT NULL`
	args := []any{ws}
	if user != 0 {
		q += ` AND added_by = ?`
		args = append(args, user)
	}
	rows, err := s.db.QueryContext(ctx, q+` GROUP BY added_by`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, n int64
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return nil, err
		}
		out[id] += n
	}
	rows.Close()
	q = `SELECT created_by, CAST(COALESCE(SUM(size), 0) AS BIGINT) FROM document_versions
		WHERE workspace_id = ? AND current = 0 AND created_by IS NOT NULL`
	if user != 0 {
		q += ` AND created_by = ?`
	}
	rows, err = s.db.QueryContext(ctx, q+` GROUP BY created_by`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, n int64
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] += n
	}
	return out, rows.Err()
}

// memberPending adds up a member's uploads waiting to be processed.
func (s *Service) memberPending(ctx context.Context, ws, user int64) (int64, error) {
	rows, err := s.q.PendingUploadPayloads(ctx, sql.NullInt64{Int64: ws, Valid: true})
	if err != nil {
		return 0, err
	}
	var n int64
	for _, raw := range rows {
		var p struct {
			Size  int64                  `json:"size"`
			Actor struct{ UserID int64 } `json:"actor"`
		}
		if json.Unmarshal([]byte(raw), &p) == nil && p.Actor.UserID == user {
			n += Estimate(p.Size)
		}
	}
	return n, nil
}

// checkMember refuses need more bytes from user over their own limit.
func (s *Service) checkMember(ctx context.Context, ws, user, need int64) error {
	var limit sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT upload_quota FROM workspace_members WHERE workspace_id = ? AND user_id = ?`, ws, user).Scan(&limit)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (!limit.Valid || limit.Int64 == 0)) {
		return nil
	}
	if err != nil {
		return err
	}
	used, err := s.memberUsed(ctx, ws, user)
	if err != nil {
		return err
	}
	waiting, err := s.memberPending(ctx, ws, user)
	if err != nil {
		return err
	}
	if used[user]+waiting+need > limit.Int64 {
		return apperr.WithCode(apperr.KindConflict, "member_quota_exceeded",
			fmt.Sprintf("你在这个空间的上传额度用完了（已上传 %s / 额度 %s），请空间管理员调整", Human(used[user]), Human(limit.Int64)))
	}
	return nil
}
