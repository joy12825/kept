package dedupe

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"kept/server/internal/audit"
	"kept/server/internal/document"
	"kept/server/internal/store"
	"kept/server/internal/tenant"
)

func TestScanDismissAndKeep(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := audit.WithActor(context.Background(), audit.Actor{UserID: 1, Name: "小王", Via: audit.ViaSession})
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := st.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO users (id, username, display_name, password_hash, password_changed_at, created_at, updated_at) VALUES (1, 'wang', '小王', 'x', 1, 1, 1)`)
	exec(`INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'a', 'family', 1), (2, 'b', 'family', 1)`)
	exec(`INSERT INTO tags (id, workspace_id, name, sort_key, color, created_at, updated_at) VALUES (7, 1, '发票', 'fapiao', '#000000', 1, 1), (8, 1, '报销', 'baoxiao', '#000000', 1, 1)`)
	doc := func(id, ws int64, title, content string) {
		exec(`INSERT INTO documents (id, workspace_id, title, content, created_date, added_at, modified_at, mime_type, original_filename, original_ext, checksum)
			VALUES (?, ?, ?, ?, '2024-03-05', 1, 1, 'text/plain', 'a.txt', 'txt', ?)`, id, ws, title, content, title)
	}
	doc(1, 1, "电子发票", invoice)
	doc(2, 1, "发票扫描件", scanned)
	doc(3, 1, "3月电费", bill("03", "12312", "152.35", "04月15日"))
	doc(4, 1, "4月电费", bill("04", "12588", "134.77", "05月15日"))
	doc(5, 1, "便条", "短")
	doc(6, 2, "别人的发票", invoice+" ")
	exec(`INSERT INTO document_tags (document_id, tag_id) VALUES (1, 7), (2, 8)`)

	docs := document.NewService(st.DB, document.Options{})
	s := NewService(st.DB, Options{Documents: docs})
	scope := tenant.New(1, tenant.RoleEditor)
	res, err := s.Scan(ctx, 1)
	if err != nil || res.Fingerprinted != 5 || res.Compared != 4 || res.Open != 1 {
		t.Fatalf("scan: %+v %v", res, err)
	}
	pairs, err := s.List(ctx, scope)
	if err != nil || len(pairs) != 1 || pairs[0].A.ID != 1 || pairs[0].B.ID != 2 || pairs[0].Similarity < 90 {
		t.Fatalf("pairs: %+v %v", pairs, err)
	}
	// Scanning again finds nothing new and fingerprints nothing.
	if res, _ := s.Scan(ctx, 1); res.Fingerprinted != 0 || res.Open != 1 {
		t.Fatalf("again: %+v", res)
	}
	// A dismissed pair stays dismissed.
	if err := s.Dismiss(ctx, scope, pairs[0].ID); err != nil {
		t.Fatal(err)
	}
	if res, _ := s.Scan(ctx, 1); res.Open != 0 {
		t.Fatalf("after dismiss: %+v", res)
	}
	if err := s.Dismiss(ctx, scope, pairs[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dismiss twice: %v", err)
	}

	// Keeping one: the other's tags move over, the other goes to the trash.
	exec(`UPDATE duplicate_pairs SET status = 'open'`)
	if err := s.Keep(ctx, scope, pairs[0].ID, 99); err == nil {
		t.Fatal("keeping a document outside the pair")
	}
	if err := s.Keep(ctx, tenant.New(2, tenant.RoleEditor), pairs[0].ID, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another workspace: %v", err)
	}
	if err := s.Keep(ctx, scope, pairs[0].ID, 1); err != nil {
		t.Fatal(err)
	}
	kept, _ := docs.GetDetail(ctx, scope, 1)
	if slices.Sort(kept.TagIDs); !slices.Equal(kept.TagIDs, []int64{7, 8}) {
		t.Errorf("tags: %v", kept.TagIDs)
	}
	if _, err := docs.GetDetail(ctx, scope, 2); !errors.Is(err, document.ErrNotFound) {
		t.Errorf("the other is not in the trash: %v", err)
	}
	if pairs, _ := s.List(ctx, scope); len(pairs) != 0 {
		t.Errorf("pairs left: %+v", pairs)
	}
}
