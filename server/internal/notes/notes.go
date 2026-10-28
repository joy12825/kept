// Package notes keeps the notes people add to documents (备注).
package notes

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"kept/server/internal/i18n"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/document"
	"kept/server/internal/events"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

const maxLen = 2000

var (
	errBody      = apperr.New(apperr.KindInvalid, "备注不能为空，最多 2000 个字")
	errNotFound  = apperr.New(apperr.KindNotFound, "找不到这条备注")
	errNotAuthor = apperr.New(apperr.KindForbidden, "只能删除自己的备注；空间管理员可以删除任何备注")
)

// Note is one note on a document.
type Note struct {
	ID         int64
	DocumentID int64
	Body       string
	AuthorID   int64  // 0 once the account is gone
	AuthorName string // "" once the account is gone
	CreatedAt  time.Time
}

type Service struct {
	db  *sql.DB
	q   *sqlc.Queries
	bus *events.Bus
	now func() time.Time
}

func NewService(db *sql.DB, bus *events.Bus) *Service {
	return &Service{db: db, q: sqlc.New(db), bus: bus, now: time.Now}
}

// document returns the title of a document outside the trash.
func (s *Service) document(ctx context.Context, q *sqlc.Queries, scope tenant.Scope, id int64) (string, error) {
	d, err := q.GetDocument(ctx, sqlc.GetDocumentParams{WorkspaceID: scope.WorkspaceID(), ID: id, Viewer: scope.Viewer()})
	if errors.Is(err, sql.ErrNoRows) {
		return "", document.ErrNotFound
	}
	return d.Title, err
}

// List returns a document's notes, newest first.
func (s *Service) List(ctx context.Context, scope tenant.Scope, docID int64) ([]Note, error) {
	if _, err := s.document(ctx, s.q, scope, docID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListNotes(ctx, sqlc.ListNotesParams{WorkspaceID: scope.WorkspaceID(), DocumentID: docID})
	if err != nil {
		return nil, err
	}
	out := make([]Note, len(rows))
	for i, r := range rows {
		out[i] = Note{ID: r.ID, DocumentID: r.DocumentID, Body: r.Body, AuthorID: r.UserID.Int64, AuthorName: r.AuthorName, CreatedAt: time.UnixMilli(r.CreatedAt)}
	}
	return out, nil
}

// Create adds a note by the caller.
func (s *Service) Create(ctx context.Context, scope tenant.Scope, docID int64, body string) (Note, error) {
	body = strings.TrimSpace(body)
	if n := utf8.RuneCountInString(body); n < 1 || n > maxLen {
		return Note{}, errBody
	}
	actor := audit.ActorFrom(ctx)
	now := s.now()
	var id int64
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		title, err := s.document(ctx, q, scope, docID)
		if err != nil {
			return err
		}
		id, err = q.InsertNote(ctx, sqlc.InsertNoteParams{
			WorkspaceID: scope.WorkspaceID(), DocumentID: docID, UserID: sql.NullInt64{Int64: actor.UserID, Valid: actor.UserID != 0},
			Body: body, CreatedAt: now.UnixMilli(),
		})
		if err != nil {
			return err
		}
		if err := q.BumpIndexRev(ctx, sqlc.BumpIndexRevParams{WorkspaceID: scope.WorkspaceID(), ID: docID}); err != nil {
			return err
		}
		return s.record(ctx, q, scope, "note.create", docID, i18n.N("%s 给“%s”添加了备注"), title)
	})
	if err != nil {
		return Note{}, err
	}
	s.changed(scope, docID)
	return Note{ID: id, DocumentID: docID, Body: body, AuthorID: actor.UserID, AuthorName: actor.Name, CreatedAt: now}, nil
}

// Delete removes a note: the author's own, or any note for a workspace
// owner.
func (s *Service) Delete(ctx context.Context, scope tenant.Scope, docID, noteID int64) error {
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		title, err := s.document(ctx, q, scope, docID)
		if err != nil {
			return err
		}
		n, err := q.GetNote(ctx, sqlc.GetNoteParams{WorkspaceID: scope.WorkspaceID(), DocumentID: docID, ID: noteID})
		if errors.Is(err, sql.ErrNoRows) {
			return errNotFound
		}
		if err != nil {
			return err
		}
		if scope.Role() != tenant.RoleOwner && (!n.UserID.Valid || n.UserID.Int64 != audit.ActorFrom(ctx).UserID) {
			return errNotAuthor
		}
		if err := q.DeleteNote(ctx, sqlc.DeleteNoteParams{WorkspaceID: scope.WorkspaceID(), ID: noteID}); err != nil {
			return err
		}
		if err := q.BumpIndexRev(ctx, sqlc.BumpIndexRevParams{WorkspaceID: scope.WorkspaceID(), ID: docID}); err != nil {
			return err
		}
		return s.record(ctx, q, scope, "note.delete", docID, i18n.N("%s 删除了“%s”的一条备注"), title)
	})
	if err == nil {
		s.changed(scope, docID)
	}
	return err
}

// record files the event under the document, so it shows in its history.
// The note's text is content and stays out of the log.
func (s *Service) record(ctx context.Context, q *sqlc.Queries, scope tenant.Scope, action string, docID int64, format string, args ...any) error {
	return audit.Record(ctx, q, audit.Event{
		WorkspaceID: scope.WorkspaceID(), Action: action, TargetType: "document", TargetID: strconv.FormatInt(docID, 10),
		Summary: fmt.Sprintf(format, append([]any{audit.ActorFrom(ctx).Name}, args...)...), At: s.now(),
	})
}

func (s *Service) changed(scope tenant.Scope, docID int64) {
	if s.bus != nil {
		s.bus.Publish(events.Event{Type: events.DocumentUpdated, WorkspaceID: scope.WorkspaceID(), Data: map[string]any{"documentId": docID}})
	}
}

func (s *Service) inTx(ctx context.Context, fn func(*sqlc.Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit()
}
