package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenAppliesMigrationsOnce(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "app.db")

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	var n int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no migrations recorded")
	}
	if _, err := s.DB.ExecContext(ctx, `SELECT id FROM jobs LIMIT 1`); err != nil {
		t.Fatalf("jobs table missing: %v", err)
	}
	s.Close()

	s, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	applied, err := Migrate(ctx, s.DB)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("re-applied migrations: %v", applied)
	}
}

func TestOpenEnablesWALAndForeignKeys(t *testing.T) {
	sqliteOnly(t)
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	var mode string
	if err := s.DB.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
	var fk int
	if err := s.DB.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1", fk)
	}
}

func TestAccountTablesAndJobsForeignKey(t *testing.T) {
	sqliteOnly(t)
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	for _, table := range []string{"users", "workspaces", "workspace_members", "sessions", "api_tokens", "settings"} {
		if _, err := s.DB.ExecContext(ctx, "SELECT 1 FROM "+table+" LIMIT 1"); err != nil {
			t.Errorf("table %s missing: %v", table, err)
		}
	}
	var refs int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM pragma_foreign_key_list('jobs') WHERE "table" = 'workspaces'`).Scan(&refs); err != nil {
		t.Fatal(err)
	}
	if refs != 1 {
		t.Errorf("jobs.workspace_id should reference workspaces, found %d references", refs)
	}
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO jobs (workspace_id, kind, run_after, created_at) VALUES (999, 'x', 0, 0)`); err == nil {
		t.Error("inserting a job for a missing workspace should violate the foreign key")
	}
}

func TestOpenPathWithSpaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "my data", "app.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.Close()
}

// openAt opens a fresh database migrated only up to version max.
func openAt(t *testing.T, max int) *sql.DB {
	t.Helper()
	return openAtPath(t, filepath.Join(t.TempDir(), "app.db"), max)
}

func openAtPath(t *testing.T, path string, max int) *sql.DB {
	t.Helper()
	ctx := context.Background()
	db, err := openDB(ctx, path)
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := migrateTo(ctx, db, max); err != nil {
		t.Fatalf("migrateTo(%d): %v", max, err)
	}
	return db
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

// Migration 6 rebuilds workspaces, the parent of workspace_members and jobs
// (both ON DELETE CASCADE). It must not take their rows with it.
func TestWorkspacesRebuildKeepsMembersAndJobs(t *testing.T) {
	ctx := context.Background()
	db := openAt(t, 5)
	mustExec(t, db, `INSERT INTO users (id, username, password_hash, password_changed_at, created_at, updated_at)
		VALUES (7, 'alice', 'x', 1, 1, 1)`)
	mustExec(t, db, `INSERT INTO workspaces (id, name, kind, created_at) VALUES (3, 'alice space', 'personal', 100)`)
	mustExec(t, db, `INSERT INTO workspace_members (workspace_id, user_id, role, created_at) VALUES (3, 7, 'owner', 100)`)
	mustExec(t, db, `INSERT INTO jobs (workspace_id, kind, run_after, created_at) VALUES (3, 'x', 0, 0)`)
	mustExec(t, db, `INSERT INTO settings (key, value) VALUES ('allow_registration', 'true')`)

	applied, err := migrateTo(ctx, db, 6)
	if err != nil {
		t.Fatalf("migrate to 6: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("applied %v, want exactly migration 6", applied)
	}

	var (
		name, kind, color string
		createdBy         sql.NullInt64
		createdAt         int64
		deletedAt         sql.NullInt64
	)
	if err := db.QueryRowContext(ctx, `SELECT name, kind, color, created_by, created_at, deleted_at FROM workspaces WHERE id = 3`).
		Scan(&name, &kind, &color, &createdBy, &createdAt, &deletedAt); err != nil {
		t.Fatalf("workspace row lost: %v", err)
	}
	if name != "alice space" || kind != "personal" || color != "blue" || createdBy.Int64 != 7 || createdAt != 100 || deletedAt.Valid {
		t.Errorf("workspace = %q %q %q created_by=%v created_at=%d deleted_at=%v", name, kind, color, createdBy, createdAt, deletedAt)
	}
	for table, want := range map[string]int{"workspace_members": 1, "jobs": 1} {
		var n int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE workspace_id = 3").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != want {
			t.Errorf("%s rows for the workspace = %d, want %d (cascade fired during the rebuild?)", table, n, want)
		}
	}

	mustExec(t, db, `INSERT INTO workspaces (name, kind, created_by, created_at) VALUES ('home', 'family', 7, 200)`)
	if _, err := db.ExecContext(ctx, `INSERT INTO workspaces (name, kind, created_at) VALUES ('bad', 'club', 1)`); err == nil {
		t.Error("an unknown workspace kind should violate the CHECK constraint")
	}

	// Foreign keys must be back on for every pooled connection afterwards.
	var fk int
	if err := db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d after the migration, want 1", fk)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO workspace_members (workspace_id, user_id, role, created_at) VALUES (999, 7, 'owner', 1)`); err == nil {
		t.Error("a member of a missing workspace should violate the foreign key")
	}

	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM settings WHERE key = 'allow_registration'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("the allow_registration setting should be removed")
	}
	mustExec(t, db, `INSERT INTO audit_events (workspace_id, actor_user_id, actor_via, action, summary, created_at)
		VALUES (NULL, 7, 'session', 'user.create', 's', 1)`)
}

