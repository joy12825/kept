package ingest

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sync/singleflight"

	"kept/server/internal/apperr"
	"kept/server/internal/files"
	"kept/server/internal/parser"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// Versions side by side (v1.9.7): a version shown as a PDF. The current
// version is the document's searchable PDF; an earlier Office file, e-mail
// or OFD is made into one on demand and kept in the cache, named by its
// checksum, so the next look is immediate.

var (
	errPreviewConverter = apperr.WithCode(apperr.KindConflict, "no_converter", "这个版本是 Office 文件或邮件，要配好文档转换服务（Gotenberg）才能在网页上显示，可以先下载原件查看")
	errPreviewNone      = apperr.WithCode(apperr.KindConflict, "no_preview", "这个版本的文件不能转成 PDF 显示，可以下载原件查看")
	errPreviewEncrypted = apperr.WithCode(apperr.KindInvalid, "pdf_encrypted", "这个版本的 PDF 有密码，网页上打不开")
)

var previewFlight singleflight.Group

// VersionPreview returns the PDF a version is shown as, and its name.
func (s *Service) VersionPreview(ctx context.Context, scope tenant.Scope, docID, versionID int64) (files.Loc, string, error) {
	row, err := s.document(ctx, scope, docID)
	if err != nil {
		return files.Loc{}, "", err
	}
	var v sqlc.DocumentVersion
	if versionID != 0 {
		v, err = s.q.GetDocumentVersion(ctx, sqlc.GetDocumentVersionParams{WorkspaceID: scope.WorkspaceID(), DocumentID: docID, ID: versionID})
		if errors.Is(err, sql.ErrNoRows) {
			return files.Loc{}, "", errVersionNotFound
		}
		if err != nil {
			return files.Loc{}, "", err
		}
	}
	name := row.Title + ".pdf"
	if versionID == 0 || v.Current == 1 {
		if row.HasArchive == 1 {
			return files.At(row.Storage, files.Archive(row.WorkspaceID, row.ID)), name, nil
		}
		if row.MimeType == "application/pdf" && row.OriginalEncrypted == 0 {
			return files.At(row.Storage, files.Original(row.WorkspaceID, row.ID, row.OriginalExt)), name, nil
		}
	} else if v.Encrypted == 1 {
		return files.Loc{}, "", errPreviewEncrypted
	}
	loc, filename, mime, err := s.VersionFile(ctx, scope, docID, versionID)
	if err != nil {
		return files.Loc{}, "", err
	}
	if mime == "application/pdf" {
		return loc, name, nil
	}
	sum := row.Checksum
	if versionID != 0 && v.Current != 1 {
		sum = v.Checksum
	}
	dst := filepath.Join(s.files.CacheDir("versions"), sum+".pdf")
	_, err, _ = previewFlight.Do(dst, func() (any, error) {
		return nil, s.makePreview(ctx, loc, filename, dst)
	})
	if err != nil {
		return files.Loc{}, "", err
	}
	return files.LocalFile(dst), name, nil
}

func (s *Service) makePreview(ctx context.Context, loc files.Loc, filename, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		now := time.Now()
		_ = os.Chtimes(dst, now, now) // still in use: not cleaned
		return nil
	}
	work, err := s.files.NewTmpDir("version-preview-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	src := filepath.Join(work, "version")
	if err := s.files.Fetch(ctx, loc.Storage, loc.Ref, src); err != nil {
		return err
	}
	pdf, err := parser.ToPDF(ctx, s.opt.Parser.GotenbergURL, src, filename, work)
	switch {
	case errors.Is(err, parser.ErrNoConverter):
		return errPreviewConverter
	case errors.Is(err, parser.ErrNoPDFForm), errors.Is(err, parser.ErrUnsupported):
		return errPreviewNone
	case err != nil:
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return files.Move(pdf, dst)
}
