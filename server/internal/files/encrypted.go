package files

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"

	"kept/server/internal/filecrypt"
	"kept/server/internal/i18n"

	"github.com/minio/minio-go/v7"
)

// Encrypted storages (v1.11.0): a storage whose files kept encrypts
// before they leave this machine (package filecrypt). The storage's
// backend is wrapped in Encrypted, so code reading and writing documents'
// files does not change.
//
// Only kept's own folders are encrypted (encryptedKinds); folders people
// read or write themselves — a remote inbox, the readable export — stay as
// they are. Reading tells an encrypted file by its header, so a storage
// being converted holds both, and turning encryption off only changes what
// is written next.

// ErrLocked: the storage's key could not be opened (the master key
// changed), so its encrypted files cannot be read nor new ones written.
var ErrLocked = errors.New(i18n.N("这个存储的加密钥匙解不开（主密钥变了），请在“设置 → 存储”里输入恢复码"))

// encryptedKinds are the folders an encrypted storage encrypts: documents'
// files and the backups.
var encryptedKinds = map[string]bool{"originals": true, "archive": true, "versions": true, "incoming": true, "sealed": true, "kept-backup": true}

// Covered reports whether key is in a folder an encrypted storage encrypts.
func Covered(key string) bool {
	first, _, _ := strings.Cut(key, "/")
	return encryptedKinds[first]
}

// Crypt is a storage's encryption: its key, and whether new files are
// encrypted. It is shared by the storage's backend however often that is
// replaced (a changed address, a new secret key).
type Crypt struct {
	mu     sync.RWMutex
	key    *filecrypt.Key
	on     bool
	locked error // why there is no key

	busy sync.Mutex // see Exclusive
}

// Exclusive keeps a backup into the storage and the conversion of its
// files from overwriting each other; the returned func releases it.
func (c *Crypt) Exclusive() func() {
	c.busy.Lock()
	return c.busy.Unlock
}

// Set changes the key (nil with locked, the reason, when it cannot be
// opened) and whether new files are encrypted.
func (c *Crypt) Set(key *filecrypt.Key, on bool, locked error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.key, c.on, c.locked = key, on, locked
}

// On reports whether new files are encrypted.
func (c *Crypt) On() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.on
}

// Key is the storage's key, or the reason there is none.
func (c *Crypt) Key() (*filecrypt.Key, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.key == nil {
		if c.locked != nil {
			return nil, c.locked
		}
		return nil, ErrLocked
	}
	return c.key, nil
}

// Encrypted is a backend whose files are encrypted (see Crypt).
type Encrypted struct {
	Raw   Backend
	Crypt *Crypt
	Tmp   string // where files are encrypted before they are stored
}

// Unwrap is the backend under an Encrypted one, else b.
func Unwrap(b Backend) Backend {
	if e, ok := b.(Encrypted); ok {
		return e.Raw
	}
	return b
}

type encFile struct {
	*filecrypt.Reader
	raw  File
	info Info
}

func (f encFile) Info() Info   { return f.info }
func (f encFile) Close() error { return f.raw.Close() }

// readHead reads the first n bytes of f (fewer when it is shorter).
func readHead(f io.Reader, n int) ([]byte, error) {
	buf := make([]byte, n)
	m, err := io.ReadFull(f, buf)
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		err = nil
	}
	return buf[:m], err
}

func (e Encrypted) Open(ctx context.Context, key string) (File, error) {
	f, err := e.Raw.Open(ctx, key)
	if err != nil || !Covered(key) {
		return f, err
	}
	head, err := readHead(f, filecrypt.HeaderSize)
	if err != nil {
		f.Close()
		return nil, err
	}
	if !filecrypt.IsEncrypted(head) {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			f.Close()
			return nil, err
		}
		return f, nil
	}
	k, err := e.Crypt.Key()
	if err != nil {
		f.Close()
		return nil, err
	}
	info := f.Info()
	r, err := filecrypt.NewReader(&headSeeker{head: head, f: f}, info.Size, *k)
	if err != nil {
		f.Close()
		return nil, err
	}
	return encFile{Reader: r, raw: f, info: Info{Size: r.Size(), ModTime: info.ModTime}}, nil
}

