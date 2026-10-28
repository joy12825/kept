package notes

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/document"
	"kept/server/internal/store"
	"kept/server/internal/tenant"
)

func setup(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, q := range []string{
		`INSERT INTO users (id, username, display_name, password_hash, password_changed_at, created_at, updated_at) VALUES (1, 'wang', '小王', 'x', 1, 1, 1), (2, 'li', '小李', 'x', 1, 1, 1)`,
		`INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'a', 'family', 1), (2, 'b', 'family', 1)`,
		`INSERT INTO documents (id, workspace_id, title, created_date, added_at, modified_at, mime_type, original_filename, original_ext, checksum)
		 VALUES (1, 1, '电费账单', '2024-03-05', 1, 1, 'application/pdf', 'a.pdf', 'pdf', 'a'), (2, 2, '别人的', '2024-03-05', 1, 1, 'application/pdf', 'a.pdf', 'pdf', 'b'),
		        (3, 1, '已删除', '2024-03-05', 1, 1, 'application/pdf', 'a.pdf', 'pdf', 'c')`,
		`UPDATE documents SET deleted_at = 1 WHERE id = 3`,
	} {
		if _, err := st.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return NewService(st.DB, nil), st
}

func as(id int64, name string) context.Context {
	return audit.WithActor(context.Background(), audit.Actor{UserID: id, Name: name, Via: "session"})
}

func kind(err error) apperr.Kind {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Kind
	}
	return 0
}

func TestNotes(t *testing.T) {
	s, st := setup(t)
	editor := tenant.New(1, tenant.RoleEditor)
	wang, li := as(1, "小王"), as(2, "小李")

	rev := func() (r int64) {
		_ = st.DB.QueryRow(`SELECT index_rev FROM documents WHERE id = 1`).Scan(&r)
		return r
	}
	r0 := rev()
	n1, err := s.Create(wang, editor, 1, "  押金已退  ")
	if rev() != r0+1 {
		t.Errorf("a note should raise index_rev: %d → %d", r0, rev())
	}
	if err != nil || n1.Body != "押金已退" || n1.AuthorName != "小王" {
		t.Fatalf("create = %+v, %v", n1, err)
	}
	if _, err := s.Create(li, editor, 1, "第二条"); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(context.Background(), editor, 1)
	if err != nil || len(list) != 2 || list[0].Body != "第二条" || list[1].AuthorName != "小王" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if _, err := s.Create(wang, editor, 1, " "); kind(err) != apperr.KindInvalid {
		t.Errorf("empty note: %v", err)
	}
	if _, err := s.Create(wang, editor, 1, strings.Repeat("字", 2001)); kind(err) != apperr.KindInvalid {
		t.Errorf("long note: %v", err)
	}
	for _, doc := range []int64{2, 3, 99} {
		if _, err := s.Create(wang, editor, doc, "x"); err != document.ErrNotFound {
			t.Errorf("note on document %d: %v", doc, err)
		}
		if _, err := s.List(context.Background(), editor, doc); err != document.ErrNotFound {
			t.Errorf("list on document %d: %v", doc, err)
		}
	}

	if err := s.Delete(li, editor, 1, n1.ID); kind(err) != apperr.KindForbidden {
		t.Errorf("someone else's note: %v", err)
	}
	if err := s.Delete(li, tenant.New(1, tenant.RoleOwner), 1, n1.ID); err != nil {
		t.Errorf("an owner deletes any note: %v", err)
	}
	if err := s.Delete(wang, editor, 1, n1.ID); kind(err) != apperr.KindNotFound {
		t.Errorf("deleted twice: %v", err)
	}

	var summaries []string
	rows, _ := st.DB.Query(`SELECT summary FROM audit_events WHERE target_type = 'document' AND target_id = '1' ORDER BY id`)
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		summaries = append(summaries, s)
	}
	rows.Close()
	want := "小王 给“电费账单”添加了备注|小李 给“电费账单”添加了备注|小李 删除了“电费账单”的一条备注"
	if strings.Join(summaries, "|") != want {
		t.Errorf("history = %v", summaries)
	}

	// The audit log lists them as the document's history.
	entries, _, err := audit.NewLog(st.DB).ListTarget(context.Background(), editor, "document", "1", audit.Filter{})
	if err != nil || len(entries) != 3 || entries[0].Action != "note.delete" || entries[2].ActorName != "小王" {
		t.Errorf("ListTarget = %+v, %v", entries, err)
	}
	if entries, _, _ := audit.NewLog(st.DB).ListTarget(context.Background(), tenant.New(2, tenant.RoleOwner), "document", "1", audit.Filter{}); len(entries) != 0 {
		t.Error("another workspace must not see the history")
	}
}
