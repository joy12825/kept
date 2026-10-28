package ingest

import (
	"context"
	"fmt"
	"kept/server/internal/files"
	"kept/server/internal/i18n"
	"os"

	"kept/server/internal/apperr"
	"kept/server/internal/document"
	"kept/server/internal/parser"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// A document that came in on its own can become another's new version
// (v1.5.8, after paperless-ngx 3): a renewed contract, an updated policy.
// Its original becomes the current version; absorb then folds the rest of
// it (tags, notes, links) into the document and removes it.

var (
	errVersionSelf      = apperr.New(apperr.KindInvalid, "不能把文档并为它自己的版本")
	errVersionHasOwn    = apperr.New(apperr.KindInvalid, "这份文档自己有多个版本，不能并为另一份的版本")
	errVersionFromEmpty = apperr.New(apperr.KindInvalid, "找不到这份文档的原件")
)

// AddVersionFrom makes document fromID's original the current version of
// docID and returns the reprocess job. label defaults to “取自《标题》”.
func (s *Service) AddVersionFrom(ctx context.Context, scope tenant.Scope, docID, fromID int64, label string, absorb func(ctx context.Context) error) (int64, error) {
	if docID == fromID {
		return 0, errVersionSelf
	}
	row, err := s.editable(ctx, scope, docID)
	if err != nil {
		return 0, err
	}
	src, err := s.editable(ctx, scope, fromID)
	if err != nil {
		return 0, err
	}
	if src.Private == 1 && (row.Private == 0 || row.PersonID != src.PersonID) {
		// a private file would become a version everyone sees (v1.8.0)
		return 0, apperr.New(apperr.KindInvalid, "这是家人的私密文档，只能作为同一位家人的私密文档的版本")
	}
	ws := scope.WorkspaceID()
	var versions int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM document_versions WHERE workspace_id = ? AND document_id = ?`, ws, fromID).Scan(&versions); err != nil {
		return 0, err
	}
	if versions > 1 {
		return 0, errVersionHasOwn
	}
	f, err := s.files.Open(ctx, src.Storage, files.Original(ws, src.ID, src.OriginalExt))
	if err != nil {
		return 0, errVersionFromEmpty
	}
	st, dir, err := s.stage(src.OriginalFilename, f)
	f.Close()
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(dir)
	if parser.IsEncryptedPDF(ctx, st.path) {
		return 0, errVersionEncrypted
	}
	if err := s.checkVersionFrom(ctx, row, src.ID, st.checksum); err != nil {
		return 0, err
	}
	if label == "" {
		label = fmt.Sprintf(i18n.N("取自《%s》"), src.Title)
	}
	// The checksum is unique in the workspace: free it from the document
	// being folded in while it still exists, put it back if the swap fails.
	if _, err := s.db.ExecContext(ctx, `UPDATE documents SET checksum = checksum || '#merging' WHERE workspace_id = ? AND id = ?`, ws, src.ID); err != nil {
		return 0, err
	}
	job, err := s.swapIn(ctx, scope, row, st, label, false, func(who string, n int) string {
		return fmt.Sprintf(i18n.N("%s 把《%s》并为“%s”的第 %d 版"), who, src.Title, row.Title, n)
	})
	if err != nil {
		_, _ = s.db.ExecContext(context.WithoutCancel(ctx), `UPDATE documents SET checksum = ? WHERE workspace_id = ? AND id = ?`, src.Checksum, ws, src.ID)
		return 0, err
	}
	if absorb != nil {
		if err := absorb(ctx); err != nil {
			return job, err
		}
	}
	return job, nil
}

// checkVersionFrom is checkNewVersion where the file may belong to the
// document being folded in.
func (s *Service) checkVersionFrom(ctx context.Context, row sqlc.Document, fromID int64, sum string) error {
	if sum == row.Checksum {
		return apperr.WithCode(apperr.KindConflict, "duplicate", "这和当前版本是同一个文件")
	}
	if n, err := s.q.FindVersionByChecksum(ctx, sqlc.FindVersionByChecksumParams{WorkspaceID: row.WorkspaceID, DocumentID: row.ID, Checksum: sum}); err == nil {
		return apperr.WithCode(apperr.KindConflict, "duplicate", fmt.Sprintf("这和第 %d 版是同一个文件，可以直接把第 %d 版设为当前版本", n, n))
	}
	return nil
}

// SetFieldsWith is Options.SetFields by way of the document service: the
// values are checked as when a person edits them.
func SetFieldsWith(docs *document.Service) func(ctx context.Context, ws, docID int64, fields []FieldPreset) error {
	return func(ctx context.Context, ws, docID int64, fields []FieldPreset) error {
		in := make([]document.FieldInput, len(fields))
		for i, f := range fields {
			in[i] = document.FieldInput{FieldID: f.FieldID, Value: f.Value}
		}
		_, err := docs.Update(ctx, tenant.New(ws, tenant.RoleOwner), docID, document.Patch{CustomFields: in, Origin: &document.FieldSources{}})
		return err
	}
}
