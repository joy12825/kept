// Package sealbox encrypts family members' private documents for storage
// the server does not trust (v1.8.1): remote storages and remote backups.
//
// Each person has a key pair. The public key stays on the server, so files
// can be encrypted at any time, whoever is signed in; the private key is
// kept only wrapped — by the person's login password and by a recovery code
// the person writes down — and is needed only to restore from those copies.
//
// A sealed stream is "KEPTP1\n", an ephemeral X25519 public key, then the
// plaintext in 64 KiB chunks, each sealed with AES-256-GCM. The key comes
// from HKDF-SHA256 over the X25519 shared secret; the nonce is the chunk
// counter with a flag on the last chunk, so chunks cannot be reordered,
// dropped or the stream cut short.
package sealbox

import (
	"bufio"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	magic     = "KEPTP1\n"
	chunkSize = 64 << 10
	tagSize   = 16
	info      = "kept private file v1"
	// StringPrefix marks a sealed text value (a title, recognised text) in
	// a remote backup.
	StringPrefix = "kept-sealed:v1:"
)

var (
	ErrNotSealed  = errors.New("sealbox: not a sealed stream")
	ErrWrongKey   = errors.New("sealbox: the key does not open it, or it was changed")
	ErrWrongCode  = errors.New("sealbox: the password or recovery code is wrong")
	errBadKeySize = errors.New("sealbox: a key is 32 bytes")
)

// KeyPair is a person's keys: Public encrypts, Private decrypts.
type KeyPair struct{ Public, Private []byte }

// NewKeyPair makes a person's key pair.
func NewKeyPair() (KeyPair, error) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, err
	}
	return KeyPair{Public: k.PublicKey().Bytes(), Private: k.Bytes()}, nil
}

// PublicOf is the public key of a private key (a check after unwrapping).
func PublicOf(private []byte) ([]byte, error) {
	k, err := ecdh.X25519().NewPrivateKey(private)
	if err != nil {
		return nil, errBadKeySize
	}
	return k.PublicKey().Bytes(), nil
}

