// Package transfer moves documents to another workspace (v1.3.8): most
// often into the caller's personal space, so a document only they should
// see leaves a shared space. A moved document keeps its ID; its files,
// notes, versions, book data and the caller's reading go with it. Types,
// correspondents, tags and custom fields are matched by name in the target
// and created there when missing. What belongs to the source workspace
// stays and lets go of the document: share links, shared collections, book
// lists, drawer alerts, duplicate pairs, AI proposals and other members'
// reading state.
package transfer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/document"
	"kept/server/internal/events"
	"kept/server/internal/files"
	"kept/server/internal/i18n"
	"kept/server/internal/privacy"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/taxonomy"
	"kept/server/internal/tenant"
)

var (
	errPrivateMove = apperr.New(apperr.KindForbidden, "这是家人的私密文档：只有本人（没有本人时是照看人）可以把它移到别的空间")
	ErrNotFound    = document.ErrNotFound
	errSame        = apperr.New(apperr.KindInvalid, "文档已经在这个空间里了")
	errTarget      = apperr.WithCode(apperr.KindForbidden, "target", "只能移到你是成员或管理员的空间")
	errBusy        = apperr.WithCode(apperr.KindConflict, "busy", "这份文档正在处理，等处理完再移")
	errTooMany     = apperr.New(apperr.KindInvalid, fmt.Sprintf("一次最多移动 %d 份文档", MaxBatch))
)

// MaxBatch bounds one request.
const MaxBatch = 200

// Placer files a document by the target's rules.
type Placer interface {
	PlaceDocument(ctx context.Context, db sqlc.DBTX, ws, docID int64) error
}

// Quota refuses a move that would overfill the target.
type Quota interface {
	Check(ctx context.Context, ws, add int64) error
	MarkDirty(ws int64)
}

type Options struct {
	Documents *document.Service
	Taxonomy  *taxonomy.Service
	Files     *files.Store
	Placer    Placer
	Quota     Quota
	Bus       *events.Bus
	Now       func() time.Time
}

type Service struct {
	db  *sql.DB
	opt Options
}

func NewService(db *sql.DB, opt Options) *Service {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	return &Service{db: db, opt: opt}
}

// Result of moving one document.
type Result struct {
	DocumentID int64
	// ASNCleared: the target already used the document's archive serial
	// number, so the moved document has none.
	ASNCleared bool
	// Created lists what had to be created in the target (“标签“医疗””).
	Created []string
}

// Target is where documents go: a workspace the caller can write in.
type Target struct {
	ID   int64
	Name string
}

// CheckTarget resolves target for userID, who must be a member (editor)
// or owner there; it must differ from the scope's workspace.
func (s *Service) CheckTarget(ctx context.Context, scope tenant.Scope, userID, target int64) (Target, error) {
	if target == scope.WorkspaceID() {
		return Target{}, errSame
	}
	q := sqlc.New(s.db)
	m, err := q.GetWorkspaceMember(ctx, sqlc.GetWorkspaceMemberParams{WorkspaceID: target, UserID: userID})
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !tenant.Role(m.Role).AtLeast(tenant.RoleEditor)) {
		return Target{}, errTarget
	}
	if err != nil {
		return Target{}, err
	}
	name, err := q.GetLiveWorkspaceName(ctx, target)
	if err != nil {
		return Target{}, errTarget
	}
	return Target{ID: target, Name: name}, nil
}

