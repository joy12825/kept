// Package files keeps document files (design doc 2.5): in the data
// directory, or since v1.7.0 in other storages (see backend.go). Keys
// derive from the workspace and document IDs; the database stores only
// which storage a document's files are in.
package files

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Store struct {
	dir      string // the data directory
	mu       sync.RWMutex
	backends map[string]Backend // by storage name; "local" is <data>/media
	crypts   map[string]*Crypt  // encrypted storages (v1.11.0)
}

// NewStore keeps documents in the data directory only.
func NewStore(dataDir string) *Store {
	return &Store{dir: dataDir, backends: map[string]Backend{Local: DirBackend{Root: filepath.Join(dataDir, "media")}}}
}

// AddStorage makes another storage available (v1.7.0), or replaces one
// changed on the page (v1.7.2).
func (s *Store) AddStorage(name string, b Backend) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.backends[name] = b
}

// RemoveStorage forgets a storage deleted on the page (v1.7.2).
func (s *Store) RemoveStorage(name string) {
	if name == Local {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.backends, name)
	delete(s.crypts, name)
}

// Storages lists the storage names, "local" first.
func (s *Store) Storages() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []string{Local}
	for n := range s.backends {
		if n != Local {
			out = append(out, n)
		}
	}
	sort.Strings(out[1:])
	return out
}

// Backend returns a storage by name; an encrypted one is wrapped in
// Encrypted.
func (s *Store) Backend(name string) (Backend, error) {
	if name == "" {
		name = Local
	}
	s.mu.RLock()
	b, ok := s.backends[name]
	c := s.crypts[name]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknownStorage, name)
	}
	if c != nil {
		return Encrypted{Raw: b, Crypt: c, Tmp: s.TmpRoot()}, nil
	}
	return b, nil
}

// SetCrypt makes a storage encrypted (v1.11.0); it stays so, with c
// saying whether new files are, until the storage is removed. The data
// directory is never encrypted.
func (s *Store) SetCrypt(name string, c *Crypt) {
	if name == Local {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.crypts == nil {
		s.crypts = map[string]*Crypt{}
	}
	s.crypts[name] = c
}

// Crypt is a storage's encryption, nil when it has never been encrypted.
func (s *Store) Crypt(name string) *Crypt {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.crypts[name]
}

// Open reads a document's file from its storage.
func (s *Store) Open(ctx context.Context, storage string, r Ref) (File, error) {
	b, err := s.Backend(storage)
	if err != nil {
		return nil, err
	}
	return b.Open(ctx, r.Key())
}

// Stat reports a document's file; a missing one is fs.ErrNotExist.
func (s *Store) Stat(ctx context.Context, storage string, r Ref) (Info, error) {
	b, err := s.Backend(storage)
	if err != nil {
		return Info{}, err
	}
	return b.Stat(ctx, r.Key())
}

// Exists reports whether a document's file is there.
func (s *Store) Exists(ctx context.Context, storage string, r Ref) bool {
	_, err := s.Stat(ctx, storage, r)
	return err == nil
}

// Put stores the local file src as a document's file; with move, src is
// removed afterwards (renamed when the storage is on the same disk).
func (s *Store) Put(ctx context.Context, storage string, r Ref, src string, move bool) error {
	b, err := s.Backend(storage)
	if err != nil {
		return err
	}
	if p := b.LocalPath(r.Key()); p != "" && move {
		return Move(src, p)
	}
	if err := b.Put(ctx, r.Key(), src); err != nil {
		return err
	}
	if move {
		_ = os.Remove(src)
	}
	return nil
}

// Rename moves a document's file to another name in the same storage.
func (s *Store) Rename(ctx context.Context, storage string, from, to Ref) error {
	b, err := s.Backend(storage)
	if err != nil {
		return err
	}
	return b.Rename(ctx, from.Key(), to.Key())
}

// Remove deletes a document's file; a missing one is fine.
func (s *Store) Remove(ctx context.Context, storage string, r Ref) error {
	b, err := s.Backend(storage)
	if err != nil {
		return err
	}
	return b.Remove(ctx, r.Key())
}

// Local gives a path on this machine to read a document's file with
// tools: the file itself in a directory storage, else a copy in workDir
// (or a new temporary directory when workDir is ""). release removes the
// copy. A missing file is fs.ErrNotExist.
func (s *Store) Local(ctx context.Context, storage string, r Ref, workDir string) (string, func(), error) {
	b, err := s.Backend(storage)
	if err != nil {
		return "", nil, err
	}
	if p := b.LocalPath(r.Key()); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", nil, err
		}
		return p, func() {}, nil
	}
	dir := workDir
	release := func() {}
	if dir == "" {
		if dir, err = s.NewTmpDir("fetch-"); err != nil {
			return "", nil, err
		}
		release = func() { os.RemoveAll(dir) }
	}
	dst := filepath.Join(dir, "fetched-"+r.Name)
	if err := s.Fetch(ctx, storage, r, dst); err != nil {
		release()
		return "", nil, err
	}
	if workDir != "" {
		release = func() { os.Remove(dst) }
	}
	return dst, release, nil
}

