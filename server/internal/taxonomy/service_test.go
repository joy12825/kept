package taxonomy

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/store"
	"kept/server/internal/tenant"
)

type env struct {
	t  *testing.T
	s  *Service
	st *store.Store
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, q := range []string{
		`INSERT INTO users (id, username, display_name, password_hash, password_changed_at, created_at, updated_at) VALUES (1, 'wang', '小王', 'x', 1, 1, 1)`,
		`INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'home', 'family', 1), (2, 'other', 'family', 1)`,
	} {
		if _, err := st.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2024, 3, 5, 10, 0, 0, 0, time.UTC)
	return &env{t: t, s: NewService(st.DB, Options{Now: func() time.Time { return now }}), st: st}
}

var (
	home  = tenant.New(1, tenant.RoleEditor)
	other = tenant.New(2, tenant.RoleOwner)
	ctx   = audit.WithActor(context.Background(), audit.Actor{UserID: 1, Name: "小王", Via: audit.ViaSession})
)

func (e *env) actions() []string {
	e.t.Helper()
	rows, err := e.st.DB.Query(`SELECT action || ' ' || summary FROM audit_events ORDER BY id`)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func isKind(err error, k apperr.Kind) bool {
	var e *apperr.Error
	return errors.As(err, &e) && e.Kind == k
}

func TestCorrespondentsAndTypes(t *testing.T) {
	for _, kind := range []Kind{Correspondent, DocumentType} {
		t.Run(string(kind), func(t *testing.T) {
			e := newEnv(t)
			grid, err := e.s.Create(ctx, home, kind, Input{Name: "  国家电网 ", Rule: &MatchRule{Algorithm: "any", Match: "国家电网 国网", CaseInsensitive: true}})
			if err != nil {
				t.Fatal(err)
			}
			if grid.Name != "国家电网" || grid.SortKey != "guojiadianwang" || grid.Initial != "G" || grid.Rule.Algorithm != "any" || grid.Rule.Match != "国家电网 国网" {
				t.Errorf("created = %+v", grid)
			}
			apple, err := e.s.Create(ctx, home, kind, Input{Name: "Apple"})
			if err != nil {
				t.Fatal(err)
			}
			if apple.Initial != "#" || apple.Rule.Algorithm != "auto" || !apple.Rule.CaseInsensitive || apple.Rule.Match != "" {
				t.Errorf("defaults = %+v", apple)
			}
			if _, err := e.s.Create(ctx, home, kind, Input{Name: "阿里云"}); err != nil {
				t.Fatal(err)
			}

			list, err := e.s.List(ctx, home, kind)
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, it := range list {
				names = append(names, it.Name)
			}
			if strings.Join(names, ",") != "阿里云,Apple,国家电网" {
				t.Errorf("list order = %v, want by pinyin sort key", names)
			}
			if list, _ := e.s.List(ctx, other, kind); len(list) != 0 {
				t.Errorf("another workspace sees %v", list)
			}

			// Names are unique per workspace, ignoring letter case; other workspaces are separate.
			if _, err := e.s.Create(ctx, home, kind, Input{Name: "APPLE"}); !isKind(err, apperr.KindConflict) {
				t.Errorf("duplicate name err = %v, want conflict", err)
			}
			if _, err := e.s.Create(ctx, other, kind, Input{Name: "Apple"}); err != nil {
				t.Errorf("same name in another workspace: %v", err)
			}

			name := "国网北京"
			got, err := e.s.Update(ctx, home, kind, grid.ID, Patch{Name: &name, Rule: &MatchRule{Algorithm: "literal", Match: "国网北京"}})
			if err != nil {
				t.Fatal(err)
			}
			if got.Name != name || got.SortKey != "guowangbeijing" || got.Rule.Algorithm != "literal" || got.Rule.CaseInsensitive {
				t.Errorf("updated = %+v", got)
			}
			before := len(e.actions())
			if _, err := e.s.Update(ctx, home, kind, grid.ID, Patch{Name: &name}); err != nil {
				t.Fatal(err)
			}
			if len(e.actions()) != before {
				t.Error("an update that changes nothing was audited")
			}
			if _, err := e.s.Update(ctx, other, kind, grid.ID, Patch{Name: &name}); !errors.Is(err, ErrNotFound) {
				t.Errorf("updating from another workspace err = %v, want ErrNotFound", err)
			}
			if err := e.s.Delete(ctx, other, kind, grid.ID); !errors.Is(err, ErrNotFound) {
				t.Errorf("deleting from another workspace err = %v", err)
			}
			if err := e.s.Delete(ctx, home, kind, grid.ID); err != nil {
				t.Fatal(err)
			}
			if err := e.s.Delete(ctx, home, kind, grid.ID); !errors.Is(err, ErrNotFound) {
				t.Errorf("deleting twice err = %v", err)
			}

			label := map[Kind]string{Correspondent: "往来方", DocumentType: "类型"}[kind]
			want := []string{
				string(kind) + ".create 小王 新建了" + label + "“国家电网”",
				string(kind) + ".create 小王 新建了" + label + "“Apple”",
				string(kind) + ".create 小王 新建了" + label + "“阿里云”",
				string(kind) + ".create 小王 新建了" + label + "“Apple”", // in the other workspace
				string(kind) + ".update 小王 将" + label + "“国家电网”改名为“国网北京”",
				string(kind) + ".delete 小王 删除了" + label + "“国网北京”",
			}
			if got := e.actions(); strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Errorf("audit:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}

func TestMatchRuleValidation(t *testing.T) {
	e := newEnv(t)
	bad := []MatchRule{
		{Algorithm: "magic"},
		{Algorithm: "any"},                 // keywords required
		{Algorithm: "regex", Match: "a(b"}, // does not compile
		{Algorithm: "fuzzy", Match: "   "},
	}
	for _, r := range bad {
		r := r
		if _, err := e.s.Create(ctx, home, Correspondent, Input{Name: "x", Rule: &r}); !isKind(err, apperr.KindInvalid) {
			t.Errorf("rule %+v err = %v, want invalid", r, err)
		}
	}
	// auto and none ignore keywords.
	it, err := e.s.Create(ctx, home, Correspondent, Input{Name: "x", Rule: &MatchRule{Algorithm: "none", Match: "ignored"}})
	if err != nil || it.Rule.Match != "" {
		t.Errorf("none rule = %+v, %v", it, err)
	}
	for _, n := range []string{"", "   ", strings.Repeat("长", 65)} {
		if _, err := e.s.Create(ctx, home, Correspondent, Input{Name: n}); !isKind(err, apperr.KindInvalid) {
			t.Errorf("name %q err = %v, want invalid", n, err)
		}
	}
}
