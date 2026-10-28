package integrity

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"kept/server/internal/files"
	"kept/server/internal/search"
	"kept/server/internal/store"
)

func TestCheckFindsGapsBothWaysAndRepairsTheIndex(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ix, _, err := search.Open(filepath.Join(dir, "index"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	fs := files.NewStore(dir)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := st.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'a', 'family', 1)`)
	doc := func(id int64, title string, archive int, deleted any) {
		exec(`INSERT INTO documents (id, workspace_id, title, content, created_date, added_at, modified_at, mime_type, original_filename, original_ext, checksum, has_archive, deleted_at, indexed_rev, index_rev)
			VALUES (?, 1, ?, '', '2024-01-01', 1, 1, 'application/pdf', 'a.pdf', 'pdf', ?, ?, ?, 1, 1)`, id, title, title, archive, deleted)
	}
	write := func(p string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	doc(1, "完整", 1, nil)
	write(fs.Path(files.Original(1, 1, "pdf")))
	write(fs.Path(files.Archive(1, 1)))
	doc(2, "原件丢了", 0, nil)
	doc(3, "可搜索版丢了", 1, nil)
	write(fs.Path(files.Original(1, 3, "pdf")))
	doc(4, "在回收站", 0, 1)
	write(fs.Path(files.Original(1, 4, "pdf")))
	write(fs.Path(files.Original(1, 99, "pdf"))) // no record
	write(fs.ThumbnailPath(1, 1))                // has_thumbnail is 0
	// Index: 1 present, 3 missing, 4 (trash) and 77 (gone) linger.
	if err := ix.Put([]search.Doc{{ID: 1, WorkspaceID: 1, Title: "完整"}, {ID: 4, WorkspaceID: 1, Title: "x"}, {ID: 77, WorkspaceID: 1, Title: "y"}}); err != nil {
		t.Fatal(err)
	}

	rep, err := NewChecker(st.DB, fs, dir, ix).Check(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Documents != 3 || rep.MissingCount != 2 || rep.OrphanCount != 2 || rep.OK() {
		t.Errorf("report = %+v", rep)
	}
	if rep.MissingFiles[0].DocumentID != 2 || rep.MissingFiles[0].File != "original" || rep.MissingFiles[1].File != "archive" {
		t.Errorf("missing = %+v", rep.MissingFiles)
	}
	// 2 and 3 are not in the index: queued again; 4 and 77 removed.
	if rep.Reindexed != 2 || rep.RemovedFromIndex != 2 || !rep.IndexChecked {
		t.Errorf("index: %+v", rep)
	}
	var behind int
	st.DB.QueryRow(`SELECT count(*) FROM documents WHERE indexed_rev < index_rev`).Scan(&behind)
	if behind != 2 {
		t.Errorf("%d documents queued for indexing, want 2", behind)
	}
	if _, err := os.Stat(fs.Path(files.Original(1, 99, "pdf"))); err != nil {
		t.Error("orphan files must be kept")
	}
}

func TestCheckCoversVersions(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	fs := files.NewStore(dir)
	for _, q := range []string{
		`INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'a', 'family', 1)`,
		`INSERT INTO documents (id, workspace_id, title, content, created_date, added_at, modified_at, mime_type, original_filename, original_ext, checksum)
		 VALUES (1, 1, '合同', '', '2024-01-01', 1, 1, 'text/plain', 'b.txt', 'txt', 'c2')`,
		`INSERT INTO document_versions (id, workspace_id, document_id, number, original_filename, original_ext, mime_type, checksum, size, current, created_at) VALUES
		 (5, 1, 1, 1, 'a.pdf', 'pdf', 'application/pdf', 'c1', 1, 0, 1), (6, 1, 1, 2, 'b.txt', 'txt', 'text/plain', 'c2', 1, 1, 1),
		 (7, 1, 1, 3, 'c.pdf', 'pdf', 'application/pdf', 'c3', 1, 0, 1)`,
	} {
		if _, err := st.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{fs.Path(files.Original(1, 1, "txt")), fs.Path(files.Version(1, 1, 5, "pdf")), fs.Path(files.Version(1, 1, 99, "pdf"))} {
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := NewChecker(st.DB, fs, dir, nil).Check(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rep.MissingCount != 1 || rep.MissingFiles[0].File != "version 3" || rep.OrphanCount != 1 || rep.OrphanFiles[0] != "media/versions/1/1-99.pdf" {
		t.Fatalf("report: %+v", rep)
	}
	// Purging the document removes its earlier versions too.
	if err := fs.RemoveDocument(context.Background(), files.Local, 1, 1, "txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fs.Path(files.Version(1, 1, 5, "pdf"))); !os.IsNotExist(err) {
		t.Fatalf("version file left: %v", err)
	}
}

func TestCheckBookCovers(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	fs := files.NewStore(dir)
	for _, q := range []string{
		`INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'a', 'family', 1)`,
		`INSERT INTO documents (id, workspace_id, title, content, created_date, added_at, modified_at, mime_type, original_filename, original_ext, checksum)
		 VALUES (1, 1, '活着', '', '2024-01-01', 1, 1, 'text/plain', 'a.txt', 'txt', 'c1'), (2, 1, '呐喊', '', '2024-01-01', 1, 1, 'text/plain', 'b.txt', 'txt', 'c2')`,
		`INSERT INTO books (document_id, workspace_id, cover, updated_at) VALUES (1, 1, 1, 1), (2, 1, 0, 1)`,
	} {
		if _, err := st.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	// 1 has a cover on record but not on disk; 2 has one on disk but not on record.
	for _, p := range []string{fs.Path(files.Original(1, 1, "txt")), fs.Path(files.Original(1, 2, "txt")), fs.CoverPath(1, 2)} {
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := NewChecker(st.DB, fs, dir, nil).Check(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rep.MissingCount != 1 || rep.MissingFiles[0].File != "cover" || rep.OrphanCount != 1 || rep.OrphanFiles[0] != "media/covers/1/2.jpg" {
		t.Errorf("report = %+v", rep)
	}
}
