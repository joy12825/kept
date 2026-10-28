// Package store owns the database: opening it, running migrations and
// exposing the sqlc-generated queries. It is SQLite in the data directory,
// or Postgres (v1.0) when the address is a postgres:// URL; queries are
// written for SQLite and rewritten for Postgres (pgsql.go).
package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/driver" // "sqlite3" driver; SQLite runs as WASM, no cgo
	"github.com/ncruces/go-sqlite3/ext/vec1"

	"kept/server/internal/store/sqlc"
)

// Every connection gets vec1, SQLite's vector extension (v0.5): the
// chunk_vectors table needs it, and so do backups and integrity checks.
func init() { sqlite3.AutoExtension(vec1.Register) }

type Store struct {
	DB *sql.DB
	*sqlc.Queries
	// Backup is the copy made before this Open migrated the database; "" if none.
	Backup string
	// Postgres: the database is Postgres, not SQLite.
	Postgres bool
}

// IsURL reports whether a database address is a Postgres URL.
func IsURL(addr string) bool {
	return strings.HasPrefix(addr, "postgres://") || strings.HasPrefix(addr, "postgresql://")
}

// Open opens the database at addr — a SQLite file path, or a postgres://
// URL — and applies pending migrations. An existing SQLite database is
// first backed up to data/backups when migrations are pending (design doc
// 11.13); Backup says where, if anywhere. Under go test with
// KEPT_TEST_DATABASE_URL set, a SQLite path opens a Postgres test database
// named after the path instead (pgtest.go).
func Open(ctx context.Context, addr string) (*Store, error) {
	if !IsURL(addr) && testing.Testing() && os.Getenv(TestDatabaseEnv) != "" {
		return openTestPostgres(ctx, os.Getenv(TestDatabaseEnv), addr)
	}
	if IsURL(addr) {
		db, err := openPostgres(addr)
		if err != nil {
			return nil, err
		}
		if err := db.PingContext(ctx); err != nil {
			db.Close()
			return nil, fmt.Errorf("open database: %w", err)
		}
		return finishOpen(ctx, db, "", true)
	}
	return OpenSQLite(ctx, addr)
}

// OpenSQLite opens (and migrates) the SQLite database at path, in tests too.
func OpenSQLite(ctx context.Context, path string) (*Store, error) {
	db, err := openDB(ctx, path)
	if err != nil {
		return nil, err
	}
	backup, err := backupBeforeMigrating(ctx, db, path, time.Now())
	if err != nil {
		db.Close()
		return nil, err
	}
	return finishOpen(ctx, db, backup, false)
}

func finishOpen(ctx context.Context, db *sql.DB, backup string, postgres bool) (*Store, error) {
	if _, err := Migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	q := sqlc.New(db)
	if err := backfillTitleSort(ctx, q); err != nil {
		db.Close()
		return nil, err
	}
	if err := Optimize(ctx, db, postgres); err != nil {
		db.Close()
		return nil, fmt.Errorf("optimize database: %w", err)
	}
	return &Store{DB: db, Queries: q, Backup: backup, Postgres: postgres}, nil
}

// openDB opens the database with the connection settings every connection
// needs, without migrating.
func openDB(ctx context.Context, path string) (*sql.DB, error) {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(10000)")
	q.Add("_pragma", "journal_mode(wal)")
	q.Add("_pragma", "foreign_keys(on)")
	q.Add("_pragma", "synchronous(normal)")
	// Write transactions take the lock up front, avoiding SQLITE_BUSY on upgrade.
	q.Set("_txlock", "immediate")
	dsn := (&url.URL{Scheme: "file", OmitHost: true, Path: path, RawQuery: q.Encode()}).String()

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	return db, nil
}

func (s *Store) Close() error { return s.DB.Close() }

// Ping is used by the health check.
func (s *Store) Ping(ctx context.Context) error { return s.DB.PingContext(ctx) }
