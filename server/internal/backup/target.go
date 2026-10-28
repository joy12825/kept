package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"kept/server/internal/files"
	"kept/server/internal/i18n"
)

// Backups into a storage (v1.7.6): S3, WebDAV or a directory configured as
// a storage. The layout is the backup directory's, under kept-backup/ in
// the storage: db/app-<time>.db, media/…, storages/<name>/…, branding/…,
// kept-backup.json. A storage cannot list deep folders cheaply, so an
// index (kept-backup-index.json) records the files copied and the files
// each snapshot needs; a file is copied again only when its size or
// modification time changed.

// KindStorage is the job that backs up into the storage chosen on the page.
const KindStorage = "system.backup-storage"

// TargetPrefix is where a storage keeps its backups.
const TargetPrefix = "kept-backup"

const indexName = "kept-backup-index.json"

// ErrTargetHoldsDocuments: a storage that keeps documents is no backup of them.
var ErrTargetHoldsDocuments = errors.New(i18n.N("这个存储里放着文档，不能同时作为备份目标"))

type index struct {
	Files     map[string]indexed  `json:"files"`     // backup key → the source it was copied from
	Snapshots map[string][]string `json:"snapshots"` // snapshot → the keys it needs
	// Replaced (v1.7.12): old contents kept under replaced/<owner>/<key>
	// when a later backup overwrote key, by the last snapshot that had them.
	Replaced map[string][]string `json:"replaced,omitempty"`
}

type indexed struct {
	Size int64 `json:"size"`
	Mod  int64 `json:"mod"` // the source's modification time, unix ms
}

// prefixed puts a backend's keys under a folder.
type prefixed struct {
	files.Backend
	p string
}

func (b prefixed) k(key string) string { return b.p + "/" + key }
func (b prefixed) Open(ctx context.Context, key string) (files.File, error) {
	return b.Backend.Open(ctx, b.k(key))
}
func (b prefixed) Stat(ctx context.Context, key string) (files.Info, error) {
	return b.Backend.Stat(ctx, b.k(key))
}
func (b prefixed) Put(ctx context.Context, key, src string) error {
	return b.Backend.Put(ctx, b.k(key), src)
}
func (b prefixed) Remove(ctx context.Context, key string) error {
	return b.Backend.Remove(ctx, b.k(key))
}
func (b prefixed) Rename(ctx context.Context, from, to string) error {
	return b.Backend.Rename(ctx, b.k(from), b.k(to))
}
func (b prefixed) List(ctx context.Context, prefix string) ([]string, error) {
	keys, err := b.Backend.List(ctx, b.k(prefix))
	for i, k := range keys {
		keys[i] = strings.TrimPrefix(k, b.p+"/")
	}
	return keys, err
}

func targetOf(fs *files.Store, name string) (files.Backend, error) {
	b, err := fs.Backend(name)
	if err != nil {
		return nil, fmt.Errorf(i18n.N("备份目标“%s”没有配置"), name)
	}
	return prefixed{Backend: b, p: TargetPrefix}, nil
}

// TargetSnapshots lists the snapshots in a storage, oldest first.
func TargetSnapshots(ctx context.Context, fs *files.Store, name string) ([]string, error) {
	b, err := targetOf(fs, name)
	if err != nil {
		return nil, err
	}
	return listSnapshots(ctx, b)
}

