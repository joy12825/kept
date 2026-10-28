package taxonomy

import (
	"context"
	"errors"
	"slices"

	"kept/server/internal/apperr"
	"kept/server/internal/tenant"
)

// Tidying the directory in bulk (v1.5.8, after paperless-ngx's bulk edit
// of objects): deleting several items at once, and merging by hand (the
// organizing agent's suggestions used Merge already).

const maxBulk = 500

var (
	errBulkEmpty   = apperr.New(apperr.KindInvalid, "请选择要处理的项目")
	errBulkTooMany = apperr.New(apperr.KindInvalid, "一次最多处理 500 个")
	errMergeChild  = apperr.New(apperr.KindInvalid, "不能把标签合并到它自己的子标签里")
)

// BulkDelete deletes the items of kind, each as Delete would (documents
// keep existing, without them). An item already gone (a child tag deleted
// with its parent) is skipped. It returns how many were deleted.
func (s *Service) BulkDelete(ctx context.Context, scope tenant.Scope, kind Kind, ids []int64) (int, error) {
	if len(ids) == 0 {
		return 0, errBulkEmpty
	}
	if len(ids) > maxBulk {
		return 0, errBulkTooMany
	}
	n := 0
	for _, id := range slices.Compact(slices.Sorted(slices.Values(ids))) {
		var err error
		if kind == Tag {
			err = s.DeleteTag(ctx, scope, id)
		} else {
			err = s.Delete(ctx, scope, kind, id)
		}
		switch {
		case errors.Is(err, ErrNotFound):
		case err != nil:
			return n, err
		default:
			n++
		}
	}
	return n, nil
}

// MergeInto is Merge for a person: keep must not be one of a merged tag's
// descendants (its children would be moved under itself).
func (s *Service) MergeInto(ctx context.Context, scope tenant.Scope, kind Kind, keep int64, merge []int64) (int, error) {
	if len(merge) > maxBulk {
		return 0, errBulkTooMany
	}
	if kind == Tag {
		for id, steps := keep, 0; id != 0 && steps < 100; steps++ {
			var parent *int64
			err := s.db.QueryRowContext(ctx, `SELECT parent_id FROM tags WHERE workspace_id = ? AND id = ?`, scope.WorkspaceID(), id).Scan(&parent)
			if err != nil || parent == nil {
				break
			}
			if slices.Contains(merge, *parent) {
				return 0, errMergeChild
			}
			id = *parent
		}
	}
	return s.Merge(ctx, scope, kind, keep, merge)
}
