// Package books keeps book records (v0.4): a document marked as a book has
// a catalog entry with ISBN, authors, publisher and so on. Books are filed
// into the library drawer by its rule {"field":"book","op":"eq","value":true}.
package books

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"kept/server/internal/i18n"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/document"
	"kept/server/internal/events"
	"kept/server/internal/files"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

var (
	errNotBook  = apperr.New(apperr.KindNotFound, "这份文档不是书籍")
	errISBN     = apperr.New(apperr.KindInvalid, "ISBN 不正确：应为 10 位或 13 位，校验位要对得上")
	errTooLong  = apperr.New(apperr.KindInvalid, "书目信息太长")
	errPages    = apperr.New(apperr.KindInvalid, "页数应在 0 到 100000 之间")
	errAuthors  = apperr.New(apperr.KindInvalid, "作者最多 20 位，每位最多 100 个字")
	errDupISBN  = i18n.N("这个 ISBN 已经是“%s”的书目")
	errDupTrash = i18n.N("这个 ISBN 已经是回收站中“%s”的书目")
)

// Book is a document's catalog entry.
type Book struct {
	DocumentID  int64
	ISBN        string // ISBN-13 without hyphens, or ""
	Authors     []string
	Publisher   string
	Series      string
	Edition     string
	Published   string // free text: "2019", "2019-05"
	Pages       int
	Language    string
	Description string
	HasCover    bool
	UpdatedAt   time.Time
}

// Input is what a person can set; ISBN may carry hyphens, spaces or be
// ISBN-10.
type Input struct {
	ISBN        string
	Authors     []string
	Publisher   string
	Series      string
	Edition     string
	Published   string
	Pages       int
	Language    string
	Description string
}

// Placer files a document by the drawers' rules (drawer.Service).
type Placer interface {
	PlaceDocument(ctx context.Context, db sqlc.DBTX, ws, docID int64) error
}

type Service struct {
	db     *sql.DB
	q      *sqlc.Queries
	bus    *events.Bus
	placer Placer
	files  *files.Store
	now    func() time.Time
}

func NewService(db *sql.DB, bus *events.Bus, placer Placer, fs *files.Store) *Service {
	return &Service{db: db, q: sqlc.New(db), bus: bus, placer: placer, files: fs, now: time.Now}
}

// NormalizeISBN strips hyphens and spaces, checks the check digit and
// returns the ISBN-13 form. "" stays "".
func NormalizeISBN(s string) (string, error) {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(s)) {
		switch {
		case r == '-' || r == ' ' || r == '‐' || r == '–':
		case r >= '0' && r <= '9', r == 'X':
			b.WriteRune(r)
		default:
			return "", errISBN
		}
	}
	d := b.String()
	switch len(d) {
	case 0:
		return "", nil
	case 10:
		sum := 0
		for i := 0; i < 10; i++ {
			v := int(d[i] - '0')
			if d[i] == 'X' {
				if i != 9 {
					return "", errISBN
				}
				v = 10
			}
			sum += (10 - i) * v
		}
		if sum%11 != 0 {
			return "", errISBN
		}
		return isbn13("978" + d[:9]), nil
	case 13:
		if strings.Contains(d, "X") || (!strings.HasPrefix(d, "978") && !strings.HasPrefix(d, "979")) {
			return "", errISBN
		}
		if isbn13(d[:12]) != d {
			return "", errISBN
		}
		return d, nil
	}
	return "", errISBN
}

// isbn13 appends the check digit to 12 digits.
func isbn13(d12 string) string {
	sum := 0
	for i := 0; i < 12; i++ {
		v := int(d12[i] - '0')
		if i%2 == 1 {
			v *= 3
		}
		sum += v
	}
	return d12 + strconv.Itoa((10-sum%10)%10)
}

func toBook(r sqlc.Book) Book {
	b := Book{DocumentID: r.DocumentID, ISBN: r.Isbn, Publisher: r.Publisher, Series: r.Series, Edition: r.Edition,
		Published: r.Published, Pages: int(r.Pages), Language: r.Language, Description: r.Description,
		HasCover: r.Cover == 1, UpdatedAt: time.UnixMilli(r.UpdatedAt)}
	_ = json.Unmarshal([]byte(r.Authors), &b.Authors)
	if b.Authors == nil {
		b.Authors = []string{}
	}
	return b
}

