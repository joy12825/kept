package document

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"kept/server/internal/i18n"
	"math"
	"strconv"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/privacy"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// TrashRetention is how long deleted documents stay in the trash.
const TrashRetention = 30 * 24 * time.Hour

var errNotInTrash = apperr.New(apperr.KindNotFound, "回收站里没有这份文档")

// Delete moves the selected documents to the trash and returns how many.
func (s *Service) Delete(ctx context.Context, scope tenant.Scope, sel Selection) (int, error) {
	ids, err := s.Resolve(ctx, scope, sel)
	if err != nil {
		return 0, err
	}
	inbox := false
	err = s.inTx(ctx, func(q *sqlc.Queries) error {
		now := s.opt.Now()
		for _, id := range ids {
			row, err := q.GetDocument(ctx, sqlc.GetDocumentParams{WorkspaceID: scope.WorkspaceID(), ID: id, Viewer: scope.Viewer()})
			if err != nil {
				return err
			}
			if err := privacy.New(s.db).CheckEdit(ctx, q, scope, row.Private == 1, row.PersonID); err != nil {
				return err
			}
			inbox = inbox || row.Inbox == 1
			if err := q.SetDocumentDeleted(ctx, sqlc.SetDocumentDeletedParams{
				DeletedAt: sql.NullInt64{Int64: now.UnixMilli(), Valid: true}, ModifiedAt: now.UnixMilli(), WorkspaceID: scope.WorkspaceID(), ID: id,
			}); err != nil {
				return err
			}
			if err := s.record(ctx, q, scope.WorkspaceID(), "delete", id, i18n.N("%s 删除了“%s”"), row.Title); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	s.notify(scope.WorkspaceID(), ids, inbox)
	return len(ids), nil
}

// Restore takes a document out of the trash.
func (s *Service) Restore(ctx context.Context, scope tenant.Scope, id int64) error {
	var inbox bool
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		row, err := s.trashed(ctx, q, scope, id)
		if err != nil {
			return err
		}
		inbox = row.Inbox == 1
		if err := q.SetDocumentDeleted(ctx, sqlc.SetDocumentDeletedParams{ModifiedAt: s.opt.Now().UnixMilli(), WorkspaceID: scope.WorkspaceID(), ID: id}); err != nil {
			return err
		}
		return s.record(ctx, q, scope.WorkspaceID(), "restore", id, i18n.N("%s 从回收站恢复了“%s”"), row.Title)
	})
	if err != nil {
		return err
	}
	s.notify(scope.WorkspaceID(), []int64{id}, inbox)
	return nil
}

func (s *Service) trashed(ctx context.Context, q *sqlc.Queries, scope tenant.Scope, id int64) (sqlc.Document, error) {
	row, err := q.GetDocumentAnyState(ctx, sqlc.GetDocumentAnyStateParams{WorkspaceID: scope.WorkspaceID(), ID: id, Viewer: scope.Viewer()})
	if errors.Is(err, sql.ErrNoRows) || err == nil && !row.DeletedAt.Valid {
		return sqlc.Document{}, errNotInTrash
	}
	if err != nil {
		return row, err
	}
	return row, privacy.New(s.db).CheckEdit(ctx, q, scope, row.Private == 1, row.PersonID)
}

// Trashed is a document in the trash.
type Trashed struct {
	Document
	DeletedAt time.Time
	DaysLeft  int // until it is purged; at least 0
}

// ListTrash returns the trash, most recently deleted first.
func (s *Service) ListTrash(ctx context.Context, scope tenant.Scope) ([]Trashed, error) {
	rows, err := s.q.ListTrash(ctx, sqlc.ListTrashParams{WorkspaceID: scope.WorkspaceID(), Viewer: scope.Viewer()})
	if err != nil {
		return nil, err
	}
	now := s.opt.Now()
	out := make([]Trashed, len(rows))
	for i, r := range rows {
		deleted := time.UnixMilli(r.DeletedAt.Int64)
		left := int(math.Ceil(deleted.Add(TrashRetention).Sub(now).Hours() / 24))
		out[i] = Trashed{Document: From(r), DeletedAt: deleted, DaysLeft: max(left, 0)}
	}
	return out, nil
}

// Purge deletes documents in the trash for good, with their files.
func (s *Service) Purge(ctx context.Context, scope tenant.Scope, ids []int64) error {
	var gone []sqlc.Document
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		for _, id := range ids {
			row, err := s.trashed(ctx, q, scope, id)
			if err != nil {
				return err
			}
			if err := q.DeleteLinksTo(ctx, sql.NullInt64{Int64: id, Valid: true}); err != nil {
				return err
			}
			if err := q.DeleteDocument(ctx, sqlc.DeleteDocumentParams{WorkspaceID: scope.WorkspaceID(), ID: id}); err != nil {
				return err
			}
			if err := s.record(ctx, q, scope.WorkspaceID(), "purge", id, i18n.N("%s 彻底删除了“%s”"), row.Title); err != nil {
				return err
			}
			gone = append(gone, row)
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Files go after the commit: a failed transaction must not lose them.
	// A file left behind by a crash here is reported by the integrity check.
	if s.opt.Files != nil {
		for _, r := range gone {
			_ = s.opt.Files.RemoveDocument(context.WithoutCancel(ctx), r.Storage, r.WorkspaceID, r.ID, r.OriginalExt)
		}
	}
	if s.opt.OnPurge != nil && len(gone) > 0 {
		ids := make([]int64, len(gone))
		for i, r := range gone {
			ids[i] = r.ID
		}
		s.opt.OnPurge(ids)
	}
	return nil
}

// EmptyTrash purges everything in the trash and returns how many.
func (s *Service) EmptyTrash(ctx context.Context, scope tenant.Scope) (int, error) {
	ids, err := s.q.ListTrashIDs(ctx, sqlc.ListTrashIDsParams{WorkspaceID: scope.WorkspaceID(), Viewer: scope.Viewer()})
	if err != nil {
		return 0, err
	}
	return len(ids), s.Purge(ctx, scope, ids)
}

// PurgeExpired purges, in every workspace, documents deleted before cutoff.
// It is run daily, attributed to the system.
func (s *Service) PurgeExpired(ctx context.Context, cutoff time.Time) (int64, error) {
	rows, err := s.q.ListExpiredTrash(ctx, sql.NullInt64{Int64: cutoff.UnixMilli(), Valid: true})
	if err != nil {
		return 0, err
	}
	byWS := map[int64][]int64{}
	for _, r := range rows {
		byWS[r.WorkspaceID] = append(byWS[r.WorkspaceID], r.ID)
	}
	var n int64
	for ws, ids := range byWS {
		if err := s.Purge(ctx, tenant.New(ws, tenant.RoleOwner), ids); err != nil {
			return n, err
		}
		n += int64(len(ids))
	}
	return n, nil
}

func (s *Service) record(ctx context.Context, q *sqlc.Queries, ws int64, verb string, id int64, format string, args ...any) error {
	return audit.Record(ctx, q, audit.Event{
		WorkspaceID: ws, Action: "document." + verb, TargetType: "document", TargetID: strconv.FormatInt(id, 10),
		Summary: fmt.Sprintf(format, append([]any{audit.ActorFrom(ctx).Name}, args...)...), At: s.opt.Now(),
	})
}