func listSnapshots(ctx context.Context, b files.Backend) ([]string, error) {
	keys, err := b.List(ctx, "db/"+snapPrefix)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, k := range keys {
		if n := path.Base(k); strings.HasSuffix(n, ".db") {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out, nil
}

func readJSON(ctx context.Context, b files.Backend, key string, v any) error {
	f, err := b.Open(ctx, key)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(v)
}

// fetchKey downloads one object of the backup to dst.
func fetchKey(ctx context.Context, b files.Backend, key, dst string) error {
	f, err := b.Open(ctx, key)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	part := dst + ".part"
	out, err := os.Create(part)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, f); err != nil {
		out.Close()
		os.Remove(part)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(part)
		return err
	}
	return os.Rename(part, dst)
}

// TargetFunc reads the backup target chosen on the page: storage "" is none.
type TargetFunc func(ctx context.Context) (storage string, keep int, err error)

// RestoreFrom restores a backup kept in a storage into an empty data
// directory: files of the data directory go there, those of other
// storages back into them where missing. Originals are checked against
// their recorded SHA-256 as they arrive; the database comes last, so an
// interrupted restore can simply be run again.
func RestoreFrom(ctx context.Context, target string, opt RestoreOptions) (RestoreResult, error) {
	dbPath := filepath.Join(opt.DataDir, "app.db")
	if _, err := os.Stat(dbPath); err == nil {
		return RestoreResult{}, fmt.Errorf("数据目录 %s 里已经有数据库，只能恢复到空的数据目录", opt.DataDir)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return RestoreResult{}, err
	}
	if opt.Files == nil {
		return RestoreResult{}, errors.New("没有存储")
	}
	b, err := targetOf(opt.Files, target)
	if err != nil {
		return RestoreResult{}, err
	}
	snaps, err := listSnapshots(ctx, b)
	if err != nil {
		return RestoreResult{}, err
	}
	if len(snaps) == 0 {
		return RestoreResult{}, fmt.Errorf("存储“%s”的 %s/ 里没有找到备份", target, TargetPrefix)
	}
	name := snaps[len(snaps)-1]
	if opt.Snapshot != "" {
		if !slices.Contains(snaps, opt.Snapshot) {
			return RestoreResult{}, fmt.Errorf("没有这个快照：%s（可用：%v）", opt.Snapshot, snaps)
		}
		name = opt.Snapshot
	}
	tmp := filepath.Join(opt.DataDir, "tmp", "restore")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return RestoreResult{}, err
	}
	defer os.RemoveAll(tmp)
	snap := filepath.Join(tmp, name)
	if err := fetchKey(ctx, b, "db/"+name, snap); err != nil {
		return RestoreResult{}, fmt.Errorf("下载快照：%w", err)
	}
	var idx index
	_ = readJSON(ctx, b, indexName, &idx) // older backups have none: latest copies
	asOfKey := func(key string) string {
		want := strings.TrimSuffix(name, ".db")
		var owners []string
		for o, keys := range idx.Replaced {
			if o >= want && slices.Contains(keys, key) {
				owners = append(owners, o)
			}
		}
		if len(owners) == 0 {
			return key
		}
		slices.Sort(owners)
		return replacedDir + "/" + owners[0] + "/" + key
	}
	list, docs, err := snapshotFiles(ctx, snap)
	if err != nil {
		return RestoreResult{}, err
	}
	res := RestoreResult{Snapshot: name, Documents: docs, SealedKeys: sealedKeys(ctx, snap)}
	if list, _, err = sealPrivateFiles(ctx, snap, list); err != nil {
		return res, err
	}
	for _, f := range list {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		key := filepath.ToSlash(f.rel)
		if f.sealed != nil {
			// a private document's file comes back sealed, next to where it
			// belongs in the data directory; kept private restore opens it
			if err := fetchKey(ctx, b, asOfKey(key), filepath.Join(opt.DataDir, mediaRel(f.ref)+SealedSuffix)); err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					return res, err
				}
				res.Problems = append(res.Problems, key+"：缺失")
			} else {
				res.Sealed++
			}
			continue
		}
		local := f.local()
		if !local && !(Options{Files: opt.Files}).canRead(f.storage) {
			res.External++
			continue
		}
		if !local && opt.Files.Exists(ctx, f.storage, f.ref) {
			continue // the storage still has it
		}
		dst := filepath.Join(opt.DataDir, f.rel)
		if !local {
			dst = filepath.Join(tmp, "file")
		}
		if err := fetchKey(ctx, b, asOfKey(key), dst); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				return res, err
			}
			res.Problems = append(res.Problems, key+"：缺失")
		} else if f.checksum != "" {
			if sum, err := fileSHA256(dst); err != nil || sum != f.checksum {
				os.Remove(dst)
				res.Problems = append(res.Problems, key+"：校验值不符")
			} else if !local {
				if err := opt.Files.Put(ctx, f.storage, f.ref, dst, true); err != nil {
					return res, fmt.Errorf("放回存储“%s”：%w", f.storage, err)
				}
				res.Files++
			} else {
				res.Files++
			}
		} else if !local {
			if err := opt.Files.Put(ctx, f.storage, f.ref, dst, true); err != nil {
				return res, fmt.Errorf("放回存储“%s”：%w", f.storage, err)
			}
			res.Files++
		} else {
			res.Files++
		}
		if len(res.Problems) > 0 && !opt.Force {
			return res, &ErrDamaged{Problems: res.Problems}
		}
	}
	brand, err := b.List(ctx, "branding/")
	if err != nil {
		return res, err
	}
	for _, key := range brand {
		if err := fetchKey(ctx, b, key, filepath.Join(opt.DataDir, "branding", path.Base(key))); err != nil {
			return res, err
		}
	}
	if err := files.Copy(snap, dbPath); err != nil {
		return res, err
	}
	return res, nil
}