func streamKey(shared, eph, recipient []byte) (cipher.AEAD, error) {
	key, err := hkdf.Key(sha256.New, shared, append(append([]byte{}, eph...), recipient...), info, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func nonce(counter uint64, last bool) []byte {
	n := make([]byte, 12)
	binary.BigEndian.PutUint64(n[4:], counter)
	if last {
		n[0] = 1
	}
	return n
}

// Encrypt returns a writer that seals what is written to it for the public
// key and writes it to w. Close it to write the last chunk; Close does not
// close w.
func Encrypt(w io.Writer, public []byte) (io.WriteCloser, error) {
	recipient, err := ecdh.X25519().NewPublicKey(public)
	if err != nil {
		return nil, errBadKeySize
	}
	eph, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	shared, err := eph.ECDH(recipient)
	if err != nil {
		return nil, err
	}
	aead, err := streamKey(shared, eph.PublicKey().Bytes(), public)
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(w, magic); err != nil {
		return nil, err
	}
	if _, err := w.Write(eph.PublicKey().Bytes()); err != nil {
		return nil, err
	}
	return &writer{w: w, aead: aead, buf: make([]byte, 0, chunkSize)}, nil
}

type writer struct {
	w       io.Writer
	aead    cipher.AEAD
	buf     []byte
	counter uint64
	closed  bool
}

func (s *writer) Write(p []byte) (int, error) {
	if s.closed {
		return 0, errors.New("sealbox: write after close")
	}
	n := 0
	for len(p) > 0 {
		// a full chunk is written only once more follows: the last one,
		// even full, carries the flag
		if len(s.buf) == chunkSize {
			if err := s.flush(false); err != nil {
				return n, err
			}
		}
		k := min(chunkSize-len(s.buf), len(p))
		s.buf = append(s.buf, p[:k]...)
		p, n = p[k:], n+k
	}
	return n, nil
}

func (s *writer) flush(last bool) error {
	out := s.aead.Seal(nil, nonce(s.counter, last), s.buf, nil)
	s.counter++
	s.buf = s.buf[:0]
	_, err := s.w.Write(out)
	return err
}

func (s *writer) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.flush(true)
}

// Decrypt returns a reader of the plaintext of a sealed stream. A wrong key
// or a changed stream fails with ErrWrongKey while reading.
func Decrypt(r io.Reader, private []byte) (io.Reader, error) {
	key, err := ecdh.X25519().NewPrivateKey(private)
	if err != nil {
		return nil, errBadKeySize
	}
	head := make([]byte, len(magic)+32)
	if _, err := io.ReadFull(r, head); err != nil || string(head[:len(magic)]) != magic {
		return nil, ErrNotSealed
	}
	ephPub, err := ecdh.X25519().NewPublicKey(head[len(magic):])
	if err != nil {
		return nil, ErrNotSealed
	}
	shared, err := key.ECDH(ephPub)
	if err != nil {
		return nil, ErrWrongKey
	}
	aead, err := streamKey(shared, head[len(magic):], key.PublicKey().Bytes())
	if err != nil {
		return nil, err
	}
	return &reader{r: bufio.NewReaderSize(r, chunkSize+tagSize+1), aead: aead, in: make([]byte, chunkSize+tagSize)}, nil
}

type reader struct {
	r       *bufio.Reader
	aead    cipher.AEAD
	in, out []byte
	counter uint64
	done    bool
	err     error
}

func (s *reader) Read(p []byte) (int, error) {
	for len(s.out) == 0 {
		if s.err != nil {
			return 0, s.err
		}
		if s.done {
			return 0, io.EOF
		}
		s.next()
	}
	n := copy(p, s.out)
	s.out = s.out[n:]
	return n, nil
}

func (s *reader) next() {
	n, err := io.ReadFull(s.r, s.in)
	last := false
	switch {
	case err == io.ErrUnexpectedEOF || err == io.EOF:
		last = true
	case err != nil:
		s.err = err
		return
	default: // a full chunk is the last one when nothing follows it
		if _, perr := s.r.Peek(1); perr == io.EOF {
			last = true
		} else if perr != nil {
			s.err = perr
			return
		}
	}
	if n < tagSize {
		s.err = ErrWrongKey
		return
	}
	plain, oerr := s.aead.Open(nil, nonce(s.counter, last), s.in[:n], nil)
	if oerr != nil {
		s.err = ErrWrongKey
		return
	}
	s.counter++
	s.out, s.done = plain, last
}

// SealBytes seals a small value.
func SealBytes(public, plain []byte) ([]byte, error) {
	var b bytes.Buffer
	w, err := Encrypt(&b, public)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(plain); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// OpenBytes opens what SealBytes sealed.
func OpenBytes(private, sealed []byte) ([]byte, error) {
	r, err := Decrypt(bytes.NewReader(sealed), private)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

// SealString seals a text value as text (StringPrefix + base64).
func SealString(public []byte, s string) (string, error) {
	b, err := SealBytes(public, []byte(s))
	if err != nil {
		return "", err
	}
	return StringPrefix + base64.RawStdEncoding.EncodeToString(b), nil
}

// IsSealedString reports whether a text value was sealed by SealString.
func IsSealedString(s string) bool { return strings.HasPrefix(s, StringPrefix) }

// OpenString opens a value sealed by SealString; others come back as they are.
func OpenString(private []byte, s string) (string, error) {
	if !IsSealedString(s) {
		return s, nil
	}
	b, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(s, StringPrefix))
	if err != nil {
		return "", ErrNotSealed
	}
	plain, err := OpenBytes(private, b)
	return string(plain), err
}

// argon2id for wrapping keys: slower than login (the private key is
// opened only to restore), 64 MiB, three passes.
const (
	wrapTime    = 3
	wrapMemory  = 64 * 1024
	wrapThreads = 2
	saltSize    = 16
	wrapVersion = "w1"
)

// Wrap encrypts a private key with a secret (a password or a recovery
// code). The result is text: version, salt and sealed key.
func Wrap(secret string, private []byte) (string, error) {
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	aead, err := wrapAEAD(secret, salt)
	if err != nil {
		return "", err
	}
	n := make([]byte, aead.NonceSize())
	if _, err := rand.Read(n); err != nil {
		return "", err
	}
	out := aead.Seal(nil, n, private, []byte(wrapVersion))
	return wrapVersion + "." + base64.RawStdEncoding.EncodeToString(salt) + "." + base64.RawStdEncoding.EncodeToString(append(n, out...)), nil
}

// Unwrap opens a key wrapped with Wrap; a wrong secret fails with
// ErrWrongCode.
func Unwrap(secret, wrapped string) ([]byte, error) {
	parts := strings.Split(wrapped, ".")
	if len(parts) != 3 || parts[0] != wrapVersion {
		return nil, fmt.Errorf("sealbox: unknown wrapped key")
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[1])
	box, err2 := base64.RawStdEncoding.DecodeString(parts[2])
	if err1 != nil || err2 != nil || len(box) < 12+tagSize {
		return nil, fmt.Errorf("sealbox: damaged wrapped key")
	}
	aead, err := wrapAEAD(secret, salt)
	if err != nil {
		return nil, err
	}
	key, err := aead.Open(nil, box[:aead.NonceSize()], box[aead.NonceSize():], []byte(wrapVersion))
	if err != nil {
		return nil, ErrWrongCode
	}
	return key, nil
}

func wrapAEAD(secret string, salt []byte) (cipher.AEAD, error) {
	key := argon2.IDKey([]byte(secret), salt, wrapTime, wrapMemory, wrapThreads, 32)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// recovery codes: 25 characters of Crockford-like base32 (no I, L, O, U)
// in five groups, 125 bits.
var codeAlphabet = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

// NewRecoveryCode makes a recovery code to write down, like
// 7K2QD-9VXMA-…; NormalizeCode reads it back however it was typed.
func NewRecoveryCode() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	s := codeAlphabet.EncodeToString(b)[:25]
	return s[0:5] + "-" + s[5:10] + "-" + s[10:15] + "-" + s[15:20] + "-" + s[20:25], nil
}

// NormalizeCode upper-cases a typed recovery code, drops spaces and dashes
// and reads O as 0, I and L as 1.
func NormalizeCode(code string) string {
	r := strings.NewReplacer(" ", "", "-", "", "O", "0", "I", "1", "L", "1")
	s := r.Replace(strings.ToUpper(strings.TrimSpace(code)))
	if len(s) != 25 {
		return s
	}
	return s[0:5] + "-" + s[5:10] + "-" + s[10:15] + "-" + s[15:20] + "-" + s[20:25]
}
