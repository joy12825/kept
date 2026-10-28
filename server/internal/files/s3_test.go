package files

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
)

// fakeS3 starts an in-memory S3 service with bucket "kept".
func fakeS3(t *testing.T, prefix string) *S3Backend {
	t.Helper()
	mem := s3mem.New()
	if err := mem.CreateBucket("kept"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(gofakes3.New(mem).Server())
	t.Cleanup(srv.Close)
	b, err := NewS3(S3Config{Endpoint: strings.TrimPrefix(srv.URL, "http://"), Bucket: "kept", Prefix: prefix, AccessKey: "ak", SecretKey: "sk", PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestS3Backend(t *testing.T) {
	ctx := context.Background()
	b := fakeS3(t, "docs")
	if !strings.HasPrefix(b.Describe(), "s3://kept/docs（http://127.0.0.1:") || strings.Contains(b.Describe(), "sk") {
		t.Errorf("Describe = %q", b.Describe())
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "a.pdf")
	os.WriteFile(src, []byte("0123456789"), 0o644)
	key := Original(2, 7, "pdf").Key()

	if _, err := b.Stat(ctx, key); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := b.Open(ctx, key); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("open missing: %v", err)
	}
	if err := b.Put(ctx, key, src); err != nil {
		t.Fatal(err)
	}
	if info, err := b.Stat(ctx, key); err != nil || info.Size != 10 || info.ModTime.IsZero() {
		t.Errorf("Stat = %+v %v", info, err)
	}
	// Seeking reads a range, as a PDF viewer does.
	f, err := b.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(6, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(f)
	f.Close()
	if string(got) != "6789" || f.Info().Size != 10 {
		t.Errorf("range = %q", got)
	}
	os.WriteFile(src, []byte("v"), 0o644)
	if err := b.Put(ctx, Version(2, 7, 1, "pdf").Key(), src); err != nil {
		t.Fatal(err)
	}
	if err := b.Put(ctx, Version(2, 70, 2, "pdf").Key(), src); err != nil {
		t.Fatal(err)
	}
	if keys, err := b.List(ctx, "versions/2/7-"); err != nil || len(keys) != 1 || keys[0] != "versions/2/7-1.pdf" {
		t.Errorf("List = %v %v", keys, err)
	}
	if err := b.Rename(ctx, key, Original(3, 7, "pdf").Key()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Stat(ctx, key); !errors.Is(err, fs.ErrNotExist) {
		t.Error("renamed object still there")
	}
	if err := b.Remove(ctx, Original(3, 7, "pdf").Key()); err != nil {
		t.Error(err)
	}
	if err := b.Remove(ctx, "originals/9/nothing.pdf"); err != nil {
		t.Errorf("removing a missing object: %v", err)
	}
	if _, err := b.Stat(ctx, "../x"); err == nil {
		t.Error("bad key accepted")
	}
}

// TestStoreOnS3: the Store's helpers over an S3 storage: Local downloads a
// copy, Stage uploads ahead and Place renames, a workspace is removed.
func TestStoreOnS3(t *testing.T) {
	ctx := context.Background()
	s := NewStore(t.TempDir())
	s.AddStorage("s3", fakeS3(t, ""))
	tmp, _ := s.NewTmpDir("t-")
	src := filepath.Join(tmp, "a")
	os.WriteFile(src, []byte("hello"), 0o644)

	st, err := s.Stage(ctx, "s3", 2, src, true)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := s.Backend("s3")
	if keys, _ := b.List(ctx, "incoming/2/"); len(keys) != 1 {
		t.Fatalf("staged = %v", keys)
	}
	if err := st.Place(ctx, s, Original(2, 7, "txt")); err != nil {
		t.Fatal(err)
	}
	st.Discard(ctx, s) // placed: nothing to do
	if keys, _ := b.List(ctx, "incoming/"); len(keys) != 0 {
		t.Errorf("left in incoming: %v", keys)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source kept after a move")
	}
	p, release, err := s.Local(ctx, "s3", Original(2, 7, "txt"), "")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); string(got) != "hello" {
		t.Errorf("Local = %q", got)
	}
	release()

	// Discarding a staged file that was never placed removes the upload.
	os.WriteFile(src, []byte("again"), 0o644)
	st, err = s.Stage(ctx, "s3", 2, src, false)
	if err != nil {
		t.Fatal(err)
	}
	st.Discard(ctx, s)
	if keys, _ := b.List(ctx, "incoming/"); len(keys) != 0 {
		t.Errorf("discarded upload left: %v", keys)
	}
	if err := s.RemoveWorkspace(ctx, 2); err != nil {
		t.Fatal(err)
	}
	if s.Exists(ctx, "s3", Original(2, 7, "txt")) {
		t.Error("RemoveWorkspace left the original")
	}
}

// TestS3LargeFile: a file over the multipart size arrives byte for byte
// (an upload once stored the chunk signatures in the object).
func TestS3LargeFile(t *testing.T) {
	ctx := context.Background()
	b := fakeS3(t, "")
	data := make([]byte, 20<<20+123)
	rand.Read(data)
	src := filepath.Join(t.TempDir(), "big")
	os.WriteFile(src, data, 0o644)
	if err := b.Put(ctx, "originals/1/1.pdf", src); err != nil {
		t.Fatal(err)
	}
	f, err := b.Open(ctx, "originals/1/1.pdf")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, _ := io.ReadAll(f)
	if !bytes.Equal(got, data) || f.Info().Size != int64(len(data)) {
		t.Fatalf("stored %d bytes (info %d), want %d, equal %v", len(got), f.Info().Size, len(data), bytes.Equal(got, data))
	}
}

func TestS3Versioning(t *testing.T) {
	ctx := context.Background()
	b := fakeS3(t, "")
	if on, err := b.Versioning(ctx); err != nil || on {
		t.Fatalf("new bucket = %v %v", on, err)
	}
	if err := b.EnableVersioning(ctx); err != nil {
		t.Fatal(err)
	}
	if on, err := b.Versioning(ctx); err != nil || !on {
		t.Errorf("after enabling = %v %v", on, err)
	}
}
