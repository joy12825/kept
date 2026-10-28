package search

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"kept/server/internal/store"
)

func indexerEnv(t *testing.T) (*sql.DB, *Index, *Indexer) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ix, _, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	for _, q := range []string{
		`INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'a', 'family', 1), (2, 'b', 'family', 1)`,
		`INSERT INTO correspondents (id, workspace_id, name, sort_key, created_at, updated_at) VALUES (20, 1, '国家电网', 'g', 1, 1)`,
		`INSERT INTO document_types (id, workspace_id, name, sort_key, created_at, updated_at) VALUES (10, 1, '账单', 'z', 1, 1)`,
		`INSERT INTO tags (id, workspace_id, name, sort_key, color, parent_id, created_at, updated_at) VALUES (31, 1, '财务', 'c', 'blue', NULL, 1, 1), (32, 1, '发票', 'f', 'blue', 31, 1, 1)`,
		`INSERT INTO custom_fields (id, workspace_id, name, sort_key, data_type, extra, created_at, updated_at) VALUES (40, 1, '户号', 'h', 'text', '{}', 1, 1)`,
		`INSERT INTO documents (id, workspace_id, title, content, created_date, added_at, modified_at, correspondent_id, document_type_id, asn, mime_type, original_filename, original_ext, checksum)
		 VALUES (1, 1, '三月账单', '本期电费 128.50 元', '2024-03-05', 1, 1, 20, 10, 7, 'application/pdf', 'a.pdf', 'pdf', 'a'),
		        (2, 2, '别人的电费', '电费', '2024-03-05', 1, 1, NULL, NULL, NULL, 'application/pdf', 'b.pdf', 'pdf', 'b')`,
		`INSERT INTO document_tags (document_id, tag_id) VALUES (1, 32)`,
		`INSERT INTO custom_field_values (document_id, field_id, value_text) VALUES (1, 40, 'HH-0012345')`,
	} {
		if _, err := st.DB.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	return st.DB, ix, NewIndexer(st.DB, ix, nil, nil)
}

func hits(t *testing.T, ix *Index, ws int64, q string) int {
	t.Helper()
	h, _, err := ix.Search(ws, Parse(q, QueryOptions{}), 10)
	if err != nil {
		t.Fatal(err)
	}
	return len(h)
}

func TestIndexerKeepsTheIndexInStep(t *testing.T) {
	db, ix, x := indexerEnv(t)
	ctx := context.Background()
	if n, err := x.Pass(ctx); err != nil || n != 2 {
		t.Fatalf("first pass = %d, %v", n, err)
	}
	if n, _ := x.Pass(ctx); n != 0 {
		t.Errorf("nothing is behind after a pass, got %d", n)
	}
	for q, want := range map[string]int{"电费": 1, "from:国家电网": 1, "type:账单": 1, "tag:财务": 1, `tag:"财务 / 发票"`: 1, "HH-0012345": 1, "asn:7": 1, "created:2024-03": 1} {
		if got := hits(t, ix, 1, q); got != want {
			t.Errorf("%q = %d hits, want %d", q, got, want)
		}
	}

	bump := func() { db.Exec(`UPDATE documents SET index_rev = index_rev + 1 WHERE id = 1`) }
	db.Exec(`INSERT INTO notes (workspace_id, document_id, body, created_at) VALUES (1, 1, '押金已退', 1)`)
	bump()
	x.Pass(ctx)
	if hits(t, ix, 1, "押金") != 1 {
		t.Error("a note should be searchable after the next pass")
	}

	db.Exec(`UPDATE documents SET deleted_at = 1 WHERE id = 1`)
	bump()
	x.Pass(ctx)
	if hits(t, ix, 1, "电费") != 0 {
		t.Error("a document in the trash must leave the index")
	}
	db.Exec(`UPDATE documents SET deleted_at = NULL WHERE id = 1`)
	bump()
	x.Pass(ctx)
	if hits(t, ix, 1, "电费") != 1 {
		t.Error("a restored document must be searchable again")
	}
}

// A change committed while a document is being indexed must not be lost.
func TestIndexerDoesNotLoseAChangeMadeDuringAPass(t *testing.T) {
	db, _, x := indexerEnv(t)
	ctx := context.Background()
	var rev int64
	db.QueryRow(`SELECT index_rev FROM documents WHERE id = 1`).Scan(&rev)
	// The pass read rev; meanwhile the document changed.
	db.Exec(`UPDATE documents SET index_rev = index_rev + 1 WHERE id = 1`)
	db.Exec(`UPDATE documents SET indexed_rev = ? WHERE id = 1 AND indexed_rev < ?`, rev, rev)
	if n, _ := x.Behind(ctx); n != 2 {
		t.Errorf("behind = %d, want 2 (document 1 still behind)", n)
	}
}

func TestIndexInUseIsNotDeleted(t *testing.T) {
	dir := t.TempDir()
	ix, _, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	ix.Put([]Doc{{ID: 1, WorkspaceID: 1, Title: "电费"}})
	if _, _, err := Open(dir); err != ErrInUse {
		t.Fatalf("second open = %v, want ErrInUse", err)
	}
	if n, _ := ix.Count(); n != 1 {
		t.Errorf("the open index must be left alone, count %d", n)
	}
}