// Move moves documents to target (already checked with CheckTarget), one
// at a time; it stops at the first failure and returns what was moved.
func (s *Service) Move(ctx context.Context, scope tenant.Scope, userID int64, ids []int64, target Target) ([]Result, error) {
	if len(ids) > MaxBatch {
		return nil, errTooMany
	}
	var out []Result
	for _, id := range ids {
		r, err := s.moveOne(ctx, scope, userID, id, target)
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *Service) moveOne(ctx context.Context, scope tenant.Scope, userID, id int64, target Target) (Result, error) {
	src := scope.WorkspaceID()
	d, err := s.opt.Documents.GetDetail(ctx, scope, id)
	if err != nil {
		return Result{}, err
	}
	res := Result{DocumentID: id}
	if d.Private {
		// only the person (or a guardian) takes a private document out of
		// the family's space (v1.8.0); it is no longer private there
		ok, err := privacy.New(s.db).IsHolder(ctx, nil, scope, d.PersonID)
		if err != nil {
			return res, err
		}
		if !ok {
			return res, errPrivateMove
		}
	}
	if err := s.checkFree(ctx, d, target, userID); err != nil {
		return res, err
	}
	if s.opt.Quota != nil {
		if err := s.opt.Quota.Check(ctx, target.ID, s.size(ctx, src, d)); err != nil {
			return res, err
		}
	}
	m, err := s.mapTaxonomy(ctx, scope, d, target, &res)
	if err != nil {
		return res, err
	}
	srcName, err := sqlc.New(s.db).GetLiveWorkspaceName(ctx, src)
	if err != nil {
		return res, err
	}

	// Files first (renames within the document's storage, which stays the
	// same); put back if the database change fails.
	undo, err := s.moveFiles(ctx, src, target.ID, d)
	if err != nil {
		undo()
		return res, err
	}
	err = s.inTx(ctx, func(tx *sql.Tx, q *sqlc.Queries) error {
		cleared, err := s.moveRows(ctx, tx, d, target.ID, m)
		res.ASNCleared = cleared
		if err != nil {
			return err
		}
		if err := audit.Record(ctx, q, audit.Event{WorkspaceID: src, Action: "document.move", TargetType: "document", TargetID: strconv.FormatInt(id, 10),
			Summary: fmt.Sprintf(i18n.N("%s 把“%s”移到了空间“%s”"), audit.ActorFrom(ctx).Name, d.Title, target.Name), At: s.opt.Now()}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Event{WorkspaceID: target.ID, Action: "document.move", TargetType: "document", TargetID: strconv.FormatInt(id, 10),
			Summary: fmt.Sprintf(i18n.N("%s 把“%s”从空间“%s”移了进来"), audit.ActorFrom(ctx).Name, d.Title, srcName), At: s.opt.Now()})
	})
	if err != nil {
		undo()
		return res, err
	}
	if s.opt.Quota != nil {
		s.opt.Quota.MarkDirty(src)
		s.opt.Quota.MarkDirty(target.ID)
	}
	for _, ws := range []int64{src, target.ID} {
		s.publish(events.Event{Type: events.DocumentUpdated, WorkspaceID: ws, Data: map[string]any{"documentId": id}})
		if d.Inbox {
			s.publish(events.Event{Type: events.InboxChanged, WorkspaceID: ws})
		}
	}
	return res, nil
}

// checkFree refuses a document that is being processed, or whose file the
// target already has (a workspace keeps one document per file).
func (s *Service) checkFree(ctx context.Context, d document.Detail, target Target, userID int64) error {
	var busy int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE document_id = ? AND status IN ('queued', 'running')`, d.ID).Scan(&busy); err != nil {
		return err
	}
	if busy > 0 {
		return errBusy
	}
	var title string
	var deleted sql.NullInt64
	var visible bool
	vis, vargs := privacy.Visible("d", tenant.ForUser(target.ID, tenant.RoleViewer, userID))
	err := s.db.QueryRowContext(ctx, `SELECT d.title, d.deleted_at, CASE WHEN `+vis+` THEN 1 ELSE 0 END FROM documents d WHERE d.workspace_id = ? AND d.checksum = ?`,
		append(vargs, target.ID, d.Checksum)...).Scan(&title, &deleted, &visible)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !visible { // a family member's private document there: not named
		return apperr.WithCode(apperr.KindConflict, "duplicate", fmt.Sprintf("空间“%s”里已经有同一个文件", target.Name))
	}
	if deleted.Valid {
		return apperr.WithCode(apperr.KindConflict, "duplicate", fmt.Sprintf("空间“%s”里已经有同一个文件：《%s》（在回收站中）", target.Name, title))
	}
	return apperr.WithCode(apperr.KindConflict, "duplicate", fmt.Sprintf("空间“%s”里已经有同一个文件：《%s》", target.Name, title))
}

func (s *Service) size(ctx context.Context, ws int64, d document.Detail) int64 {
	var n int64
	stored, local := s.paths(ctx, ws, d)
	for _, r := range stored {
		if st, err := s.opt.Files.Stat(ctx, d.Storage, r); err == nil {
			n += st.Size
		}
	}
	for _, p := range local {
		if fi, err := os.Stat(p); err == nil {
			n += fi.Size()
		}
	}
	return n
}

// paths lists a document's files in a workspace (missing ones included):
// those in its storage, and its thumbnail and cover.
func (s *Service) paths(ctx context.Context, ws int64, d document.Detail) (stored []files.Ref, local []string) {
	f := s.opt.Files
	stored = []files.Ref{files.Original(ws, d.ID, d.OriginalExt), files.Archive(ws, d.ID), files.Base(ws, d.ID)}
	earlier, _ := f.Versions(ctx, d.Storage, ws, d.ID)
	return append(stored, earlier...), []string{f.ThumbnailPath(ws, d.ID), f.CoverPath(ws, d.ID)}
}

// moveFiles renames the document's files into the target's folders; the
// returned func puts back what was moved.
func (s *Service) moveFiles(ctx context.Context, src, dst int64, d document.Detail) (undo func(), err error) {
	f := s.opt.Files
	stored, local := s.paths(ctx, src, d)
	var renamed [][2]files.Ref
	var moved [][2]string
	undo = func() {
		bctx := context.WithoutCancel(ctx)
		for i := len(moved) - 1; i >= 0; i-- {
			_ = os.Rename(moved[i][1], moved[i][0])
		}
		for i := len(renamed) - 1; i >= 0; i-- {
			_ = f.Rename(bctx, d.Storage, renamed[i][1], renamed[i][0])
		}
	}
	for _, r := range stored {
		if !f.Exists(ctx, d.Storage, r) {
			continue
		}
		to := r // earlier versions keep their names in the versions folder
		to.WS = dst
		if err := f.Rename(ctx, d.Storage, r, to); err != nil {
			return undo, err
		}
		renamed = append(renamed, [2]files.Ref{r, to})
	}
	_, toLocal := s.paths(ctx, dst, d)
	for i, p := range local {
		if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(toLocal[i]), 0o750); err != nil {
			return undo, err
		}
		if err := os.Rename(p, toLocal[i]); err != nil {
			return undo, err
		}
		moved = append(moved, [2]string{p, toLocal[i]})
	}
	return undo, nil
}

func (s *Service) inTx(ctx context.Context, fn func(*sql.Tx, *sqlc.Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx, sqlc.New(tx)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) publish(e events.Event) {
	if s.opt.Bus != nil {
		e.At = s.opt.Now()
		s.opt.Bus.Publish(e)
	}
}

// joinIDs renders IDs for an IN list.
func joinIDs(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}
