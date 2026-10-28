// Package booklists keeps book lists (v0.4): named, ordered collections
// of books shared in the workspace, each book with a line about it.
package booklists

import (
	"context"
	"database/sql"
	"encoding/json"
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
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

var (
	errNotFound  = apperr.New(apperr.KindNotFound, "找不到这个书单")
	errName      = apperr.New(apperr.KindInvalid, "书单名称不能为空，最多 40 个字")
	errDesc      = apperr.New(apperr.KindInvalid, "书单说明最多 500 个字")
	errNote      = apperr.New(apperr.KindInvalid, "推荐语最多 200 个字")
	errNameTaken = apperr.WithCode(apperr.KindConflict, "name_taken", "已经有同名的书单")
	errNotBook   = apperr.New(apperr.KindInvalid, "只有书籍可以加入书单")
	errInList    = apperr.WithCode(apperr.KindConflict, "in_list", "这本书已经在书单里")
	errNoItem    = apperr.New(apperr.KindNotFound, "这本书不在书单里")
	errOrder     = apperr.New(apperr.KindInvalid, "顺序里的书要和书单里的一致")
)

// List is a book list; Preview holds its first books for the cover strip.
type List struct {
	ID          int64
	Name        string
	Description string
	CreatedBy   int64
	Count       int
	Preview     []Item
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Item is a book in a list.
type Item struct {
	DocumentID   int64
	Title        string
	Authors      []string
	HasCover     bool
	HasThumbnail bool
	Note         string
	AddedAt      time.Time
	// The caller's own reading status and progress; "" when none.
	Status  string
	Percent float64
}

type Service struct {
	db  *sql.DB
	q   *sqlc.Queries
	now func() time.Time
}

func NewService(db *sql.DB) *Service { return &Service{db: db, q: sqlc.New(db), now: time.Now} }

func cleanName(name, desc string) (string, string, error) {
	name, desc = strings.TrimSpace(name), strings.TrimSpace(desc)
	if n := utf8.RuneCountInString(name); n < 1 || n > 40 {
		return "", "", errName
	}
	if utf8.RuneCountInString(desc) > 500 {
		return "", "", errDesc
	}
	return name, desc, nil
}

func (s *Service) items(ctx context.Context, q *sqlc.Queries, scope tenant.Scope, listID int64) ([]Item, error) {
	rows, err := q.ListBookListItems(ctx, sqlc.ListBookListItemsParams{ListID: listID, Viewer: scope.Viewer()})
	if err != nil {
		return nil, err
	}
	out := make([]Item, len(rows))
	for i, r := range rows {
		it := Item{DocumentID: r.DocumentID, Title: r.Title, HasCover: r.Cover == 1, HasThumbnail: r.HasThumbnail == 1, Note: r.Note, AddedAt: time.UnixMilli(r.AddedAt)}
		_ = json.Unmarshal([]byte(r.Authors), &it.Authors)
		if it.Authors == nil {
			it.Authors = []string{}
		}
		out[i] = it
	}
	return out, nil
}

// Lists returns the workspace's lists, oldest first, each with up to four
// books to show.
func (s *Service) Lists(ctx context.Context, scope tenant.Scope) ([]List, error) {
	rows, err := s.q.ListBookLists(ctx, sqlc.ListBookListsParams{WorkspaceID: scope.WorkspaceID(), Viewer: scope.Viewer()})
	if err != nil {
		return nil, err
	}
	out := make([]List, len(rows))
	for i, r := range rows {
		items, err := s.items(ctx, s.q, scope, r.ID)
		if err != nil {
			return nil, err
		}
		out[i] = List{ID: r.ID, Name: r.Name, Description: r.Description, CreatedBy: r.CreatedBy.Int64, Count: int(r.ItemCount),
			Preview: items[:min(4, len(items))], CreatedAt: time.UnixMilli(r.CreatedAt), UpdatedAt: time.UnixMilli(r.UpdatedAt)}
	}
	return out, nil
}

// Get returns a list with all its books in order.
func (s *Service) Get(ctx context.Context, scope tenant.Scope, id int64) (List, []Item, error) {
	r, err := s.q.GetBookList(ctx, sqlc.GetBookListParams{WorkspaceID: scope.WorkspaceID(), ID: id})
	if errors.Is(err, sql.ErrNoRows) {
		return List{}, nil, errNotFound
	}
	if err != nil {
		return List{}, nil, err
	}
	items, err := s.items(ctx, s.q, scope, id)
	if err != nil {
		return List{}, nil, err
	}
	if err := s.withReading(ctx, items); err != nil {
		return List{}, nil, err
	}
	l := List{ID: r.ID, Name: r.Name, Description: r.Description, CreatedBy: r.CreatedBy.Int64, Count: len(items),
		Preview: items[:min(4, len(items))], CreatedAt: time.UnixMilli(r.CreatedAt), UpdatedAt: time.UnixMilli(r.UpdatedAt)}
	return l, items, nil
}

// withReading fills in the caller's reading status of each book.
func (s *Service) withReading(ctx context.Context, items []Item) error {
	user := audit.ActorFrom(ctx).UserID
	if user == 0 || len(items) == 0 {
		return nil
	}
	idx := map[int64]*Item{}
	args := []any{user}
	for i := range items {
		idx[items[i].DocumentID] = &items[i]
		args = append(args, items[i].DocumentID)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT document_id, status, percent FROM reading_progress WHERE user_id = ? AND document_id IN (`+
		strings.TrimSuffix(strings.Repeat("?,", len(items)), ",")+`)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var doc int64
		var status string
		var pct float64
		if err := rows.Scan(&doc, &status, &pct); err != nil {
			return err
		}
		idx[doc].Status, idx[doc].Percent = status, pct
	}
	return rows.Err()
}

// Create makes an empty list.
func (s *Service) Create(ctx context.Context, scope tenant.Scope, name, desc string) (List, error) {
	name, desc, err := cleanName(name, desc)
	if err != nil {
		return List{}, err
	}
	actor := audit.ActorFrom(ctx)
	var id int64
	err = s.inTx(ctx, func(q *sqlc.Queries) error {
		if _, err := q.FindBookListByName(ctx, sqlc.FindBookListByNameParams{WorkspaceID: scope.WorkspaceID(), Name: name}); err == nil {
			return errNameTaken
		}
		r, err := q.InsertBookList(ctx, sqlc.InsertBookListParams{WorkspaceID: scope.WorkspaceID(), Name: name, Description: desc,
			CreatedBy: sql.NullInt64{Int64: actor.UserID, Valid: actor.UserID != 0}, Now: s.now().UnixMilli()})
		if err != nil {
			return err
		}
		id = r.ID
		return s.record(ctx, q, scope, "booklist.create", id, i18n.N("%s 新建了书单“%s”"), name)
	})
	if err != nil {
		return List{}, err
	}
	l, _, err := s.Get(ctx, scope, id)
	return l, err
}

// Update renames a list or changes its description (nil keeps them).
func (s *Service) Update(ctx context.Context, scope tenant.Scope, id int64, name, desc *string) (List, error) {
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		r, err := q.GetBookList(ctx, sqlc.GetBookListParams{WorkspaceID: scope.WorkspaceID(), ID: id})
		if errors.Is(err, sql.ErrNoRows) {
			return errNotFound
		}
		if err != nil {
			return err
		}
		n, d := r.Name, r.Description
		if name != nil {
			n = *name
		}
		if desc != nil {
			d = *desc
		}
		if n, d, err = cleanName(n, d); err != nil {
			return err
		}
		if n != r.Name {
			if _, err := q.FindBookListByName(ctx, sqlc.FindBookListByNameParams{WorkspaceID: scope.WorkspaceID(), Name: n}); err == nil {
				return errNameTaken
			}
		}
		if err := q.UpdateBookList(ctx, sqlc.UpdateBookListParams{Name: n, Description: d, Now: s.now().UnixMilli(), WorkspaceID: scope.WorkspaceID(), ID: id}); err != nil {
			return err
		}
		if n != r.Name {
			return s.record(ctx, q, scope, "booklist.update", id, i18n.N("%s 把书单“%s”改名为“%s”"), r.Name, n)
		}
		return s.record(ctx, q, scope, "booklist.update", id, i18n.N("%s 修改了书单“%s”"), n)
	})
	if err != nil {
		return List{}, err
	}
	l, _, err := s.Get(ctx, scope, id)
	return l, err
}

