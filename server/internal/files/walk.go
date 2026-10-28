package files

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/minio/minio-go/v7"
)

// Entry is a file found by Walk.
type Entry struct {
	Key  string
	Info Info
}

// CanWalk: Walk works on b.
func CanWalk(b Backend) bool {
	switch Unwrap(b).(type) {
	case DirBackend, *S3Backend, *WebDAVBackend:
		return true
	}
	return false
}

// Walk lists every file under the folder prefix (subfolders included), for
// reading folders people put files into (v1.7.7). Keys are full, sorted.
func Walk(ctx context.Context, b Backend, prefix string) ([]Entry, error) {
	prefix = strings.Trim(prefix, "/")
	var out []Entry
	var err error
	switch b := Unwrap(b).(type) {
	case DirBackend:
		out, err = b.walk(prefix)
	case *S3Backend:
		out, err = b.walk(ctx, prefix)
	case *WebDAVBackend:
		out, err = b.walk(ctx, prefix)
	default:
		return nil, fmt.Errorf("this storage cannot list folders")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, err
}

func (d DirBackend) walk(prefix string) ([]Entry, error) {
	root, err := d.path(prefix)
	if err != nil {
		return nil, err
	}
	var out []Entry
	err = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && p == root {
				return fs.SkipAll
			}
			return err
		}
		if e.IsDir() {
			return nil
		}
		st, err := e.Info()
		if err != nil {
			return nil // gone meanwhile
		}
		rel, err := filepath.Rel(d.Root, p)
		if err != nil {
			return err
		}
		out = append(out, Entry{Key: filepath.ToSlash(rel), Info: Info{Size: st.Size(), ModTime: st.ModTime()}})
		return nil
	})
	return out, err
}

func (b *S3Backend) walk(ctx context.Context, prefix string) ([]Entry, error) {
	full := prefix + "/"
	if b.prefix != "" {
		full = b.prefix + "/" + full
	}
	var out []Entry
	for obj := range b.c.ListObjects(ctx, b.bucket, minio.ListObjectsOptions{Prefix: full, Recursive: true}) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		if strings.HasSuffix(obj.Key, "/") { // a folder marker
			continue
		}
		key := obj.Key
		if b.prefix != "" {
			key = strings.TrimPrefix(key, b.prefix+"/")
		}
		out = append(out, Entry{Key: key, Info: Info{Size: obj.Size, ModTime: obj.LastModified}})
	}
	return out, nil
}

// walk lists folder by folder (Depth 1): many servers refuse Depth infinity.
func (b *WebDAVBackend) walk(ctx context.Context, prefix string) ([]Entry, error) {
	var out []Entry
	var visit func(dir string, depth int) error
	visit = func(dir string, depth int) error {
		if depth > 20 {
			return nil
		}
		u := *b.base
		u.Path += dir + "/"
		rs, err := b.propfind(ctx, u.String(), "1")
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		self := strings.TrimSuffix(u.Path, "/")
		for _, r := range rs {
			h, err := url.Parse(r.Href) // a path, or a whole URL on some servers
			if err != nil {
				continue
			}
			p := strings.TrimSuffix(h.Path, "/")
			if p == self {
				continue // the folder itself
			}
			name := path.Base(p)
			key := path.Join(dir, name)
			if r.Prop.Type.Collection != nil {
				if err := visit(key, depth+1); err != nil {
					return err
				}
				continue
			}
			out = append(out, Entry{Key: key, Info: infoOf(r)})
		}
		return nil
	}
	return out, visit(prefix, 0)
}
