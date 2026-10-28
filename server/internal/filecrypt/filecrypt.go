// Package filecrypt encrypts the files kept in a storage (v1.11.0), so a
// storage service or whoever holds the bucket or the NAS sees only
// ciphertext.
//
// An encrypted file is a 48-byte header — "KEPTENC1", the storage key's ID
// (8 bytes) and a random salt (32 bytes) — then the plaintext in chunks of
// 64 KiB, each sealed with AES-256-GCM under a key of its own for the file,
// HKDF-SHA256(storage key, salt). A chunk's nonce is its number and whether
// it is the last, and the header is its additional data: chunks cannot be
// dropped, reordered or moved to another file. A file can be read from any
// position, and the plaintext size follows from the ciphertext size.
package filecrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	// Magic starts every encrypted file.
	Magic = "KEPTENC1"
	// HeaderSize is the size of the header.
	HeaderSize = len(Magic) + IDSize + saltSize
	// IDSize is the size of a key's ID.
	IDSize = 8
	// KeySize is the size of a storage key.
	KeySize = 32

	saltSize  = 32
	chunkSize = 64 * 1024
	tagSize   = 16
	sealed    = chunkSize + tagSize // a full chunk in the file
)

// ErrDamaged: the file was changed, cut short, or is not encrypted with
// this key.
var ErrDamaged = errors.New("filecrypt: the file is damaged or was encrypted with another key")

// Key is a storage key with its ID.
type Key struct {
	ID  [IDSize]byte
	Raw []byte // KeySize bytes
}

// NewKey makes a random key.
func NewKey() (Key, error) {
	var k Key
	k.Raw = make([]byte, KeySize)
	if _, err := rand.Read(k.ID[:]); err != nil {
		return Key{}, err
	}
	if _, err := rand.Read(k.Raw); err != nil {
		return Key{}, err
	}
	return k, nil
}

// Equal reports whether two keys are the same.
func Equal(a, b Key) bool { return a.ID == b.ID && string(a.Raw) == string(b.Raw) }

// Header is the start of an encrypted file.
type Header struct {
	ID   [IDSize]byte
	salt [saltSize]byte
	raw  [HeaderSize]byte
}

// ParseHeader reads the header at the start of b; ok is false when b does
// not start like an encrypted file.
func ParseHeader(b []byte) (Header, bool) {
	var h Header
	if len(b) < HeaderSize || string(b[:len(Magic)]) != Magic {
		return h, false
	}
	copy(h.raw[:], b[:HeaderSize])
	copy(h.ID[:], b[len(Magic):])
	copy(h.salt[:], b[len(Magic)+IDSize:HeaderSize])
	return h, true
}

// IsEncrypted reports whether b starts like an encrypted file.
func IsEncrypted(b []byte) bool {
	_, ok := ParseHeader(b)
	return ok
}

// PlainSize is the plaintext size of an encrypted file of size n, or -1
// when no encrypted file has that size.
func PlainSize(n int64) int64 {
	body := n - int64(HeaderSize)
	if body < tagSize {
		return -1
	}
	chunks := (body + sealed - 1) / sealed
	last := body - (chunks-1)*sealed
	if last < tagSize || (last == tagSize && chunks > 1) {
		return -1
	}
	return body - chunks*tagSize
}

// SealedSize is the size of the encrypted file of a plaintext of size n.
func SealedSize(n int64) int64 {
	chunks := (n + chunkSize - 1) / chunkSize
	if chunks == 0 {
		chunks = 1
	}
	return int64(HeaderSize) + n + chunks*tagSize
}

func fileAEAD(k Key, h Header) (cipher.AEAD, error) {
	if len(k.Raw) != KeySize {
		return nil, fmt.Errorf("filecrypt: the key must be %d bytes", KeySize)
	}
	if h.ID != k.ID {
		return nil, ErrDamaged
	}
	fk, err := hkdf.Key(sha256.New, k.Raw, h.salt[:], "kept file v1", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(fk)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func nonce(n uint64, last bool) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint64(b[3:11], n)
	if last {
		b[11] = 1
	}
	return b
}

// Encrypt writes the encrypted r to w and returns the bytes written.
func Encrypt(w io.Writer, r io.Reader, k Key) (int64, error) {
	var h Header
	h.ID = k.ID
	if _, err := rand.Read(h.salt[:]); err != nil {
		return 0, err
	}
	copy(h.raw[:], Magic)
	copy(h.raw[len(Magic):], h.ID[:])
	copy(h.raw[len(Magic)+IDSize:], h.salt[:])
	aead, err := fileAEAD(k, h)
	if err != nil {
		return 0, err
	}
	written, err := w.Write(h.raw[:])
	total := int64(written)
	if err != nil {
		return total, err
	}
	// Read one chunk ahead, to know which is the last.
	cur := make([]byte, chunkSize)
	next := make([]byte, chunkSize)
	n, err := io.ReadFull(r, cur)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return total, err
	}
	out := make([]byte, 0, sealed)
	for i := uint64(0); ; i++ {
		var m int
		last := n < chunkSize
		if !last {
			m, err = io.ReadFull(r, next)
			if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
				return total, err
			}
			last = m == 0
		}
		out = aead.Seal(out[:0], nonce(i, last), cur[:n], h.raw[:])
		written, err := w.Write(out)
		total += int64(written)
		if err != nil {
			return total, err
		}
		if last {
			return total, nil
		}
		cur, next, n = next, cur, m
	}
}

