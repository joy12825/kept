package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"kept/server/internal/files"
)

// RestoreOptions choose what Restore does.
type RestoreOptions struct {
	DataDir  string
	Snapshot string // file name in db/; "" means the newest
	// Force restores even when files are missing or damaged, leaving them
	// out (the integrity check reports them afterwards).
	Force bool
	// Files puts documents of other storages back into them, where they
	// are missing (v1.7.6); nil, or a storage not configured, leaves them.
	Files *files.Store
}

// RestoreResult is what a restore did.
type RestoreResult struct {
	Snapshot  string
	Documents int
	Files     int
	External  int // files in another storage, not part of the backup
	// SealedKeys: storages added on the page keep keys sealed with the
	// master key (v1.7.2), which backups leave out.
	SealedKeys bool
	Problems   []string // missing or damaged files (only with Force)
	// Sealed counts private documents' files that came back sealed (v1.8.1):
	// kept private restore opens them with the person's recovery code.
	Sealed int
}

// ErrDamaged lists the files that keep a backup from being restored.
type ErrDamaged struct{ Problems []string }

func (e *ErrDamaged) Error() string {
	return fmt.Sprintf("备份中有 %d 个文件缺失或损坏", len(e.Problems))
}

// Restore copies a backup into an empty data directory. The server must
// not be running on it; the search index is rebuilt when it starts.
func Restore(ctx context.Context, dir string, opt RestoreOptions) (RestoreResult, error) {
	dbPath := filepath.Join(opt.DataDir, "app.db")
	if _, err := os.Stat(dbPath); err == nil {
		return RestoreResult{}, fmt.Errorf("数据目录 %s 里已经有数据库，只能恢复到空的数据目录", opt.DataDir)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return RestoreResult{}, err
	}
	snaps, err := Snapshots(dir)
	if err != nil {
		return RestoreResult{}, err
	}
	if len(snaps) == 0 {
		return RestoreResult{}, fmt.Errorf("%s 里没有找到备份（db/app-*.db）", dir)
	}
	name := snaps[len(snaps)-1]
	if opt.Snapshot != "" {
		if !slices.Contains(snaps, opt.Snapshot) {
			return RestoreResult{}, fmt.Errorf("没有这个快照：%s（可用：%v）", opt.Snapshot, snaps)
		}
		name = opt.Snapshot
	}
	snap := filepath.Join(dir, "db", name)
	list, docs, err := snapshotFiles(ctx, snap)
	if err != nil {
		return RestoreResult{}, err
	}

	// Check before writing anything: every file present, originals intact.
	var problems []string
	bad := map[string]bool{}
	external := 0
	for _, f := range list {
		if err := ctx.Err(); err != nil {
			return RestoreResult{}, err
		}
		if !f.local() && !(Options{Files: opt.Files}).canRead(f.storage) {
			external++
			continue
		}
		p := asOf(dir, name, f.rel)
		if _, err := os.Stat(p); err != nil {
			problems = append(problems, f.rel+"：缺失")
			bad[f.rel] = true
			continue
		}
		if f.checksum != "" {
			if sum, err := fileSHA256(p); err != nil || sum != f.checksum {
				problems = append(problems, f.rel+"：校验值不符")
				bad[f.rel] = true
			}
		}
	}
	if len(problems) > 0 && !opt.Force {
		return RestoreResult{}, &ErrDamaged{Problems: problems}
	}

	res := RestoreResult{Snapshot: name, Documents: docs, External: external, Problems: problems, SealedKeys: sealedKeys(ctx, snap)}
	for _, f := range list {
		if bad[f.rel] {
			continue
		}
		if f.local() {
			if err := files.Copy(asOf(dir, name, f.rel), filepath.Join(opt.DataDir, f.rel)); err != nil {
				return res, err
			}
			res.Files++
			continue
		}
		if !(Options{Files: opt.Files}).canRead(f.storage) {
			continue
		}
		// Another storage: put back only what it lost.
		if opt.Files.Exists(ctx, f.storage, f.ref) {
			continue
		}
		if err := opt.Files.Put(ctx, f.storage, f.ref, asOf(dir, name, f.rel), false); err != nil {
			return res, fmt.Errorf("放回存储“%s”：%w", f.storage, err)
		}
		res.Files++
	}
	if err := mirror(filepath.Join(dir, "branding"), filepath.Join(opt.DataDir, "branding")); err != nil {
		return res, err
	}
	// The database goes last: a half-finished restore leaves no app.db, so
	// it can simply be run again.
	if err := files.Copy(snap, dbPath); err != nil {
		return res, err
	}
	return res, nil
}

// sealedKeys reports whether a snapshot holds storage keys sealed with the
// master key (snapshots before v1.7.2 have no storages table).
func sealedKeys(ctx context.Context, snap string) bool {
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(snap)+"?mode=ro")
	if err != nil {
		return false
	}
	defer db.Close()
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM storages WHERE secret <> ''`).Scan(&n); err != nil {
		return false
	}
	return n > 0
}
