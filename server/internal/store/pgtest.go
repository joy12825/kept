package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strconv"
	"time"

	"kept/server/internal/store/sqlc"
)

// TestDatabaseEnv points the tests at a Postgres server (any database on it,
// usually postgres): store.Open of a SQLite path in a test then opens a
// database named after the path, copied from a migrated template the first
// time. Tests use fresh temporary directories, so each gets its own
// database, and opening the same path again finds the same data, as with
// SQLite. Test databases are dropped after a few hours.
const TestDatabaseEnv = "KEPT_TEST_DATABASE_URL"

// TestingPostgres reports whether the tests run against Postgres; tests of
// SQLite-only behaviour skip then.
func TestingPostgres() bool { return testDatabaseURL() != "" }

func testDatabaseURL() string { return os.Getenv(TestDatabaseEnv) }

// withDatabase returns the URL with another database name.
func withDatabase(base, name string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	u.Path = "/" + name
	return u.String(), nil
}

// templateName names the template after the migrations it has, so a change
// to them makes a new one.
func templateName() (string, error) {
	h := sha256.New()
	err := fs.WalkDir(migrationsFS, postgresDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := migrationsFS.ReadFile(p)
		h.Write([]byte(p))
		h.Write(b)
		return err
	})
	return "kept_tpl_" + hex.EncodeToString(h.Sum(nil))[:12], err
}

// testLock serializes template and database creation across test processes.
const testLock = 7319

// testMaxAge is how long a test database is kept.
const testMaxAge = 3 * time.Hour

func openTestPostgres(ctx context.Context, base, path string) (*Store, error) {
	admin, err := sql.Open("pgx", base)
	if err != nil {
		return nil, err
	}
	defer admin.Close()
	conn, err := admin.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, testLock); err != nil {
		return nil, err
	}
	defer conn.ExecContext(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, testLock) //nolint:errcheck
	tpl, err := templateName()
	if err != nil {
		return nil, err
	}
	var n int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM pg_database WHERE datname = $1`, tpl).Scan(&n); err != nil {
		return nil, err
	}
	if n == 0 {
		if _, err := conn.ExecContext(ctx, `CREATE DATABASE `+tpl); err != nil {
			return nil, err
		}
		u, _ := withDatabase(base, tpl)
		db, err := openPostgres(u)
		if err != nil {
			return nil, err
		}
		_, err = Migrate(ctx, db)
		db.Close()
		if err != nil {
			_, _ = conn.ExecContext(ctx, `DROP DATABASE `+tpl)
			return nil, fmt.Errorf("migrate the test template: %w", err)
		}
	}
	dropOld(ctx, conn)
	h := sha256.Sum256([]byte(path))
	name := "kept_t_" + hex.EncodeToString(h[:8])
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM pg_database WHERE datname = $1`, name).Scan(&n); err != nil {
		return nil, err
	}
	if n == 0 {
		if _, err := conn.ExecContext(ctx, `CREATE DATABASE `+name+` TEMPLATE `+tpl); err != nil {
			return nil, err
		}
		if _, err := conn.ExecContext(ctx, fmt.Sprintf(`COMMENT ON DATABASE %s IS '%d'`, name, time.Now().Unix())); err != nil {
			return nil, err
		}
	}
	u, _ := withDatabase(base, name)
	db, err := openPostgres(u)
	if err != nil {
		return nil, err
	}
	return &Store{DB: db, Queries: sqlc.New(db), Postgres: true}, nil
}

// dropOld drops the test databases older than testMaxAge.
func dropOld(ctx context.Context, conn *sql.Conn) {
	rows, err := conn.QueryContext(ctx, `SELECT datname, coalesce(shobj_description(oid, 'pg_database'), '') FROM pg_database WHERE datname LIKE 'kept\_t\_%'`)
	if err != nil {
		return
	}
	var old []string
	cutoff := time.Now().Add(-testMaxAge).Unix()
	for rows.Next() {
		var name, created string
		if rows.Scan(&name, &created) == nil {
			if t, err := strconv.ParseInt(created, 10, 64); err != nil || t < cutoff {
				old = append(old, name)
			}
		}
	}
	rows.Close()
	for _, name := range old {
		_, _ = conn.ExecContext(ctx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
	}
}