// Fetch copies a document's file to the local path dst (its directory is
// made). A missing file is fs.ErrNotExist.
func (s *Store) Fetch(ctx context.Context, storage string, r Ref, dst string) error {
	b, err := s.Backend(storage)
	if err != nil {
		return err
	}
	f, err := b.Open(ctx, r.Key())
	if err != nil {
		return err
	}
	defer f.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return writeFile(dst, f)
}

// ThumbnailPath is a JPEG of the first page, 400 px wide; always in the
// data directory.
func (s *Store) ThumbnailPath(ws, id int64) string {
	return filepath.Join(s.dir, "media", "thumbnails", strconv.FormatInt(ws, 10), strconv.FormatInt(id, 10)+".jpg")
}

// CoverPath is a book's cover (v0.4), a JPEG at most 600 px wide; always
// in the data directory.
func (s *Store) CoverPath(ws, id int64) string {
	return filepath.Join(s.dir, "media", "covers", strconv.FormatInt(ws, 10), strconv.FormatInt(id, 10)+".jpg")
}

// Versions lists the earlier versions kept of a document (their refs).
func (s *Store) Versions(ctx context.Context, storage string, ws, id int64) ([]Ref, error) {
	b, err := s.Backend(storage)
	if err != nil {
		return nil, err
	}
	keys, err := b.List(ctx, path.Join("versions", strconv.FormatInt(ws, 10), strconv.FormatInt(id, 10)+"-"))
	if err != nil {
		return nil, err
	}
	out := make([]Ref, len(keys))
	for i, k := range keys {
		out[i] = Ref{WS: ws, Doc: id, Kind: "versions", Name: path.Base(k)}
	}
	return out, nil
}

// RemoveWorkspace deletes every file of a workspace, in every storage
// (after it is purged).
func (s *Store) RemoveWorkspace(ctx context.Context, ws int64) error {
	for _, kind := range []string{"thumbnails", "covers"} {
		if err := os.RemoveAll(filepath.Join(s.dir, "media", kind, strconv.FormatInt(ws, 10))); err != nil {
			return err
		}
	}
	var errs []error
	for _, name := range s.Storages() {
		b, err := s.Backend(name)
		if err != nil {
			continue
		}
		for _, kind := range docKinds {
			keys, err := b.List(ctx, path.Join(kind, strconv.FormatInt(ws, 10))+"/")
			if err != nil {
				errs = append(errs, err)
				continue
			}
			for _, k := range keys {
				if err := b.Remove(ctx, k); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}
	return errors.Join(errs...)
}

// CacheDir holds files made again on demand (v1.9.2: watermarked shares);
// always in the data directory, and safe to delete.
func (s *Store) CacheDir(kind string) string { return filepath.Join(s.dir, "cache", kind) }

// CleanCache removes cached files not used for maxAge; it returns how many.
func (s *Store) CleanCache(maxAge time.Duration) (int64, error) {
	root := filepath.Join(s.dir, "cache")
	cutoff := time.Now().Add(-maxAge)
	var n int64
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return filepath.SkipAll
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().Before(cutoff) {
			if os.Remove(p) == nil {
				n++
			}
		}
		return nil
	})
	return n, err
}

// TmpRoot holds the working directories of uploads and ingest jobs.
func (s *Store) TmpRoot() string { return filepath.Join(s.dir, "tmp") }

// NewTmpDir creates a fresh working directory under TmpRoot.
func (s *Store) NewTmpDir(prefix string) (string, error) {
	if err := os.MkdirAll(s.TmpRoot(), 0o755); err != nil {
		return "", err
	}
	return os.MkdirTemp(s.TmpRoot(), prefix)
}

