package store

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"path/filepath"
	"testing"
)

func TestCopyToPostgres(t *testing.T) {
	if !TestingPostgres() {
		t.Skip("needs " + TestDatabaseEnv)
	}
	ctx := context.Background()
	src, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	vec := make([]byte, 12)
	for i, f := range []float32{0.5, -1, 2.25} {
		binary.LittleEndian.PutUint32(vec[4*i:], math.Float32bits(f))
	}
	for _, q := range []string{
		`INSERT INTO users (id, username, display_name, password_hash, password_changed_at, created_at, updated_at) VALUES (7, 'wang', '小王', 'x', 1, 1, 1)`,
		`INSERT INTO workspaces (id, name, kind, created_at) VALUES (3, '王家', 'family', 1)`,
		`INSERT INTO workspace_members (workspace_id, user_id, role, created_at) VALUES (3, 7, 'owner', 1)`,
		// A child tag with a smaller id than its parent.
		`INSERT INTO tags (id, workspace_id, name, sort_key, color, created_at, updated_at) VALUES (9, 3, '账单', 'zhangdan', '', 1, 1)`,
		`INSERT INTO tags (id, workspace_id, name, sort_key, color, parent_id, created_at, updated_at) VALUES (5, 3, '电费', 'dianfei', '', 9, 1, 1)`,
		// SQLite keeps a number in a text column as a number.
		`INSERT INTO documents (id, workspace_id, title, content, created_date, added_at, modified_at, mime_type, original_filename, original_ext, checksum)
			VALUES (11, 3, 'd', '电费 128.50', '2024-03-01', 1, 1, 'application/pdf', 'a.pdf', 'pdf', 12345)`,
		`INSERT INTO document_tags (document_id, tag_id) VALUES (11, 5)`,
		`INSERT INTO doc_chunks (id, document_id, workspace_id, seq, text, hash) VALUES (21, 11, 3, 0, '电费', 'h')`,
	} {
		if _, err := src.DB.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := src.DB.ExecContext(ctx, `INSERT INTO chunk_vectors (rowid, vector, workspace_id) VALUES (21, ?, 3)`, vec); err != nil {
		t.Fatal(err)
	}

	dst, err := Open(ctx, filepath.Join(t.TempDir(), "app.db")) // a Postgres test database
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	var seen []string
	tables, err := CopyToPostgres(ctx, src.DB, dst.DB, func(table string, _ int) { seen = append(seen, table) })
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]int{}
	for _, c := range tables {
		rows[c.Name] = c.Rows
	}
	if rows["users"] != 1 || rows["tags"] != 2 || rows["documents"] != 1 || rows["chunk_vectors"] != 1 || len(seen) != len(tables) {
		t.Errorf("copied %v", rows)
	}
	var checksum, parent string
	if err := dst.DB.QueryRow(`SELECT d.checksum, p.name FROM documents d JOIN document_tags dt ON dt.document_id = d.id
		JOIN tags c ON c.id = dt.tag_id JOIN tags p ON p.id = c.parent_id WHERE d.id = 11`).Scan(&checksum, &parent); err != nil || checksum != "12345" || parent != "账单" {
		t.Errorf("document = %q %q %v", checksum, parent, err)
	}
	var v string
	if err := dst.DB.QueryRow(`SELECT vector::text FROM chunk_vectors WHERE id = 21 AND workspace_id = 3`).Scan(&v); err != nil || v != "[0.5,-1,2.25]" {
		t.Errorf("vector = %q %v", v, err)
	}
	// New rows continue after the copied ids.
	var id int64
	if err := dst.DB.QueryRow(`INSERT INTO tags (workspace_id, name, sort_key, color, created_at, updated_at) VALUES (3, '水费', 'shuifei', '', 1, 1) RETURNING id`).Scan(&id); err != nil || id != 10 {
		t.Errorf("next tag id = %d %v", id, err)
	}

	if _, err := CopyToPostgres(ctx, src.DB, dst.DB, nil); !errors.Is(err, ErrNotEmpty) {
		t.Errorf("copy into a database with data: %v", err)
	}

	// Back again (v1.9.10): the Postgres database as a SQLite snapshot,
	// which copies into another Postgres database row for row.
	snap := filepath.Join(t.TempDir(), "snap.db")
	if err := SnapshotPostgres(ctx, dst.DB, snap); err != nil {
		t.Fatal(err)
	}
	if err := SnapshotPostgres(ctx, dst.DB, snap); err == nil {
		t.Error("a snapshot over an existing file should be refused")
	}
	lite, err := OpenSQLite(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	defer lite.Close()
	if err := lite.DB.QueryRow(`SELECT d.checksum, p.name FROM documents d JOIN document_tags dt ON dt.document_id = d.id
		JOIN tags c ON c.id = dt.tag_id JOIN tags p ON p.id = c.parent_id WHERE d.id = 11`).Scan(&checksum, &parent); err != nil || checksum != "12345" || parent != "账单" {
		t.Errorf("snapshot document = %q %q %v", checksum, parent, err)
	}
	var blob []byte
	if err := lite.DB.QueryRow(`SELECT vector FROM chunk_vectors WHERE rowid = 21 AND workspace_id = 3`).Scan(&blob); err != nil || string(blob) != string(vec) {
		t.Errorf("snapshot vector = %v %v", blob, err)
	}
	var n int
	if err := lite.DB.QueryRow(`SELECT count(*) FROM tags`).Scan(&n); err != nil || n != 3 {
		t.Errorf("snapshot tags = %d %v", n, err)
	}
	again, err := Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	back, err := CopyToPostgres(ctx, lite.DB, again.DB, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range back {
		if c.Rows != rows[c.Name] && c.Name != "tags" {
			t.Errorf("round trip %s: %d rows, was %d", c.Name, c.Rows, rows[c.Name])
		}
	}
}

func TestVectorBlob(t *testing.T) {
	b, err := vectorBlob("[1,-0.125]")
	if err != nil || vectorText(b) != "[1,-0.125]" {
		t.Errorf("vectorBlob = %v %v", b, err)
	}
	if b, err := vectorBlob("[]"); err != nil || len(b) != 0 {
		t.Errorf("empty vector = %v %v", b, err)
	}
	if _, err := vectorBlob("1,2"); err == nil {
		t.Error("a vector without brackets should be refused")
	}
}

func TestVectorText(t *testing.T) {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b, math.Float32bits(1))
	binary.LittleEndian.PutUint32(b[4:], math.Float32bits(-0.125))
	if got := vectorText(b); got != "[1,-0.125]" {
		t.Errorf("vectorText = %s", got)
	}
}
