package document

import (
	"fmt"
	"testing"

	"kept/server/internal/apperr"
)

// v1.5.6: sorting by any custom field, and its values on each row.
func TestListByCustomFields(t *testing.T) {
	e := newEnv(t)
	e.fixture()
	e.exec(`INSERT INTO custom_fields (id, workspace_id, name, sort_key, data_type, extra, created_at, updated_at) VALUES
		(60, 1, '状态', 'zhuangtai', 'select', '{"options":[{"id":"z","label":"待报销"},{"id":"y","label":"已提交"},{"id":"x","label":"已报销"}]}', 1, 1),
		(61, 1, '车牌', 'chepai', 'text', '{}', 1, 1),
		(62, 1, '里程', 'licheng', 'float', '{}', 1, 1),
		(63, 1, '已归档', 'yiguidang', 'boolean', '{}', 1, 1),
		(64, 1, '关联', 'guanlian', 'document_link', '{}', 1, 1),
		(65, 2, '别人的', 'bierende', 'text', '{}', 1, 1)`)
	// status by the options' order (待报销 z, 已提交 y, 已报销 x), not by the IDs
	e.exec(`INSERT INTO custom_field_values (document_id, field_id, value_select) VALUES (1, 60, 'x'), (2, 60, 'z'), (4, 60, 'y')`)
	e.exec(`INSERT INTO custom_field_values (document_id, field_id, value_text) VALUES (3, 61, '京B 23456'), (5, 61, '京A 12345')`)
	e.exec(`INSERT INTO custom_field_values (document_id, field_id, value_float) VALUES (2, 62, 1520.5), (5, 62, 88)`)
	e.exec(`INSERT INTO custom_field_values (document_id, field_id, value_bool) VALUES (1, 63, 1), (3, 63, 0)`)
	cases := []struct {
		sort string
		desc bool
		want string
	}{
		{"field:60", false, "2 4 1 3 5"}, {"field:60", true, "1 4 2 5 3"}, // missing last either way
		{"field:61", false, "5 3 1 2 4"}, {"field:61", true, "3 5 4 2 1"},
		{"field:62", false, "5 2 1 3 4"},
		{"field:63", true, "1 3 5 4 2"},
		{"field:50", true, "1 5 4 3 2"}, // the built-in amount works the same way
	}
	for _, c := range cases {
		p, err := e.svc.List(e.ctx, e.scope, Query{Sort: c.sort, Desc: c.desc})
		if err != nil {
			t.Fatal(err)
		}
		if got := ids(p.Items); got != c.want {
			t.Errorf("sort %s desc=%v: %s, want %s", c.sort, c.desc, got, c.want)
		}
		// paging with the cursor gives the same order
		var paged []Summary
		cursor := ""
		for range 10 {
			pg, err := e.svc.List(e.ctx, e.scope, Query{Sort: c.sort, Desc: c.desc, Limit: 2, Cursor: cursor})
			if err != nil {
				t.Fatal(err)
			}
			paged = append(paged, pg.Items...)
			if cursor = pg.NextCursor; cursor == "" {
				break
			}
		}
		if ids(paged) != c.want {
			t.Errorf("sort %s desc=%v paged: %s", c.sort, c.desc, ids(paged))
		}
	}
	for _, bad := range []string{"field:64", "field:65", "field:999", "field:x"} {
		if _, err := e.svc.List(e.ctx, e.scope, Query{Sort: bad}); kindOf(err) != apperr.KindInvalid {
			t.Errorf("sort %s: err = %v", bad, err)
		}
	}

	// values of the asked fields only, set ones only, never another workspace's
	p, err := e.svc.List(e.ctx, e.scope, Query{Sort: SortTitle, Fields: []int64{60, 61, 63, 65}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]string{}
	for _, it := range p.Items {
		got[it.ID] = fmt.Sprint(it.FieldValues)
	}
	want := map[int64]string{
		1: "[{60  select x} {63  boolean true}]",
		2: "[{60  select z}]",
		3: "[{61  text 京B 23456} {63  boolean false}]",
		4: "[{60  select y}]",
		5: "[{61  text 京A 12345}]",
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("doc %d fields = %s, want %s", id, got[id], w)
		}
	}
	if p, _ := e.svc.List(e.ctx, e.scope, Query{}); p.Items[0].FieldValues != nil {
		t.Error("no fields asked, none loaded")
	}
}
