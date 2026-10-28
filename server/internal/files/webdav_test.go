package files

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/net/webdav"
)

// fakeDAV is a WebDAV server in memory under /dav/, with a password; it
// counts the bytes it sends.
func fakeDAV(t *testing.T) (*WebDAVBackend, *atomic.Int64) {
	t.Helper()
	h := &webdav.Handler{Prefix: "/dav", FileSystem: webdav.NewMemFS(), LockSystem: webdav.NewMemLS()}
	var sent atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "wang" || p != "dav-pass" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(countingWriter{w, &sent}, r)
	}))
	t.Cleanup(srv.Close)
	b, err := NewWebDAV(WebDAVConfig{URL: srv.URL + "/dav/kept", Username: "wang", Password: "dav-pass"})
	if err != nil {
		t.Fatal(err)
	}
	return b, &sent
}

type countingWriter struct {
	http.ResponseWriter
	n *atomic.Int64
}

func (c countingWriter) Write(p []byte) (int, error) {
	c.n.Add(int64(len(p)))
	return c.ResponseWriter.Write(p)
}

func TestWebDAVBackend(t *testing.T) {
	ctx := context.Background()
	b, sent := fakeDAV(t)
	if !strings.HasSuffix(b.Describe(), "/dav/kept/") || strings.Contains(b.Describe(), "dav-pass") {
		t.Errorf("Describe = %q", b.Describe())
	}
	key := Original(2, 7, "pdf").Key()
	if _, err := b.Stat(ctx, key); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
	if keys, err := b.List(ctx, "originals/2/"); err != nil || len(keys) != 0 {
		t.Fatalf("empty folder: %v %v", keys, err)
	}
	data := make([]byte, 3<<20)
	rand.Read(data)
	src := filepath.Join(t.TempDir(), "a.pdf")
	os.WriteFile(src, data, 0o644)
	if err := b.Put(ctx, key, src); err != nil { // makes originals/2/ on the way
		t.Fatal(err)
	}
	if info, err := b.Stat(ctx, key); err != nil || info.Size != int64(len(data)) || info.ModTime.IsZero() {
		t.Fatalf("Stat = %+v %v", info, err)
	}
	// A range near the end downloads only that part.
	f, err := b.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	before := sent.Load()
	f.Seek(-10, io.SeekEnd)
	tail, _ := io.ReadAll(f)
	f.Seek(0, io.SeekStart)
	head := make([]byte, 4)
	io.ReadFull(f, head)
	f.Close()
	if !bytes.Equal(tail, data[len(data)-10:]) || !bytes.Equal(head, data[:4]) {
		t.Fatal("ranges read wrong bytes")
	}
	if got := sent.Load() - before; got > 1<<20 {
		t.Errorf("reading two ranges downloaded %d bytes", got)
	}
	whole, _ := b.Open(ctx, key)
	all, _ := io.ReadAll(whole)
	whole.Close()
	if !bytes.Equal(all, data) {
		t.Fatal("whole file differs")
	}

	os.WriteFile(src, []byte("v"), 0o644)
	b.Put(ctx, Version(2, 7, 1, "pdf").Key(), src)
	b.Put(ctx, Version(2, 70, 2, "pdf").Key(), src)
	if keys, err := b.List(ctx, "versions/2/7-"); err != nil || len(keys) != 1 || keys[0] != "versions/2/7-1.pdf" {
		t.Errorf("List = %v %v", keys, err)
	}
	if err := b.Rename(ctx, key, Original(3, 7, "pdf").Key()); err != nil { // into a new folder
		t.Fatal(err)
	}
	if _, err := b.Stat(ctx, key); !errors.Is(err, fs.ErrNotExist) {
		t.Error("renamed file still there")
	}
	if err := b.Remove(ctx, Original(3, 7, "pdf").Key()); err != nil {
		t.Error(err)
	}
	if err := b.Remove(ctx, "originals/9/none.pdf"); err != nil {
		t.Errorf("removing a missing file: %v", err)
	}

	// A wrong password says so.
	bad, _ := NewWebDAV(WebDAVConfig{URL: strings.TrimSuffix(b.Describe(), "/"), Username: "wang", Password: "nope"})
	if _, err := bad.Stat(ctx, key); err == nil || !strings.Contains(err.Error(), "用户名或密码") {
		t.Errorf("wrong password: %v", err)
	}
}

// TestStoreOnWebDAV: staging and placing, as uploads do.
func TestStoreOnWebDAV(t *testing.T) {
	ctx := context.Background()
	s := NewStore(t.TempDir())
	b, _ := fakeDAV(t)
	s.AddStorage("dav", b)
	src := filepath.Join(t.TempDir(), "a")
	os.WriteFile(src, []byte("hello"), 0o644)
	st, err := s.Stage(ctx, "dav", 2, src, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Place(ctx, s, Original(2, 7, "txt")); err != nil {
		t.Fatal(err)
	}
	if keys, _ := b.List(ctx, "incoming/2/"); len(keys) != 0 {
		t.Errorf("left in incoming: %v", keys)
	}
	p, release, err := s.Local(ctx, "dav", Original(2, 7, "txt"), "")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); string(got) != "hello" {
		t.Errorf("Local = %q", got)
	}
	release()
	if err := s.RemoveWorkspace(ctx, 2); err != nil {
		t.Fatal(err)
	}
	if s.Exists(ctx, "dav", Original(2, 7, "txt")) {
		t.Error("RemoveWorkspace left the original")
	}
}
