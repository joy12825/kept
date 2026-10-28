// Package watch ingests files dropped into a consume directory, one folder
// per workspace, sub-folders as tags, and merges duplex scans (v0.2).
package watch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/files"
	"kept/server/internal/i18n"
	"kept/server/internal/ingest"
	"kept/server/internal/taxonomy"
	"kept/server/internal/tenant"
)

// FailedDir holds the files that could not be ingested, with the reason.
const FailedDir = "_未能入库"

// duplexDirs name the duplex folder inside a workspace folder.
var duplexDirs = map[string]bool{"双面": true, "duplex": true}

// wsFolder matches a workspace folder: its ID, then anything.
var wsFolder = regexp.MustCompile(`^(\d+)(?:[-_ ].*)?$`)

type Options struct {
	Dir           string
	Interval      time.Duration    // between scans; default 10s
	Settle        time.Duration    // a file must be this old; default 5s
	DuplexTimeout time.Duration    // a lone duplex half waits this long; default 30m
	Now           func() time.Time // default time.Now
	Log           *slog.Logger
	// Merge interleaves a duplex scan's fronts and backs into out;
	// default qpdf (see merge.go).
	Merge func(ctx context.Context, front, back, out string) error
	// Pages counts a PDF's pages; default qpdf --show-npages.
	Pages func(ctx context.Context, path string) (int, error)
}

// Folder is one workspace's folder.
type Folder struct {
	WorkspaceID   int64
	WorkspaceName string
	Name          string
}

// Status is what the system page shows.
type Status struct {
	Dir      string
	Folders  []Folder
	LastScan time.Time
	Waiting  int // files seen but not yet ingested
	Failed   int // files in _未能入库
	Err      string
}

type seenFile struct {
	size  int64
	mtime time.Time
	first time.Time
}

type Watcher struct {
	db     *sql.DB
	ingest *ingest.Service
	tax    *taxonomy.Service
	opt    Options

	mu     sync.Mutex
	seen   map[string]seenFile
	status Status
}

