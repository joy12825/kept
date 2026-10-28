package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql migrations_pg/*.sql
var migrationsFS embed.FS

// Postgres starts from a baseline at version 33 (migrations_pg); later
// versions exist in both directories.
const (
	sqliteDir   = "migrations"
	postgresDir = "migrations_pg"
)

type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations() ([]migration, error) { return loadMigrationsFrom(sqliteDir) }

func loadMigrationsFrom(dir string) ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, dir)
	if err != nil {
		return nil, err
	}
	var ms []migration
	seen := map[int]string{}
	for _, e := range entries {
		name := e.Name()
		prefix, _, ok := strings.Cut(name, "_")
		v, err := strconv.Atoi(prefix)
		if !ok || err != nil || v <= 0 {
			return nil, fmt.Errorf("migration %s: name must start with a positive number and an underscore", name)
		}
		if other, dup := seen[v]; dup {
			return nil, fmt.Errorf("migrations %s and %s share version %d", other, name, v)
		}
		seen[v] = name
		body, err := migrationsFS.ReadFile(dir + "/" + name)
		if err != nil {
			return nil, err
		}
		ms = append(ms, migration{version: v, name: name, sql: string(body)})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })
	return ms, nil
}

// foreignKeysOff marks a migration that must run with foreign keys disabled.
// Rebuilding a table that others reference (DROP TABLE + RENAME) would
// otherwise cascade-delete the referencing rows: with foreign keys on,
// DROP TABLE first deletes every row of the table. SQLite ignores the pragma
// inside a transaction, so it is switched on the connection around it.
const foreignKeysOff = "-- kept:foreign_keys=off"

// Migrate applies every embedded migration that has not run yet, each in its
// own transaction, and returns the names of the ones it applied.
func Migrate(ctx context.Context, db *sql.DB) ([]string, error) {
	return migrateTo(ctx, db, math.MaxInt)
}

// migrateTo is Migrate stopping after version max; tests use it to build a
// database as an older release left it.
func migrateTo(ctx context.Context, db *sql.DB, max int) ([]string, error) {
	integer := "INTEGER" // 64 bits on SQLite; Postgres needs BIGINT for milliseconds
	if IsPostgres(db) {
		integer = "BIGINT"
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    `+integer+` PRIMARY KEY,
		name       TEXT    NOT NULL,
		applied_at `+integer+` NOT NULL
	)`); err != nil {
		return nil, fmt.Errorf("create schema_migrations: %w", err)
	}
	dir := sqliteDir
	if IsPostgres(db) {
		dir = postgresDir
	}
	ms, err := loadMigrationsFrom(dir)
	if err != nil {
		return nil, err
	}
	var applied []string
	for _, m := range ms {
		if m.version > max {
			break
		}
		ok, err := applyOne(ctx, db, m)
		if err != nil {
			return applied, fmt.Errorf("migration %s: %w", m.name, err)
		}
		if ok {
			applied = append(applied, m.name)
		}
	}
	return applied, nil
}

func applyOne(ctx context.Context, db *sql.DB, m migration) (applied bool, err error) {
	// One connection throughout: the foreign_keys pragma is per connection.
	conn, err := db.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()

	fkOff := strings.HasPrefix(m.sql, foreignKeysOff)
	if fkOff {
		if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
			return false, err
		}
		// Runs after the transaction below has ended, whatever its outcome,
		// so the connection never returns to the pool with checks disabled.
		defer func() {
			if _, rerr := conn.ExecContext(context.WithoutCancel(ctx), `PRAGMA foreign_keys=ON`); rerr != nil {
				applied, err = false, errors.Join(err, fmt.Errorf("re-enable foreign keys: %w", rerr))
			}
		}()
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version = ?`, m.version).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return false, err
	}
	if fkOff {
		if err := checkForeignKeys(ctx, tx); err != nil {
			return false, err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		m.version, m.name, time.Now().UnixMilli()); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// checkForeignKeys fails if any row references a missing parent.
func checkForeignKeys(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT "table", "parent" FROM pragma_foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		var table, parent string
		if err := rows.Scan(&table, &parent); err != nil {
			return err
		}
		return fmt.Errorf("foreign key violation: a row in %s references a missing row in %s", table, parent)
	}
	return rows.Err()
}
