package ingest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"kept/server/internal/i18n"
	"kept/server/internal/quota"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/events"
	"kept/server/internal/files"
	"kept/server/internal/parser"
	"kept/server/internal/privacy"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// Document versions (v0.3 stage 19): the current version's file is the
// original; earlier ones live under media/versions. Making another file
// current swaps them, clears the derived files and page edits, and queues
// a reprocess that rebuilds them from the new original.

var (
	errVersionNotFound  = apperr.New(apperr.KindNotFound, "找不到这个版本")
	errVersionCurrent   = apperr.New(apperr.KindConflict, "这已经是当前版本")
	errVersionEncrypted = apperr.WithCode(apperr.KindInvalid, "pdf_encrypted", "加密的 PDF 不能作为新版本，请先移除密码")
	errEncryptedVersion = apperr.New(apperr.KindInvalid, "这个版本是加密的 PDF，不能设为当前版本；可以下载后移除密码再上传")
	errNotEncrypted     = apperr.New(apperr.KindInvalid, "这份文档的原件没有密码")
)

// Version is one version of a document's original. ID 0 is the implied
// first version of a document that never got another.
type Version struct {
	ID          int64
	Number      int
	Label       string
	Filename    string
	MimeType    string
	Size        int64
	Encrypted   bool
	Current     bool
	CreatorName string
	CreatedAt   time.Time
}

func (s *Service) document(ctx context.Context, scope tenant.Scope, id int64) (sqlc.Document, error) {
	row, err := s.q.GetDocument(ctx, sqlc.GetDocumentParams{WorkspaceID: scope.WorkspaceID(), ID: id, Viewer: scope.Viewer()})
	if errors.Is(err, sql.ErrNoRows) {
		return row, apperr.New(apperr.KindNotFound, "找不到这份文档，它可能已被删除")
	}
	return row, err
}

// editable is document for a change: a private document only for its
// person, a guardian or a manager (v1.8.0).
func (s *Service) editable(ctx context.Context, scope tenant.Scope, id int64) (sqlc.Document, error) {
	row, err := s.document(ctx, scope, id)
	if err != nil {
		return row, err
	}
	return row, privacy.New(s.db).CheckEdit(ctx, nil, scope, row.Private == 1, row.PersonID)
}

func fileSize(path string) int64 {
	if st, err := os.Stat(path); err == nil {
		return st.Size()
	}
	return 0
}

// Versions lists a document's versions, newest first.
func (s *Service) Versions(ctx context.Context, scope tenant.Scope, docID int64) ([]Version, error) {
	row, err := s.document(ctx, scope, docID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListDocumentVersions(ctx, sqlc.ListDocumentVersionsParams{WorkspaceID: scope.WorkspaceID(), DocumentID: docID})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []Version{{Number: 1, Filename: row.OriginalFilename, MimeType: row.MimeType, Encrypted: row.OriginalEncrypted == 1, Current: true,
			Size: s.storedSize(ctx, row), CreatedAt: time.UnixMilli(row.AddedAt)}}, nil
	}
	out := make([]Version, len(rows))
	for i, r := range rows {
		out[i] = Version{ID: r.ID, Number: int(r.Number), Label: r.Label, Filename: r.OriginalFilename, MimeType: r.MimeType, Size: r.Size,
			Encrypted: r.Encrypted == 1, Current: r.Current == 1, CreatorName: r.CreatorName, CreatedAt: time.UnixMilli(r.CreatedAt)}
	}
	return out, nil
}

// storedSize is the size of a document's original (0 when it is missing).
func (s *Service) storedSize(ctx context.Context, row sqlc.Document) int64 {
	if st, err := s.files.Stat(ctx, row.Storage, files.Original(row.WorkspaceID, row.ID, row.OriginalExt)); err == nil {
		return st.Size
	}
	return 0
}

