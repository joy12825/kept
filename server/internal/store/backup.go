package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// keepBackups is how many automatic backups are kept (design doc 11.13).
const keepBackups = 3

// BackupDir is where automatic backups go: data/backups next to app.db.
func BackupDir(dbPath string) string { return filepath.Join(filepath.Dir(dbPath), "backups") }

// pending reports the newest applied migration version and how many embedded
// migrations have not run yet. A database without schema_migrations is new.
func pending(ctx context.Context, db *sql.DB) (current, todo int, err error) {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`).Scan(&n); err != nil {
		return 0, 0, err
	}
	ms, err := loadMigrations()
	if err != nil {
		return 0, 0, err
	}
	if n == 0 {
		return 0, len(ms), nil
	}
	done := map[int]bool{}
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return 0, 0, err
		}
		done[v] = true
		current = max(current, v)
	}
	for _, m := range ms {
		if !done[m.version] {
			todo++
		}
	}
	return current, todo, rows.Err()
}

// backupBeforeMigrating copies the database with VACUUM INTO when it has
// data and migrations are about to run, and keeps the newest keepBackups
// copies. It returns the backup's path ("" when none was needed).
func backupBeforeMigrating(ctx context.Context, db *sql.DB, dbPath string, now time.Time) (string, error) {
	current, todo, err := pending(ctx, db)
	if err != nil || current == 0 || todo == 0 {
		return "", err
	}
	dir := BackupDir(dbPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}
	dst := filepath.Join(dir, fmt.Sprintf("app-schema%d-%s.db", current, now.Format("20060102-150405")))
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, dst); err != nil {
		return "", fmt.Errorf("back up the database before migrating: %w", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dst, err
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "app-schema") && strings.HasSuffix(e.Name(), ".db") {
			names = append(names, e.Name())
		}
	}
	// Names sort by time within a schema version; order by modification time to be safe.
	sort.Slice(names, func(i, j int) bool {
		a, _ := os.Stat(filepath.Join(dir, names[i]))
		b, _ := os.Stat(filepath.Join(dir, names[j]))
		return a.ModTime().After(b.ModTime())
	})
	for _, n := range names[min(keepBackups, len(names)):] {
		_ = os.Remove(filepath.Join(dir, n))
	}
	return dst, nil
}