// Get returns a document's book record (nil when it is not a book).
func (s *Service) Get(ctx context.Context, scope tenant.Scope, docID int64) (*Book, error) {
	r, err := s.q.GetBook(ctx, sqlc.GetBookParams{WorkspaceID: scope.WorkspaceID(), DocumentID: docID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b := toBook(r)
	return &b, nil
}

func clean(in Input) (Input, string, error) {
	isbn, err := NormalizeISBN(in.ISBN)
	if err != nil {
		return in, "", err
	}
	for _, p := range []*string{&in.Publisher, &in.Series, &in.Edition, &in.Published, &in.Language} {
		*p = strings.TrimSpace(*p)
		if utf8.RuneCountInString(*p) > 200 {
			return in, "", errTooLong
		}
	}
	in.Description = strings.TrimSpace(in.Description)
	if utf8.RuneCountInString(in.Description) > 5000 {
		return in, "", errTooLong
	}
	if in.Pages < 0 || in.Pages > 100000 {
		return in, "", errPages
	}
	authors := []string{}
	for _, a := range in.Authors {
		if a = strings.TrimSpace(a); a != "" {
			if utf8.RuneCountInString(a) > 100 {
				return in, "", errAuthors
			}
			authors = append(authors, a)
		}
	}
	if len(authors) > 20 {
		return in, "", errAuthors
	}
	in.Authors = authors
	return in, isbn, nil
}

// Set creates or replaces a document's book record (making it a book).
func (s *Service) Set(ctx context.Context, scope tenant.Scope, docID int64, in Input) (Book, error) {
	in, isbn, err := clean(in)
	if err != nil {
		return Book{}, err
	}
	authors, _ := json.Marshal(in.Authors)
	err = s.inTx(ctx, func(tx *sql.Tx, q *sqlc.Queries) error {
		d, err := q.GetDocument(ctx, sqlc.GetDocumentParams{WorkspaceID: scope.WorkspaceID(), ID: docID, Viewer: scope.Viewer()})
		if errors.Is(err, sql.ErrNoRows) || (err == nil && d.DeletedAt.Valid) {
			return document.ErrNotFound
		}
		if err != nil {
			return err
		}
		if isbn != "" {
			other, err := q.FindBookByISBN(ctx, sqlc.FindBookByISBNParams{WorkspaceID: scope.WorkspaceID(), Isbn: isbn})
			if err == nil && other.DocumentID != docID {
				msg := errDupISBN
				if other.DeletedAt.Valid {
					msg = errDupTrash
				}
				return apperr.WithCode(apperr.KindConflict, "isbn_taken", strings.Replace(msg, "%s", other.Title, 1))
			} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		_, getErr := q.GetBook(ctx, sqlc.GetBookParams{WorkspaceID: scope.WorkspaceID(), DocumentID: docID})
		if err := q.UpsertBook(ctx, sqlc.UpsertBookParams{
			DocumentID: docID, WorkspaceID: scope.WorkspaceID(), Isbn: isbn, Authors: string(authors), Publisher: in.Publisher,
			Series: in.Series, Edition: in.Edition, Published: in.Published, Pages: int64(in.Pages), Language: in.Language,
			Description: in.Description, Now: s.now().UnixMilli(),
		}); err != nil {
			return err
		}
		if err := s.touched(ctx, tx, q, scope, docID); err != nil {
			return err
		}
		if getErr == nil {
			return s.record(ctx, q, scope, "document.book", docID, i18n.N("%s 修改了“%s”的书目"), d.Title)
		}
		return s.record(ctx, q, scope, "document.book", docID, i18n.N("%s 把“%s”设为书籍"), d.Title)
	})
	if err != nil {
		return Book{}, err
	}
	s.changed(scope, docID)
	b, err := s.Get(ctx, scope, docID)
	if err != nil || b == nil {
		return Book{}, err
	}
	return *b, nil
}

// Delete removes the book record; the document stays.
func (s *Service) Delete(ctx context.Context, scope tenant.Scope, docID int64) error {
	err := s.inTx(ctx, func(tx *sql.Tx, q *sqlc.Queries) error {
		d, err := q.GetDocument(ctx, sqlc.GetDocumentParams{WorkspaceID: scope.WorkspaceID(), ID: docID, Viewer: scope.Viewer()})
		if errors.Is(err, sql.ErrNoRows) {
			return document.ErrNotFound
		}
		if err != nil {
			return err
		}
		n, err := q.DeleteBook(ctx, sqlc.DeleteBookParams{WorkspaceID: scope.WorkspaceID(), DocumentID: docID})
		if err != nil {
			return err
		}
		if n == 0 {
			return errNotBook
		}
		if err := s.touched(ctx, tx, q, scope, docID); err != nil {
			return err
		}
		return s.record(ctx, q, scope, "document.book", docID, i18n.N("%s 让“%s”不再作为书籍"), d.Title)
	})
	if err == nil {
		if s.files != nil {
			_ = os.Remove(s.files.CoverPath(scope.WorkspaceID(), docID))
		}
		s.changed(scope, docID)
	}
	return err
}

// touched re-indexes the document and re-files it (the library's rule).
func (s *Service) touched(ctx context.Context, tx *sql.Tx, q *sqlc.Queries, scope tenant.Scope, docID int64) error {
	if err := q.BumpIndexRev(ctx, sqlc.BumpIndexRevParams{WorkspaceID: scope.WorkspaceID(), ID: docID}); err != nil {
		return err
	}
	if s.placer == nil {
		return nil
	}
	return s.placer.PlaceDocument(ctx, tx, scope.WorkspaceID(), docID)
}

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

func (s *Service) inTx(ctx context.Context, fn func(*sql.Tx, *sqlc.Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx, s.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit()
}