// Delete removes a list; the books stay.
func (s *Service) Delete(ctx context.Context, scope tenant.Scope, id int64) error {
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		r, err := q.GetBookList(ctx, sqlc.GetBookListParams{WorkspaceID: scope.WorkspaceID(), ID: id})
		if errors.Is(err, sql.ErrNoRows) {
			return errNotFound
		}
		if err != nil {
			return err
		}
		if _, err := q.DeleteBookList(ctx, sqlc.DeleteBookListParams{WorkspaceID: scope.WorkspaceID(), ID: id}); err != nil {
			return err
		}
		return s.record(ctx, q, scope, "booklist.delete", id, i18n.N("%s 删除了书单“%s”"), r.Name)
	})
}

// list checks a list belongs to the workspace.
func (s *Service) list(ctx context.Context, q *sqlc.Queries, scope tenant.Scope, id int64) error {
	_, err := q.GetBookList(ctx, sqlc.GetBookListParams{WorkspaceID: scope.WorkspaceID(), ID: id})
	if errors.Is(err, sql.ErrNoRows) {
		return errNotFound
	}
	return err
}

func cleanNote(note string) (string, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > 200 {
		return "", errNote
	}
	return note, nil
}

// Add puts a book at the end of a list.
func (s *Service) Add(ctx context.Context, scope tenant.Scope, id, docID int64, note string) error {
	note, err := cleanNote(note)
	if err != nil {
		return err
	}
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		if err := s.list(ctx, q, scope, id); err != nil {
			return err
		}
		d, err := q.GetDocument(ctx, sqlc.GetDocumentParams{WorkspaceID: scope.WorkspaceID(), ID: docID, Viewer: scope.Viewer()})
		if errors.Is(err, sql.ErrNoRows) || (err == nil && d.DeletedAt.Valid) {
			return document.ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := q.GetBook(ctx, sqlc.GetBookParams{WorkspaceID: scope.WorkspaceID(), DocumentID: docID}); errors.Is(err, sql.ErrNoRows) {
			return errNotBook
		} else if err != nil {
			return err
		}
		if _, err := q.GetBookListItem(ctx, sqlc.GetBookListItemParams{ListID: id, DocumentID: docID}); err == nil {
			return errInList
		}
		last, err := q.MaxBookListPosition(ctx, id)
		if err != nil {
			return err
		}
		now := s.now().UnixMilli()
		if err := q.InsertBookListItem(ctx, sqlc.InsertBookListItemParams{ListID: id, DocumentID: docID, Position: last + 1, Note: note, Now: now}); err != nil {
			return err
		}
		return q.TouchBookList(ctx, sqlc.TouchBookListParams{Now: now, ID: id})
	})
}