// VersionFile returns where a version's file is, its name and type.
func (s *Service) VersionFile(ctx context.Context, scope tenant.Scope, docID, versionID int64) (loc files.Loc, filename, mime string, err error) {
	row, err := s.document(ctx, scope, docID)
	if err != nil {
		return loc, "", "", err
	}
	original := files.At(row.Storage, files.Original(row.WorkspaceID, row.ID, row.OriginalExt))
	if versionID == 0 {
		return original, row.OriginalFilename, row.MimeType, nil
	}
	v, err := s.q.GetDocumentVersion(ctx, sqlc.GetDocumentVersionParams{WorkspaceID: scope.WorkspaceID(), DocumentID: docID, ID: versionID})
	if errors.Is(err, sql.ErrNoRows) {
		return loc, "", "", errVersionNotFound
	}
	if err != nil {
		return loc, "", "", err
	}
	if v.Current == 1 {
		return original, v.OriginalFilename, v.MimeType, nil
	}
	return files.At(row.Storage, files.Version(row.WorkspaceID, row.ID, v.ID, v.OriginalExt)), v.OriginalFilename, v.MimeType, nil
}

// current returns the current version's row, inserting the implied first
// version for a document without rows.
func (s *Service) current(ctx context.Context, q *sqlc.Queries, row sqlc.Document) (sqlc.ListDocumentVersionsRow, int, error) {
	rows, err := q.ListDocumentVersions(ctx, sqlc.ListDocumentVersionsParams{WorkspaceID: row.WorkspaceID, DocumentID: row.ID})
	if err != nil {
		return sqlc.ListDocumentVersionsRow{}, 0, err
	}
	if len(rows) == 0 {
		id, err := q.InsertDocumentVersion(ctx, sqlc.InsertDocumentVersionParams{WorkspaceID: row.WorkspaceID, DocumentID: row.ID, Number: 1,
			OriginalFilename: row.OriginalFilename, OriginalExt: row.OriginalExt, MimeType: row.MimeType, Checksum: row.Checksum,
			Size: s.storedSize(ctx, row), Encrypted: row.OriginalEncrypted, Current: 1, CreatedAt: row.AddedAt})
		if err != nil {
			return sqlc.ListDocumentVersionsRow{}, 0, err
		}
		return sqlc.ListDocumentVersionsRow{ID: id, Number: 1, OriginalExt: row.OriginalExt, Current: 1}, 1, nil
	}
	top := int(rows[0].Number)
	for _, r := range rows {
		if r.Current == 1 {
			return r, top, nil
		}
	}
	return sqlc.ListDocumentVersionsRow{}, 0, fmt.Errorf("document %d has versions but none is current", row.ID)
}

// staged is a file ready to become a version.
type staged struct {
	path, filename, ext, mime, checksum string
	size                                int64
}

