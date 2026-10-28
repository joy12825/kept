package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/filecrypt"
	"kept/server/internal/files"
	"kept/server/internal/i18n"
	"kept/server/internal/jobs"
	"kept/server/internal/sealbox"
	"kept/server/internal/secret"
)

// Encrypted storages (v1.11.0): kept encrypts a storage's files before
// they leave this machine (files.Encrypted, package filecrypt). Each
// storage has a key of its own (storage_keys), sealed with the master key
// and, for when that is lost, sealed to the instance's recovery key: a key
// pair whose private half is wrapped with a recovery code shown once.
// The storage also holds a copy, kept-key.json, so the recovery code opens
// it without this server's database (restoring a backup from it).
//
// Turning encryption on or off changes what is written from then on, and
// queues a job converting the files already there.

// KindEncrypt converts a storage's files to what its encryption says.
const KindEncrypt = "storage.encrypt"

// KeyFile is the copy of a storage's key in the storage.
const KeyFile = "kept-key.json"

// settings of the recovery key
const (
	setRecoveryPublic  = "storage_recovery_public"
	setRecoveryPrivate = "storage_recovery_private" // wrapped with the code
	setRecoveryAt      = "storage_recovery_at"
)

var errNoRecovery = errors.New(i18n.N("还没有生成恢复码"))

type recoveryKey struct {
	public  []byte
	wrapped string
	at      time.Time
}

type querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Service) recoveryOf(ctx context.Context, q querier) (*recoveryKey, error) {
	get := func(k string) (string, error) {
		var v string
		err := q.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, k).Scan(&v)
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return v, err
	}
	pub, err := get(setRecoveryPublic)
	if err != nil || pub == "" {
		return nil, err
	}
	wrapped, err := get(setRecoveryPrivate)
	if err != nil {
		return nil, err
	}
	at, err := get(setRecoveryAt)
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(pub)
	if err != nil {
		return nil, err
	}
	ms, _ := strconv.ParseInt(at, 10, 64)
	return &recoveryKey{public: raw, wrapped: wrapped, at: time.UnixMilli(ms)}, nil
}

type keyRow struct {
	storage, keyID, sealed, recovery string
	enabled                          bool
}

