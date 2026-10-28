package pages

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"testing"
	"time"

	"kept/server/internal/document"
	"kept/server/internal/events"
	"kept/server/internal/ingest"
	"kept/server/internal/jobs"
	"kept/server/internal/parser"
)

// withJobs gives the service a running job pool, ingest and documents.
func (e *env) withJobs(db *sql.DB) *jobs.Queue {
	e.t.Helper()
	queue := jobs.NewQueue(db)
	pool := jobs.NewPool(queue, jobs.PoolConfig{Workers: 1, PollInterval: 10 * time.Millisecond}, slog.New(slog.DiscardHandler))
	ing := ingest.NewService(db, queue, e.fs, events.NewBus(), ingest.Options{Parser: parser.Config{OCRLanguages: "eng", OCRMode: "skip", MinTextChars: 2}})
	e.s.Register(pool, Deps{Queue: queue, Ingest: ing, Documents: document.NewService(db, document.Options{Files: e.fs})})
	ctx, cancel := context.WithCancel(e.ctx)
	done := make(chan struct{})
	go func() { _ = pool.Run(ctx); close(done) }()
	e.t.Cleanup(func() { cancel(); <-done })
	return queue
}

func (e *env) wait(db *sql.DB, job int64) string {
	e.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var status string
		var errMsg sql.NullString
		db.QueryRow(`SELECT status, error FROM jobs WHERE id = ?`, job).Scan(&status, &errMsg)
		if status == "succeeded" || status == "failed" {
			if status == "failed" {
				e.t.Logf("job error: %s", errMsg.String)
			}
			return status
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.t.Fatal("job did not finish")
	return ""
}

type docInfo struct {
	id      int64
	title   string
	pages   int64
	content string
	date    string
	typeID  sql.NullInt64
	deleted bool
	tags    int
}

func docsByTitle(t *testing.T, db *sql.DB, like string) []docInfo {
	t.Helper()
	rows, err := db.Query(`SELECT id, title, page_count, content, created_date, document_type_id, deleted_at IS NOT NULL,
		(SELECT count(*) FROM document_tags WHERE document_id = documents.id) FROM documents WHERE title LIKE ? ORDER BY id`, like)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []docInfo
	for rows.Next() {
		var d docInfo
		rows.Scan(&d.id, &d.title, &d.pages, &d.content, &d.date, &d.typeID, &d.deleted, &d.tags)
		out = append(out, d)
	}
	return out
}

func TestSplit(t *testing.T) {
	e := newEnv(t)
	db := e.s.db
	e.withJobs(db)
	id := e.doc([]string{"P1", "P2", "P3", "P4", "P5"}, false)
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO document_types (workspace_id, name, sort_key, created_at, updated_at) VALUES (?, '合同', 'hetong', 1, 1)`, []any{e.ws}},
		{`UPDATE documents SET document_type_id = (SELECT id FROM document_types LIMIT 1), created_date = '2023-09-01' WHERE id = ?`, []any{id}},
		{`INSERT INTO tags (workspace_id, name, sort_key, color, created_at, updated_at) VALUES (?, '房子', 'fangzi', '#000000', 1, 1)`, []any{e.ws}},
		{`INSERT INTO document_tags (document_id, tag_id) VALUES (?, (SELECT id FROM tags LIMIT 1))`, []any{id}},
	} {
		if _, err := db.Exec(q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := e.s.Split(e.ctx, e.scope, id, []int{5}, false, "web"); err == nil {
		t.Error("splitting after the last page should be refused")
	}
	job, err := e.s.Split(e.ctx, e.scope, id, []int{2, 3}, true, "web")
	if err != nil {
		t.Fatal(err)
	}
	if st := e.wait(db, job); st != "succeeded" {
		t.Fatalf("split job %s", st)
	}
	parts := docsByTitle(t, db, "合同（%")
	if len(parts) != 3 || parts[0].title != "合同（1/3）" || parts[0].pages != 2 || parts[1].pages != 1 || parts[2].pages != 2 ||
		!strings.Contains(parts[2].content, "P5") || strings.Contains(parts[2].content, "P3") {
		t.Fatalf("parts = %+v", parts)
	}
	for _, p := range parts {
		if p.date != "2023-09-01" || !p.typeID.Valid || p.tags != 1 {
			t.Errorf("part %s lost the metadata: %+v", p.title, p)
		}
	}
	if orig := docsByTitle(t, db, "合同"); len(orig) != 1 || !orig[0].deleted {
		t.Errorf("the original should be in the trash: %+v", orig)
	}
}

func TestMerge(t *testing.T) {
	e := newEnv(t)
	db := e.s.db
	e.withJobs(db)
	a := e.doc([]string{"A1", "A2"}, false)
	b := e.doc([]string{"B1"}, true)
	db.Exec(`UPDATE documents SET title = '甲' WHERE id = ?`, a)
	db.Exec(`UPDATE documents SET title = '乙' WHERE id = ?`, b)

	if _, err := e.s.Merge(e.ctx, e.scope, []int64{a}, false, "web"); err == nil {
		t.Error("merging one document should be refused")
	}
	job, err := e.s.Merge(e.ctx, e.scope, []int64{b, a}, false, "web")
	if err != nil {
		t.Fatal(err)
	}
	if st := e.wait(db, job); st != "succeeded" {
		t.Fatalf("merge job %s", st)
	}
	var title, content string
	var pages int64
	if err := db.QueryRow(`SELECT title, content, page_count FROM documents WHERE id > ? ORDER BY id DESC LIMIT 1`, b).Scan(&title, &content, &pages); err != nil {
		t.Fatal(err)
	}
	if title != "乙" || pages != 3 || strings.Index(content, "B1") > strings.Index(content, "A1") {
		t.Errorf("merged = %q, %d pages, content %q", title, pages, content)
	}
	var trashed int
	db.QueryRow(`SELECT count(*) FROM documents WHERE deleted_at IS NOT NULL`).Scan(&trashed)
	if trashed != 0 {
		t.Error("without trash the originals stay")
	}
}
