package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kept/server/internal/auth"
	"kept/server/internal/edition"
	"kept/server/internal/store"
)

func TestCopyToPostgresUsage(t *testing.T) {
	t.Setenv("KEPT_DATA_DIR", t.TempDir())
	if code, out := runCLI(t, "copy-to-postgres"); code != 2 || !strings.Contains(out, "KEPT_DATABASE_URL") {
		t.Errorf("no address: exit %d, output %q", code, out)
	}
	if code, out := runCLI(t, "copy-to-postgres", "postgres://kept@127.0.0.1:1/kept"); code != 1 || !strings.Contains(out, "没有 SQLite 数据库") {
		t.Errorf("no SQLite database: exit %d, output %q", code, out)
	}
}

func TestCopyToPostgres(t *testing.T) {
	base := os.Getenv(store.TestDatabaseEnv)
	if base == "" {
		t.Skip("needs " + store.TestDatabaseEnv)
	}
	ctx := context.Background()
	dir := t.TempDir()
	t.Setenv("KEPT_DATA_DIR", dir)
	team := edition.Team // Postgres is a Business feature (v1.12)
	testEdition = &team
	t.Cleanup(func() { testEdition = nil })
	src, err := store.OpenSQLite(ctx, filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.NewService(src.DB, auth.Options{}).CreateUser(ctx, auth.NewUser{Username: "boss", Password: "password-123", Email: "boss@example.com", IsAdmin: true}); err != nil {
		t.Fatal(err)
	}
	src.Close()

	// An empty database on the test server.
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "kept_copy_" + hex.EncodeToString(b)
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+name+` TEMPLATE template0`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { // after the commands have closed their connections
		_, _ = admin.ExecContext(context.Background(), `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
		admin.Close()
	})
	u, _ := url.Parse(base)
	u.Path = "/" + name
	target := u.String()

	code, out := runCLI(t, "copy-to-postgres", target)
	if code != 0 || !strings.Contains(out, "行数核对一致") || !strings.Contains(out, "users") {
		t.Fatalf("copy: exit %d, output %q", code, out)
	}
	t.Setenv("KEPT_DATABASE_URL", target)
	t.Setenv("KEPT_PASSWORD", "brand-new-pass")
	if code, out := runCLI(t, "user", "set-password", "boss"); code != 0 {
		t.Errorf("the copied account: exit %d, output %q", code, out)
	}
	if code, out := runCLI(t, "copy-to-postgres"); code != 1 || !strings.Contains(out, "已经有数据") {
		t.Errorf("second copy: exit %d, output %q", code, out)
	}
}