func (s *Service) keyRows(ctx context.Context) ([]keyRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT storage, key_id, sealed, recovery, enabled FROM storage_keys ORDER BY storage`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []keyRow
	for rows.Next() {
		var r keyRow
		var on int64
		if err := rows.Scan(&r.storage, &r.keyID, &r.sealed, &r.recovery, &on); err != nil {
			return nil, err
		}
		r.enabled = on == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func keyOf(idHex string, raw []byte) (*filecrypt.Key, error) {
	id, err := hex.DecodeString(idHex)
	if err != nil || len(id) != filecrypt.IDSize || len(raw) != filecrypt.KeySize {
		return nil, errors.New("damaged storage key")
	}
	k := &filecrypt.Key{Raw: raw}
	copy(k.ID[:], id)
	return k, nil
}

// openSealed opens a key sealed with the master key.
func openSealed(box *secret.Box, idHex, sealed string) (*filecrypt.Key, error) {
	if box == nil {
		return nil, files.ErrLocked
	}
	plain, err := box.Open(sealed)
	if err != nil {
		return nil, files.ErrLocked
	}
	raw, err := base64.StdEncoding.DecodeString(plain)
	if err != nil {
		return nil, files.ErrLocked
	}
	return keyOf(idHex, raw)
}

func sealKey(box *secret.Box, k *filecrypt.Key) (string, error) {
	if box == nil {
		return "", errors.New("no master key")
	}
	return box.Seal(base64.StdEncoding.EncodeToString(k.Raw))
}

func sealRecovery(public []byte, k *filecrypt.Key) (string, error) {
	b, err := sealbox.SealBytes(public, k.Raw)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// LoadKeys sets up the encrypted storages, at start. A key the master key
// cannot open leaves its storage locked: its encrypted files cannot be
// read until the recovery code is entered.
func (s *Service) LoadKeys(ctx context.Context) error {
	list, err := s.keyRows(ctx)
	if err != nil {
		return err
	}
	for _, r := range list {
		k, err := openSealed(s.opt.Box, r.keyID, r.sealed)
		if err != nil {
			s.opt.Log.Error("open a storage's encryption key", "storage", r.storage, "err", err)
		}
		s.crypt(r.storage).Set(k, r.enabled, err)
	}
	return nil
}

// crypt is a storage's encryption, made when it has none.
func (s *Service) crypt(name string) *files.Crypt {
	c := s.files.Crypt(name)
	if c == nil {
		c = &files.Crypt{}
		s.files.SetCrypt(name, c)
	}
	return c
}

// keyFile is kept-key.json: the storage's key sealed as in the database,
// and the recovery key wrapped with the code, so the code alone opens it.
type keyFile struct {
	Kept        string    `json:"kept"` // "storage-key"
	Version     int       `json:"version"`
	Storage     string    `json:"storage"`
	KeyID       string    `json:"keyId"`
	Sealed      string    `json:"sealed"`      // with the master key
	Recovery    string    `json:"recovery"`    // to the recovery key
	RecoveryKey string    `json:"recoveryKey"` // the recovery key's private half, wrapped with the code
	Written     time.Time `json:"written"`
}

func (s *Service) writeKeyFile(ctx context.Context, name string, r keyRow, rec *recoveryKey) error {
	b, err := s.files.Backend(name)
	if err != nil {
		return err
	}
	kf := keyFile{Kept: "storage-key", Version: 1, Storage: name, KeyID: r.keyID, Sealed: r.sealed, Recovery: r.recovery, Written: s.opt.Now().UTC()}
	if rec != nil {
		kf.RecoveryKey = rec.wrapped
	}
	data, _ := json.MarshalIndent(kf, "", "  ")
	dir, err := s.files.NewTmpDir("key-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, KeyFile)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return err
	}
	return files.Unwrap(b).Put(ctx, KeyFile, p)
}

// KeyFromStorage opens the key in a storage's kept-key.json, with the
// master key or else the recovery code (for restoring a backup from the
// storage without this server's database). A storage without one is
// fs.ErrNotExist.
func KeyFromStorage(ctx context.Context, b files.Backend, box *secret.Box, code func() (string, error)) (*filecrypt.Key, error) {
	f, err := files.Unwrap(b).Open(ctx, KeyFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var kf keyFile
	if err := json.NewDecoder(io.LimitReader(f, 1<<20)).Decode(&kf); err != nil || kf.Kept != "storage-key" {
		return nil, fmt.Errorf("%s: damaged", KeyFile)
	}
	if k, err := openSealed(box, kf.KeyID, kf.Sealed); err == nil {
		return k, nil
	}
	if kf.RecoveryKey == "" || code == nil {
		return nil, files.ErrLocked
	}
	c, err := code()
	if err != nil {
		return nil, err
	}
	priv, err := sealbox.Unwrap(sealbox.NormalizeCode(c), kf.RecoveryKey)
	if err != nil {
		return nil, err
	}
	sealed, err := base64.StdEncoding.DecodeString(kf.Recovery)
	if err != nil {
		return nil, err
	}
	raw, err := sealbox.OpenBytes(priv, sealed)
	if err != nil {
		return nil, err
	}
	return keyOf(kf.KeyID, raw)
}

// UseKey makes b's storage readable with k (restoring from a storage).
func UseKey(fs *files.Store, name string, k *filecrypt.Key) {
	c := &files.Crypt{}
	c.Set(k, false, nil)
	fs.SetCrypt(name, c)
}

// EncryptionStatus is the instance's encryption: its recovery code and
// the storages whose key cannot be opened.
type EncryptionStatus struct {
	Recovery   bool
	RecoveryAt time.Time
	Locked     []string
}

func (s *Service) EncryptionStatus(ctx context.Context) (EncryptionStatus, error) {
	var st EncryptionStatus
	rec, err := s.recoveryOf(ctx, s.db)
	if err != nil {
		return st, err
	}
	if rec != nil {
		st.Recovery, st.RecoveryAt = true, rec.at
	}
	rows, err := s.keyRows(ctx)
	if err != nil {
		return st, err
	}
	st.Locked = []string{}
	for _, r := range rows {
		if c := s.files.Crypt(r.storage); c != nil {
			if _, err := c.Key(); err != nil {
				st.Locked = append(st.Locked, r.storage)
			}
		}
	}
	return st, nil
}

// HasRecovery reports whether the recovery code was made.
func (s *Service) HasRecovery(ctx context.Context) (bool, error) {
	rec, err := s.recoveryOf(ctx, s.db)
	return rec != nil, err
}

// NewRecoveryCode makes the instance's recovery code, or a new one (the
// old one stops working). Every storage key is sealed to it, so a key the
// master key cannot open must be recovered first. The code is shown once.
func (s *Service) NewRecoveryCode(ctx context.Context) (string, error) {
	list, err := s.keyRows(ctx)
	if err != nil {
		return "", err
	}
	keys := map[string]*filecrypt.Key{}
	for _, r := range list {
		k, err := s.crypt(r.storage).Key()
		if err != nil {
			return "", apperr.New(apperr.KindConflict, fmt.Sprintf("存储“%s”的钥匙解不开，请先用原来的恢复码解开", r.storage))
		}
		keys[r.storage] = k
	}
	kp, err := sealbox.NewKeyPair()
	if err != nil {
		return "", err
	}
	code, err := sealbox.NewRecoveryCode()
	if err != nil {
		return "", err
	}
	wrapped, err := sealbox.Wrap(code, kp.Private)
	if err != nil {
		return "", err
	}
	now := s.opt.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	for k, v := range map[string]string{
		setRecoveryPublic: base64.StdEncoding.EncodeToString(kp.Public), setRecoveryPrivate: wrapped,
		setRecoveryAt: strconv.FormatInt(now.UnixMilli(), 10),
	} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			return "", err
		}
	}
	for i, r := range list {
		sealed, err := sealRecovery(kp.Public, keys[r.storage])
		if err != nil {
			return "", err
		}
		list[i].recovery = sealed
		if _, err := tx.ExecContext(ctx, `UPDATE storage_keys SET recovery = ?, updated_at = ? WHERE storage = ?`, sealed, now.UnixMilli(), r.storage); err != nil {
			return "", err
		}
	}
	summary := fmt.Sprintf(i18n.N("%s 生成了存储加密的恢复码"), audit.ActorFrom(ctx).Name)
	if err := audit.Record(ctx, s.q.WithTx(tx), audit.Event{Action: "storage.recovery", TargetType: "storage", Summary: summary, At: now}); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	rec := &recoveryKey{public: kp.Public, wrapped: wrapped, at: now}
	for _, r := range list {
		if err := s.writeKeyFile(ctx, r.storage, r, rec); err != nil {
			s.opt.Log.Warn("write a storage's key file", "storage", r.storage, "err", err)
		}
	}
	return code, nil
}

// Encryptable: a storage that can be encrypted, which is any but the
// data directory.
func (s *Service) encryptable(name string) error {
	if name == files.Local {
		return apperr.New(apperr.KindInvalid, "数据目录就在这台服务器上，不需要加密")
	}
	if _, err := s.files.Backend(name); err != nil {
		return apperr.New(apperr.KindNotFound, "没有这个存储")
	}
	return nil
}

type encryptPayload struct {
	Storage string `json:"storage"`
}

func (s *Service) queueEncrypt(ctx context.Context, tx *sql.Tx, name string) (int64, error) {
	payload, _ := json.Marshal(encryptPayload{Storage: name})
	var active int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM jobs WHERE kind = ? AND payload = ? AND status IN ('queued', 'running') ORDER BY id LIMIT 1`, KindEncrypt, string(payload)).Scan(&active)
	if err == nil {
		return active, nil // it follows the setting as it runs
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	return s.opt.Queue.EnqueueWith(ctx, s.q.WithTx(tx), KindEncrypt, encryptPayload{Storage: name}, jobs.EnqueueOptions{MaxAttempts: 10})
}

