// Package storage manages where documents' files are kept (v1.7.0): the
// storages an instance has (the data directory "local" and those of
// KEPT_STORAGES), the storage each workspace writes new documents to, and
// the job that moves a workspace's existing documents to it.
//
// A document's files — original, searchable PDF, unedited base, earlier
// versions — are always in one storage, named on the document. Moving a
// document copies each file to the new storage, reads it back to compare
// checksums, switches the document over, and only then removes the old
// copies; a failure at any step leaves the document where it was.
package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/config"
	"kept/server/internal/files"
	"kept/server/internal/i18n"
	"kept/server/internal/jobs"
	"kept/server/internal/secret"
	"kept/server/internal/store/sqlc"
)

// KindMigrate moves a workspace's documents to its storage.
const KindMigrate = "storage.migrate"

type Options struct {
	Queue *jobs.Queue
	Log   *slog.Logger
	Now   func() time.Time
	// v1.7.2: the storages of the deployment (KEPT_STORAGES), which the
	// page shows but cannot change; the master key sealing the keys of
	// those added on the page; the data directory no storage may be in.
	Env     []config.Storage
	Box     *secret.Box
	DataDir string
}

type Service struct {
	db     *sql.DB
	q      *sqlc.Queries
	files  *files.Store
	opt    Options
	broken brokenSet // saved storages that could not be set up
}

func NewService(db *sql.DB, fs *files.Store, opt Options) *Service {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Log == nil {
		opt.Log = slog.New(slog.DiscardHandler)
	}
	return &Service{db: db, q: sqlc.New(db), files: fs, opt: opt}
}

// Where a storage is configured.
const (
	SourceLocal = "local" // the data directory
	SourceEnv   = "env"   // KEPT_STORAGES
	SourcePage  = "page"  // added on the page (v1.7.2)
)

// Info is a storage in the administrator's list.
type Info struct {
	Name        string
	Kind        string // local | dir | s3 | webdav | missing (documents name it, it is not configured)
	Source      string // local | env | page; "" for missing
	Location    string // where it keeps files, without secrets
	Description string
	Selectable  bool      // workspace administrators may pick it
	Settings    *Settings // added on the page: what the edit form shows (no secret key)
	Error       string    // added on the page and could not be set up
	Documents   int       // the trash included
	Used        int64     // what its documents take (v1.7.10)
	Capacity    int64     // the most it may hold; 0 no limit
	Workspaces  []string
	Encryption  *Encryption // nil: never encrypted (v1.11.0)
}

func kindOf(name string, b files.Backend) string {
	if name == files.Local {
		return "local"
	}
	switch files.Unwrap(b).(type) {
	case files.DirBackend:
		return "dir"
	case *files.S3Backend:
		return "s3"
	case *files.WebDAVBackend:
		return "webdav"
	}
	return "other"
}

