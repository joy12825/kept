package ingest

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"kept/server/internal/apperr"
	"kept/server/internal/files"
	"kept/server/internal/parser"
	"kept/server/internal/tenant"
)

// The Word reading view (v1.9.8): the current version's .docx read into
// blocks, and its pictures; since v1.11.1 a .doc or .odt too.

var (
	errNotWord     = apperr.WithCode(apperr.KindInvalid, "not_word", "阅读视图只支持 Word 文档（.docx、.doc）和 OpenDocument 文本（.odt）")
	errWordLocked  = apperr.WithCode(apperr.KindInvalid, "word_encrypted", "这份文档设了打开密码，阅读视图打不开；可以下载原件")
	errWordOld     = apperr.WithCode(apperr.KindInvalid, "word_old", "这是 Word 95 或更早的文档，阅读视图打不开；可以看可搜索版或下载原件")
	errWordBroken  = apperr.WithCode(apperr.KindInvalid, "word_unreadable", "这份 Word 文档读不出来，可能已损坏；可以看可搜索版或下载原件")
	errWordTooBig  = apperr.WithCode(apperr.KindInvalid, "word_too_big", "这份 Word 文档太大，阅读视图显示不了；可以看可搜索版")
	errWordNoImage = apperr.New(apperr.KindNotFound, "找不到这张图片")
)

// withOriginal fetches a Word document's original into a temporary
// directory and runs fn on it.
func (s *Service) withOriginal(ctx context.Context, scope tenant.Scope, docID int64, fn func(src string) error) error {
	return s.withFile(ctx, scope, docID, parser.IsWordType, errNotWord, fn)
}

// withFile fetches a document's original, when its type is one ok takes
// (else notIt), into a temporary directory and runs fn on it.
func (s *Service) withFile(ctx context.Context, scope tenant.Scope, docID int64, ok func(mime string) bool, notIt error, fn func(src string) error) error {
	row, err := s.document(ctx, scope, docID)
	if err != nil {
		return err
	}
	if !ok(row.MimeType) {
		return notIt
	}
	dir, err := s.files.NewTmpDir("word-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	src := filepath.Join(dir, "original") // the reader tells the format by the content
	if err := s.files.Fetch(ctx, row.Storage, files.Original(row.WorkspaceID, row.ID, row.OriginalExt), src); err != nil {
		return errors.Join(errOriginalGone, err)
	}
	return fn(src)
}

func wordError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, parser.ErrNotWord):
		return errWordBroken
	case errors.Is(err, parser.ErrWordTooBig):
		return errWordTooBig
	case errors.Is(err, parser.ErrWordEncrypted):
		return errWordLocked
	case errors.Is(err, parser.ErrWordOld):
		return errWordOld
	}
	return errWordBroken
}

// Word reads a Word document for the reading view.
func (s *Service) Word(ctx context.Context, scope tenant.Scope, docID int64) (*parser.WordDoc, error) {
	var doc *parser.WordDoc
	err := s.withOriginal(ctx, scope, docID, func(src string) error {
		d, err := parser.ReadWord(src)
		doc = d
		return wordError(err)
	})
	return doc, err
}

// WordImage returns the n-th picture (from 1) of a Word document's reading
// view, and its type.
func (s *Service) WordImage(ctx context.Context, scope tenant.Scope, docID int64, n int) ([]byte, string, error) {
	var data []byte
	var mime string
	err := s.withOriginal(ctx, scope, docID, func(src string) error {
		b, m, err := parser.ReadWordImage(src, n)
		if errors.Is(err, parser.ErrNotWord) {
			return errWordNoImage
		}
		data, mime = b, m
		return wordError(err)
	})
	return data, mime, err
}
