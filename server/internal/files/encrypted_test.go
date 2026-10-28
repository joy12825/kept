package files

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"kept/server/internal/filecrypt"
)

func TestEncryptedStorage(t *testing.T) {
	ctx := context.Background()
	s := NewStore(t.TempDir())
	nas := t.TempDir()
	s.AddStorage("nas", DirBackend{Root: nas})
	src := filepath.Join(t.TempDir(), "doc.pdf")
	plain := make([]byte, 200*1024)
	rand.Read(plain)
	os.WriteFile(src, plain, 0o644)
	read := func(r Ref) []byte {
		t.Helper()
		f, err := s.Open(ctx, "nas", r)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if f.Info().Size != int64(len(plain)) {
			t.Errorf("%s: size %d", r.Key(), f.Info().Size)
		}
		b, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	old := Original(1, 1, "pdf")
	if err := s.Put(ctx, "nas", old, src, false); err != nil {
		t.Fatal(err)
	}

	k, _ := filecrypt.NewKey()
	c := &Crypt{}
	c.Set(&k, true, nil)
	s.SetCrypt("nas", c)
	// replacing the backend keeps it encrypted
	s.AddStorage("nas", DirBackend{Root: nas})

	enc := Original(1, 2, "pdf")
	if err := s.Put(ctx, "nas", enc, src, false); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(nas, "originals/1/2.pdf"))
	if !filecrypt.IsEncrypted(raw) || bytes.Contains(raw, plain[:64]) {
		t.Fatal("the file was stored as it is")
	}
	// both read back
	if !bytes.Equal(read(old), plain) || !bytes.Equal(read(enc), plain) {
		t.Fatal("read back differs")
	}
	if info, err := s.Stat(ctx, "nas", enc); err != nil || info.Size != int64(len(plain)) {
		t.Errorf("stat %+v %v", info, err)
	}
	// reading from the middle
	f, _ := s.Open(ctx, "nas", enc)
	f.Seek(100_000, io.SeekStart)
	part := make([]byte, 50)
	io.ReadFull(f, part)
	f.Close()
	if !bytes.Equal(part, plain[100_000:100_050]) {
		t.Error("seek")
	}
	// tools never get the encrypted file in place
	p, release, err := s.Local(ctx, "nas", enc, "")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, plain) || p == filepath.Join(nas, "originals/1/2.pdf") {
		t.Error("Local gave the encrypted file")
	}
	release()
	// moving a file in encrypts it too
	moved := filepath.Join(t.TempDir(), "m.pdf")
	os.WriteFile(moved, plain, 0o644)
	if err := s.Put(ctx, "nas", Original(1, 3, "pdf"), moved, true); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(nas, "originals/1/3.pdf")); !filecrypt.IsEncrypted(raw) {
		t.Error("a moved file was not encrypted")
	}
	if _, err := os.Stat(moved); err == nil {
		t.Error("the moved file stayed")
	}
	// staged uploads too
	st, err := s.Stage(ctx, "nas", 1, src, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Place(ctx, s, Archive(1, 4)); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(nas, "archive/1/4.pdf")); !filecrypt.IsEncrypted(raw) {
		t.Error("a staged file was not encrypted")
	}

	// folders people use stay as they are
	b, _ := s.Backend("nas")
	if err := b.Put(ctx, "导出/a.pdf", src); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(nas, "导出/a.pdf")); !bytes.Equal(raw, plain) {
		t.Error("an export was encrypted")
	}

	// turned off: new files plain, old ones still read
	c.Set(&k, false, nil)
	plainRef := Original(1, 5, "pdf")
	s.Put(ctx, "nas", plainRef, src, false)
	if raw, _ := os.ReadFile(filepath.Join(nas, "originals/1/5.pdf")); !bytes.Equal(raw, plain) {
		t.Error("written encrypted while off")
	}
	if !bytes.Equal(read(enc), plain) {
		t.Error("an encrypted file stopped reading")
	}
	if p, _, _ := s.Local(ctx, "nas", plainRef, ""); p != filepath.Join(nas, "originals/1/5.pdf") {
		t.Error("a plain file is read in place")
	}
	if p, release, _ := s.Local(ctx, "nas", enc, ""); p == filepath.Join(nas, "originals/1/2.pdf") {
		t.Error("an encrypted file given in place while off")
	} else {
		release()
	}

	// no key: encrypted files cannot be read, plain ones can
	c.Set(nil, true, nil)
	if _, err := s.Open(ctx, "nas", enc); !errors.Is(err, ErrLocked) {
		t.Errorf("open without the key: %v", err)
	}
	if err := s.Put(ctx, "nas", Original(1, 6, "pdf"), src, false); !errors.Is(err, ErrLocked) {
		t.Errorf("put without the key: %v", err)
	}
	if !bytes.Equal(read(old), plain) {
		t.Error("a plain file stopped reading")
	}

	// the type of storage still shows through
	if !CanWalk(b) {
		t.Error("CanWalk")
	}
	if _, ok := Unwrap(b).(DirBackend); !ok {
		t.Error("Unwrap")
	}
}

