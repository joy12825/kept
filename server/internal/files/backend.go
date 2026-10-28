package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Storage (v1.7.0): the files of a document — its original, the searchable
// PDF, the unedited base of a page edit and earlier versions — live in one
// named storage, recorded on the document (documents.storage). A storage
// is a Backend holding objects by key; keys keep the data directory's
// layout ("originals/2/80.pdf"), so the built-in storage "local" is the
// media directory as before. Thumbnails and covers always stay in the
// data directory: they are a cache the pages show often, and can be made
// again.

// Local is the built-in storage: <data>/media.
const Local = "local"

// ErrUnknownStorage: a document names a storage no longer configured.
var ErrUnknownStorage = errors.New("unknown storage")

// Info describes a stored object.
type Info struct {
	Size    int64
	ModTime time.Time
}

// File is an open stored object.
type File interface {
	io.ReadSeekCloser
	Info() Info
}

// Backend holds objects by key. Missing objects are fs.ErrNotExist.
type Backend interface {
	// Open reads an object.
	Open(ctx context.Context, key string) (File, error)
	Stat(ctx context.Context, key string) (Info, error)
	// Put stores the local file src under key (src stays).
	Put(ctx context.Context, key, src string) error
	// Rename moves an object to another key of the same backend.
	Rename(ctx context.Context, from, to string) error
	// Remove deletes an object; a missing one is not an error.
	Remove(ctx context.Context, key string) error
	// List returns the keys starting with prefix, sorted.
	List(ctx context.Context, prefix string) ([]string, error)
	// LocalPath is the object's file when the backend is a directory of
	// this machine ("" otherwise): tools read it in place.
	LocalPath(key string) string
	// Describe is where the backend keeps its objects, without secrets.
	Describe() string
}

// Ref names one file of a document.
type Ref struct {
	WS, Doc int64
	Kind    string // originals, archive, versions
	Name    string // file name inside the kind's folder
}

// Key is the object key: kind/workspace/name, the data directory layout.
func (r Ref) Key() string {
	return path.Join(r.Kind, strconv.FormatInt(r.WS, 10), r.Name)
}

// Original is the uploaded file, never modified. ext has no dot.
func Original(ws, id int64, ext string) Ref {
	name := strconv.FormatInt(id, 10)
	if ext != "" {
		name += "." + ext
	}
	return Ref{WS: ws, Doc: id, Kind: "originals", Name: name}
}

// Archive is the searchable PDF/A made by OCR.
func Archive(ws, id int64) Ref {
	return Ref{WS: ws, Doc: id, Kind: "archive", Name: strconv.FormatInt(id, 10) + ".pdf"}
}

// Base keeps the unedited archive while the archive holds an edited copy
// (rotated or deleted pages).
func Base(ws, id int64) Ref {
	return Ref{WS: ws, Doc: id, Kind: "archive", Name: strconv.FormatInt(id, 10) + ".base.pdf"}
}

// Version is an earlier (not current) version of a document's original.
func Version(ws, docID, versionID int64, ext string) Ref {
	name := strconv.FormatInt(docID, 10) + "-" + strconv.FormatInt(versionID, 10)
	if ext != "" {
		name += "." + ext
	}
	return Ref{WS: ws, Doc: docID, Kind: "versions", Name: name}
}

// Sealed is the encrypted copy of one file of a family member's private
// document on a remote storage (v1.8.1, package sealbox), named after the
// plain file: sealed/<ws>/<doc>-<kind>_<name>.kept.
func Sealed(plain Ref) Ref {
	return Ref{WS: plain.WS, Doc: plain.Doc, Kind: "sealed", Name: strconv.FormatInt(plain.Doc, 10) + "-" + plain.Kind + "_" + plain.Name + ".kept"}
}

// docKinds are the kinds a storage holds; incoming holds files uploaded
// ahead of a database change (Stage).
var docKinds = []string{"originals", "archive", "versions", "incoming", "sealed"}

// ---------- a directory of this machine ----------

// DirBackend keeps objects as files under a directory.
type DirBackend struct{ Root string }

type osFile struct {
	*os.File
	info Info
}

func (f osFile) Info() Info { return f.info }

func (d DirBackend) path(key string) (string, error) {
	clean := path.Clean("/" + key)[1:]
	if clean == "" || clean != key {
		return "", fmt.Errorf("bad key %q", key)
	}
	return filepath.Join(d.Root, filepath.FromSlash(clean)), nil
}

func (d DirBackend) Open(_ context.Context, key string) (File, error) {
	p, err := d.path(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return osFile{File: f, info: Info{Size: st.Size(), ModTime: st.ModTime()}}, nil
}

func (d DirBackend) Stat(_ context.Context, key string) (Info, error) {
	p, err := d.path(key)
	if err != nil {
		return Info{}, err
	}
	st, err := os.Stat(p)
	if err != nil {
		return Info{}, err
	}
	return Info{Size: st.Size(), ModTime: st.ModTime()}, nil
}

func (d DirBackend) Put(_ context.Context, key, src string) error {
	p, err := d.path(key)
	if err != nil {
		return err
	}
	return Copy(src, p)
}

func (d DirBackend) Rename(_ context.Context, from, to string) error {
	a, err := d.path(from)
	if err != nil {
		return err
	}
	b, err := d.path(to)
	if err != nil {
		return err
	}
	return Move(a, b)
}

func (d DirBackend) Remove(_ context.Context, key string) error {
	p, err := d.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (d DirBackend) List(_ context.Context, prefix string) ([]string, error) {
	dir, base := path.Split(prefix)
	p, err := d.path(strings.TrimSuffix(dir, "/"))
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), base) && !strings.HasSuffix(e.Name(), ".part") {
			out = append(out, dir+e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

func (d DirBackend) LocalPath(key string) string {
	p, err := d.path(key)
	if err != nil {
		return ""
	}
	return p
}

func (d DirBackend) Describe() string { return d.Root }

// Reserved: the first folder of a path kept uses itself in a storage, so a
// folder people read or write there (an inbox, an export) cannot be in it.
func Reserved(folder string) bool {
	switch strings.SplitN(strings.Trim(folder, "/"), "/", 2)[0] {
	case "originals", "archive", "versions", "incoming", "kept-backup", ".kept-probe":
		return true
	}
	return false
}