// List returns the configured storages ("local" first), then any that
// documents still name but are no longer configured.
func (s *Service) List(ctx context.Context) ([]Info, error) {
	counts := map[string]int{}
	rows, err := s.db.QueryContext(ctx, `SELECT storage, count(*) FROM documents GROUP BY storage`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name string
		var n int
		if err := rows.Scan(&name, &n); err != nil {
			rows.Close()
			return nil, err
		}
		counts[name] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	using := map[string][]string{}
	wrows, err := s.db.QueryContext(ctx, `SELECT storage, name FROM workspaces WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for wrows.Next() {
		var st, name string
		if err := wrows.Scan(&st, &name); err != nil {
			wrows.Close()
			return nil, err
		}
		using[st] = append(using[st], name)
	}
	wrows.Close()
	if err := wrows.Err(); err != nil {
		return nil, err
	}
	// Storages added on the page.
	saved := map[string]savedRow{}
	srows, err := s.db.QueryContext(ctx, `SELECT name, kind, description, settings, selectable FROM storages`)
	if err != nil {
		return nil, err
	}
	for srows.Next() {
		var name string
		var r savedRow
		var sel int64
		if err := srows.Scan(&name, &r.kind, &r.description, &r.settings, &sel); err != nil {
			srows.Close()
			return nil, err
		}
		r.selectable = sel == 1
		saved[name] = r
	}
	srows.Close()
	if err := srows.Err(); err != nil {
		return nil, err
	}
	var out []Info
	seen := map[string]bool{}
	for _, name := range s.files.Storages() {
		b, err := s.files.Backend(name)
		if err != nil {
			continue // removed meanwhile
		}
		seen[name] = true
		info := Info{Name: name, Kind: kindOf(name, b), Location: b.Describe(), Selectable: true, Documents: counts[name], Workspaces: nonNil(using[name])}
		switch r, ok := saved[name]; {
		case name == files.Local:
			info.Source = SourceLocal
		case ok && !s.isEnv(name):
			info.Source, info.Description, info.Selectable = SourcePage, r.description, r.selectable
			info.Settings = settingsOf(r.settings)
		default: // KEPT_STORAGES
			info.Source = SourceEnv
		}
		out = append(out, info)
	}
	// Added on the page but not set up (the master key changed).
	for name, r := range saved {
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, Info{Name: name, Kind: r.kind, Source: SourcePage, Description: r.description, Selectable: r.selectable,
			Settings: settingsOf(r.settings), Error: s.broken.get(name), Documents: counts[name], Workspaces: nonNil(using[name])})
	}
	enc, err := s.encryptionOf(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Kind == "missing" {
			continue
		}
		out[i].Encryption = enc[out[i].Name]
		if out[i].Used, err = s.usedOf(ctx, out[i].Name); err != nil {
			return nil, err
		}
		if out[i].Capacity, err = s.capacityOf(ctx, out[i].Name); err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool { // the data directory first, then by name
		if a, b := out[i].Name == files.Local, out[j].Name == files.Local; a || b {
			return a && !b
		}
		return out[i].Name < out[j].Name
	})
	var missing []string
	for name := range counts {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	for name := range using {
		if !seen[name] && counts[name] == 0 {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	for _, name := range missing {
		out = append(out, Info{Name: name, Kind: "missing", Documents: counts[name], Workspaces: nonNil(using[name])})
	}
	return out, nil
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// Test writes a small file to a storage, reads it back and removes it.
func (s *Service) Test(ctx context.Context, name string) (time.Duration, error) {
	b, err := s.files.Backend(name)
	if err != nil {
		if msg := s.broken.get(name); msg != "" {
			return 0, errors.New(msg)
		}
		return 0, apperr.New(apperr.KindNotFound, "没有这个存储")
	}
	return s.probe(ctx, b)
}

// probe writes, reads back and removes a small file, within 20 seconds.
func (s *Service) probe(ctx context.Context, b files.Backend) (time.Duration, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	dir, err := s.files.NewTmpDir("probe-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(dir)
	want := "kept " + strconv.FormatInt(s.opt.Now().UnixNano(), 10)
	src := filepath.Join(dir, "probe")
	if err := os.WriteFile(src, []byte(want), 0o644); err != nil {
		return 0, err
	}
	key := ".kept-probe/" + strconv.FormatInt(s.opt.Now().UnixNano(), 36)
	if err := b.Put(ctx, key, src); err != nil {
		return 0, fmt.Errorf(i18n.N("写入失败：%w"), err)
	}
	defer b.Remove(context.WithoutCancel(ctx), key)
	f, err := b.Open(ctx, key)
	if err != nil {
		return 0, fmt.Errorf(i18n.N("读取失败：%w"), err)
	}
	got, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		return 0, fmt.Errorf(i18n.N("读取失败：%w"), err)
	}
	if string(got) != want {
		return 0, errors.New(i18n.N("读回的内容和写入的不一致"))
	}
	if err := b.Remove(ctx, key); err != nil {
		return 0, fmt.Errorf(i18n.N("删除失败：%w"), err)
	}
	return time.Since(start), nil
}

// Workspace is a workspace's storage and how far moving its documents
// there has come.
type Workspace struct {
	Storage   string
	Cold      string // cold storage (v1.7.8); "" none
	ColdAfter int    // months
	Pending   int    // documents (the trash included) still in another storage
	Migrating bool   // a move is queued or running
	Progress  *Moved // of the running move
	LastError string // of the last move, when it failed
	Last      *Done  // the last finished move (v1.7.14)
}

// Done is a finished move: its job, when it ended and what it did.
type Done struct {
	JobID      int64
	FinishedAt time.Time
	Moved
}

// Moved is a move's progress and result.
type Moved struct {
	Total   int `json:"total"`
	Moved   int `json:"moved"`
	Skipped int `json:"skipped"` // busy (being processed) or changed meanwhile; tried again later
	Failed  int `json:"failed"`
	Sealed  int `json:"sealed,omitempty"` // private documents' files sealed onto remote storage (v1.8.1)
}

// Workspaces returns the storage of every workspace, by ID.
func (s *Service) Workspaces(ctx context.Context) (map[int64]Workspace, error) {
	out := map[int64]Workspace{}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM workspaces WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		p, err := s.planOf(ctx, s.db, id)
		if err != nil {
			return nil, err
		}
		n, err := s.pendingOf(ctx, s.db, id, p)
		if err != nil {
			return nil, err
		}
		out[id] = Workspace{Storage: p.hot, Cold: p.cold, ColdAfter: p.months, Pending: n}
	}
	// The newest move of each workspace.
	jrows, err := s.db.QueryContext(ctx, `SELECT j.workspace_id, j.status, j.error, j.result FROM jobs j
		WHERE j.kind = ? AND j.id = (SELECT max(k.id) FROM jobs k WHERE k.kind = j.kind AND k.workspace_id = j.workspace_id)`, KindMigrate)
	if err != nil {
		return nil, err
	}
	defer jrows.Close()
	for jrows.Next() {
		var ws int64
		var status string
		var jerr, result sql.NullString
		if err := jrows.Scan(&ws, &status, &jerr, &result); err != nil {
			return nil, err
		}
		w, ok := out[ws]
		if !ok {
			continue
		}
		switch status {
		case "queued", "running":
			w.Migrating = true
			// a retry waiting its turn still shows the last error
			w.LastError = jerr.String
			var m Moved
			if result.Valid && json.Unmarshal([]byte(result.String), &m) == nil && m.Total > 0 {
				w.Progress = &m
			}
		case "failed":
			if w.Pending > 0 {
				w.LastError = jerr.String
			}
		}
		out[ws] = w
	}
	if err := jrows.Err(); err != nil {
		return nil, err
	}
	// The newest finished move of each workspace (v1.7.14).
	drows, err := s.db.QueryContext(ctx, `SELECT j.workspace_id, j.id, j.finished_at, j.result FROM jobs j
		WHERE j.kind = ? AND j.status = 'succeeded' AND j.id = (SELECT max(k.id) FROM jobs k
		  WHERE k.kind = j.kind AND k.workspace_id = j.workspace_id AND k.status = 'succeeded')`, KindMigrate)
	if err != nil {
		return nil, err
	}
	defer drows.Close()
	for drows.Next() {
		var ws, id int64
		var at sql.NullInt64
		var result sql.NullString
		if err := drows.Scan(&ws, &id, &at, &result); err != nil {
			return nil, err
		}
		w, ok := out[ws]
		if !ok || !at.Valid {
			continue
		}
		d := Done{JobID: id, FinishedAt: time.UnixMilli(at.Int64)}
		_ = json.Unmarshal([]byte(result.String), &d.Moved)
		w.Last = &d
		out[ws] = w
	}
	return out, drows.Err()
}

type migratePayload struct {
	Target string `json:"target"`
}

// SetWorkspace makes a workspace write new documents to storage name;
// with move, its existing documents are moved there too (a job). It
// returns the job, or 0 when nothing needs moving.
func (s *Service) SetWorkspace(ctx context.Context, ws int64, name string, move bool) (int64, error) {
	if _, err := s.files.Backend(name); err != nil {
		return 0, apperr.New(apperr.KindInvalid, fmt.Sprintf("没有这个存储：%s", name))
	}
	if s.isBackupTarget(ctx, name) {
		return 0, errBackupTarget(name)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	q := s.q.WithTx(tx)
	var wsName, old string
	err = tx.QueryRowContext(ctx, `SELECT name, storage FROM workspaces WHERE id = ? AND deleted_at IS NULL`, ws).Scan(&wsName, &old)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, apperr.New(apperr.KindNotFound, "找不到这个空间")
	}
	if err != nil {
		return 0, err
	}
	var cold string
	if err := tx.QueryRowContext(ctx, `SELECT cold_storage FROM workspaces WHERE id = ?`, ws).Scan(&cold); err != nil {
		return 0, err
	}
	if cold != "" && cold == name {
		return 0, apperr.New(apperr.KindInvalid, "这是空间的冷存储；要用它存新文档，请先取消冷存储")
	}
	if _, err := q.SetWorkspaceStorage(ctx, sqlc.SetWorkspaceStorageParams{Storage: name, ID: ws}); err != nil {
		return 0, err
	}
	if old != name {
		if err := audit.Record(ctx, q, audit.Event{Action: "workspace.storage", TargetType: "workspace", TargetID: strconv.FormatInt(ws, 10),
			Summary: fmt.Sprintf(i18n.N("%s 把空间“%s”的存储设为“%s”"), audit.ActorFrom(ctx).Name, wsName, name), At: s.opt.Now()}); err != nil {
			return 0, err
		}
	}
	var jobID int64
	if move {
		p, err := s.planOf(ctx, tx, ws)
		if err != nil {
			return 0, err
		}
		if jobID, err = s.queueMove(ctx, tx, q, ws, p); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	if jobID != 0 {
		s.opt.Queue.Wake()
	}
	return jobID, nil
}

// Register handles the move job (the cold storage check is RegisterTiers,
// in the official build).
func (s *Service) Register(p *jobs.Pool) {
	p.Handle(KindMigrate, s.migrate)
	p.Handle(KindEncrypt, s.convert)
}

type docRow struct {
	id       int64
	ext      string
	storage  string
	modified int64
	date     string // the document's date: hot or cold (v1.7.8)
	private  bool   // a family member's private document stays here (v1.8.1)
}

func (s *Service) migrate(ctx context.Context, job jobs.Job) (any, error) {
	if job.WorkspaceID == nil {
		return nil, jobs.Permanent(errors.New("no workspace"))
	}
	ws := *job.WorkspaceID
	// Where documents belong now: a later change wins over the payload.
	p, err := s.planOf(ctx, s.db, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, jobs.Permanent(errors.New(i18n.N("找不到这个空间")))
	}
	if err != nil {
		return nil, err
	}
	for _, target := range []string{p.hot, p.cold} {
		if target == "" {
			continue
		}
		if _, err := s.files.Backend(target); err != nil {
			return nil, jobs.Permanent(fmt.Errorf(i18n.N("存储“%s”没有配置"), target))
		}
		if s.isBackupTarget(ctx, target) {
			return nil, jobs.Permanent(errBackupTarget(target))
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, original_ext, storage, modified_at, created_date, private FROM documents WHERE workspace_id = ? ORDER BY id`, ws)
	if err != nil {
		return nil, err
	}
	var docs []docRow
	for rows.Next() {
		var d docRow
		if err := rows.Scan(&d.id, &d.ext, &d.storage, &d.modified, &d.date, &d.private); err != nil {
			rows.Close()
			return nil, err
		}
		if p.want(d.date, d.private) != d.storage {
			docs = append(docs, d)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tmp, err := s.files.NewTmpDir("migrate-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	res := Moved{Total: len(docs)}
	var lastErr error
	for _, d := range docs {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		// Changed meanwhile: the retry follows the new settings.
		if now, err := s.planOf(ctx, s.db, ws); err != nil {
			return res, err
		} else if !now.same(p) {
			return res, errors.New(i18n.N("空间的存储设置改了，稍后按新的设置继续"))
		}
		target := p.want(d.date, d.private)
		// A full storage takes no more (v1.7.10): stop, try again later.
		if size, err := s.docSize(ctx, d.id); err != nil {
			return res, err
		} else if err := s.roomIn(ctx, target, size); err != nil {
			return res, jobs.Permanent(err) // stays full until someone acts
		}
		moved, err := s.moveDocument(ctx, ws, d, target, tmp)
		switch {
		case err != nil:
			res.Failed++
			lastErr = err
			s.opt.Log.Warn("move a document's files", "workspace", ws, "document", d.id, "from", d.storage, "to", target, "err", err)
		case moved:
			res.Moved++
		default:
			res.Skipped++
		}
		_ = s.opt.Queue.SetProgress(ctx, job.ID, res)
	}
	// private documents' sealed copies (v1.8.1)
	sealed, failed, err := s.sealAll(ctx, ws, p, tmp)
	res.Sealed = sealed
	if err != nil && failed == 0 {
		return res, err
	}
	if failed > 0 {
		res.Failed += failed
		lastErr = err
	}
	switch {
	case res.Failed > 0:
		return res, fmt.Errorf(i18n.N("%d 份文档没能移过去，稍后会再试。例如：%w"), res.Failed, lastErr)
	case res.Skipped > 0:
		return res, fmt.Errorf(i18n.N("%d 份文档正在处理或刚改过，稍后会再试"), res.Skipped)
	}
	s.opt.Log.Info("moved a workspace's documents", "workspace", ws, "hot", p.hot, "cold", p.cold, "documents", res.Moved)
	return res, nil
}

// moveDocument moves one document's files; false means it was busy or
// changed meanwhile and stays where it is for now.
func (s *Service) moveDocument(ctx context.Context, ws int64, d docRow, target, tmp string) (bool, error) {
	var busy int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE document_id = ? AND status IN ('queued', 'running')`, d.id).Scan(&busy); err != nil {
		return false, err
	}
	if busy > 0 {
		return false, nil
	}
	refs := []files.Ref{files.Original(ws, d.id, d.ext), files.Archive(ws, d.id), files.Base(ws, d.id)}
	versions, err := s.files.Versions(ctx, d.storage, ws, d.id)
	if err != nil {
		return false, err
	}
	refs = append(refs, versions...)

	bctx := context.WithoutCancel(ctx)
	var copied []files.Ref
	undo := func() {
		for _, r := range copied {
			_ = s.files.Remove(bctx, target, r)
		}
	}
	for i, r := range refs {
		if i > 0 && !s.files.Exists(ctx, d.storage, r) { // only the original must be there
			continue
		}
		if err := s.copyVerified(ctx, d.storage, target, r, tmp); err != nil {
			undo()
			return false, err
		}
		copied = append(copied, r)
	}
	// Switch over unless the document changed while its files were copied
	// (a new version, page edits): then it is tried again later.
	res, err := s.db.ExecContext(ctx, `UPDATE documents SET storage = ? WHERE id = ? AND storage = ? AND modified_at = ?`, target, d.id, d.storage, d.modified)
	if err != nil {
		undo()
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		undo()
		return false, nil
	}
	for _, r := range copied {
		if err := s.files.Remove(bctx, d.storage, r); err != nil {
			s.opt.Log.Warn("remove a moved file from the old storage", "storage", d.storage, "key", r.Key(), "err", err)
		}
	}
	return true, nil
}

// copyVerified copies one file between storages and reads the copy back
// to compare checksums.
func (s *Service) copyVerified(ctx context.Context, from, to string, r files.Ref, tmp string) error {
	src, release, err := s.files.Local(ctx, from, r, tmp)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf(i18n.N("%s 不在存储“%s”里"), r.Key(), from)
	}
	if err != nil {
		return fmt.Errorf(i18n.N("读取 %s：%w"), r.Key(), err)
	}
	defer release()
	want, err := fileSum(src)
	if err != nil {
		return err
	}
	if err := s.files.Put(ctx, to, r, src, false); err != nil {
		return fmt.Errorf(i18n.N("写入 %s：%w"), r.Key(), err)
	}
	f, err := s.files.Open(ctx, to, r)
	if err != nil {
		return fmt.Errorf(i18n.N("读回 %s：%w"), r.Key(), err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf(i18n.N("读回 %s：%w"), r.Key(), err)
	}
	if hex.EncodeToString(h.Sum(nil)) != want {
		_ = s.files.Remove(context.WithoutCancel(ctx), to, r)
		return fmt.Errorf(i18n.N("%s 复制后校验值不符"), r.Key())
	}
	return nil
}

func fileSum(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func settingsOf(raw string) *Settings {
	var set Settings
	_ = json.Unmarshal([]byte(raw), &set)
	return &set
}
