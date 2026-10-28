package ingest

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"

	"kept/server/internal/apperr"
	"kept/server/internal/files"
	"kept/server/internal/i18n"
	"kept/server/internal/parser"
	"kept/server/internal/tenant"
)

// Zip documents (v1.9.3): the files in one can be listed, and imported one
// by one as documents of their own, into the inbox. Each is read from the
// zip as a stream; nothing is unpacked by its path.

// MaxZipImport bounds the files imported at once.
const MaxZipImport = 200

var errNotZip = apperr.New(apperr.KindInvalid, "这份文档不是压缩包")

// zipLocal fetches a zip document's original into a working directory.
func (s *Service) zipLocal(ctx context.Context, scope tenant.Scope, docID int64) (string, func(), string, error) {
	row, err := s.document(ctx, scope, docID)
	if err != nil {
		return "", nil, "", err
	}
	if row.MimeType != "application/zip" {
		return "", nil, "", errNotZip
	}
	dir, err := s.files.NewTmpDir("zip-")
	if err != nil {
		return "", nil, "", err
	}
	done := func() { os.RemoveAll(dir) }
	src := filepath.Join(dir, "original.zip")
	if err := s.files.Fetch(ctx, row.Storage, files.Original(row.WorkspaceID, row.ID, row.OriginalExt), src); err != nil {
		done()
		return "", nil, "", errors.Join(errOriginalGone, err)
	}
	return src, done, row.OriginalFilename, nil
}

// ZipEntries lists the files in a zip document.
func (s *Service) ZipEntries(ctx context.Context, scope tenant.Scope, docID int64) ([]parser.ZipEntry, bool, error) {
	src, done, _, err := s.zipLocal(ctx, scope, docID)
	if err != nil {
		return nil, false, err
	}
	defer done()
	entries, truncated, err := parser.ListZip(src)
	return entries, truncated, asInvalid(err)
}

// asInvalid turns a file the parser cannot read into a 400 with its message.
func asInvalid(err error) error {
	var pe *parser.Error
	if errors.As(err, &pe) {
		return apperr.New(apperr.KindInvalid, pe.Message)
	}
	return err
}

// Imported is what became of one file asked for.
type Imported struct {
	Path   string
	JobID  int64  // the upload's task, when it was taken
	Reason string // why it was not
}

// ImportEntries makes documents of the files at paths in a zip document.
// They keep its family member and privacy; a file that cannot be taken (a
// duplicate, a type not kept, too large, with a password) is reported, not
// an error.
func (s *Service) ImportEntries(ctx context.Context, scope tenant.Scope, docID int64, paths []string) ([]Imported, error) {
	if len(paths) == 0 || len(paths) > MaxZipImport {
		return nil, apperr.New(apperr.KindInvalid, "一次可以导入 1 到 200 个文件")
	}
	row, err := s.document(ctx, scope, docID)
	if err != nil {
		return nil, err
	}
	src, done, name, err := s.zipLocal(ctx, scope, docID)
	if err != nil {
		return nil, err
	}
	defer done()
	ov := Overrides{}
	if row.PersonID.Valid {
		ov.PersonID, ov.Private = row.PersonID.Int64, row.Private == 1
	}
	out := make([]Imported, 0, len(paths))
	seen := map[string]bool{}
	for _, p := range paths {
		if seen[p] {
			continue
		}
		seen[p] = true
		r := Imported{Path: p}
		r.JobID, r.Reason = s.importEntry(ctx, scope, src, name, p, ov)
		out = append(out, r)
	}
	return out, nil
}

func (s *Service) importEntry(ctx context.Context, scope tenant.Scope, src, zipName, p string, ov Overrides) (int64, string) {
	rc, _, err := parser.OpenZipEntry(src, p, s.opt.MaxUploadSize)
	if err != nil {
		return 0, reason(err)
	}
	defer rc.Close()
	// the type first, from the bytes: a file the parser does not take is
	// reported here rather than as a failed task
	dir, err := s.files.NewTmpDir("zip-entry-")
	if err != nil {
		return 0, err.Error()
	}
	defer os.RemoveAll(dir)
	tmp := filepath.Join(dir, "entry")
	f, err := os.Create(tmp)
	if err != nil {
		return 0, err.Error()
	}
	n, err := io.Copy(f, rc)
	f.Close()
	if err != nil {
		return 0, i18n.N("读不出来")
	}
	if n > s.opt.MaxUploadSize {
		return 0, errTooLarge(s.opt.MaxUploadSize).Error()
	}
	if _, _, err := parser.Detect(tmp, path.Base(p)); err != nil {
		return 0, reason(err)
	}
	in, err := os.Open(tmp)
	if err != nil {
		return 0, err.Error()
	}
	defer in.Close()
	job, err := s.Submit(ctx, scope, path.Base(p), in, ov, Upload{Origin: "zip:" + zipName + "/" + p})
	if err != nil {
		return 0, reason(err)
	}
	return job, ""
}

func reason(err error) string {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Message
	}
	var pe *parser.Error
	if errors.As(err, &pe) {
		if pe == parser.ErrUnsupported {
			return i18n.N("不支持这种文件格式")
		}
		return pe.Message
	}
	return err.Error()
}

// Sheets reads the tables of a spreadsheet or CSV document (v1.9.3).
func (s *Service) Sheets(ctx context.Context, scope tenant.Scope, docID int64) ([]parser.Sheet, error) {
	row, err := s.document(ctx, scope, docID)
	if err != nil {
		return nil, err
	}
	ext := row.OriginalExt
	if e := path.Ext(row.OriginalFilename); len(e) > 1 && (row.MimeType == "text/plain") {
		ext = e[1:] // a CSV is plain text named .csv
	}
	dir, err := s.files.NewTmpDir("sheets-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	src := filepath.Join(dir, "original")
	if err := s.files.Fetch(ctx, row.Storage, files.Original(row.WorkspaceID, row.ID, row.OriginalExt), src); err != nil {
		return nil, errors.Join(errOriginalGone, err)
	}
	sheets, err := parser.ReadSheets(src, ext)
	return sheets, asInvalid(err)
}
