package books

import (
	"context"
	"database/sql"
	"errors"
	"kept/server/internal/i18n"
	"os"
	"path/filepath"

	"kept/server/internal/apperr"
	"kept/server/internal/document"
	"kept/server/internal/imgutil"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// MaxCoverSize is the largest cover image accepted, before scaling.
const MaxCoverSize = 5 << 20

// CoverWidth is the widest a stored cover is.
const CoverWidth = 600

var (
	errCoverSize = apperr.New(apperr.KindInvalid, "封面图最大 5 MB")
	errCoverType = apperr.New(apperr.KindInvalid, "封面图只能是 JPEG 或 PNG")
	errNoCover   = apperr.New(apperr.KindNotFound, "这本书没有封面")
)

// SaveCover stores an image (JPEG or PNG, up to MaxCoverSize; one byte
// more means too big) as the book's cover, scaled to at most 600 px wide.
func (s *Service) SaveCover(ctx context.Context, scope tenant.Scope, docID int64, data []byte) error {
	if len(data) > MaxCoverSize {
		return errCoverSize
	}
	jpg, err := imgutil.ToJPEG(data, CoverWidth, "jpeg", "png")
	if errors.Is(err, imgutil.ErrFormat) {
		return errCoverType
	}
	if err != nil {
		return err
	}
	if s.files == nil {
		return errors.New("books: no file store")
	}
	path := s.files.CoverPath(scope.WorkspaceID(), docID)
	had := false
	err = s.inTx(ctx, func(_ *sql.Tx, q *sqlc.Queries) error {
		d, b, err := s.bookOf(ctx, q, scope, docID)
		if err != nil {
			return err
		}
		had = b.Cover == 1
		// The file goes in place before the row says so; a failed
		// transaction removes a file that was not there before.
		if err := writeFile(path, jpg); err != nil {
			return err
		}
		if err := q.SetBookCover(ctx, sqlc.SetBookCoverParams{Cover: 1, Now: s.now().UnixMilli(), WorkspaceID: scope.WorkspaceID(), DocumentID: docID}); err != nil {
			return err
		}
		return s.record(ctx, q, scope, "document.book", docID, i18n.N("%s 更换了“%s”的封面"), d.Title)
	})
	if err != nil {
		if !had {
			_ = os.Remove(path)
		}
		return err
	}
	s.changed(scope, docID)
	return nil
}

// DeleteCover removes the book's cover.
func (s *Service) DeleteCover(ctx context.Context, scope tenant.Scope, docID int64) error {
	err := s.inTx(ctx, func(_ *sql.Tx, q *sqlc.Queries) error {
		d, b, err := s.bookOf(ctx, q, scope, docID)
		if err != nil {
			return err
		}
		if b.Cover == 0 {
			return errNoCover
		}
		if err := q.SetBookCover(ctx, sqlc.SetBookCoverParams{Cover: 0, Now: s.now().UnixMilli(), WorkspaceID: scope.WorkspaceID(), DocumentID: docID}); err != nil {
			return err
		}
		return s.record(ctx, q, scope, "document.book", docID, i18n.N("%s 删除了“%s”的封面"), d.Title)
	})
	if err != nil {
		return err
	}
	if s.files != nil {
		_ = os.Remove(s.files.CoverPath(scope.WorkspaceID(), docID))
	}
	s.changed(scope, docID)
	return nil
}

// CoverPath is the cover file of a book outside the trash that has one.
func (s *Service) CoverPath(ctx context.Context, scope tenant.Scope, docID int64) (string, error) {
	_, b, err := s.bookOf(ctx, s.q, scope, docID)
	if err != nil {
		return "", err
	}
	if b.Cover == 0 || s.files == nil {
		return "", errNoCover
	}
	return s.files.CoverPath(scope.WorkspaceID(), docID), nil
}

// bookOf loads a document outside the trash and its book record.
func (s *Service) bookOf(ctx context.Context, q *sqlc.Queries, scope tenant.Scope, docID int64) (sqlc.Document, sqlc.Book, error) {
	d, err := q.GetDocument(ctx, sqlc.GetDocumentParams{WorkspaceID: scope.WorkspaceID(), ID: docID, Viewer: scope.Viewer()})
	if errors.Is(err, sql.ErrNoRows) || (err == nil && d.DeletedAt.Valid) {
		return d, sqlc.Book{}, document.ErrNotFound
	}
	if err != nil {
		return d, sqlc.Book{}, err
	}
	b, err := q.GetBook(ctx, sqlc.GetBookParams{WorkspaceID: scope.WorkspaceID(), DocumentID: docID})
	if errors.Is(err, sql.ErrNoRows) {
		return d, b, errNotBook
	}
	return d, b, err
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".part"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