// Reader reads the plaintext of an encrypted file from any position.
type Reader struct {
	src    io.ReaderAt
	size   int64 // plaintext
	chunks int64
	h      Header
	aead   cipher.AEAD

	pos   int64
	cur   int64 // the chunk in buf, -1 for none
	buf   []byte
	plain []byte
}

// NewReader reads the encrypted file src of size n (ciphertext) with k.
func NewReader(src io.ReaderAt, n int64, k Key) (*Reader, error) {
	size := PlainSize(n)
	if size < 0 {
		return nil, ErrDamaged
	}
	raw := make([]byte, HeaderSize)
	if _, err := src.ReadAt(raw, 0); err != nil {
		return nil, err
	}
	h, ok := ParseHeader(raw)
	if !ok {
		return nil, ErrDamaged
	}
	aead, err := fileAEAD(k, h)
	if err != nil {
		return nil, err
	}
	chunks := (n - int64(HeaderSize) + sealed - 1) / sealed
	return &Reader{src: src, size: size, chunks: chunks, h: h, aead: aead, cur: -1, buf: make([]byte, sealed)}, nil
}

// Size is the plaintext size.
func (r *Reader) Size() int64 { return r.size }

func (r *Reader) load(i int64) error {
	if i == r.cur {
		return nil
	}
	off := int64(HeaderSize) + i*sealed
	want := sealed
	if i == r.chunks-1 {
		want = int(r.size - i*chunkSize + tagSize)
	}
	n, err := r.src.ReadAt(r.buf[:want], off)
	if n < want {
		if err == nil || err == io.EOF {
			err = ErrDamaged
		}
		return err
	}
	plain, err := r.aead.Open(r.plain[:0], nonce(uint64(i), i == r.chunks-1), r.buf[:want], r.h.raw[:])
	if err != nil {
		r.cur = -1
		return ErrDamaged
	}
	r.plain, r.cur = plain, i
	return nil
}

// ReadAt reads the plaintext at off.
func (r *Reader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("filecrypt: negative offset")
	}
	done := 0
	for done < len(p) {
		if off >= r.size {
			if r.size == 0 && r.cur != 0 { // an empty file still has its one chunk to check
				if err := r.load(0); err != nil {
					return done, err
				}
			}
			return done, io.EOF
		}
		i := off / chunkSize
		if err := r.load(i); err != nil {
			return done, err
		}
		n := copy(p[done:], r.plain[off-i*chunkSize:])
		done += n
		off += int64(n)
	}
	return done, nil
}

// Read reads the plaintext from the current position.
func (r *Reader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	n, err := r.ReadAt(p, r.pos)
	r.pos += int64(n)
	if err == io.EOF && n > 0 {
		err = nil
	}
	return n, err
}

// Seek moves the position, as io.Seeker.
func (r *Reader) Seek(off int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		off += r.pos
	case io.SeekEnd:
		off += r.size
	default:
		return 0, errors.New("filecrypt: bad whence")
	}
	if off < 0 {
		return 0, errors.New("filecrypt: negative position")
	}
	r.pos = off
	return off, nil
}

// SeekerAt reads at offsets with Seek and Read, for sources that are
// io.ReadSeeker only (an object of a storage service).
type SeekerAt struct {
	R   io.ReadSeeker
	pos int64
}

// SetPos tells where R is now, when it was read from already.
func (s *SeekerAt) SetPos(pos int64) { s.pos = pos }

// ReadAt reads len(p) bytes at off, or fewer at the end.
func (s *SeekerAt) ReadAt(p []byte, off int64) (int, error) {
	if off != s.pos {
		if _, err := s.R.Seek(off, io.SeekStart); err != nil {
			return 0, err
		}
		s.pos = off
	}
	n, err := io.ReadFull(s.R, p)
	s.pos += int64(n)
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	return n, err
}