// headSeeker reads at offsets from a file whose first bytes were read
// already, without reading them again.
type headSeeker struct {
	head []byte
	f    io.ReadSeeker
	at   filecrypt.SeekerAt
	init bool
}

func (h *headSeeker) ReadAt(p []byte, off int64) (int, error) {
	if off+int64(len(p)) <= int64(len(h.head)) {
		return copy(p, h.head[off:]), nil
	}
	if !h.init {
		h.at = filecrypt.SeekerAt{R: h.f}
		h.at.SetPos(int64(len(h.head)))
		h.init = true
	}
	return h.at.ReadAt(p, off)
}

func (e Encrypted) Stat(ctx context.Context, key string) (Info, error) {
	if !Covered(key) {
		return e.Raw.Stat(ctx, key)
	}
	head, info, err := statHead(ctx, e.Raw, key)
	if err != nil {
		return Info{}, err
	}
	if filecrypt.IsEncrypted(head) {
		if n := filecrypt.PlainSize(info.Size); n >= 0 {
			info.Size = n
		}
	}
	return info, nil
}

// IsEncrypted reports whether the stored file at key is encrypted.
func (e Encrypted) IsEncrypted(ctx context.Context, key string) (bool, error) {
	head, _, err := statHead(ctx, e.Raw, key)
	if err != nil {
		return false, err
	}
	return filecrypt.IsEncrypted(head), nil
}

// statHead is an object's information and first bytes.
func statHead(ctx context.Context, b Backend, key string) ([]byte, Info, error) {
	if s3, ok := b.(*S3Backend); ok { // two small requests, not a download
		info, err := s3.Stat(ctx, key)
		if err != nil || info.Size == 0 {
			return nil, info, err
		}
		name, _ := s3.name(key)
		opts := minio.GetObjectOptions{}
		if err := opts.SetRange(0, int64(min(int64(filecrypt.HeaderSize), info.Size))-1); err != nil {
			return nil, Info{}, err
		}
		obj, err := s3.c.GetObject(ctx, s3.bucket, name, opts)
		if err != nil {
			return nil, Info{}, notFound(err)
		}
		defer obj.Close()
		head, err := readHead(obj, filecrypt.HeaderSize)
		return head, info, notFound(err)
	}
	f, err := b.Open(ctx, key)
	if err != nil {
		return nil, Info{}, err
	}
	defer f.Close()
	head, err := readHead(f, filecrypt.HeaderSize)
	return head, f.Info(), err
}

func (e Encrypted) Put(ctx context.Context, key, src string) error {
	if !Covered(key) || !e.Crypt.On() {
		return e.Raw.Put(ctx, key, src)
	}
	k, err := e.Crypt.Key()
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(e.Tmp, 0o755); err != nil {
		return err
	}
	out, err := os.CreateTemp(e.Tmp, "enc-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	if _, err := filecrypt.Encrypt(out, in, *k); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return e.Raw.Put(ctx, key, out.Name())
}

func (e Encrypted) Rename(ctx context.Context, from, to string) error {
	return e.Raw.Rename(ctx, from, to)
}

func (e Encrypted) Remove(ctx context.Context, key string) error { return e.Raw.Remove(ctx, key) }

func (e Encrypted) List(ctx context.Context, prefix string) ([]string, error) {
	return e.Raw.List(ctx, prefix)
}

// LocalPath gives tools the file in place only when it is not encrypted
// and new files are not either (they would be written there as they are).
func (e Encrypted) LocalPath(key string) string {
	p := e.Raw.LocalPath(key)
	if p == "" || !Covered(key) {
		return p
	}
	if e.Crypt.On() {
		return ""
	}
	f, err := os.Open(p)
	if err != nil {
		return p // not there yet
	}
	defer f.Close()
	if head, _ := readHead(f, filecrypt.HeaderSize); filecrypt.IsEncrypted(head) {
		return ""
	}
	return p
}

func (e Encrypted) Describe() string { return e.Raw.Describe() }
