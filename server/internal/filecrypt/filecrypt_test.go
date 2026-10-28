package filecrypt

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"testing"
)

func seal(t *testing.T, plain []byte, k Key) []byte {
	t.Helper()
	var buf bytes.Buffer
	n, err := Encrypt(&buf, bytes.NewReader(plain), k)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(buf.Len()) || n != SealedSize(int64(len(plain))) {
		t.Fatalf("wrote %d, buffer %d, expected %d", n, buf.Len(), SealedSize(int64(len(plain))))
	}
	return buf.Bytes()
}

func open(t *testing.T, enc []byte, k Key) (*Reader, error) {
	t.Helper()
	return NewReader(bytes.NewReader(enc), int64(len(enc)), k)
}

func TestRoundTrip(t *testing.T) {
	k, _ := NewKey()
	for _, n := range []int{0, 1, 100, chunkSize - 1, chunkSize, chunkSize + 1, 3 * chunkSize, 3*chunkSize + 77} {
		plain := make([]byte, n)
		rand.Read(plain)
		enc := seal(t, plain, k)
		if !IsEncrypted(enc) {
			t.Fatalf("%d: no header", n)
		}
		if PlainSize(int64(len(enc))) != int64(n) {
			t.Fatalf("%d: plain size %d", n, PlainSize(int64(len(enc))))
		}
		r, err := open(t, enc, k)
		if err != nil {
			t.Fatal(n, err)
		}
		got, err := io.ReadAll(r)
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("%d: read back %d bytes, %v", n, len(got), err)
		}
		if n > 0 && bytes.Contains(enc, plain[:min(n, 32)]) && n >= 16 {
			t.Fatalf("%d: the plaintext shows", n)
		}
	}
}

func TestSeek(t *testing.T) {
	k, _ := NewKey()
	plain := make([]byte, 3*chunkSize+500)
	rand.Read(plain)
	r, err := open(t, seal(t, plain, k), k)
	if err != nil {
		t.Fatal(err)
	}
	for _, off := range []int64{0, 5, chunkSize - 3, chunkSize, 2*chunkSize + 9, int64(len(plain)) - 1} {
		if _, err := r.Seek(off, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 10)
		n, err := io.ReadFull(r, buf)
		if err != nil && err != io.ErrUnexpectedEOF {
			t.Fatal(off, err)
		}
		if !bytes.Equal(buf[:n], plain[off:min(off+10, int64(len(plain)))]) {
			t.Fatalf("at %d: wrong bytes", off)
		}
	}
	if end, _ := r.Seek(0, io.SeekEnd); end != int64(len(plain)) {
		t.Fatalf("end %d", end)
	}
	// through a source that only seeks (an object of a storage service)
	enc := seal(t, plain, k)
	r2, err := NewReader(&SeekerAt{R: bytes.NewReader(enc)}, int64(len(enc)), k)
	if err != nil {
		t.Fatal(err)
	}
	r2.Seek(2*chunkSize+1, io.SeekStart)
	got, _ := io.ReadAll(r2)
	if !bytes.Equal(got, plain[2*chunkSize+1:]) {
		t.Fatal("seeker source")
	}
}

func TestTampering(t *testing.T) {
	k, _ := NewKey()
	plain := make([]byte, 2*chunkSize+10)
	rand.Read(plain)
	enc := seal(t, plain, k)
	readAll := func(b []byte, key Key) error {
		r, err := open(t, b, key)
		if err != nil {
			return err
		}
		_, err = io.ReadAll(r)
		return err
	}
	if err := readAll(enc, k); err != nil {
		t.Fatal(err)
	}
	flip := bytes.Clone(enc)
	flip[HeaderSize+chunkSize+20] ^= 1
	cutChunk := bytes.Clone(enc[:HeaderSize+2*sealed]) // the last chunk dropped
	cutByte := bytes.Clone(enc[:len(enc)-1])
	swapped := bytes.Clone(enc)
	copy(swapped[HeaderSize:], enc[HeaderSize+sealed:HeaderSize+2*sealed])
	copy(swapped[HeaderSize+sealed:], enc[HeaderSize:HeaderSize+sealed])
	salt := bytes.Clone(enc)
	salt[len(Magic)+IDSize] ^= 1
	other := seal(t, plain, k) // a chunk from another file
	mixed := bytes.Clone(enc)
	copy(mixed[HeaderSize:], other[HeaderSize:HeaderSize+sealed])
	for name, b := range map[string][]byte{"flipped": flip, "last chunk dropped": cutChunk, "cut": cutByte, "swapped": swapped, "salt": salt, "mixed": mixed} {
		if err := readAll(b, k); !errors.Is(err, ErrDamaged) {
			t.Errorf("%s: %v", name, err)
		}
	}
	wrong, _ := NewKey()
	if err := readAll(enc, wrong); !errors.Is(err, ErrDamaged) {
		t.Errorf("wrong key: %v", err)
	}
	wrong.ID = k.ID
	if err := readAll(enc, wrong); !errors.Is(err, ErrDamaged) {
		t.Errorf("wrong key with the same ID: %v", err)
	}
	// an empty file's one chunk is checked too
	empty := seal(t, nil, k)
	empty[len(empty)-1] ^= 1
	if err := readAll(empty, k); !errors.Is(err, ErrDamaged) {
		t.Errorf("empty: %v", err)
	}
}

func TestSizes(t *testing.T) {
	for _, n := range []int64{0, 1, chunkSize, chunkSize + 1, 10 * chunkSize} {
		if got := PlainSize(SealedSize(n)); got != n {
			t.Errorf("%d → %d", n, got)
		}
	}
	for _, bad := range []int64{0, int64(HeaderSize), int64(HeaderSize) + 15, int64(HeaderSize + sealed + tagSize)} {
		if PlainSize(bad) != -1 {
			t.Errorf("%d should not be a size", bad)
		}
	}
	if IsEncrypted([]byte("%PDF-1.7 hello world, not encrypted at all, long enough")) {
		t.Error("a PDF is not encrypted")
	}
}
