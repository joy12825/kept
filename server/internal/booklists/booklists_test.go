package booklists

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/store"
	"kept/server/internal/tenant"
)

func kind(err error) apperr.Kind {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Kind
	}
	return 0
}

func TestBookLists(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, q := range []string{
		`INSERT INTO users (id, username, display_name, password_hash, password_changed_at, created_at, updated_at) VALUES (1, 'wang', '小王', 'x', 1, 1, 1)`,
		`INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'a', 'family', 1), (2, 'b', 'family', 1)`,
		`INSERT INTO documents (id, workspace_id, title, created_date, added_at, modified_at, mime_type, original_filename, original_ext, checksum) VALUES
		 (1, 1, '红楼梦', '2024-03-05', 1, 1, 'application/pdf', 'a.pdf', 'pdf', 'a'), (2, 1, '西游记', '2024-03-05', 1, 1, 'application/pdf', 'b.pdf', 'pdf', 'b'),
		 (3, 1, '电费账单', '2024-03-05', 1, 1, 'application/pdf', 'c.pdf', 'pdf', 'c'), (4, 2, '别人的书', '2024-03-05', 1, 1, 'application/pdf', 'd.pdf', 'pdf', 'd')`,
		`INSERT INTO books (document_id, workspace_id, authors, cover, updated_at) VALUES (1, 1, '["曹雪芹"]', 1, 1), (2, 1, '[]', 0, 1), (4, 2, '[]', 0, 1)`,
	} {
		if _, err := st.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ctx := audit.WithActor(context.Background(), audit.Actor{UserID: 1, Name: "小王", Via: "session"})
	scope := tenant.New(1, tenant.RoleEditor)
	s := NewService(st.DB)

	l, err := s.Create(ctx, scope, " 四大名著 ", "经典")
	if err != nil || l.Name != "四大名著" || l.CreatedBy != 1 {
		t.Fatalf("create = %+v %v", l, err)
	}
	if _, err := s.Create(ctx, scope, "四大名著", ""); kind(err) != apperr.KindConflict {
		t.Errorf("same name: %v", err)
	}
	if _, err := s.Create(ctx, scope, "", ""); kind(err) != apperr.KindInvalid {
		t.Errorf("empty name: %v", err)
	}
	if err := s.Add(ctx, scope, l.ID, 1, "必读"); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(ctx, scope, l.ID, 2, ""); err != nil {
		t.Fatal(err)
	}
	for doc, want := range map[int64]apperr.Kind{1: apperr.KindConflict, 3: apperr.KindInvalid, 4: apperr.KindNotFound} {
		if err := s.Add(ctx, scope, l.ID, doc, ""); kind(err) != want {
			t.Errorf("add %d: %v", doc, err)
		}
	}
	_, items, err := s.Get(ctx, scope, l.ID)
	if err != nil || len(items) != 2 || items[0].Title != "红楼梦" || items[0].Note != "必读" || !items[0].HasCover || items[0].Authors[0] != "曹雪芹" {
		t.Fatalf("items = %+v %v", items, err)
	}
	if err := s.Reorder(ctx, scope, l.ID, []int64{2, 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.Reorder(ctx, scope, l.ID, []int64{2}); kind(err) != apperr.KindInvalid {
		t.Errorf("partial order: %v", err)
	}
	if err := s.SetNote(ctx, scope, l.ID, 2, "取经"); err != nil {
		t.Fatal(err)
	}
	_, items, _ = s.Get(ctx, scope, l.ID)
	if items[0].DocumentID != 2 || items[0].Note != "取经" {
		t.Errorf("after reorder = %+v", items)
	}
	if ids, _ := s.ListsOf(ctx, scope, 1); len(ids) != 1 || ids[0] != l.ID {
		t.Errorf("lists of 1 = %v", ids)
	}
	lists, _ := s.Lists(ctx, scope)
	if len(lists) != 1 || lists[0].Count != 2 || len(lists[0].Preview) != 2 {
		t.Errorf("lists = %+v", lists)
	}
	// A book in the trash leaves the list, and comes back when restored.
	st.DB.Exec(`UPDATE documents SET deleted_at = 1 WHERE id = 1`)
	if lists, _ := s.Lists(ctx, scope); lists[0].Count != 1 {
		t.Errorf("trash: %+v", lists)
	}
	name := "名著"
	if l2, err := s.Update(ctx, scope, l.ID, &name, nil); err != nil || l2.Name != "名著" || l2.Description != "经典" {
		t.Errorf("rename = %+v %v", l2, err)
	}
	if err := s.Remove(ctx, scope, l.ID, 2); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(ctx, scope, l.ID, 2); kind(err) != apperr.KindNotFound {
		t.Errorf("remove twice: %v", err)
	}
	if _, _, err := s.Get(ctx, tenant.New(2, tenant.RoleOwner), l.ID); kind(err) != apperr.KindNotFound {
		t.Errorf("other workspace: %v", err)
	}
	if err := s.Delete(ctx, scope, l.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	st.DB.QueryRow(`SELECT count(*) FROM audit_events WHERE action LIKE 'booklist.%'`).Scan(&n)
	if n != 3 {
		t.Errorf("audit = %d", n)
	}
}
