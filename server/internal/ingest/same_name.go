package ingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/files"
	"kept/server/internal/i18n"
	"kept/server/internal/privacy"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// Same-name uploads (v1.7.13): a file uploaded under the name of a
// document already in the same drawer can become that document's new
// version instead of a document of its own. The web page asks; a drawer
// can be set to do it without asking. Mail and upload links always make
// new documents. A file put into the consume folder again under the same
// path can do the same, when the instance turns that on.

// What to do with an upload named like a document in its drawer.
const (
	ConflictDefault = ""        // as the drawer is set: a new version when it says so
	ConflictVersion = "version" // a new version of that document
	ConflictNew     = "new"     // a document of its own
)

// Match is a document a file of the same name would become a version of.
type Match struct {
	Name       string
	DocumentID int64
	Title      string
}

var errNoDrawer = apperr.New(apperr.KindNotFound, "找不到这个抽屉，它可能已被删除")

// drawerAuto reports whether drawerID (0: no drawer) makes same-name
// uploads new versions without asking.
func (s *Service) drawerAuto(ctx context.Context, ws, drawerID int64) (bool, error) {
	if drawerID == 0 {
		return false, nil
	}
	var auto int64
	err := s.db.QueryRowContext(ctx, `SELECT same_name_versions FROM drawers WHERE workspace_id = ? AND id = ?`, ws, drawerID).Scan(&auto)
	if errors.Is(err, sql.ErrNoRows) {
		return false, errNoDrawer
	}
	return auto == 1, err
}

// sameName is the latest document in drawerID (0: in no drawer) whose
// original has the name; 0 when there is none. Only documents the scope
// may change count: a family member's private document is not someone
// else's to add versions to (v1.8.0).
func (s *Service) sameName(ctx context.Context, scope tenant.Scope, drawerID int64, name string) (Match, error) {
	m := Match{Name: name}
	edit, eargs := privacy.Editable("d", scope)
	where, args := `d.drawer_id IS NULL`, append([]any{scope.WorkspaceID(), name}, eargs...)
	if drawerID != 0 {
		where, args = `d.drawer_id = ?`, append(args, drawerID)
	}
	err := s.db.QueryRowContext(ctx, `SELECT d.id, d.title FROM documents d
		WHERE d.workspace_id = ? AND d.original_filename = ? AND d.deleted_at IS NULL AND `+edit+` AND `+where+`
		ORDER BY d.id DESC LIMIT 1`, args...).Scan(&m.DocumentID, &m.Title)
	if errors.Is(err, sql.ErrNoRows) {
		return m, nil
	}
	return m, err
}

// SameName finds the documents in drawerID (0: in no drawer) that files
// with these names would become new versions of, and whether the drawer
// does so without asking.
func (s *Service) SameName(ctx context.Context, scope tenant.Scope, drawerID int64, names []string) ([]Match, bool, error) {
	if len(names) > 500 {
		return nil, false, apperr.New(apperr.KindInvalid, "一次最多检查 500 个文件")
	}
	ws := scope.WorkspaceID()
	auto, err := s.drawerAuto(ctx, ws, drawerID)
	if err != nil {
		return nil, false, err
	}
	out := []Match{}
	seen := map[string]bool{}
	for _, n := range names {
		n = cleanFilename(n)
		if seen[n] {
			continue
		}
		seen[n] = true
		m, err := s.sameName(ctx, scope, drawerID, n)
		if err != nil {
			return nil, false, err
		}
		if m.DocumentID != 0 {
			out = append(out, m)
		}
	}
	return out, auto, nil
}

// VersionTarget is the document an upload named filename into drawerID
// becomes a new version of, 0 for a document of its own.
func (s *Service) VersionTarget(ctx context.Context, scope tenant.Scope, drawerID int64, filename, onConflict string) (int64, error) {
	switch onConflict {
	case ConflictNew:
		return 0, nil
	case ConflictDefault:
		auto, err := s.drawerAuto(ctx, scope.WorkspaceID(), drawerID)
		if err != nil || !auto {
			return 0, err
		}
	case ConflictVersion:
	default:
		return 0, apperr.New(apperr.KindInvalid, "onConflict 只能是 version 或 new")
	}
	m, err := s.sameName(ctx, scope, drawerID, cleanFilename(filename))
	return m.DocumentID, err
}