// stage writes r to a working directory, checking size and type.
func (s *Service) stage(filename string, r io.Reader) (staged, string, error) {
	dir, err := s.files.NewTmpDir("version-")
	if err != nil {
		return staged{}, "", err
	}
	fail := func(err error) (staged, string, error) { os.RemoveAll(dir); return staged{}, "", err }
	path := filepath.Join(dir, "original")
	f, err := os.Create(path)
	if err != nil {
		return fail(err)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(r, s.opt.MaxUploadSize+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
		return fail(err)
	case n > s.opt.MaxUploadSize:
		return fail(errTooLarge(s.opt.MaxUploadSize))
	case n == 0:
		return fail(errEmpty)
	}
	filename = cleanFilename(filename)
	mime, ext, err := parser.Detect(path, filename)
	if err != nil {
		var pe *parser.Error
		if errors.As(err, &pe) {
			return fail(apperr.New(apperr.KindInvalid, pe.Message))
		}
		return fail(err)
	}
	return staged{path: path, filename: filename, ext: ext, mime: mime, checksum: hex.EncodeToString(h.Sum(nil)), size: n}, dir, nil
}

// AddVersion makes an uploaded file the document's current version.
func (s *Service) AddVersion(ctx context.Context, scope tenant.Scope, docID int64, filename string, r io.Reader, label string) (int64, error) {
	row, err := s.editable(ctx, scope, docID)
	if err != nil {
		return 0, err
	}
	st, dir, err := s.stage(filename, r)
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(dir)
	if s.opt.Quota != nil {
		// The new original and the searchable PDF it gets (v1.7.4).
		release, err := s.opt.Quota.Reserve(ctx, scope.WorkspaceID(), quota.Estimate(st.size))
		if err != nil {
			return 0, err
		}
		defer release()
	}
	if parser.IsEncryptedPDF(ctx, st.path) {
		return 0, errVersionEncrypted
	}
	if err := s.checkNewVersion(ctx, row, st.checksum); err != nil {
		return 0, err
	}
	return s.swapIn(ctx, scope, row, st, label, false, func(who string, n int) string {
		return fmt.Sprintf(i18n.N("%s 上传了“%s”的第 %d 版"), who, row.Title, n)
	})
}

func (s *Service) checkNewVersion(ctx context.Context, row sqlc.Document, sum string) error {
	if sum == row.Checksum {
		return apperr.WithCode(apperr.KindConflict, "duplicate", "这和当前版本是同一个文件")
	}
	if n, err := s.q.FindVersionByChecksum(ctx, sqlc.FindVersionByChecksumParams{WorkspaceID: row.WorkspaceID, DocumentID: row.ID, Checksum: sum}); err == nil {
		return apperr.WithCode(apperr.KindConflict, "duplicate", fmt.Sprintf("这和第 %d 版是同一个文件，可以直接把第 %d 版设为当前版本", n, n))
	}
	if dup, err := s.q.FindDocumentByChecksum(ctx, sqlc.FindDocumentByChecksumParams{WorkspaceID: row.WorkspaceID, Checksum: sum}); err == nil && dup.ID != row.ID {
		return s.duplicateOf(ctx, row.WorkspaceID, dup.ID)
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return nil
}

// RemovePassword decrypts the current original with password into a new
// version; the password is not kept.
func (s *Service) RemovePassword(ctx context.Context, scope tenant.Scope, docID int64, password string) (int64, error) {
	row, err := s.editable(ctx, scope, docID)
	if err != nil {
		return 0, err
	}
	if row.OriginalEncrypted != 1 {
		return 0, errNotEncrypted
	}
	dir, err := s.files.NewTmpDir("decrypt-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "original")
	in, release, err := s.files.Local(ctx, row.Storage, files.Original(row.WorkspaceID, row.ID, row.OriginalExt), dir)
	if err != nil {
		return 0, err
	}
	defer release()
	if err := parser.DecryptPDF(ctx, in, out, password); err != nil {
		if errors.Is(err, parser.ErrWrongPassword) {
			return 0, errWrongPassword
		}
		return 0, err
	}
	sum, err := fileSHA256(out)
	if err != nil {
		return 0, err
	}
	if err := s.checkNewVersion(ctx, row, sum); err != nil {
		return 0, err
	}
	st := staged{path: out, filename: row.OriginalFilename, ext: "pdf", mime: "application/pdf", checksum: sum, size: fileSize(out)}
	return s.swapIn(ctx, scope, row, st, i18n.N("移除了密码"), false, func(who string, n int) string {
		return fmt.Sprintf(i18n.N("%s 移除了“%s”原件的密码（第 %d 版）"), who, row.Title, n)
	})
}

// swapIn makes st the current version: the old current file moves to the
// versions directory, st to the original's path.
func (s *Service) swapIn(ctx context.Context, scope tenant.Scope, row sqlc.Document, st staged, label string, encrypted bool, summary func(who string, n int) string) (int64, error) {
	ws, now := row.WorkspaceID, s.opt.Now()
	actor := audit.ActorFrom(ctx)
	// Uploaded ahead when the storage is on another machine (v1.7.1).
	staged, err := s.files.Stage(ctx, row.Storage, ws, st.path, true)
	if err != nil {
		return 0, err
	}
	defer staged.Discard(ctx, s.files)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	q := s.q.WithTx(tx)
	cur, top, err := s.current(ctx, q, row)
	if err != nil {
		return 0, err
	}
	if err := q.ClearCurrentVersion(ctx, sqlc.ClearCurrentVersionParams{WorkspaceID: ws, DocumentID: row.ID}); err != nil {
		return 0, err
	}
	n := top + 1
	if _, err := q.InsertDocumentVersion(ctx, sqlc.InsertDocumentVersionParams{WorkspaceID: ws, DocumentID: row.ID, Number: int64(n), Label: label,
		OriginalFilename: st.filename, OriginalExt: st.ext, MimeType: st.mime, Checksum: st.checksum, Size: st.size, Encrypted: boolInt(encrypted), Current: 1,
		CreatedBy: sql.NullInt64{Int64: actor.UserID, Valid: actor.UserID != 0}, CreatedAt: now.UnixMilli()}); err != nil {
		return 0, err
	}
	if err := q.SetDocumentOriginal(ctx, sqlc.SetDocumentOriginalParams{OriginalFilename: st.filename, OriginalExt: st.ext, MimeType: st.mime,
		Checksum: st.checksum, OriginalEncrypted: boolInt(encrypted), ModifiedAt: now.UnixMilli(), WorkspaceID: ws, ID: row.ID}); err != nil {
		if isUniqueViolation(err) {
			return 0, errDuplicate
		}
		return 0, err
	}
	if err := audit.Record(ctx, q, audit.Event{WorkspaceID: ws, Action: "document.version_add", TargetType: "document", TargetID: strconv.FormatInt(row.ID, 10),
		Summary: summary(actor.Name, n), At: now}); err != nil {
		return 0, err
	}
	original, parked := files.Original(ws, row.ID, row.OriginalExt), files.Version(ws, row.ID, cur.ID, cur.OriginalExt)
	bctx := context.WithoutCancel(ctx)
	if err := s.files.Rename(ctx, row.Storage, original, parked); err != nil {
		return 0, err
	}
	next := files.Original(ws, row.ID, st.ext)
	if err := staged.Place(ctx, s.files, next); err != nil {
		_ = s.files.Rename(bctx, row.Storage, parked, original)
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		_ = s.files.Remove(bctx, row.Storage, next)
		_ = s.files.Rename(bctx, row.Storage, parked, original)
		return 0, err
	}
	s.pruneVersions(bctx, row) // v1.7.13
	return s.afterSwap(ctx, scope, row)
}

// afterSwap drops the files derived from the old original and queues the
// reprocess that rebuilds them.
func (s *Service) afterSwap(ctx context.Context, scope tenant.Scope, row sqlc.Document) (int64, error) {
	ws := row.WorkspaceID
	for _, r := range []files.Ref{files.Archive(ws, row.ID), files.Base(ws, row.ID)} {
		_ = s.files.Remove(ctx, row.Storage, r)
	}
	_ = os.Remove(s.files.ThumbnailPath(ws, row.ID))
	s.bus.Publish(events.Event{Type: events.DocumentUpdated, WorkspaceID: ws, Data: map[string]any{"documentId": row.ID}})
	return s.Reprocess(ctx, scope, row.ID)
}

// SetCurrentVersion makes an earlier version current again.
func (s *Service) SetCurrentVersion(ctx context.Context, scope tenant.Scope, docID, versionID int64) (int64, error) {
	row, err := s.editable(ctx, scope, docID)
	if err != nil {
		return 0, err
	}
	ws, now := row.WorkspaceID, s.opt.Now()
	target, err := s.q.GetDocumentVersion(ctx, sqlc.GetDocumentVersionParams{WorkspaceID: ws, DocumentID: docID, ID: versionID})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errVersionNotFound
	}
	if err != nil {
		return 0, err
	}
	if target.Current == 1 {
		return 0, errVersionCurrent
	}
	if target.Encrypted == 1 {
		return 0, errEncryptedVersion
	}
	if dup, err := s.q.FindDocumentByChecksum(ctx, sqlc.FindDocumentByChecksumParams{WorkspaceID: ws, Checksum: target.Checksum}); err == nil && dup.ID != row.ID {
		return 0, s.duplicateOf(ctx, ws, dup.ID)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	q := s.q.WithTx(tx)
	cur, _, err := s.current(ctx, q, row)
	if err != nil {
		return 0, err
	}
	if err := q.ClearCurrentVersion(ctx, sqlc.ClearCurrentVersionParams{WorkspaceID: ws, DocumentID: docID}); err != nil {
		return 0, err
	}
	if err := q.SetCurrentVersion(ctx, sqlc.SetCurrentVersionParams{WorkspaceID: ws, ID: target.ID}); err != nil {
		return 0, err
	}
	if err := q.SetDocumentOriginal(ctx, sqlc.SetDocumentOriginalParams{OriginalFilename: target.OriginalFilename, OriginalExt: target.OriginalExt,
		MimeType: target.MimeType, Checksum: target.Checksum, OriginalEncrypted: 0, ModifiedAt: now.UnixMilli(), WorkspaceID: ws, ID: docID}); err != nil {
		return 0, err
	}
	if err := audit.Record(ctx, q, audit.Event{WorkspaceID: ws, Action: "document.version_current", TargetType: "document", TargetID: strconv.FormatInt(docID, 10),
		Summary: fmt.Sprintf(i18n.N("%s 把“%s”的当前版本换成了第 %d 版"), audit.ActorFrom(ctx).Name, row.Title, target.Number), At: now}); err != nil {
		return 0, err
	}
	original, parked := files.Original(ws, docID, row.OriginalExt), files.Version(ws, docID, cur.ID, cur.OriginalExt)
	stored, next := files.Version(ws, docID, target.ID, target.OriginalExt), files.Original(ws, docID, target.OriginalExt)
	bctx := context.WithoutCancel(ctx)
	if err := s.files.Rename(ctx, row.Storage, original, parked); err != nil {
		return 0, err
	}
	if err := s.files.Rename(ctx, row.Storage, stored, next); err != nil {
		_ = s.files.Rename(bctx, row.Storage, parked, original)
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		_ = s.files.Rename(bctx, row.Storage, next, stored)
		_ = s.files.Rename(bctx, row.Storage, parked, original)
		return 0, err
	}
	return s.afterSwap(ctx, scope, row)
}

// DeleteVersion removes an earlier version and its file.
func (s *Service) DeleteVersion(ctx context.Context, scope tenant.Scope, docID, versionID int64) error {
	row, err := s.editable(ctx, scope, docID)
	if err != nil {
		return err
	}
	ws := row.WorkspaceID
	v, err := s.q.GetDocumentVersion(ctx, sqlc.GetDocumentVersionParams{WorkspaceID: ws, DocumentID: docID, ID: versionID})
	if errors.Is(err, sql.ErrNoRows) {
		return errVersionNotFound
	}
	if err != nil {
		return err
	}
	if v.Current == 1 {
		return apperr.New(apperr.KindConflict, "不能删除当前版本")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := s.q.WithTx(tx)
	if err := q.DeleteDocumentVersion(ctx, sqlc.DeleteDocumentVersionParams{WorkspaceID: ws, ID: v.ID}); err != nil {
		return err
	}
	if err := audit.Record(ctx, q, audit.Event{WorkspaceID: ws, Action: "document.version_delete", TargetType: "document", TargetID: strconv.FormatInt(docID, 10),
		Summary: fmt.Sprintf(i18n.N("%s 删除了“%s”的第 %d 版"), audit.ActorFrom(ctx).Name, row.Title, v.Number), At: s.opt.Now()}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := s.files.Remove(context.WithoutCancel(ctx), row.Storage, files.Version(ws, docID, v.ID, v.OriginalExt)); err != nil {
		s.opt.Log.Warn("remove a deleted version's file", "document", docID, "version", v.ID, "err", err)
	}
	return nil
}

// VersionText is a version's text for comparing versions (v1.9.2): the
// current version's is the document's (with what was recognised); an
// earlier one's is read from its file, without recognising anything.
// source is "content", "file" or "none".
func (s *Service) VersionText(ctx context.Context, scope tenant.Scope, docID, versionID int64) (text, source string, err error) {
	row, err := s.document(ctx, scope, docID)
	if err != nil {
		return "", "", err
	}
	current := versionID == 0
	if !current {
		v, err := s.q.GetDocumentVersion(ctx, sqlc.GetDocumentVersionParams{WorkspaceID: scope.WorkspaceID(), DocumentID: docID, ID: versionID})
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", errVersionNotFound
		}
		if err != nil {
			return "", "", err
		}
		if v.Encrypted == 1 {
			return "", "none", nil
		}
		current = v.Current == 1
	}
	if current {
		if strings.TrimSpace(row.Content) == "" {
			return "", "none", nil
		}
		return row.Content, "content", nil
	}
	loc, name, _, err := s.VersionFile(ctx, scope, docID, versionID)
	if err != nil {
		return "", "", err
	}
	dir, err := s.files.NewTmpDir("version-text-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(dir)
	src := filepath.Join(dir, "version")
	if err := s.files.Fetch(ctx, loc.Storage, loc.Ref, src); err != nil {
		return "", "", err
	}
	text, ok, err := parser.ReadText(ctx, src, name)
	if err != nil || !ok {
		return "", "none", nil // unreadable: shown side by side only
	}
	return text, "file", nil
}