// SetNote changes the line about a book in a list.
func (s *Service) SetNote(ctx context.Context, scope tenant.Scope, id, docID int64, note string) error {
	note, err := cleanNote(note)
	if err != nil {
		return err
	}
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		if err := s.list(ctx, q, scope, id); err != nil {
			return err
		}
		n, err := q.SetBookListItemNote(ctx, sqlc.SetBookListItemNoteParams{Note: note, ListID: id, DocumentID: docID})
		if err != nil {
			return err
		}
		if n == 0 {
			return errNoItem
		}
		return q.TouchBookList(ctx, sqlc.TouchBookListParams{Now: s.now().UnixMilli(), ID: id})
	})
}

// Remove takes a book out of a list.
func (s *Service) Remove(ctx context.Context, scope tenant.Scope, id, docID int64) error {
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		if err := s.list(ctx, q, scope, id); err != nil {
			return err
		}
		n, err := q.DeleteBookListItem(ctx, sqlc.DeleteBookListItemParams{ListID: id, DocumentID: docID})
		if err != nil {
			return err
		}
		if n == 0 {
			return errNoItem
		}
		return q.TouchBookList(ctx, sqlc.TouchBookListParams{Now: s.now().UnixMilli(), ID: id})
	})
}

// Reorder sets the order; ids must be exactly the list's books outside
// the trash (books in the trash keep their places after them).
func (s *Service) Reorder(ctx context.Context, scope tenant.Scope, id int64, ids []int64) error {
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		if err := s.list(ctx, q, scope, id); err != nil {
			return err
		}
		items, err := s.items(ctx, q, scope, id)
		if err != nil {
			return err
		}
		have := map[int64]bool{}
		for _, it := range items {
			have[it.DocumentID] = true
		}
		seen := map[int64]bool{}
		for _, d := range ids {
			if !have[d] || seen[d] {
				return errOrder
			}
			seen[d] = true
		}
		if len(seen) != len(have) {
			return errOrder
		}
		for i, d := range ids {
			if err := q.SetBookListItemPosition(ctx, sqlc.SetBookListItemPositionParams{Position: int64(i + 1), ListID: id, DocumentID: d}); err != nil {
				return err
			}
		}
		return q.TouchBookList(ctx, sqlc.TouchBookListParams{Now: s.now().UnixMilli(), ID: id})
	})
}

// ListsOf returns the IDs of the lists a book is in.
func (s *Service) ListsOf(ctx context.Context, scope tenant.Scope, docID int64) ([]int64, error) {
	ids, err := s.q.ListsOfDocument(ctx, sqlc.ListsOfDocumentParams{WorkspaceID: scope.WorkspaceID(), DocumentID: docID})
	if ids == nil {
		ids = []int64{}
	}
	return ids, err
}

func (s *Service) record(ctx context.Context, q *sqlc.Queries, scope tenant.Scope, action string, id int64, format string, args ...any) error {
	return audit.Record(ctx, q, audit.Event{
		WorkspaceID: scope.WorkspaceID(), Action: action, TargetType: "booklist", TargetID: strconv.FormatInt(id, 10),
		Summary: fmt.Sprintf(format, append([]any{audit.ActorFrom(ctx).Name}, args...)...), At: s.now(),
	})
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