// Move puts src at dst, creating dst's directory. It renames when it can
// and copies (then removes src) across file systems.
func Move(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

// Copy copies src to dst, creating dst's directory.
func Copy(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return writeFile(dst, in)
}

// writeFile writes r to dst through dst.part, so dst is whole or absent.
func writeFile(dst string, r io.Reader) error {
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// RemoveDocument deletes every file of a document in its storage, and
// its thumbnail and cover (missing ones are fine).
func (s *Store) RemoveDocument(ctx context.Context, storage string, ws, id int64, ext string) error {
	var errs []error
	for _, p := range []string{s.ThumbnailPath(ws, id), s.CoverPath(ws, id)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	b, err := s.Backend(storage)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	refs := []Ref{Original(ws, id, ext), Archive(ws, id), Base(ws, id)}
	earlier, err := s.Versions(ctx, storage, ws, id)
	if err != nil {
		errs = append(errs, err)
	}
	for _, r := range append(refs, earlier...) {
		if err := b.Remove(ctx, r.Key()); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// CleanTmp removes working directories under TmpRoot except those keep
// reports as in use, and returns how many it removed.
func (s *Store) CleanTmp(keep func(name string) bool) (int, error) {
	entries, err := os.ReadDir(s.TmpRoot())
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if keep != nil && keep(e.Name()) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(s.TmpRoot(), e.Name())); err != nil {
			return n, fmt.Errorf("clean %s: %w", e.Name(), err)
		}
		n++
	}
	return n, nil
}

// SafeExt returns a lower-case extension of name without the dot, limited
// to letters and digits ("" when there is none).
func SafeExt(name string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	if len(ext) > 10 {
		return ""
	}
	for _, r := range ext {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return ""
		}
	}
	return ext
}

// FileName makes a file name from a title: no path separators or
// characters Windows rejects, and “(2)” for a repeated name.
func FileName(title, ext string, used map[string]int) string {
	base := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 0x20 {
			return '_'
		}
		return r
	}, strings.TrimSpace(title))
	if base == "" {
		base = "文档"
	}
	name := base + "." + ext
	used[name]++
	if n := used[name]; n > 1 {
		name = fmt.Sprintf("%s (%d).%s", base, n, ext)
	}
	return name
}

// Path is r's file in the built-in storage (the data directory). Code that
// serves documents goes through Open/Local with the document's storage;
// this is for tests and tools that only know the data directory.
func (s *Store) Path(r Ref) string {
	return filepath.Join(s.dir, "media", filepath.FromSlash(r.Key()))
}

// Loc is where a file to serve is: a document's file in a storage, or a
// file of the data directory (Path: thumbnails, covers).
type Loc struct {
	Storage string
	Ref     Ref
	Path    string
}

// At is a document's file in a storage.
func At(storage string, r Ref) Loc { return Loc{Storage: storage, Ref: r} }

// LocalFile is a file of this machine.
func LocalFile(p string) Loc { return Loc{Path: p} }

// OpenLoc opens a file wherever it is.
func (s *Store) OpenLoc(ctx context.Context, l Loc) (File, error) {
	if l.Path != "" {
		return DirBackend{Root: filepath.Dir(l.Path)}.Open(ctx, filepath.Base(l.Path))
	}
	return s.Open(ctx, l.Storage, l.Ref)
}

// Staged is a local file on its way into a storage. A storage on another
// machine is slow to write to, so the file is uploaded under a temporary
// key before the database transaction that records it, and only renamed
// (a copy on the service) inside it. A directory of this machine needs
// none of this: Place moves the file in.
type Staged struct {
	storage string
	src     string
	move    bool
	tmp     *Ref // uploaded here; nil for a directory storage
	placed  bool
}

// Stage gets src ready to be placed in storage for workspace ws; with
// move, src is removed once placed.
func (s *Store) Stage(ctx context.Context, storage string, ws int64, src string, move bool) (*Staged, error) {
	b, err := s.Backend(storage)
	if err != nil {
		return nil, err
	}
	st := &Staged{storage: storage, src: src, move: move}
	if b.LocalPath("incoming/x") != "" {
		return st, nil
	}
	var rnd [12]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return nil, err
	}
	r := Ref{WS: ws, Kind: "incoming", Name: hex.EncodeToString(rnd[:])}
	if err := b.Put(ctx, r.Key(), src); err != nil {
		return nil, err
	}
	st.tmp = &r
	return st, nil
}

// Place puts the staged file at r.
func (st *Staged) Place(ctx context.Context, s *Store, r Ref) error {
	if st.tmp == nil {
		if err := s.Put(ctx, st.storage, r, st.src, st.move); err != nil {
			return err
		}
	} else {
		if err := s.Rename(ctx, st.storage, *st.tmp, r); err != nil {
			return err
		}
		if st.move {
			_ = os.Remove(st.src)
		}
	}
	st.placed = true
	return nil
}

// Discard removes the uploaded copy of a file that was not placed.
func (st *Staged) Discard(ctx context.Context, s *Store) {
	if st == nil || st.placed || st.tmp == nil {
		return
	}
	_ = s.Remove(context.WithoutCancel(ctx), st.storage, *st.tmp)
}