// Recover opens the storage keys the master key cannot open with the
// recovery code, and seals them with the master key again. It returns how
// many it opened.
func (s *Service) Recover(ctx context.Context, code string) (int, error) {
	rec, err := s.recoveryOf(ctx, s.db)
	if err != nil {
		return 0, err
	}
	if rec == nil {
		return 0, apperr.New(apperr.KindConflict, errNoRecovery.Error())
	}
	priv, err := sealbox.Unwrap(sealbox.NormalizeCode(code), rec.wrapped)
	if errors.Is(err, sealbox.ErrWrongCode) {
		return 0, apperr.New(apperr.KindInvalid, "恢复码不对")
	}
	if err != nil {
		return 0, err
	}
	list, err := s.keyRows(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	now := s.opt.Now()
	for _, r := range list {
		c := s.crypt(r.storage)
		if _, err := c.Key(); err == nil {
			continue
		}
		sealed, err := base64.StdEncoding.DecodeString(r.recovery)
		if err != nil {
			return n, err
		}
		raw, err := sealbox.OpenBytes(priv, sealed)
		if err != nil {
			return n, fmt.Errorf("storage %s: %w", r.storage, err)
		}
		k, err := keyOf(r.keyID, raw)
		if err != nil {
			return n, err
		}
		resealed, err := sealKey(s.opt.Box, k)
		if err != nil {
			return n, err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return n, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE storage_keys SET sealed = ?, updated_at = ? WHERE storage = ?`, resealed, now.UnixMilli(), r.storage); err != nil {
			tx.Rollback()
			return n, err
		}
		if err := audit.Record(ctx, s.q.WithTx(tx), audit.Event{Action: "storage.recover", TargetType: "storage", TargetID: r.storage,
			Summary: fmt.Sprintf(i18n.N("%s 用恢复码解开了存储“%s”的钥匙"), audit.ActorFrom(ctx).Name, r.storage), At: now}); err != nil {
			tx.Rollback()
			return n, err
		}
		if _, err := s.queueEncrypt(ctx, tx, r.storage); err != nil {
			tx.Rollback()
			return n, err
		}
		if err := tx.Commit(); err != nil {
			return n, err
		}
		c.Set(k, r.enabled, nil)
		r.sealed = resealed
		if err := s.writeKeyFile(ctx, r.storage, r, rec); err != nil {
			s.opt.Log.Warn("write a storage's key file", "storage", r.storage, "err", err)
		}
		n++
	}
	s.opt.Queue.Wake()
	return n, nil
}

// forgetKey drops a deleted storage's key.
func forgetKey(ctx context.Context, tx *sql.Tx, name string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM storage_keys WHERE storage = ?`, name)
	return err
}

// Converted is a conversion's progress and result, in files.
type Converted struct {
	Total     int `json:"total"`
	Converted int `json:"converted"`
	Skipped   int `json:"skipped"` // the document was busy or changed meanwhile; tried again later
	Failed    int `json:"failed"`
}

// docKinds holding documents' files, and the document a file is of: the
// number its name starts with.
var convertKinds = []string{"originals", "archive", "versions", "sealed"}

func docOfKey(key string) int64 {
	name := path.Base(key)
	end := 0
	for end < len(name) && name[end] >= '0' && name[end] <= '9' {
		end++
	}
	id, _ := strconv.ParseInt(name[:end], 10, 64)
	return id
}

// convert brings one storage's files in line with its encryption.
func (s *Service) convert(ctx context.Context, job jobs.Job) (any, error) {
	var p encryptPayload
	if err := json.Unmarshal([]byte(job.Payload), &p); err != nil || p.Storage == "" {
		return nil, jobs.Permanent(errors.New("no storage"))
	}
	c := s.files.Crypt(p.Storage)
	if c == nil {
		return nil, nil // never encrypted
	}
	b, err := s.files.Backend(p.Storage)
	if err != nil {
		return nil, jobs.Permanent(fmt.Errorf(i18n.N("存储“%s”没有配置"), p.Storage))
	}
	enc, ok := b.(files.Encrypted)
	if !ok {
		return nil, nil
	}
	if _, err := c.Key(); err != nil {
		return nil, jobs.Permanent(err) // the recovery code queues it again
	}
	want := c.On()
	tmp, err := s.files.NewTmpDir("encrypt-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	var res Converted
	var lastErr error
	progress := func() { _ = s.opt.Queue.SetProgress(ctx, job.ID, res) }

	// Documents' files, workspace by workspace, document by document.
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM workspaces ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var spaces []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		spaces = append(spaces, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, ws := range spaces {
		byDoc := map[int64][]string{}
		var order []int64
		for _, kind := range convertKinds {
			keys, err := enc.List(ctx, kind+"/"+strconv.FormatInt(ws, 10)+"/")
			if err != nil {
				return res, err
			}
			for _, k := range keys {
				d := docOfKey(k)
				if _, ok := byDoc[d]; !ok {
					order = append(order, d)
				}
				byDoc[d] = append(byDoc[d], k)
			}
		}
		for _, d := range order {
			if err := ctx.Err(); err != nil {
				return res, err
			}
			done, skipped, err := s.convertDocument(ctx, enc, ws, d, byDoc[d], tmp)
			res.Total += len(byDoc[d])
			res.Converted += done
			if skipped {
				res.Skipped += len(byDoc[d]) - done
			}
			if err != nil {
				res.Failed++
				lastErr = err
				s.opt.Log.Warn("convert a document's files", "storage", p.Storage, "document", d, "err", err)
			}
			progress()
		}
	}

	// Backups in the storage.
	if files.CanWalk(enc) {
		entries, err := files.Walk(ctx, enc, backupFolder)
		if err != nil {
			return res, err
		}
		for _, e := range entries {
			if err := ctx.Err(); err != nil {
				return res, err
			}
			res.Total++
			release := c.Exclusive() // not while a backup writes there
			n, err := s.convertFiles(ctx, enc, 0, []string{e.Key}, tmp, nil)
			release()
			res.Converted += n
			if err != nil {
				res.Failed++
				lastErr = err
				s.opt.Log.Warn("convert a backup file", "storage", p.Storage, "key", e.Key, "err", err)
			}
			progress()
		}
	}

	switch {
	case c.On() != want:
		return res, errors.New(i18n.N("加密设置改了，稍后按新的设置继续"))
	case res.Failed > 0:
		return res, fmt.Errorf(i18n.N("%d 个文件没能转换，稍后会再试。例如：%w"), res.Failed, lastErr)
	case res.Skipped > 0:
		return res, fmt.Errorf(i18n.N("%d 个文件所在的文档正在处理或刚改过，稍后会再试"), res.Skipped)
	}
	s.opt.Log.Info("converted a storage's files", "storage", p.Storage, "encrypted", want, "files", res.Converted)
	return res, nil
}

// backupFolder is where backups go in a storage (backup.TargetPrefix).
const backupFolder = "kept-backup"

// convertDocument converts one document's files; skipped means it was
// busy or changed meanwhile and stays as it is for now.
func (s *Service) convertDocument(ctx context.Context, enc files.Encrypted, ws, doc int64, keys []string, tmp string) (int, bool, error) {
	state := func() (string, error) {
		var busy int
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE document_id = ? AND status IN ('queued', 'running')`, doc).Scan(&busy); err != nil {
			return "", err
		}
		if busy > 0 {
			return "busy", nil
		}
		var mod int64
		err := s.db.QueryRowContext(ctx, `SELECT modified_at FROM documents WHERE id = ?`, doc).Scan(&mod)
		if errors.Is(err, sql.ErrNoRows) {
			return "gone", nil // a file without a document: nothing changes it
		}
		return strconv.FormatInt(mod, 10), err
	}
	before, err := state()
	if err != nil {
		return 0, false, err
	}
	if before == "busy" {
		return 0, true, nil
	}
	skipped := false
	n, err := s.convertFiles(ctx, enc, ws, keys, tmp, func() bool {
		after, err := state()
		if err != nil || after != before {
			skipped = true
			return false
		}
		return true
	})
	return n, skipped, err
}

// convertFiles converts the files at keys: each is written in its new
// form under a temporary key and read back, and when unchanged (still
// says yes) put in place of the old one. It returns how many it converted.
func (s *Service) convertFiles(ctx context.Context, enc files.Encrypted, ws int64, keys []string, tmp string, still func() bool) (int, error) {
	want := enc.Crypt.On()
	type ready struct{ key, tmpKey string }
	var made []ready
	bctx := context.WithoutCancel(ctx)
	defer func() {
		for _, r := range made {
			_ = enc.Remove(bctx, r.tmpKey)
		}
	}()
	for _, key := range keys {
		is, err := enc.IsEncrypted(ctx, key)
		if errors.Is(err, fs.ErrNotExist) {
			continue // removed meanwhile
		}
		if err != nil {
			return 0, err
		}
		if is == want {
			continue
		}
		tmpKey, err := s.rewrite(ctx, enc, ws, key, want, tmp)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", key, err)
		}
		made = append(made, ready{key, tmpKey})
	}
	if len(made) == 0 || (still != nil && !still()) {
		return 0, nil
	}
	n := 0
	for len(made) > 0 {
		r := made[0]
		if err := enc.Rename(ctx, r.tmpKey, r.key); err != nil {
			return n, fmt.Errorf("%s: %w", r.key, err)
		}
		made = made[1:]
		n++
	}
	return n, nil
}

// rewrite writes the file at key in its new form under a temporary key,
// and reads it back to compare.
func (s *Service) rewrite(ctx context.Context, enc files.Encrypted, ws int64, key string, want bool, tmp string) (string, error) {
	f, err := enc.Open(ctx, key)
	if err != nil {
		return "", err
	}
	local := filepath.Join(tmp, "plain")
	out, err := os.Create(local)
	if err != nil {
		f.Close()
		return "", err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, h), f)
	f.Close()
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	defer os.Remove(local)
	if err != nil {
		return "", err
	}
	sum := h.Sum(nil)
	var rnd [12]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", err
	}
	tmpKey := path.Join("incoming", strconv.FormatInt(ws, 10), "convert-"+hex.EncodeToString(rnd[:]))
	if enc.Crypt.On() != want {
		return "", errors.New(i18n.N("加密设置改了，稍后按新的设置继续"))
	}
	if err := enc.Put(ctx, tmpKey, local); err != nil {
		return "", err
	}
	ok := false
	defer func() {
		if !ok {
			_ = enc.Remove(context.WithoutCancel(ctx), tmpKey)
		}
	}()
	if is, err := enc.IsEncrypted(ctx, tmpKey); err != nil {
		return "", err
	} else if is != want {
		return "", errors.New(i18n.N("加密设置改了，稍后按新的设置继续"))
	}
	back, err := enc.Open(ctx, tmpKey)
	if err != nil {
		return "", err
	}
	h.Reset()
	_, err = io.Copy(h, back)
	back.Close()
	if err != nil {
		return "", err
	}
	if string(h.Sum(nil)) != string(sum) {
		return "", errors.New(i18n.N("转换后校验值不符"))
	}
	ok = true
	return tmpKey, nil
}

// Encryption is a storage's encryption in the administrator's list.
type Encryption struct {
	Enabled    bool
	Locked     bool // its key cannot be opened
	Converting bool
	Progress   *Converted
	LastError  string
}

// encryptionOf lists the storages' encryption, by name.
func (s *Service) encryptionOf(ctx context.Context) (map[string]*Encryption, error) {
	out := map[string]*Encryption{}
	list, err := s.keyRows(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range list {
		e := &Encryption{Enabled: r.enabled}
		if c := s.files.Crypt(r.storage); c != nil {
			_, err := c.Key()
			e.Locked = err != nil
		}
		out[r.storage] = e
	}
	// the newest conversion of each storage
	rows, err := s.db.QueryContext(ctx, `SELECT payload, status, error, result FROM jobs WHERE kind = ? ORDER BY id`, KindEncrypt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var payload, status string
		var jerr, result sql.NullString
		if err := rows.Scan(&payload, &status, &jerr, &result); err != nil {
			return nil, err
		}
		var p encryptPayload
		_ = json.Unmarshal([]byte(payload), &p)
		e, ok := out[p.Storage]
		if !ok {
			continue
		}
		e.Converting, e.Progress, e.LastError = false, nil, ""
		switch status {
		case "queued", "running":
			e.Converting = true
			e.LastError = jerr.String
			var m Converted
			if result.Valid && json.Unmarshal([]byte(result.String), &m) == nil && m.Total > 0 {
				e.Progress = &m
			}
		case "failed":
			e.LastError = jerr.String
		}
	}
	return out, rows.Err()
}