func New(db *sql.DB, ing *ingest.Service, tax *taxonomy.Service, opt Options) *Watcher {
	if opt.Interval <= 0 {
		opt.Interval = 10 * time.Second
	}
	if opt.Settle <= 0 {
		opt.Settle = 5 * time.Second
	}
	if opt.DuplexTimeout <= 0 {
		opt.DuplexTimeout = 30 * time.Minute
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Log == nil {
		opt.Log = slog.New(slog.DiscardHandler)
	}
	if opt.Merge == nil {
		opt.Merge = qpdfMerge
	}
	if opt.Pages == nil {
		opt.Pages = qpdfPages
	}
	w := &Watcher{db: db, ingest: ing, tax: tax, opt: opt, seen: map[string]seenFile{}, status: Status{Dir: opt.Dir}}
	ing.OnFailed(w.keepFailed)
	return w
}

// keepFailed puts back a file from the consume directory whose processing
// failed (it was removed when it was accepted), into _未能入库 with why.
func (w *Watcher) keepFailed(u ingest.FailedUpload) {
	if u.Source != ingest.SourceWatch || u.Origin == "" {
		return
	}
	dst := filepath.Join(w.opt.Dir, FailedDir, filepath.FromSlash(u.Origin))
	if _, err := os.Stat(dst); err == nil {
		ext := filepath.Ext(dst)
		dst = strings.TrimSuffix(dst, ext) + "-" + w.opt.Now().Format("20060102-150405") + ext
	}
	if err := files.Copy(u.Path, dst); err != nil {
		w.opt.Log.Error("keep a file that failed to ingest", "file", u.Origin, "err", err)
		return
	}
	_ = os.WriteFile(dst+".原因.txt", []byte(u.Reason+"\n"), 0o644)
}

// Run scans every Interval until ctx ends.
func (w *Watcher) Run(ctx context.Context) {
	w.opt.Log.Info("watching the consume directory", "dir", w.opt.Dir, "every", w.opt.Interval)
	t := time.NewTicker(w.opt.Interval)
	defer t.Stop()
	for {
		if err := w.Scan(ctx); err != nil && ctx.Err() == nil {
			w.opt.Log.Warn("scan the consume directory", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Status reports the latest scan.
func (w *Watcher) Status() Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	st := w.status
	st.Folders = append([]Folder(nil), st.Folders...)
	return st
}

// Scan makes one pass over the consume directory.
func (w *Watcher) Scan(ctx context.Context) error {
	ctx = audit.WithActor(ctx, audit.Actor{Via: audit.ViaSystem, Name: i18n.N("监听目录")})
	folders, err := w.folders(ctx)
	w.mu.Lock()
	w.status.LastScan = w.opt.Now()
	w.status.Folders = folders
	if err != nil {
		w.status.Err = err.Error()
	} else {
		w.status.Err = ""
	}
	w.mu.Unlock()
	if err != nil {
		return err
	}
	live := map[string]bool{}
	waiting := 0
	for _, f := range folders {
		n, err := w.scanFolder(ctx, f, live)
		waiting += n
		if err != nil {
			return err
		}
	}
	w.mu.Lock()
	for p := range w.seen {
		if !live[p] {
			delete(w.seen, p)
		}
	}
	w.status.Waiting = waiting
	w.status.Failed = countFiles(filepath.Join(w.opt.Dir, FailedDir))
	w.mu.Unlock()
	return nil
}

// folders matches workspace folders and creates the missing ones.
func (w *Watcher) folders(ctx context.Context) ([]Folder, error) {
	if err := os.MkdirAll(w.opt.Dir, 0o755); err != nil {
		return nil, err
	}
	rows, err := w.db.QueryContext(ctx, `SELECT id, name FROM workspaces WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	var ids []int64
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return nil, err
		}
		names[id] = name
		ids = append(ids, id)
	}
	rows.Close()
	entries, err := os.ReadDir(w.opt.Dir)
	if err != nil {
		return nil, err
	}
	have := map[int64]string{}
	for _, e := range entries {
		m := wsFolder.FindStringSubmatch(e.Name())
		if !e.IsDir() || m == nil {
			continue
		}
		id, _ := strconv.ParseInt(m[1], 10, 64)
		if _, ok := names[id]; ok {
			if _, dup := have[id]; !dup || e.Name() < have[id] {
				have[id] = e.Name()
			}
		}
	}
	var out []Folder
	for _, id := range ids {
		name, ok := have[id]
		if !ok {
			name = fmt.Sprintf("%d-%s", id, folderSafe(names[id]))
			if err := os.MkdirAll(filepath.Join(w.opt.Dir, name), 0o755); err != nil {
				return nil, err
			}
		}
		out = append(out, Folder{WorkspaceID: id, WorkspaceName: names[id], Name: name})
	}
	return out, nil
}

// folderSafe keeps a workspace name usable as a folder name.
func folderSafe(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 32 {
			return '_'
		}
		return r
	}, strings.TrimSpace(s))
	if s == "" || s == "." || s == ".." {
		return "空间"
	}
	return s
}

// ready reports whether a file has stopped changing: same size and time on
// two scans, and last modified at least Settle ago.
func (w *Watcher) ready(p string, info fs.FileInfo) bool {
	now := w.opt.Now()
	w.mu.Lock()
	defer w.mu.Unlock()
	prev, ok := w.seen[p]
	cur := seenFile{size: info.Size(), mtime: info.ModTime(), first: now}
	if ok && prev.size == cur.size && prev.mtime.Equal(cur.mtime) {
		cur.first = prev.first
		w.seen[p] = cur
		return now.Sub(info.ModTime()) >= w.opt.Settle
	}
	w.seen[p] = cur
	return false
}

func skipName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~$") ||
		strings.HasSuffix(lower, ".part") || strings.HasSuffix(lower, ".tmp") || strings.HasSuffix(lower, ".crdownload") ||
		strings.HasSuffix(name, ".原因.txt")
}

type pending struct {
	path string
	rel  string // relative to the workspace folder
	info fs.FileInfo
}

// scanFolder ingests a workspace folder's settled files and returns how
// many are still waiting.
func (w *Watcher) scanFolder(ctx context.Context, f Folder, live map[string]bool) (int, error) {
	root := filepath.Join(w.opt.Dir, f.Name)
	var normal []pending
	duplex := map[string][]pending{} // duplex folder -> settled files
	waiting := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if skipName(d.Name()) || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		live[p] = true
		rel, _ := filepath.Rel(root, p)
		item := pending{path: p, rel: rel, info: info}
		if !w.ready(p, info) {
			waiting++
			return nil
		}
		if dd := duplexDir(rel); dd != "" {
			duplex[dd] = append(duplex[dd], item)
			return nil
		}
		normal = append(normal, item)
		return nil
	})
	if err != nil {
		return waiting, err
	}
	for _, it := range normal {
		if err := ctx.Err(); err != nil {
			return waiting, err
		}
		if err := w.ingestFile(ctx, f, it.path, it.rel, filepath.Base(it.path)); err != nil {
			return waiting, err
		}
	}
	for _, items := range duplex {
		n, err := w.duplex(ctx, f, items)
		waiting += n
		if err != nil {
			return waiting, err
		}
	}
	return waiting, nil
}

// duplexDir returns the duplex folder (relative) a file is in, or "".
func duplexDir(rel string) string {
	parts := strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/")
	for i, p := range parts {
		if duplexDirs[strings.ToLower(p)] {
			return strings.Join(parts[:i+1], "/")
		}
	}
	return ""
}

// ingestFile submits one file (or a merged duplex file at path, named
// name) with the tags of rel's folders, then removes the source(s).
func (w *Watcher) ingestFile(ctx context.Context, f Folder, path, rel, name string, sources ...string) error {
	scope := tenant.New(f.WorkspaceID, tenant.RoleOwner)
	tags, err := w.tagsFor(ctx, scope, rel)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // gone meanwhile
		}
		return err
	}
	origin, at := filepath.ToSlash(filepath.Join(f.Name, rel)), filepath.ToSlash(rel)
	// The same path again becomes a new version, when that is on (v1.7.13).
	var doc int64
	if on, err := w.ingest.WatchVersions(ctx); err != nil {
		file.Close()
		return err
	} else if on {
		if doc, err = w.ingest.DocumentAt(ctx, f.WorkspaceID, at); err != nil {
			file.Close()
			return err
		}
	}
	if doc != 0 {
		_, err = w.ingest.AddVersion(ctx, scope, doc, name, file, i18n.N("监听目录"))
	} else {
		_, err = w.ingest.Submit(ctx, scope, name, file, ingest.Overrides{TagIDs: tags}, ingest.Upload{Source: ingest.SourceWatch, Origin: origin, Path: at})
	}
	file.Close()
	if len(sources) == 0 {
		sources = []string{path}
	}
	var ae *apperr.Error
	switch {
	case err == nil:
		w.opt.Log.Info("ingested from the consume directory", "workspace", f.WorkspaceID, "file", rel)
		for _, s := range sources {
			if rerr := os.Remove(s); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
				return rerr
			}
		}
		return nil
	case errors.As(err, &ae) && (ae.Kind == apperr.KindInvalid || ae.Kind == apperr.KindConflict):
		// Refused by the upload itself: retrying will not help.
		for _, s := range sources {
			r, _ := filepath.Rel(filepath.Join(w.opt.Dir, f.Name), s)
			if merr := w.moveFailed(f, s, r, ae.Message); merr != nil {
				return merr
			}
		}
		w.opt.Log.Warn("could not ingest a file from the consume directory", "workspace", f.WorkspaceID, "file", rel, "reason", ae.Message)
		return nil
	default:
		return err // temporary: try again on the next scan
	}
}

// moveFailed moves a file into _未能入库, keeping its path, and writes why.
func (w *Watcher) moveFailed(f Folder, src, rel, reason string) error {
	dst := filepath.Join(w.opt.Dir, FailedDir, f.Name, rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		ext := filepath.Ext(dst)
		dst = strings.TrimSuffix(dst, ext) + "-" + w.opt.Now().Format("20060102-150405") + ext
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	return os.WriteFile(dst+".原因.txt", []byte(reason+"\n"), 0o644)
}

// tagsFor returns the tags named by rel's folders (outside the duplex
// folder), creating the missing ones at the top level.
func (w *Watcher) tagsFor(ctx context.Context, scope tenant.Scope, rel string) ([]int64, error) {
	dir := filepath.ToSlash(filepath.Dir(rel))
	if dir == "." {
		return nil, nil
	}
	var names []string
	for _, p := range strings.Split(dir, "/") {
		if p != "" && !duplexDirs[strings.ToLower(p)] {
			names = append(names, p)
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	existing, err := w.tax.ListTags(ctx, scope)
	if err != nil {
		return nil, err
	}
	var out []int64
	for _, n := range names {
		var id int64
		for _, t := range existing {
			if strings.EqualFold(t.Name, n) {
				id = t.ID
				break
			}
		}
		if id == 0 {
			t, err := w.tax.CreateTag(ctx, scope, taxonomy.TagInput{Name: n})
			if err != nil {
				return nil, fmt.Errorf("create tag %q: %w", n, err)
			}
			id = t.ID
			existing = append(existing, t)
		}
		out = append(out, id)
	}
	return out, nil
}

func countFiles(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !strings.HasSuffix(d.Name(), ".原因.txt") {
			n++
		}
		return nil
	})
	return n
}

// byTime orders files by modification time, then name.
func byTime(items []pending) {
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i].info.ModTime(), items[j].info.ModTime()
		if !a.Equal(b) {
			return a.Before(b)
		}
		return items[i].rel < items[j].rel
	})
}