// A foreign_keys=off migration that leaves a dangling reference must fail
// and leave no trace.
func TestForeignKeysOffMigrationIsChecked(t *testing.T) {
	ctx := context.Background()
	db := openAt(t, 5)
	mustExec(t, db, `INSERT INTO users (id, username, password_hash, password_changed_at, created_at, updated_at)
		VALUES (7, 'alice', 'x', 1, 1, 1)`)
	bad := migration{version: 900, name: "0900_bad.sql", sql: "-- kept:foreign_keys=off\n" +
		`INSERT INTO workspace_members (workspace_id, user_id, role, created_at) VALUES (999, 7, 'owner', 1);`}
	ok, err := applyOne(ctx, db, bad)
	if err == nil || ok {
		t.Fatalf("applyOne = %v, %v; want a foreign key error", ok, err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version = 900`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("the failed migration was recorded")
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM workspace_members`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("the failed migration's rows were kept")
	}
	var fk int
	if err := db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d after a failed migration, want 1", fk)
	}
}

// Migration 7 gives every existing workspace the two built-in fields.
func TestTaxonomyMigrationAddsBuiltInFields(t *testing.T) {
	ctx := context.Background()
	db := openAt(t, 6)
	mustExec(t, db, `INSERT INTO workspaces (id, name, kind, created_at) VALUES (3, 'a', 'personal', 1), (4, 'b', 'family', 1)`)
	if _, err := migrateTo(ctx, db, 7); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(ctx, `SELECT workspace_id, key, name, sort_key, data_type, extra FROM custom_fields ORDER BY workspace_id, key`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var ws int
		var key, name, sortKey, typ, extra string
		if err := rows.Scan(&ws, &key, &name, &sortKey, &typ, &extra); err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%d %s %s %s %s %s", ws, key, name, sortKey, typ, extra))
	}
	want := []string{
		`3 amount 金额 jine monetary {"currency":"CNY"}`, `3 due_date 到期日 daoqiri date {}`,
		`4 amount 金额 jine monetary {"currency":"CNY"}`, `4 due_date 到期日 daoqiri date {}`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("built-in fields:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// Names are unique per workspace regardless of letter case.
	mustExec(t, db, `INSERT INTO correspondents (workspace_id, name, sort_key, created_at, updated_at) VALUES (3, 'Apple', 'apple', 1, 1)`)
	if _, err := db.ExecContext(ctx, `INSERT INTO correspondents (workspace_id, name, sort_key, created_at, updated_at) VALUES (3, 'APPLE', 'apple', 1, 1)`); err == nil {
		t.Error("a correspondent name differing only in case should be rejected")
	}
	mustExec(t, db, `INSERT INTO correspondents (workspace_id, name, sort_key, created_at, updated_at) VALUES (4, 'Apple', 'apple', 1, 1)`)
}

func TestDocumentsMigration(t *testing.T) {
	ctx := context.Background()
	db := openAt(t, 8)
	mustExec(t, db, `INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'a', 'family', 1)`)
	ins := `INSERT INTO documents (workspace_id, title, created_date, added_at, modified_at, mime_type, original_filename, original_ext, checksum)
		VALUES (1, 't', '2024-03-05', 1, 1, 'application/pdf', 'a.pdf', 'pdf', ?)`
	mustExec(t, db, ins, "abc")
	if _, err := db.ExecContext(ctx, ins, "abc"); err == nil {
		t.Error("the same checksum twice in one workspace should be rejected")
	}
	var inbox int
	if err := db.QueryRowContext(ctx, `SELECT inbox FROM documents`).Scan(&inbox); err != nil || inbox != 1 {
		t.Errorf("new documents should be in the inbox: %d, %v", inbox, err)
	}
	mustExec(t, db, `INSERT INTO jobs (workspace_id, kind, run_after, created_at, document_id) VALUES (1, 'x', 0, 0, 1)`)
}

func TestNotesViewsMigrationAndTitleSortBackfill(t *testing.T) {
	sqliteOnly(t)
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.db")
	db := openAtPath(t, path, 8)
	mustExec(t, db, `INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'a', 'family', 1)`)
	mustExec(t, db, `INSERT INTO documents (workspace_id, title, created_date, added_at, modified_at, mime_type, original_filename, original_ext, checksum)
		VALUES (1, '2024年3月电费账单', '2024-03-05', 1, 1, 'application/pdf', 'a.pdf', 'pdf', 'x')`)
	db.Close()

	st, err := Open(ctx, path) // migrates to the latest version and backfills
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var key string
	if err := st.DB.QueryRowContext(ctx, `SELECT title_sort FROM documents`).Scan(&key); err != nil || key != "0000002024nian0000000003yuedianfeizhangdan" {
		t.Errorf("title_sort = %q, %v", key, err)
	}
	mustExec(t, st.DB, `INSERT INTO saved_views (workspace_id, name, sort_key, filter, created_at, updated_at) VALUES (1, 'Tax', 'tax', '{}', 1, 1)`)
	if _, err := st.DB.ExecContext(ctx, `INSERT INTO saved_views (workspace_id, name, sort_key, filter, created_at, updated_at) VALUES (1, 'TAX', 'tax', '{}', 1, 1)`); err == nil {
		t.Error("saved view names should be unique per workspace regardless of case")
	}
	mustExec(t, st.DB, `INSERT INTO notes (workspace_id, document_id, body, created_at) VALUES (1, 1, 'n', 1)`)
	mustExec(t, st.DB, `DELETE FROM documents`)
	var n int
	if err := st.DB.QueryRowContext(ctx, `SELECT count(*) FROM notes`).Scan(&n); err != nil || n != 0 {
		t.Errorf("notes should go with their document: %d, %v", n, err)
	}
}

func TestBackupBeforeMigrating(t *testing.T) {
	sqliteOnly(t)
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.db")

	// A new database is not backed up.
	st, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Backup != "" {
		t.Errorf("new database backed up to %s", st.Backup)
	}
	st.Close()

	// Nothing pending: no backup.
	st, _ = Open(ctx, path)
	if st.Backup != "" {
		t.Errorf("up to date, backed up to %s", st.Backup)
	}
	st.Close()

	// Pretend an older release: hide the newest migration's record, back up, put it back.
	// Each backup is kept until there are more than keepBackups.
	db, err := openDB(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	var name string
	if err := db.QueryRow(`SELECT version, name FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&version, &name); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		mustExec(t, db, `INSERT INTO workspaces (name, kind, created_at) VALUES ('w', 'family', 1)`)
		mustExec(t, db, `DELETE FROM schema_migrations WHERE version = ?`, version)
		backup, err := backupBeforeMigrating(ctx, db, path, time.Date(2026, 1, 1, 0, 0, i, 0, time.Local))
		if err != nil || backup == "" {
			t.Fatalf("backup = %q, %v", backup, err)
		}
		mustExec(t, db, `INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, 1)`, version, name)
		bdb, err := openDB(ctx, backup)
		if err != nil {
			t.Fatal(err)
		}
		var n int
		if err := bdb.QueryRow(`SELECT count(*) FROM workspaces`).Scan(&n); err != nil || n != i+1 {
			t.Errorf("backup %d has %d workspaces, %v", i, n, err)
		}
		bdb.Close()
		time.Sleep(10 * time.Millisecond) // distinct modification times
	}
	entries, _ := os.ReadDir(BackupDir(path))
	if len(entries) != keepBackups {
		t.Errorf("%d backups kept, want %d", len(entries), keepBackups)
	}
}

// sqliteOnly skips tests of SQLite itself (pragmas, upgrading older SQLite
// databases, VACUUM INTO) when the tests run against Postgres.
func sqliteOnly(t *testing.T) {
	t.Helper()
	if TestingPostgres() {
		t.Skip("SQLite only")
	}
}

// TestMigrationVersionsMatch: Postgres starts from a baseline at 33; every
// later SQLite migration needs its Postgres version, and the other way round.
func TestMigrationVersionsMatch(t *testing.T) {
	lite, err := loadMigrationsFrom(sqliteDir)
	if err != nil {
		t.Fatal(err)
	}
	pg, err := loadMigrationsFrom(postgresDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pg) == 0 || pg[0].version != 33 {
		t.Fatalf("the Postgres baseline should be version 33: %+v", pg)
	}
	want := map[int]bool{}
	for _, m := range lite {
		if m.version > 33 {
			want[m.version] = true
		}
	}
	for _, m := range pg[1:] {
		if !want[m.version] {
			t.Errorf("%s has no SQLite counterpart", m.name)
		}
		delete(want, m.version)
	}
	for v := range want {
		t.Errorf("SQLite migration %d has no Postgres counterpart in %s", v, postgresDir)
	}
}