func TestSmallPlainFilesInAnEncryptedStorage(t *testing.T) {
	ctx := context.Background()
	s := NewStore(t.TempDir())
	nas := t.TempDir()
	s.AddStorage("nas", DirBackend{Root: nas})
	os.MkdirAll(filepath.Join(nas, "originals/1"), 0o755)
	os.WriteFile(filepath.Join(nas, "originals/1/1.txt"), []byte("hi"), 0o644)
	os.WriteFile(filepath.Join(nas, "originals/1/2.txt"), nil, 0o644)
	k, _ := filecrypt.NewKey()
	c := &Crypt{}
	c.Set(&k, true, nil)
	s.SetCrypt("nas", c)
	for name, want := range map[string]string{"1": "hi", "2": ""} {
		f, err := s.Open(ctx, "nas", Original(1, map[string]int64{"1": 1, "2": 2}[name], "txt"))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(f)
		f.Close()
		if string(b) != want {
			t.Errorf("%s: %q", name, b)
		}
	}
	// an empty file encrypted
	empty := filepath.Join(t.TempDir(), "e")
	os.WriteFile(empty, nil, 0o644)
	if err := s.Put(ctx, "nas", Original(1, 3, "txt"), empty, false); err != nil {
		t.Fatal(err)
	}
	if info, err := s.Stat(ctx, "nas", Original(1, 3, "txt")); err != nil || info.Size != 0 {
		t.Errorf("%+v %v", info, err)
	}
}

// Encrypted files on S3 and WebDAV: read whole and from the middle, sizes
// right, and the stored object is ciphertext.
func TestEncryptedOnServices(t *testing.T) {
	ctx := context.Background()
	dav, _ := fakeDAV(t)
	for name, raw := range map[string]Backend{"s3": fakeS3(t, "kept"), "webdav": dav} {
		t.Run(name, func(t *testing.T) {
			s := NewStore(t.TempDir())
			s.AddStorage("cloud", raw)
			k, _ := filecrypt.NewKey()
			c := &Crypt{}
			c.Set(&k, true, nil)
			s.SetCrypt("cloud", c)
			plain := make([]byte, 150*1024+3)
			rand.Read(plain)
			src := filepath.Join(t.TempDir(), "a")
			os.WriteFile(src, plain, 0o644)
			r := Archive(4, 9)
			if err := s.Put(ctx, "cloud", r, src, false); err != nil {
				t.Fatal(err)
			}
			rf, err := raw.Open(ctx, r.Key())
			if err != nil {
				t.Fatal(err)
			}
			stored, _ := io.ReadAll(rf)
			rf.Close()
			if !filecrypt.IsEncrypted(stored) || int64(len(stored)) != filecrypt.SealedSize(int64(len(plain))) {
				t.Fatalf("stored %d bytes", len(stored))
			}
			if info, err := s.Stat(ctx, "cloud", r); err != nil || info.Size != int64(len(plain)) {
				t.Fatalf("stat %+v %v", info, err)
			}
			f, err := s.Open(ctx, "cloud", r)
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(f)
			if err != nil || !bytes.Equal(got, plain) {
				t.Fatalf("read %d, %v", len(got), err)
			}
			f.Seek(70_000, io.SeekStart)
			part := make([]byte, 100)
			io.ReadFull(f, part)
			f.Close()
			if !bytes.Equal(part, plain[70_000:70_100]) {
				t.Fatal("seek")
			}
			// a plain object next to it
			raw.Put(ctx, Original(4, 1, "txt").Key(), src)
			f, err = s.Open(ctx, "cloud", Original(4, 1, "txt"))
			if err != nil {
				t.Fatal(err)
			}
			got, _ = io.ReadAll(f)
			f.Close()
			if !bytes.Equal(got, plain) {
				t.Fatal("plain object")
			}
		})
	}
}