// DocumentAt is the document that came from path in its workspace's
// consume folder, 0 when there is none.
func (s *Service) DocumentAt(ctx context.Context, ws int64, path string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM documents WHERE workspace_id = ? AND origin_path = ? AND deleted_at IS NULL
		ORDER BY id DESC LIMIT 1`, ws, path).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

const watchVersionsSetting = "watch_versions"

// WatchVersions reports whether a file put into the consume folder again
// under the same path becomes a new version.
func (s *Service) WatchVersions(ctx context.Context) (bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, watchVersionsSetting).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return v == "1", err
}

// SetWatchVersions turns that on or off.
func (s *Service) SetWatchVersions(ctx context.Context, on bool) error {
	if !on {
		_, err := s.db.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, watchVersionsSetting)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, '1') ON CONFLICT (key) DO UPDATE SET value = excluded.value`, watchVersionsSetting)
	return err
}

// KeepVersions is the most earlier versions a workspace's documents keep;
// 0 keeps them all.
func (s *Service) KeepVersions(ctx context.Context, ws int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT keep_versions FROM workspaces WHERE id = ?`, ws).Scan(&n)
	return n, err
}

// SetKeepVersions changes it; documents with more drop the oldest the
// next time they get a version.
func (s *Service) SetKeepVersions(ctx context.Context, scope tenant.Scope, n int) error {
	if n < 0 || n > 1000 {
		return apperr.New(apperr.KindInvalid, "保留的旧版本数应在 0 到 1000 之间，0 为全部保留")
	}
	ws := scope.WorkspaceID()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE workspaces SET keep_versions = ? WHERE id = ?`, n, ws); err != nil {
		return err
	}
	actor := audit.ActorFrom(ctx)
	summary := fmt.Sprintf(i18n.N("%s 设为全部保留旧版本"), actor.Name)
	if n > 0 {
		summary = fmt.Sprintf(i18n.N("%s 设为每份文档最多保留 %d 个旧版本"), actor.Name, n)
	}
	if err := audit.Record(ctx, s.q.WithTx(tx), audit.Event{WorkspaceID: ws, Action: "workspace.keep_versions", TargetType: "workspace",
		TargetID: strconv.FormatInt(ws, 10), Summary: summary, At: s.opt.Now()}); err != nil {
		return err
	}
	return tx.Commit()
}

// pruneVersions drops a document's oldest earlier versions beyond what its
// workspace keeps. Failing leaves them; the next version tries again.
func (s *Service) pruneVersions(ctx context.Context, row sqlc.Document) {
	ws := row.WorkspaceID
	keep, err := s.KeepVersions(ctx, ws)
	if err != nil || keep <= 0 {
		return
	}
	rows, err := s.q.ListDocumentVersions(ctx, sqlc.ListDocumentVersionsParams{WorkspaceID: ws, DocumentID: row.ID})
	if err != nil {
		s.opt.Log.Warn("list versions to prune", "document", row.ID, "err", err)
		return
	}
	var old []sqlc.ListDocumentVersionsRow // newest first
	for _, r := range rows {
		if r.Current != 1 {
			old = append(old, r)
		}
	}
	if len(old) <= keep {
		return
	}
	drop := old[keep:]
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()
	q := s.q.WithTx(tx)
	for _, v := range drop {
		if err := q.DeleteDocumentVersion(ctx, sqlc.DeleteDocumentVersionParams{WorkspaceID: ws, ID: v.ID}); err != nil {
			return
		}
	}
	if err := audit.Record(ctx, q, audit.Event{WorkspaceID: ws, Action: "document.version_prune", TargetType: "document", TargetID: strconv.FormatInt(row.ID, 10),
		Summary: fmt.Sprintf(i18n.N("按空间的设置（最多保留 %d 个旧版本）删除了“%s”最早的 %d 个旧版本"), keep, row.Title, len(drop)), At: s.opt.Now()}); err != nil {
		return
	}
	if err := tx.Commit(); err != nil {
		s.opt.Log.Warn("prune versions", "document", row.ID, "err", err)
		return
	}
	for _, v := range drop {
		if err := s.files.Remove(ctx, row.Storage, files.Version(ws, row.ID, v.ID, v.OriginalExt)); err != nil {
			s.opt.Log.Warn("remove a pruned version's file", "document", row.ID, "version", v.ID, "err", err)
		}
	}
}
