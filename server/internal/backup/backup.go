// Package backup makes consistent online backups of a kept data directory
// and restores them into an empty one (design doc 11.13).
//
// A backup directory holds database snapshots (db/app-<time>.db, the
// newest Keep kept), the files those snapshots reference (media/, same
// layout as the data directory), the logo (branding/) and a manifest
// (kept-backup.json). The search index is not backed up: the server
// rebuilds it.
package backup

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
	"slices"
	"strings"
	"sync"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"

	"kept/server/internal/files"
	"kept/server/internal/i18n"
	"kept/server/internal/jobs"
	"kept/server/internal/store"
	"kept/server/internal/version"
)

// Kind is the job that runs the daily backup (KEPT_BACKUP_DIR).
const Kind = "system.backup"

// DefaultKeep is how many database snapshots a backup directory keeps.
const DefaultKeep = 7

const (
	manifestName = "kept-backup.json"
	lockName     = ".kept-backup.lock"
	snapPrefix   = "app-"
	snapLayout   = "20060102-150405"
)

// Manifest describes the latest backup in a directory.
type Manifest struct {
	Format    int       `json:"format"`
	Version   string    `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	Snapshot  string    `json:"snapshot"`
	Snapshots []string  `json:"snapshots"`
	Documents int       `json:"documents"`
	Files     int       `json:"files"`
	Bytes     int64     `json:"bytes"`
}

// Result is what one backup did.
type Result struct {
	Dir       string   `json:"dir"`
	Snapshot  string   `json:"snapshot"`
	Documents int      `json:"documents"`
	Files     int      `json:"files"`  // referenced by the snapshot
	Copied    int      `json:"copied"` // new or changed this time
	Bytes     int64    `json:"bytes"`  // copied this time
	Missing   []string `json:"missing,omitempty"`
	// External counts the files of documents in a storage this backup
	// could not read: not configured here (the command line without its
	// keys), or the backup target itself. Since v1.7.6 files in other
	// storages are backed up too, under storages/<name>/.
	External int `json:"external,omitempty"`
	// PrivateLeft counts the files of private documents a remote backup
	// does not take: their person has no key yet, and thumbnails (v1.8.1).
	PrivateLeft int           `json:"privateLeft,omitempty"`
	Pruned      int           `json:"pruned"` // files no retained snapshot needs any more
	Took        time.Duration `json:"took"`
}

type Options struct {
	DataDir string
	Keep    int              // snapshots kept; default DefaultKeep
	Now     func() time.Time // default time.Now
	// Files reads documents kept in other storages (v1.7.6); nil backs up
	// the data directory only.
	Files *files.Store
}

var running sync.Mutex // one backup at a time in this process

// snapshotDB writes the live database into the SQLite file path: VACUUM
// INTO on SQLite; on Postgres its rows are copied into a SQLite database
// (v1.9.10), so backups and restores are the same on both.
func snapshotDB(ctx context.Context, db *sql.DB, path string) error {
	if store.IsPostgres(db) {
		return store.SnapshotPostgres(ctx, db, path)
	}
	_, err := db.ExecContext(ctx, `VACUUM INTO ?`, path)
	return err
}

// Run backs up the live database db and the data directory into dir.
func Run(ctx context.Context, db *sql.DB, dir string, opt Options) (Result, error) {
	start := time.Now()
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Keep <= 0 {
		opt.Keep = DefaultKeep
	}
	dir, err := checkDir(dir, opt.DataDir)
	if err != nil {
		return Result{}, err
	}
	if !running.TryLock() {
		return Result{}, errors.New(i18n.N("另一次备份正在进行"))
	}
	defer running.Unlock()
	unlock, err := lockDir(dir, opt.Now())
	if err != nil {
		return Result{}, err
	}
	defer unlock()

	// 1. The database snapshot.
	if err := os.MkdirAll(filepath.Join(dir, "db"), 0o755); err != nil {
		return Result{}, err
	}
	name := snapPrefix + opt.Now().Format(snapLayout) + ".db"
	snap := filepath.Join(dir, "db", name)
	part := snap + ".part"
	os.Remove(part)
	if err := snapshotDB(ctx, db, part); err != nil {
		os.Remove(part)
		return Result{}, fmt.Errorf(i18n.N("数据库快照失败：%w"), err)
	}
	if err := os.Rename(part, snap); err != nil {
		return Result{}, err
	}
	res := Result{Dir: dir, Snapshot: name}

	// 2. The files the snapshot references.
	list, docs, err := snapshotFiles(ctx, snap)
	if err != nil {
		return res, err
	}
	res.Documents = docs
	// A file about to be overwritten with new content keeps its old
	// content under replaced/<the last snapshot that had it>/ (v1.7.12),
	// so every retained snapshot restores as it was.
	keepOld := func(rel string) func() error { return func() error { return nil } }
	if snaps, err := Snapshots(dir); err == nil {
		if i := slices.Index(snaps, name); i > 0 {
			owner := strings.TrimSuffix(snaps[i-1], ".db")
			keepOld = func(rel string) func() error {
				return func() error { return preserve(dir, owner, rel) }
			}
		}
	}
	var refs []string
	for _, f := range list {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		var copied bool
		var n int64
		var err error
		if f.local() {
			copied, n, err = syncFile(filepath.Join(opt.DataDir, f.rel), filepath.Join(dir, f.rel), keepOld(f.rel))
		} else if !opt.canRead(f.storage) {
			res.External++
			continue
		} else {
			copied, n, err = syncStored(ctx, opt.Files, f, filepath.Join(dir, f.rel), keepOld(f.rel))
		}
		refs = append(refs, f.rel)
		rel := f.rel
		switch {
		case errors.Is(err, fs.ErrNotExist):
			res.Missing = append(res.Missing, rel)
		case err != nil:
			return res, err
		case copied:
			res.Copied++
			res.Bytes += n
		}
	}
	res.Files = len(refs)
	if err := mirror(filepath.Join(opt.DataDir, "branding"), filepath.Join(dir, "branding")); err != nil {
		return res, err
	}

	// 3. Rotation: keep the newest snapshots and the files they need.
	snaps, err := Snapshots(dir)
	if err != nil {
		return res, err
	}
	for len(snaps) > opt.Keep {
		if err := os.Remove(filepath.Join(dir, "db", snaps[0])); err != nil {
			return res, err
		}
		snaps = snaps[1:]
	}
	if res.Pruned, err = prune(ctx, dir, snaps); err != nil {
		return res, err
	}
	if n, err := pruneReplaced(dir, snaps); err != nil {
		return res, err
	} else {
		res.Pruned += n
	}

	var total int64
	for _, rel := range refs {
		if st, err := os.Stat(filepath.Join(dir, rel)); err == nil {
			total += st.Size()
		}
	}
	m := Manifest{Format: 1, Version: version.Version, CreatedAt: opt.Now(), Snapshot: name, Snapshots: snaps,
		Documents: docs, Files: len(refs) - len(res.Missing), Bytes: total}
	if err := writeJSON(filepath.Join(dir, manifestName), m); err != nil {
		return res, err
	}
	res.Took = time.Since(start).Round(time.Millisecond)
	return res, nil
}

// checkDir makes dir absolute and refuses one inside the data directory.
func checkDir(dir, dataDir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", errors.New(i18n.N("请指定备份目录"))
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	data, err := filepath.Abs(dataDir)
	if err != nil {
		return "", err
	}
	if rel, err := filepath.Rel(data, abs); err == nil && (rel == "." || !strings.HasPrefix(rel, "..")) {
		return "", fmt.Errorf(i18n.N("备份目录不能在数据目录（%s）里面"), data)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", err
	}
	return abs, nil
}

// lockDir keeps two backups (say a scheduled one and `kept backup`) out of
// the same directory. A lock older than a day is taken as left over.
func lockDir(dir string, now time.Time) (func(), error) {
	p := filepath.Join(dir, lockName)
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, "%d %s\n", os.Getpid(), now.Format(time.RFC3339))
			f.Close()
			return func() { os.Remove(p) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		if st, serr := os.Stat(p); serr == nil && now.Sub(st.ModTime()) > 24*time.Hour {
			os.Remove(p)
			continue
		}
		break
	}
	return nil, errors.New(i18n.N("这个目录里另一次备份正在进行"))
}

// Snapshots lists a backup directory's database snapshots, oldest first.
func Snapshots(dir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(dir, "db"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if n := e.Name(); !e.IsDir() && strings.HasPrefix(n, snapPrefix) && strings.HasSuffix(n, ".db") {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out, nil
}

// ReadManifest reads a backup directory's manifest.
func ReadManifest(dir string) (Manifest, error) {
	var m Manifest
	b, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(b, &m)
}

// docFile is one file a snapshot references.
type docFile struct {
	rel      string    // in the backup: the data directory's path, or storages/<name>/<key> (v1.7.6)
	checksum string    // originals only: SHA-256 recorded at upload
	storage  string    // "" or files.Local: in the data directory
	ref      files.Ref // a document's stored file; zero for thumbnails and covers
	doc      int64     // the document (thumbnails and covers too)
	// sealed: a private document's file goes into a remote backup
	// encrypted for this public key, as rel + SealedSuffix (v1.8.1)
	sealed []byte
}

// storedFile is a document's file in the snapshot's list: one in the data
// directory keeps its path there; one in another storage goes under
// storages/<name>/ (v1.7.6).
func storedFile(storage string, r files.Ref, checksum string) docFile {
	f := docFile{checksum: checksum, storage: storage, ref: r}
	if f.local() {
		f.rel = mediaRel(r)
	} else {
		f.rel = filepath.Join("storages", storage, filepath.FromSlash(r.Key()))
	}
	return f
}

func (f docFile) local() bool { return f.storage == "" || f.storage == files.Local }

// mediaRel is a stored file's path relative to the data directory when it
// is in the built-in storage.
func mediaRel(r files.Ref) string { return filepath.Join("media", filepath.FromSlash(r.Key())) }

// rels are the paths in the backup of a snapshot's files.
func rels(list []docFile) []string {
	out := make([]string, len(list))
	for i, f := range list {
		out[i] = f.rel
	}
	return out
}

// snapshotFiles lists every file of every document in a snapshot (the
// trash included), as paths relative to the data directory.
func snapshotFiles(ctx context.Context, snap string) ([]docFile, int, error) {
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(snap)+"?mode=ro")
	if err != nil {
		return nil, 0, err
	}
	defer db.Close()
	// Page edits (migration 0014); snapshots of older databases have none.
	base := "0"
	var hasEdits int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('documents') WHERE name = 'page_edits'`).Scan(&hasEdits); err != nil {
		return nil, 0, fmt.Errorf("读取快照 %s：%w", filepath.Base(snap), err)
	}
	if hasEdits == 1 {
		base = `instr(page_edits, '"base":"archive"') > 0`
	}
	// Storages (migration 0050); snapshots of older databases have only the data directory.
	storage := "'local'"
	var hasStorage int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('documents') WHERE name = 'storage'`).Scan(&hasStorage); err != nil {
		return nil, 0, err
	}
	if hasStorage == 1 {
		storage = "storage"
	}
	rows, err := db.QueryContext(ctx, `SELECT workspace_id, id, original_ext, checksum, has_archive, has_thumbnail,
		`+base+`, `+storage+` FROM documents ORDER BY id`)
	if err != nil {
		return nil, 0, fmt.Errorf("读取快照 %s：%w", filepath.Base(snap), err)
	}
	defer rows.Close()
	layout := files.NewStore("")
	var out []docFile
	docs := 0
	storages := map[int64]string{} // document → storage, for its versions
	for rows.Next() {
		var ws, id int64
		var ext, sum, st string
		var archive, thumb, base int
		if err := rows.Scan(&ws, &id, &ext, &sum, &archive, &thumb, &base, &st); err != nil {
			return nil, 0, err
		}
		docs++
		storages[id] = st
		out = append(out, storedFile(st, files.Original(ws, id, ext), sum))
		if archive == 1 {
			out = append(out, storedFile(st, files.Archive(ws, id), ""))
		}
		if thumb == 1 {
			out = append(out, docFile{rel: layout.ThumbnailPath(ws, id), doc: id})
		}
		if base == 1 { // the unedited archive of a document with page edits
			out = append(out, storedFile(st, files.Base(ws, id), ""))
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	// Book covers (v0.4); snapshots of older databases have none.
	var hasBooks int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'books'`).Scan(&hasBooks); err != nil {
		return nil, 0, err
	}
	if hasBooks == 1 {
		crows, err := db.QueryContext(ctx, `SELECT workspace_id, document_id FROM books WHERE cover = 1 ORDER BY document_id`)
		if err != nil {
			return nil, 0, err
		}
		for crows.Next() {
			var ws, id int64
			if err := crows.Scan(&ws, &id); err != nil {
				crows.Close()
				return nil, 0, err
			}
			out = append(out, docFile{rel: layout.CoverPath(ws, id), doc: id})
		}
		crows.Close()
		if err := crows.Err(); err != nil {
			return nil, 0, err
		}
	}
	// Earlier versions (stage 19); snapshots of older databases have none.
	var hasVersions int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'document_versions'`).Scan(&hasVersions); err != nil {
		return nil, 0, err
	}
	if hasVersions == 0 {
		return out, docs, nil
	}
	vrows, err := db.QueryContext(ctx, `SELECT workspace_id, document_id, id, original_ext, checksum FROM document_versions WHERE current = 0 ORDER BY id`)
	if err != nil {
		return nil, 0, err
	}
	defer vrows.Close()
	for vrows.Next() {
		var ws, doc, id int64
		var ext, sum string
		if err := vrows.Scan(&ws, &doc, &id, &ext, &sum); err != nil {
			return nil, 0, err
		}
		out = append(out, storedFile(storages[doc], files.Version(ws, doc, id, ext), sum))
	}
	return out, docs, vrows.Err()
}

func referenced(ctx context.Context, snap string) ([]string, int, error) {
	list, docs, err := snapshotFiles(ctx, snap)
	if err != nil {
		return nil, 0, err
	}
	return rels(list), docs, nil
}

// canRead: the documents of storage can be read for the backup.
func (o Options) canRead(storage string) bool {
	if o.Files == nil {
		return false
	}
	_, err := o.Files.Backend(storage)
	return err == nil
}

// syncStored copies a document's file from its storage to dst unless dst
// already holds it (same size: stored files are replaced, never edited in
// place, and a changed one gets a new size or a new name in practice).
func syncStored(ctx context.Context, fs *files.Store, f docFile, dst string, before func() error) (bool, int64, error) {
	info, err := fs.Stat(ctx, f.storage, f.ref)
	if err != nil {
		return false, 0, err
	}
	if d, err := os.Stat(dst); err == nil {
		if d.Size() == info.Size {
			return false, 0, nil
		}
		if err := before(); err != nil {
			return false, 0, err
		}
	}
	if err := fs.Fetch(ctx, f.storage, f.ref, dst); err != nil {
		return false, 0, err
	}
	return true, info.Size, nil
}

// syncFile copies src to dst unless dst already holds it (same size, not
// older). It keeps src's modification time.
func syncFile(src, dst string, before func() error) (bool, int64, error) {
	st, err := os.Stat(src)
	if err != nil {
		return false, 0, err
	}
	if d, err := os.Stat(dst); err == nil {
		if d.Size() == st.Size() && !d.ModTime().Before(st.ModTime()) {
			return false, 0, nil
		}
		if err := before(); err != nil {
			return false, 0, err
		}
	}
	if err := files.Copy(src, dst); err != nil {
		return false, 0, err
	}
	if err := os.Chtimes(dst, st.ModTime(), st.ModTime()); err != nil {
		return false, 0, err
	}
	return true, st.Size(), nil
}

// mirror makes dst an exact copy of the flat directory src (the logo).
func mirror(src, dst string) error {
	want := map[string]bool{}
	entries, err := os.ReadDir(src)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		want[e.Name()] = true
		if _, _, err := syncFile(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()), func() error { return nil }); err != nil {
			return err
		}
	}
	old, err := os.ReadDir(dst)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, e := range old {
		if !e.IsDir() && !want[e.Name()] {
			if err := os.Remove(filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// prune deletes backed-up files no retained snapshot references.
func prune(ctx context.Context, dir string, snaps []string) (int, error) {
	keep := map[string]bool{}
	for _, s := range snaps {
		refs, _, err := referenced(ctx, filepath.Join(dir, "db", s))
		if err != nil {
			return 0, err
		}
		for _, r := range refs {
			keep[filepath.Clean(r)] = true
		}
	}
	n := 0
	walk := func(p string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if !keep[rel] {
			if err := os.Remove(p); err != nil {
				return err
			}
			n++
		}
		return nil
	}
	for _, top := range []string{"media", "storages"} { // storages/: other storages' files (v1.7.6)
		if err := filepath.WalkDir(filepath.Join(dir, top), walk); err != nil {
			return n, err
		}
	}
	return n, nil
}

func writeJSON(p string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".part"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func fileSHA256(p string) (string, error) {
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

// Register handles the backup job: a backup into dir (KEPT_BACKUP_DIR).
// It is registered even without a directory so a queued run fails with a
// reason instead of an unknown job kind.
func Register(p *jobs.Pool, db *sql.DB, dirs *Dir, dataDir string, fs *files.Store, log *slog.Logger) {
	p.Handle(Kind, func(ctx context.Context, _ jobs.Job) (any, error) {
		dir := dirs.Get(ctx)
		if dir == "" {
			return nil, errors.New(i18n.N("没有设置备份目录"))
		}
		res, err := Run(ctx, db, dir, Options{DataDir: dataDir, Files: fs})
		if err != nil {
			return nil, err
		}
		log.Info("backup finished", "dir", res.Dir, "snapshot", res.Snapshot, "copied", res.Copied, "missing", len(res.Missing), "took", res.Took)
		return res, nil
	})
}

// replacedDir holds the content of backed-up files as it was before a
// later backup overwrote them, by the last snapshot that had it (v1.7.12).
const replacedDir = "replaced"

// preserve moves the backup's copy of rel to replaced/<owner>/rel.
func preserve(dir, owner, rel string) error {
	dst := filepath.Join(dir, replacedDir, owner, rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Rename(filepath.Join(dir, rel), dst)
}

// pruneReplaced removes the old contents no retained snapshot needs: those
// kept for snapshots older than the oldest one retained.
func pruneReplaced(dir string, snaps []string) (int, error) {
	if len(snaps) == 0 {
		return 0, nil
	}
	oldest := strings.TrimSuffix(snaps[0], ".db")
	entries, err := os.ReadDir(filepath.Join(dir, replacedDir))
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() || e.Name() >= oldest {
			continue
		}
		_ = filepath.WalkDir(filepath.Join(dir, replacedDir, e.Name()), func(_ string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				n++
			}
			return nil
		})
		if err := os.RemoveAll(filepath.Join(dir, replacedDir, e.Name())); err != nil {
			return n, err
		}
	}
	return n, nil
}

// asOf is where a backup directory keeps rel as it was in snapshot snap:
// the old content kept for the first snapshot at or after snap, or the
// latest copy.
func asOf(dir, snap, rel string) string {
	want := strings.TrimSuffix(snap, ".db")
	if entries, err := os.ReadDir(filepath.Join(dir, replacedDir)); err == nil {
		var owners []string
		for _, e := range entries {
			if e.IsDir() && e.Name() >= want {
				owners = append(owners, e.Name())
			}
		}
		slices.Sort(owners)
		for _, o := range owners {
			p := filepath.Join(dir, replacedDir, o, rel)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return filepath.Join(dir, rel)
}
