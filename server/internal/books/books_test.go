package books

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/document"
	"kept/server/internal/drawer"
	"kept/server/internal/files"
	"kept/server/internal/store"
	"kept/server/internal/tenant"
)

func TestNormalizeISBN(t *testing.T) {
	for in, want := range map[string]string{
		"":                  "",
		"978-7-5442-9116-3": "9787544291163",
		"7-5442-9116-X":     "",
		"0-306-40615-2":     "9780306406157",
		"080442957X":        "9780804429573",
		" 978 0306406157 ":  "9780306406157",
	} {
		got, err := NormalizeISBN(in)
		if want == "" && in != "" {
			if err == nil {
				t.Errorf("%q: want an error, got %q", in, got)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("%q = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"9787544291164", "978754429116", "12345678X0", "9770306406157", "abc"} {
		if _, err := NormalizeISBN(bad); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}

func kind(err error) apperr.Kind {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Kind
	}
	return 0
}

func TestBooksAndLibrary(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, q := range []string{
		`INSERT INTO users (id, username, display_name, password_hash, password_changed_at, created_at, updated_at) VALUES (1, 'wang', '小王', 'x', 1, 1, 1)`,
		`INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'a', 'family', 1), (2, 'b', 'family', 1)`,
		`INSERT INTO documents (id, workspace_id, title, created_date, added_at, modified_at, mime_type, original_filename, original_ext, checksum)
		 VALUES (1, 1, '活着', '2024-03-05', 1, 1, 'application/pdf', 'a.pdf', 'pdf', 'a'), (2, 1, '许三观卖血记', '2024-03-05', 1, 1, 'application/pdf', 'b.pdf', 'pdf', 'b'),
		        (3, 2, '别人的', '2024-03-05', 1, 1, 'application/pdf', 'c.pdf', 'pdf', 'c')`,
	} {
		if _, err := st.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ctx := audit.WithActor(context.Background(), audit.Actor{UserID: 1, Name: "小王", Via: "session"})
	scope := tenant.New(1, tenant.RoleEditor)
	drawers := drawer.NewService(st.DB, drawer.Options{})
	lib, _, err := drawers.EnableLibrary(ctx, tenant.New(1, tenant.RoleOwner))
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(st.DB, nil, drawers, files.NewStore(t.TempDir()))
	drawerOf := func(id int64) (d int64) {
		_ = st.DB.QueryRow(`SELECT coalesce(drawer_id, 0) FROM documents WHERE id = ?`, id).Scan(&d)
		return d
	}

	b, err := s.Set(ctx, scope, 1, Input{ISBN: "978-7-5063-6543-7", Authors: []string{" 余华 ", ""}, Publisher: "作家出版社", Pages: 191})
	if err != nil {
		t.Fatal(err)
	}
	if b.ISBN != "9787506365437" || len(b.Authors) != 1 || b.Authors[0] != "余华" || b.Pages != 191 {
		t.Errorf("book = %+v", b)
	}
	if drawerOf(1) != lib.ID {
		t.Errorf("drawer = %d, want the library %d", drawerOf(1), lib.ID)
	}
	if _, err := s.Set(ctx, scope, 2, Input{ISBN: "9787506365437"}); kind(err) != apperr.KindConflict {
		t.Errorf("duplicate ISBN: %v", err)
	}
	if _, err := s.Set(ctx, scope, 2, Input{ISBN: "123"}); kind(err) != apperr.KindInvalid {
		t.Errorf("bad ISBN: %v", err)
	}
	if _, err := s.Set(ctx, scope, 3, Input{}); !errors.Is(err, document.ErrNotFound) {
		t.Errorf("other workspace: %v", err)
	}
	if got, _ := s.Get(ctx, scope, 2); got != nil {
		t.Errorf("not a book: %+v", got)
	}
	var n int
	_ = st.DB.QueryRow(`SELECT count(*) FROM audit_events WHERE action = 'document.book'`).Scan(&n)
	if n != 1 {
		t.Errorf("audit rows = %d", n)
	}

	if err := s.Delete(ctx, scope, 1); err != nil {
		t.Fatal(err)
	}
	if drawerOf(1) != 0 {
		t.Errorf("still in the library")
	}
	if err := s.Delete(ctx, scope, 1); kind(err) != apperr.KindNotFound {
		t.Errorf("delete twice: %v", err)
	}
}
