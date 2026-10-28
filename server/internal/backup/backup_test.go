package backup

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"kept/server/internal/files"
	"kept/server/internal/store"
	"kept/server/internal/store/sqlc"
)

type env struct {
	t    *testing.T
	ctx  context.Context
	data string
	db   *sql.DB
	fs   *files.Store
	ws   int64
	now  time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	data := t.TempDir()
	st, err := store.Open(context.Background(), filepath.Join(data, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	var ws int64
	if err := st.DB.QueryRow(`INSERT INTO workspaces (name, kind, created_at) VALUES ('家', 'family', 1) RETURNING id`).Scan(&ws); err != nil {
		t.Fatal(err)
	}
	return &env{t: t, ctx: context.Background(), data: data, db: st.DB, fs: files.NewStore(data), ws: ws, now: time.Date(2024, 3, 5, 10, 0, 0, 0, time.Local)}
}

// doc stores a document with an original (and a thumbnail) on disk.
func (e *env) doc(content string) int64 {
	e.t.Helper()
	sum := sha256.Sum256([]byte(content))
	id, err := sqlc.New(e.db).InsertDocument(e.ctx, sqlc.InsertDocumentParams{
		WorkspaceID: e.ws, Title: content, TitleSort: content, Content: content, CreatedDate: "2024-03-05", AddedAt: 1, ModifiedAt: 1,
		MimeType: "text/plain", OriginalFilename: content + ".txt", OriginalExt: "txt", PageCount: 1, Checksum: hex.EncodeToString(sum[:]),
		HasThumbnail: 1, FieldSources: "{}", Source: "web", Storage: files.Local,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	for p, body := range map[string]string{e.fs.Path(files.Original(e.ws, id, "txt")): content, e.fs.ThumbnailPath(e.ws, id): "jpeg of " + content} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			e.t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			e.t.Fatal(err)
		}
	}
	return id
}

func (e *env) purge(id int64) {
	e.t.Helper()
	if _, err := e.db.Exec(`DELETE FROM documents WHERE id = ?`, id); err != nil {
		e.t.Fatal(err)
	}
	if err := e.fs.RemoveDocument(context.Background(), files.Local, e.ws, id, "txt"); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) run(dir string, keep int) Result {
	e.t.Helper()
	e.now = e.now.Add(time.Hour)
	now := e.now
	res, err := Run(e.ctx, e.db, dir, Options{DataDir: e.data, Keep: keep, Now: func() time.Time { return now }})
	if err != nil {
		e.t.Fatal(err)
	}
	return res
}

func TestBackupIsIncrementalAndRotates(t *testing.T) {
	e := newEnv(t)
	a := e.doc("电费")
	b := e.doc("水费")
	if err := os.MkdirAll(filepath.Join(e.data, "branding"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(e.data, "branding", "logo-1.png"), []byte("png"), 0o644)
	dir := t.TempDir()

	first := e.run(dir, 2)
	if first.Documents != 2 || first.Files != 4 || first.Copied != 4 || len(first.Missing) != 0 {
		t.Fatalf("first backup = %+v", first)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, e.fs.Path(files.Original(e.ws, a, "txt"))[len(e.data):])); string(got) != "电费" {
		t.Errorf("backed-up original = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "branding", "logo-1.png")); err != nil {
		t.Errorf("logo not backed up: %v", err)
	}

	// Nothing changed: nothing copied.
	if again := e.run(dir, 2); again.Copied != 0 || again.Files != 4 {
		t.Errorf("unchanged backup = %+v", again)
	}
	// A new document is copied; a purged one stays while a snapshot needs it.
	c := e.doc("燃气费")
	e.purge(b)
	third := e.run(dir, 2)
	if third.Copied != 2 || third.Files != 4 || third.Pruned != 0 {
		t.Errorf("third backup = %+v", third)
	}
	if snaps, _ := Snapshots(dir); len(snaps) != 2 {
		t.Errorf("snapshots = %v, want 2 kept", snaps)
	}
	// Once no kept snapshot mentions 水费, its files go.
	fourth := e.run(dir, 2)
	if fourth.Pruned != 2 {
		t.Errorf("fourth backup pruned %d files, want 2 (水费's original and thumbnail)", fourth.Pruned)
	}
	m, err := ReadManifest(dir)
	if err != nil || m.Documents != 2 || m.Files != 4 || len(m.Snapshots) != 2 || m.Snapshot != fourth.Snapshot {
		t.Errorf("manifest = %+v, %v", m, err)
	}
	_ = c
}

func TestBackupRefusesTheDataDirectoryAndConcurrentRuns(t *testing.T) {
	e := newEnv(t)
	if _, err := Run(e.ctx, e.db, filepath.Join(e.data, "backups-here"), Options{DataDir: e.data}); err == nil {
		t.Error("a backup inside the data directory should be refused")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, lockName), []byte("1"), 0o644)
	if _, err := Run(e.ctx, e.db, dir, Options{DataDir: e.data}); err == nil || !strings.Contains(err.Error(), "正在进行") {
		t.Errorf("locked directory: %v", err)
	}
}

func TestMissingFilesAreReported(t *testing.T) {
	e := newEnv(t)
	id := e.doc("电费")
	os.Remove(e.fs.ThumbnailPath(e.ws, id))
	res := e.run(t.TempDir(), 0)
	if len(res.Missing) != 1 || !strings.HasSuffix(res.Missing[0], ".jpg") {
		t.Errorf("missing = %v", res.Missing)
	}
}

func TestRestore(t *testing.T) {
	e := newEnv(t)
	a := e.doc("电费")
	e.doc("水费")
	dir := t.TempDir()
	e.run(dir, 0)

	target := t.TempDir()
	res, err := Restore(e.ctx, dir, RestoreOptions{DataDir: target})
	if err != nil || res.Documents != 2 || res.Files != 4 {
		t.Fatalf("restore = %+v, %v", res, err)
	}
	// The restored directory opens as a normal data directory.
	st, err := store.OpenSQLite(e.ctx, filepath.Join(target, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	var n int
	st.DB.QueryRow(`SELECT count(*) FROM documents`).Scan(&n)
	st.Close()
	if n != 2 {
		t.Errorf("restored documents = %d", n)
	}
	if got, _ := os.ReadFile(files.NewStore(target).Path(files.Original(e.ws, a, "txt"))); string(got) != "电费" {
		t.Errorf("restored original = %q", got)
	}
	// Never over an existing database.
	if _, err := Restore(e.ctx, dir, RestoreOptions{DataDir: target}); err == nil {
		t.Error("restoring over a database should be refused")
	}

	// A damaged original stops the restore, unless forced.
	backedUp := filepath.Join(dir, files.NewStore("").Path(files.Original(e.ws, a, "txt")))
	os.WriteFile(backedUp, []byte("篡改"), 0o644)
	var dmg *ErrDamaged
	if _, err := Restore(e.ctx, dir, RestoreOptions{DataDir: t.TempDir()}); !errors.As(err, &dmg) || len(dmg.Problems) != 1 || !strings.Contains(dmg.Problems[0], "校验值不符") {
		t.Errorf("damaged backup: %v", err)
	}
	forced, err := Restore(e.ctx, dir, RestoreOptions{DataDir: t.TempDir(), Force: true})
	if err != nil || forced.Files != 3 || len(forced.Problems) != 1 {
		t.Errorf("forced restore = %+v, %v", forced, err)
	}
	if _, err := Restore(e.ctx, dir, RestoreOptions{DataDir: t.TempDir(), Snapshot: "app-nope.db"}); err == nil {
		t.Error("an unknown snapshot should be refused")
	}
}

func TestBackupKeepsEarlierVersions(t *testing.T) {
	e := newEnv(t)
	a := e.doc("合同第二稿")
	old := "合同第一稿"
	sum := sha256.Sum256([]byte(old))
	if _, err := e.db.Exec(`INSERT INTO document_versions (id, workspace_id, document_id, number, original_filename, original_ext, mime_type, checksum, size, current, created_at)
		VALUES (8, ?, ?, 1, '合同.txt', 'txt', 'text/plain', ?, 1, 0, 1)`, e.ws, a, hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	p := e.fs.Path(files.Version(e.ws, a, 8, "txt"))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	res := e.run(dir, 2)
	if res.Files != 3 || len(res.Missing) != 0 {
		t.Fatalf("backup = %+v", res)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, p[len(e.data):])); string(got) != old {
		t.Fatalf("backed-up version = %q", got)
	}
}

// A snapshot made before page edits (migration 0014) is still read, so
// backups keep working after an upgrade until it rotates out.
func TestBackupReadsSnapshotsOfOlderDatabases(t *testing.T) {
	e := newEnv(t)
	e.doc("电费")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "db"), 0o755); err != nil {
		t.Fatal(err)
	}
	old, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(filepath.Join(dir, "db", "app-20200101-000000.db")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE documents (id INTEGER PRIMARY KEY, workspace_id INTEGER, original_ext TEXT, checksum TEXT, has_archive INTEGER, has_thumbnail INTEGER);
		INSERT INTO documents VALUES (1, 1, 'pdf', 'x', 0, 0)`); err != nil {
		t.Fatal(err)
	}
	old.Close()
	res := e.run(dir, 7)
	if res.Documents != 1 || res.Pruned != 0 {
		t.Errorf("backup = %+v", res)
	}
}

// TestBackupLeavesOtherStorages: files kept in another storage (v1.7.0)
// are counted, not copied; a restore leaves them where they are.
func TestBackupLeavesOtherStorages(t *testing.T) {
	e := newEnv(t)
	a := e.doc("电费")
	b := e.doc("水费")
	if _, err := e.db.Exec(`UPDATE documents SET storage = 'nas' WHERE id = ?`, b); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`INSERT INTO storages (name, kind, secret, created_at, updated_at) VALUES ('nas', 's3', 'v1:sealed', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	res := e.run(dir, 2)
	// a: original + thumbnail; b: its thumbnail stays in the data directory
	if res.Documents != 2 || res.Files != 3 || res.External != 1 || len(res.Missing) != 0 {
		t.Fatalf("backup = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "media", "originals", strconv.FormatInt(e.ws, 10), strconv.FormatInt(b, 10)+".txt")); !os.IsNotExist(err) {
		t.Errorf("the nas original was backed up: %v", err)
	}
	target := t.TempDir()
	rr, err := Restore(e.ctx, dir, RestoreOptions{DataDir: target})
	if err != nil || rr.Files != 3 || rr.External != 1 || !rr.SealedKeys {
		t.Fatalf("restore = %+v, %v", rr, err)
	}
	if got, _ := os.ReadFile(files.NewStore(target).Path(files.Original(e.ws, a, "txt"))); string(got) != "电费" {
		t.Errorf("restored original = %q", got)
	}
}
