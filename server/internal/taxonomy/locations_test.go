package taxonomy

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

func TestLocationsAndPersons(t *testing.T) {
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
	for i, asn := range []string{"1", "2", "3", "NULL", "5"} {
		exec(`INSERT INTO documents (id, workspace_id, title, created_date, added_at, modified_at, mime_type, original_filename, original_ext, checksum, asn)
			VALUES (` + itoa(i+1) + `, 1, 'd', '2024-01-01', 1, 1, 'text/plain', 'a.txt', 'txt', 'c` + itoa(i) + `', ` + asn + `)`)
	}
	exec(`UPDATE documents SET deleted_at = 9 WHERE id = 5`)
	s := NewService(st.DB, Options{})
	ctx := audit.WithActor(context.Background(), audit.Actor{Name: "小王", Via: audit.ViaSession})
	scope := tenant.New(1, tenant.RoleEditor)

	box, err := s.CreateLocation(ctx, scope, LocationInput{Name: " 文件盒 A ", Note: "书房柜子第 2 层"})
	if err != nil || box.Name != "文件盒 A" || box.Initial != "W" {
		t.Fatalf("create = %+v %v", box, err)
	}
	if _, err := s.CreateLocation(ctx, scope, LocationInput{Name: "文件盒 a"}); err == nil {
		t.Error("a duplicate name (case aside) should fail")
	}
	if _, err := s.CreateLocation(ctx, tenant.New(2, tenant.RoleEditor), LocationInput{Name: "文件盒 A"}); err != nil {
		t.Errorf("another workspace may use the name: %v", err)
	}

	// originals 1..5: 4 has no number, 5 is in the trash
	if n, err := s.AssignLocationByASN(ctx, scope, box.ID, 1, 5); err != nil || n != 3 {
		t.Fatalf("assign = %d %v", n, err)
	}
	for _, bad := range [][2]int64{{0, 3}, {5, 2}, {1, 200000}} {
		if _, err := s.AssignLocationByASN(ctx, scope, box.ID, bad[0], bad[1]); err == nil {
			t.Errorf("range %v should fail", bad)
		}
	}
	if _, err := s.AssignLocationByASN(ctx, tenant.New(2, tenant.RoleEditor), box.ID, 1, 3); !errors.Is(err, ErrNotFound) {
		t.Errorf("another workspace's location: %v", err)
	}
	list, err := s.ListLocations(ctx, scope)
	if err != nil || len(list) != 1 || list[0].Documents != 3 {
		t.Fatalf("list = %+v %v", list, err)
	}
	if n, _ := s.AssignLocationByASN(ctx, scope, 0, 3, 3); n != 1 {
		t.Errorf("clearing = %d", n)
	}
	up, err := s.UpdateLocation(ctx, scope, box.ID, LocationInput{Name: "文件盒 B", Note: ""})
	if err != nil || up.Name != "文件盒 B" || up.Documents != 2 {
		t.Errorf("update = %+v %v", up, err)
	}
	if err := s.DeleteLocation(ctx, scope, box.ID); err != nil {
		t.Fatal(err)
	}
	var left int
	st.DB.QueryRow(`SELECT count(*) FROM documents WHERE location_id IS NOT NULL`).Scan(&left)
	if left != 0 {
		t.Errorf("documents still in a deleted location: %d", left)
	}
	var e *apperr.Error
	if _, err := s.CreateLocation(ctx, scope, LocationInput{Name: "x", Note: string(make([]rune, 201))}); !errors.As(err, &e) || e.Kind != apperr.KindInvalid {
		t.Errorf("long note: %v", err)
	}
	var hist int
	st.DB.QueryRow(`SELECT count(*) FROM audit_events WHERE action LIKE 'location.%'`).Scan(&hist)
	if hist != 6 {
		t.Errorf("audit events = %d", hist)
	}

	// persons work like correspondents, merges included
	dad, err := s.Create(ctx, scope, Person, Input{Name: "爸爸"})
	if err != nil || dad.Rule.Algorithm != AlgorithmAuto {
		t.Fatalf("person = %+v %v", dad, err)
	}
	papa, _ := s.Create(ctx, scope, Person, Input{Name: "老王"})
	exec(`UPDATE documents SET person_id = ` + itoa(int(papa.ID)) + ` WHERE id IN (1, 2)`)
	if n, err := s.MergeInto(ctx, scope, Person, dad.ID, []int64{papa.ID}); err != nil || n != 2 {
		t.Errorf("merge persons = %d %v", n, err)
	}
	people, _ := s.List(ctx, scope, Person)
	if len(people) != 1 || people[0].Name != "爸爸" {
		t.Errorf("persons = %+v", people)
	}
}
