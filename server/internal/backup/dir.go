package backup

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"kept/server/internal/i18n"
)

// The daily backup directory (v1.13.9): KEPT_BACKUP_DIR when the
// deployment sets one, otherwise the one an administrator chose on the page
// ("File safety"). It is the free edition's way to keep a copy every day,
// for example on a second disk attached to the server.

const dirSetting = "backup_dir"

var (
	// ErrDirFromEnv: the deployment's KEPT_BACKUP_DIR decides; the page cannot change it.
	ErrDirFromEnv = errors.New(i18n.N("备份目录是部署时设置的（KEPT_BACKUP_DIR），请在部署配置里修改"))
	// ErrDirNotAbsolute and the others: why a directory cannot hold backups.
	ErrDirNotAbsolute = errors.New(i18n.N("请填服务器上的完整路径，例如 /mnt/usb/kept-backup"))
	ErrDirMissing     = errors.New(i18n.N("服务器上没有这个目录，请先建好或接上硬盘"))
	ErrDirNotDir      = errors.New(i18n.N("这个路径不是目录"))
	ErrDirInData      = errors.New(i18n.N("备份目录不能放在数据目录里，也不能包含数据目录"))
	ErrDirNotWritable = errors.New(i18n.N("收好不能往这个目录里写，请检查权限"))
)

// Dir says where daily backups go.
type Dir struct {
	db      *sql.DB
	env     string
	dataDir string
}

// NewDir: env is KEPT_BACKUP_DIR ("" when not set).
func NewDir(db *sql.DB, env, dataDir string) *Dir {
	return &Dir{db: db, env: env, dataDir: dataDir}
}

// FromEnv: the deployment sets the directory.
func (d *Dir) FromEnv() bool { return d != nil && d.env != "" }

// Get is the directory, "" when daily backups are off. A setting that
// cannot be read counts as none.
func (d *Dir) Get(ctx context.Context) string {
	if d == nil {
		return ""
	}
	if d.env != "" {
		return d.env
	}
	var v string
	if err := d.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, dirSetting).Scan(&v); err != nil {
		return ""
	}
	return v
}

// Set chooses the directory after checking it; "" turns daily backups off.
func (d *Dir) Set(ctx context.Context, dir string) error {
	if d.FromEnv() {
		return ErrDirFromEnv
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		_, err := d.db.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, dirSetting)
		return err
	}
	dir = filepath.Clean(dir)
	if err := CheckDir(dir, d.dataDir); err != nil {
		return err
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, dirSetting, dir)
	return err
}

// CheckDir: dir is an existing directory kept can write to, apart from the
// data directory (a backup inside the data it backs up is no backup, and
// one around it would copy itself).
func CheckDir(dir, dataDir string) error {
	if !filepath.IsAbs(dir) {
		return ErrDirNotAbsolute
	}
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return ErrDirMissing
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return ErrDirNotDir
	}
	if dataDir != "" {
		if abs, err := filepath.Abs(dataDir); err == nil && (within(dir, abs) || within(abs, dir)) {
			return ErrDirInData
		}
	}
	f, err := os.CreateTemp(dir, ".kept-write-check-*")
	if err != nil {
		return ErrDirNotWritable
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}

// within: path is base or inside it.
func within(path, base string) bool {
	rel, err := filepath.Rel(base, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
