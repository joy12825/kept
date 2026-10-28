package taxonomy

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"kept/server/internal/audit"
	"kept/server/internal/store"
	"kept/server/internal/tenant"
)

func TestMerge(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	exec := func(q string) {
		t.Helper()
		if _, err := st.DB.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'a', 'family', 1), (2, 'b', 'family', 1)`)
	exec(`INSERT INTO correspondents (id, workspace_id, name, sort_key, created_at, updated_at) VALUES (20, 1, '国家电网', 'g', 1, 1), (21, 1, '国网北京', 'g', 1, 1), (22, 2, '别人的', 'b', 1, 1)`)
	exec(`INSERT INTO tags (id, workspace_id, name, sort_key, color, created_at, updated_at) VALUES (30, 1, '水电', 's', 'x', 1, 1), (31, 1, '水电费', 's', 'x', 1, 1), (32, 1, '电', 'd', 'x', 1, 1)`)
	exec(`UPDATE tags SET parent_id = 31 WHERE id = 32`)
	for i, c := range []int{20, 21, 21} {
		exec(`INSERT INTO documents (id, workspace_id, title, created_date, added_at, modified_at, mime_type, original_filename, original_ext, checksum, correspondent_id)
			VALUES (` + itoa(i+1) + `, 1, 'd', '2024-01-01', 1, 1, 'text/plain', 'a.txt', 'txt', 'c` + itoa(i) + `', ` + itoa(c) + `)`)
	}
	exec(`INSERT INTO document_tags (document_id, tag_id) VALUES (1, 30), (2, 31), (1, 31)`)
	exec(`INSERT INTO drawers (id, workspace_id, name, rule, created_at, updated_at) VALUES (40, 1, '房子', '{"any":[{"field":"correspondent","op":"in","value":[21,20]},{"field":"tag","op":"has_any","value":[31]}]}', 1, 1)`)
	s := NewService(st.DB, Options{})
	ctx := audit.WithActor(context.Background(), audit.Actor{Name: "小王", Via: audit.ViaSession})
	scope := tenant.New(1, tenant.RoleEditor)

	n, err := s.Merge(ctx, scope, Correspondent, 20, []int64{21})
	if err != nil || n != 2 {
		t.Fatalf("merge correspondents = %d %v", n, err)
	}
	var cnt int
	st.DB.QueryRow(`SELECT count(*) FROM documents WHERE correspondent_id = 20`).Scan(&cnt)
	if cnt != 3 {
		t.Errorf("documents of 20 = %d", cnt)
	}
	if n, err := s.Merge(ctx, scope, Tag, 30, []int64{31}); err != nil || n != 1 {
		t.Fatalf("merge tags = %d %v", n, err)
	}
	var parent int64
	st.DB.QueryRow(`SELECT parent_id FROM tags WHERE id = 32`).Scan(&parent)
	st.DB.QueryRow(`SELECT count(*) FROM document_tags WHERE tag_id = 30`).Scan(&cnt)
	if parent != 30 || cnt != 2 {
		t.Errorf("tags: parent %d, documents %d", parent, cnt)
	}
	var rule string
	st.DB.QueryRow(`SELECT rule FROM drawers WHERE id = 40`).Scan(&rule)
	if !strings.Contains(rule, `"value":[20]`) || !strings.Contains(rule, `"value":[30]`) {
		t.Errorf("rule = %s", rule)
	}
	var summary string
	st.DB.QueryRow(`SELECT summary FROM audit_events WHERE action = 'correspondent.merge'`).Scan(&summary)
	if summary != "小王 把往来方“国网北京”合并到“国家电网”" {
		t.Errorf("audit = %q", summary)
	}
	for _, bad := range [][]int64{{20}, {}, {22}} {
		if _, err := s.Merge(ctx, scope, Correspondent, 20, bad); err == nil {
			t.Errorf("merge %v should fail", bad)
		}
	}
}

func itoa(i int) string { return strconv.Itoa(i) }
