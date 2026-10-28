package files

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestPathsAndMove(t *testing.T) {
	s := NewStore(t.TempDir())
	if got := s.Path(Original(3, 42, "pdf")); filepath.Base(got) != "42.pdf" || filepath.Base(filepath.Dir(got)) != "3" {
		t.Errorf("OriginalPath = %s", got)
	}
	if got := s.ThumbnailPath(3, 42); filepath.Base(got) != "42.jpg" {
		t.Errorf("ThumbnailPath = %s", got)
	}
	tmp, err := s.NewTmpDir("job-")
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(tmp, "original")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := s.Path(Original(3, 42, "txt"))
	if err := Move(src, dst); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(dst); err != nil || string(b) != "hello" {
		t.Errorf("moved file = %q, %v", b, err)
	}
	if err := s.RemoveDocument(context.Background(), Local, 3, 42, "txt"); err != nil {
		t.Errorf("RemoveDocument: %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Error("original still there")
	}
}

func TestCleanTmpKeepsActiveDirectories(t *testing.T) {
	s := NewStore(t.TempDir())
	a, _ := s.NewTmpDir("job-")
	b, _ := s.NewTmpDir("job-")
	n, err := s.CleanTmp(func(name string) bool { return name == filepath.Base(a) })
	if err != nil || n != 1 {
		t.Fatalf("CleanTmp = %d, %v", n, err)
	}
	if _, err := os.Stat(a); err != nil {
		t.Error("the kept directory was removed")
	}
	if _, err := os.Stat(b); !os.IsNotExist(err) {
		t.Error("the stale directory was kept")
	}
}

func TestSafeExt(t *testing.T) {
	for in, want := range map[string]string{"a.PDF": "pdf", "scan.jpeg": "jpeg", "noext": "", "x.tar.gz": "gz", "bad.p$f": "", "../../etc/passwd": ""} {
		if got := SafeExt(in); got != want {
			t.Errorf("SafeExt(%q) = %q, want %q", in, got, want)
		}
	}
}

// memBackend keeps objects in memory: a storage that is not a directory
// of this machine (as S3 will be).
type memBackend struct{ m map[string][]byte }

type memFile struct {
	*bytes.Reader
	info Info
}

func (f memFile) Close() error { return nil }
func (f memFile) Info() Info   { return f.info }

func (b memBackend) Open(_ context.Context, key string) (File, error) {
	v, ok := b.m[key]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return memFile{bytes.NewReader(v), Info{Size: int64(len(v))}}, nil
}
func (b memBackend) Stat(ctx context.Context, key string) (Info, error) {
	f, err := b.Open(ctx, key)
	if err != nil {
		return Info{}, err
	}
	return f.Info(), nil
}
func (b memBackend) Put(_ context.Context, key, src string) error {
	v, err := os.ReadFile(src)
	b.m[key] = v
	return err
}
func (b memBackend) Rename(_ context.Context, from, to string) error {
	v, ok := b.m[from]
	if !ok {
		return fs.ErrNotExist
	}
	b.m[to] = v
	delete(b.m, from)
	return nil
}
func (b memBackend) Remove(_ context.Context, key string) error { delete(b.m, key); return nil }
func (b memBackend) List(_ context.Context, prefix string) ([]string, error) {
	var out []string
	for k := range b.m {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out, nil
}
func (b memBackend) LocalPath(string) string { return "" }
func (b memBackend) Describe() string        { return "memory" }

func TestStorages(t *testing.T) {
	ctx := context.Background()
	s := NewStore(t.TempDir())
	mem := memBackend{m: map[string][]byte{}}
	s.AddStorage("mem", mem)
	s.AddStorage("nas", DirBackend{Root: t.TempDir()})
	if got := s.Storages(); strings.Join(got, ",") != "local,mem,nas" {
		t.Errorf("Storages = %v", got)
	}
	if _, err := s.Backend("gone"); !errors.Is(err, ErrUnknownStorage) {
		t.Errorf("unknown storage: %v", err)
	}
	if b, err := s.Backend(""); err != nil || b.Describe() != filepath.Join(s.dir, "media") {
		t.Errorf("\"\" = local: %v", err)
	}

	tmp, _ := s.NewTmpDir("t-")
	src := filepath.Join(tmp, "a")
	os.WriteFile(src, []byte("hello"), 0o644)
	for _, st := range []string{"mem", "nas"} {
		r := Original(2, 7, "pdf")
		if err := s.Put(ctx, st, r, src, false); err != nil {
			t.Fatal(st, err)
		}
		if info, err := s.Stat(ctx, st, r); err != nil || info.Size != 5 {
			t.Errorf("%s: Stat = %v %v", st, info, err)
		}
		// Local: the file itself in a directory, a copy otherwise.
		p, release, err := s.Local(ctx, st, r, "")
		if err != nil {
			t.Fatal(st, err)
		}
		if b, _ := os.ReadFile(p); string(b) != "hello" {
			t.Errorf("%s: Local = %q", st, b)
		}
		release()
		_, statErr := os.Stat(p)
		if st == "mem" && !os.IsNotExist(statErr) || st == "nas" && statErr != nil {
			t.Errorf("%s: after release: %v", st, statErr)
		}
		if _, _, err := s.Local(ctx, st, Archive(2, 7), ""); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: missing file: %v", st, err)
		}
		dst := filepath.Join(tmp, st, "fetched")
		if err := s.Fetch(ctx, st, r, dst); err != nil {
			t.Error(st, err)
		}
		// Versions, renames and removal.
		if err := s.Put(ctx, st, Version(2, 7, 1, "pdf"), src, false); err != nil {
			t.Fatal(err)
		}
		if err := s.Put(ctx, st, Version(2, 70, 2, "pdf"), src, false); err != nil {
			t.Fatal(err)
		}
		if vs, err := s.Versions(ctx, st, 2, 7); err != nil || len(vs) != 1 || vs[0] != Version(2, 7, 1, "pdf") {
			t.Errorf("%s: Versions = %v %v", st, vs, err)
		}
		if err := s.Rename(ctx, st, r, Original(3, 7, "pdf")); err != nil || s.Exists(ctx, st, r) || !s.Exists(ctx, st, Original(3, 7, "pdf")) {
			t.Errorf("%s: Rename: %v", st, err)
		}
		if err := s.RemoveDocument(ctx, st, 2, 7, "pdf"); err != nil || s.Exists(ctx, st, Version(2, 7, 1, "pdf")) || !s.Exists(ctx, st, Version(2, 70, 2, "pdf")) {
			t.Errorf("%s: RemoveDocument: %v", st, err)
		}
	}
	// A workspace's files go from every storage.
	if err := s.RemoveWorkspace(ctx, 2); err != nil {
		t.Fatal(err)
	}
	for _, st := range []string{"mem", "nas"} {
		if s.Exists(ctx, st, Version(2, 70, 2, "pdf")) || !s.Exists(ctx, st, Original(3, 7, "pdf")) {
			t.Errorf("%s: RemoveWorkspace", st)
		}
	}
	// Put with move removes the source, from any storage.
	if err := s.Put(ctx, "mem", Archive(3, 7), src, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source kept after a move")
	}
}

func TestDirBackendKeys(t *testing.T) {
	d := DirBackend{Root: t.TempDir()}
	for _, key := range []string{"", "../x", "a/../../x", "/abs", "a//b", "a/"} {
		if _, err := d.Stat(context.Background(), key); err == nil || errors.Is(err, fs.ErrNotExist) {
			t.Errorf("key %q: %v", key, err)
		}
	}
	if d.LocalPath("../x") != "" {
		t.Error("LocalPath of a bad key")
	}
	if keys, err := d.List(context.Background(), "versions/9/"); err != nil || keys != nil {
		t.Errorf("List of a missing folder = %v %v", keys, err)
	}
}

func TestWalk(t *testing.T) {
	ctx := context.Background()
	dav, _ := fakeDAV(t)
	for name, b := range map[string]Backend{"dir": DirBackend{Root: t.TempDir()}, "s3": fakeS3(t, "p"), "dav": dav} {
		src := filepath.Join(t.TempDir(), "f")
		os.WriteFile(src, []byte("abc"), 0o644)
		for _, k := range []string{"inbox/a.pdf", "inbox/发票/专票/b.pdf", "inbox/发票/c.jpg", "other/d.pdf"} {
			if err := b.Put(ctx, k, src); err != nil {
				t.Fatal(name, err)
			}
		}
		got, err := Walk(ctx, b, "inbox")
		if err != nil {
			t.Fatal(name, err)
		}
		var keys []string
		for _, e := range got {
			keys = append(keys, e.Key)
			if e.Info.Size != 3 {
				t.Errorf("%s: %s size %d", name, e.Key, e.Info.Size)
			}
		}
		if strings.Join(keys, ",") != "inbox/a.pdf,inbox/发票/c.jpg,inbox/发票/专票/b.pdf" {
			t.Errorf("%s: walk = %v", name, keys)
		}
		if got, err := Walk(ctx, b, "nothing-here"); err != nil || len(got) != 0 {
			t.Errorf("%s: empty = %v %v", name, got, err)
		}
	}
}
