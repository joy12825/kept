package views

import (
	"context"
	"encoding/json"
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

func ptr[T any](v T) *T { return &v }

func raw(s string) *json.RawMessage { r := json.RawMessage(s); return &r }

func TestViews(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, q := range []string{
		`INSERT INTO users (id, username, display_name, password_hash, password_changed_at, created_at, updated_at) VALUES (1, 'wang', '小王', 'x', 1, 1, 1), (2, 'li', '小李', 'x', 1, 1, 1)`,
		`INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'a', 'family', 1), (2, 'b', 'family', 1)`,
		`INSERT INTO tags (id, workspace_id, name, sort_key, color, created_at, updated_at) VALUES (31, 1, '税务', 'shuiwu', 'blue', 1, 1), (32, 2, '其他', 'qita', 'blue', 1, 1)`,
	} {
		if _, err := st.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s := NewService(st.DB)
	editor := tenant.New(1, tenant.RoleEditor)
	wang := audit.WithActor(context.Background(), audit.Actor{UserID: 1, Name: "小王", Via: "session"})
	li := audit.WithActor(context.Background(), audit.Actor{UserID: 2, Name: "小李", Via: "session"})

	tax, err := s.Create(wang, editor, Input{Name: ptr(" 2024 税务 "), Filter: raw(`{ "all": [ {"field":"tag","op":"has_any","value":[31]} ] }`), Sort: ptr("created:desc")})
	if err != nil {
		t.Fatal(err)
	}
	if tax.Name != "2024 税务" || string(tax.Filter) != `{"all":[{"field":"tag","op":"has_any","value":[31]}]}` || tax.CreatorName != "小王" {
		t.Errorf("created = %+v (filter %s)", tax, tax.Filter)
	}
	if _, err := s.Create(li, editor, Input{Name: ptr("待报销发票"), Filter: raw(`{}`)}); err != nil {
		t.Fatal(err)
	}
	list, _ := s.List(context.Background(), tenant.New(1, tenant.RoleViewer))
	if len(list) != 2 || list[0].Name != "2024 税务" || list[1].Name != "待报销发票" {
		t.Errorf("list = %+v", list)
	}

	for name, in := range map[string]Input{
		"taken":      {Name: ptr("2024 税务"), Filter: raw(`{}`)},
		"empty name": {Name: ptr(" "), Filter: raw(`{}`)},
		"other tag":  {Name: ptr("x"), Filter: raw(`{"field":"tag","op":"has_any","value":[32]}`)},
		"bad filter": {Name: ptr("x"), Filter: raw(`{"nope":1}`)},
		"bad sort":   {Name: ptr("x"), Filter: raw(`{}`), Sort: ptr("size:desc")},
	} {
		if _, err := s.Create(wang, editor, in); err == nil {
			t.Errorf("%s: should fail", name)
		}
	}

	if _, err := s.Update(li, editor, tax.ID, Input{Name: ptr("改名")}); kind(err) != apperr.KindForbidden {
		t.Errorf("someone else's view: %v", err)
	}
	if v, err := s.Update(wang, editor, tax.ID, Input{Name: ptr("税务")}); err != nil || v.Name != "税务" || v.Sort != "created:desc" {
		t.Errorf("rename = %+v, %v", v, err)
	}
	if err := s.Delete(wang, tenant.New(2, tenant.RoleOwner), tax.ID); kind(err) != apperr.KindNotFound {
		t.Errorf("another workspace: %v", err)
	}
	if err := s.Delete(li, tenant.New(1, tenant.RoleOwner), tax.ID); err != nil {
		t.Errorf("an owner deletes any view: %v", err)
	}
	var n int
	_ = st.DB.QueryRow(`SELECT count(*) FROM audit_events WHERE action LIKE 'view.%'`).Scan(&n)
	if n != 4 {
		t.Errorf("audit events = %d, want 4", n)
	}
}

func TestDisplay(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.DB.Exec(`INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'a', 'family', 1)`); err != nil {
		t.Fatal(err)
	}
	s := NewService(st.DB)
	scope := tenant.New(1, tenant.RoleOwner)
	ctx := context.Background()
	for _, d := range []Display{{View: "table"}, {Columns: []string{"title"}}, {Columns: []string{"tags", "tags"}}} {
		if _, err := s.Create(ctx, scope, Input{Name: ptr("x"), Filter: raw(`{}`), Display: &d}); kind(err) != apperr.KindInvalid {
			t.Errorf("%+v: %v", d, err)
		}
	}
	v, err := s.Create(ctx, scope, Input{Name: ptr("网格"), Filter: raw(`{}`), Display: &Display{View: "grid", Columns: []string{"amount", "dueDate"}}})
	if err != nil {
		t.Fatal(err)
	}
	if v.Display.View != "grid" || len(v.Display.Columns) != 2 {
		t.Fatalf("created: %+v", v.Display)
	}
	// Changing only the display is an update; other fields stay.
	v, err = s.Update(ctx, scope, v.ID, Input{Display: &Display{View: "list"}})
	if err != nil || v.Display.View != "list" || v.Display.Columns != nil || v.Name != "网格" {
		t.Fatalf("updated: %+v %v", v, err)
	}
	plain, err := s.Create(ctx, scope, Input{Name: ptr("默认"), Filter: raw(`{}`)})
	if err != nil || plain.Display.View != "" || plain.Display.Columns != nil {
		t.Fatalf("default: %+v %v", plain.Display, err)
	}
}

func TestDisplayAcceptsFieldColumns(t *testing.T) {
	for _, ok := range [][]string{{"field:12"}, {"date", "field:3", "field:40"}, {"notes", "pages"}} {
		if _, err := (Display{Columns: ok}).encode(); err != nil {
			t.Errorf("%v: %v", ok, err)
		}
	}
	for _, bad := range [][]string{{"field:"}, {"field:0"}, {"field:x"}, {"field:1", "field:1"}, {"nope"}} {
		if _, err := (Display{Columns: bad}).encode(); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}
