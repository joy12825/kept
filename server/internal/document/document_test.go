package document

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"kept/server/internal/filter"
	"kept/server/internal/pinyin"
	"kept/server/internal/search"
	"kept/server/internal/store"
	"kept/server/internal/tenant"
)

type env struct {
	t     *testing.T
	st    *store.Store
	svc   *Service
	ix    *search.Index
	scope tenant.Scope
	ctx   context.Context
}

// newEnv creates workspace 1 (types 10 账单 / 11 合同, correspondents
// 20 国家电网 / 21 北京燃气, tags 31 财务 > 32 发票, 33 水电, built-in fields)
// and workspace 2.
func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ix, _, err := search.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	e := &env{t: t, st: st, svc: NewService(st.DB, Options{Search: ix}), ix: ix, scope: tenant.New(1, tenant.RoleOwner), ctx: ctx}
	e.exec(`INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'a', 'family', 1), (2, 'b', 'family', 1)`)
	e.exec(`INSERT INTO document_types (id, workspace_id, name, sort_key, created_at, updated_at) VALUES (10, 1, '账单', 'zhangdan', 1, 1), (11, 1, '合同', 'hetong', 1, 1)`)
	e.exec(`INSERT INTO correspondents (id, workspace_id, name, sort_key, created_at, updated_at) VALUES (20, 1, '国家电网', 'guojiadianwang', 1, 1), (21, 1, '北京燃气', 'beijingranqi', 1, 1)`)
	e.exec(`INSERT INTO tags (id, workspace_id, name, sort_key, color, parent_id, created_at, updated_at) VALUES
		(31, 1, '财务', 'caiwu', 'blue', NULL, 1, 1), (32, 1, '发票', 'fapiao', 'blue', 31, 1, 1), (33, 1, '水电', 'shuidian', 'blue', NULL, 1, 1)`)
	e.exec(`INSERT INTO custom_fields (id, workspace_id, key, name, sort_key, data_type, extra, created_at, updated_at) VALUES
		(50, 1, 'amount', '金额', 'jine', 'monetary', '{"currency":"CNY"}', 1, 1), (51, 1, 'due_date', '到期日', 'daoqiri', 'date', '{}', 1, 1),
		(52, 1, NULL, '级别', 'jibie', 'select', '{"options":[{"id":"a","label":"A"}]}', 1, 1)`)
	return e
}

// index brings the search index up to date, as the background indexer would.
func (e *env) index() {
	e.t.Helper()
	x := search.NewIndexer(e.st.DB, e.ix, nil, nil)
	for {
		n, err := x.Pass(e.ctx)
		if err != nil {
			e.t.Fatal(err)
		}
		if n == 0 {
			return
		}
	}
}

func (e *env) exec(q string, args ...any) {
	e.t.Helper()
	if _, err := e.st.DB.ExecContext(e.ctx, q, args...); err != nil {
		e.t.Fatalf("%s: %v", q, err)
	}
}

// doc inserts an archived document (not in the inbox); typ and corr may be nil.
func (e *env) doc(id, ws int64, title, created string, added int64, typ, corr any, tags ...int64) {
	e.t.Helper()
	e.exec(`INSERT INTO documents (id, workspace_id, title, title_sort, content, created_date, added_at, modified_at, document_type_id, correspondent_id,
		mime_type, original_filename, original_ext, checksum, inbox) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?, 'application/pdf', 'x.pdf', 'pdf', ?, 0)`,
		id, ws, title, pinyin.TitleKey(title), "正文："+title, created, added, typ, corr, fmt.Sprint(id))
	for _, tg := range tags {
		e.exec(`INSERT INTO document_tags (document_id, tag_id) VALUES (?, ?)`, id, tg)
	}
}

func ids(items []Summary) string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = fmt.Sprint(it.ID)
	}
	return strings.Join(out, " ")
}

func mustFilter(t *testing.T, js string) filter.Cond {
	t.Helper()
	c, err := filter.ParseString(js)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (e *env) fixture() {
	e.doc(1, 1, "2024年3月电费账单", "2024-03-05", 300, 10, 20, 32, 33)
	e.doc(2, 1, "2024年10月电费账单", "2024-10-05", 100, 10, 20, 33)
	e.doc(3, 1, "房屋租赁合同", "2023-09-01", 200, 11, nil, 31)
	e.doc(4, 1, "2024年4月燃气账单", "2024-04-03", 400, 10, 21)
	e.doc(5, 1, "Apple 发票", "2024-03-05", 500, nil, nil)
	e.doc(6, 1, "已删除", "2024-05-01", 600, 10, 20)
	e.doc(7, 2, "别人的电费账单", "2024-03-05", 700, nil, nil)
	e.exec(`UPDATE documents SET deleted_at = 1 WHERE id = 6`)
	e.exec(`INSERT INTO custom_field_values (document_id, field_id, value_monetary) VALUES (1, 50, 32640)`)
	e.exec(`INSERT INTO custom_field_values (document_id, field_id, value_date) VALUES (1, 51, '2024-12-31')`)
	e.exec(`INSERT INTO custom_field_values (document_id, field_id, value_select) VALUES (1, 52, 'a')`)
	e.exec(`INSERT INTO notes (workspace_id, document_id, body, created_at) VALUES (1, 3, '押金 3000 元已退还', 1), (1, 3, '第二条', 2)`)
	e.index()
}

func TestListSortsAndExcludesTrashAndOtherWorkspaces(t *testing.T) {
	e := newEnv(t)
	e.fixture()
	cases := []struct {
		sort string
		desc bool
		want string
	}{
		{"", true, "2 4 5 1 3"}, // created, newest first; ties by id
		{SortCreated, false, "3 1 5 4 2"},
		{SortAdded, false, "2 3 1 4 5"},
		{SortTitle, false, "1 4 2 5 3"},         // 2024年3月 < 2024年4月 < 2024年10月 < apple < fangwu
		{SortCorrespondent, false, "4 1 2 3 5"}, // 北京燃气, 国家电网, then none (by id)
		{SortCorrespondent, true, "5 3 2 1 4"},
	}
	// v1.4.3: amount, due date and archive serial number, missing ones last
	e.exec(`INSERT INTO custom_field_values (document_id, field_id, value_monetary) VALUES (4, 50, 1000)`)
	e.exec(`INSERT INTO custom_field_values (document_id, field_id, value_date) VALUES (3, 51, '2025-06-30')`)
	e.exec(`UPDATE documents SET asn = 7 WHERE id = 2`)
	e.exec(`UPDATE documents SET asn = 3 WHERE id = 5`)
	cases = append(cases, []struct {
		sort string
		desc bool
		want string
	}{
		{SortAmount, false, "4 1 2 3 5"}, {SortAmount, true, "1 4 5 3 2"},
		{SortDue, false, "1 3 2 4 5"}, {SortDue, true, "3 1 5 4 2"},
		{SortASN, false, "5 2 1 3 4"}, {SortASN, true, "2 5 4 3 1"},
		{SortModified, false, "1 2 3 4 5"},
	}...)
	for _, c := range cases {
		p, err := e.svc.List(e.ctx, e.scope, Query{Sort: c.sort, Desc: c.desc})
		if err != nil {
			t.Fatal(err)
		}
		if got := ids(p.Items); got != c.want || p.Total != 5 {
			t.Errorf("sort %q desc=%v: %s (total %d), want %s", c.sort, c.desc, got, p.Total, c.want)
		}
	}
}

func TestListPagesWithCursor(t *testing.T) {
	e := newEnv(t)
	e.fixture()
	e.exec(`INSERT INTO custom_field_values (document_id, field_id, value_monetary) VALUES (4, 50, 1000)`)
	e.exec(`UPDATE documents SET asn = 7 WHERE id = 2`)
	for _, sort := range []string{SortCreated, SortAdded, SortTitle, SortCorrespondent, SortAmount, SortDue, SortASN, SortModified} {
		for _, desc := range []bool{false, true} {
			all, _ := e.svc.List(e.ctx, e.scope, Query{Sort: sort, Desc: desc})
			var got []Summary
			cursor := ""
			for range 10 {
				p, err := e.svc.List(e.ctx, e.scope, Query{Sort: sort, Desc: desc, Limit: 2, Cursor: cursor})
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, p.Items...)
				if p.NextCursor == "" {
					break
				}
				cursor = p.NextCursor
			}
			if ids(got) != ids(all.Items) {
				t.Errorf("%s desc=%v: paged %s, whole %s", sort, desc, ids(got), ids(all.Items))
			}
		}
	}
	if _, err := e.svc.List(e.ctx, e.scope, Query{Cursor: "garbage!"}); err == nil {
		t.Error("a bad cursor should be rejected")
	}
}

func TestListFilterDecorationAndHighlight(t *testing.T) {
	e := newEnv(t)
	e.fixture()
	p, err := e.svc.List(e.ctx, e.scope, Query{Desc: true, Filter: mustFilter(t, `{"all":[{"field":"tag","op":"has_any","value":[31],"descendants":true}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	if ids(p.Items) != "1 3" || p.Total != 2 {
		t.Fatalf("tag 财务 with children = %s", ids(p.Items))
	}
	d1 := p.Items[0]
	if fmt.Sprint(d1.TagIDs) != "[32 33]" || d1.Amount == nil || *d1.Amount != 32640 || d1.DueDate != "2024-12-31" || d1.Content != "" {
		t.Errorf("decoration = %+v", d1)
	}
	if p.Items[1].NoteCount != 2 {
		t.Errorf("note count = %d", p.Items[1].NoteCount)
	}

}

func show(segs []search.Segment) string {
	var b strings.Builder
	for _, s := range segs {
		if s.Hit {
			b.WriteString("[" + s.Text + "]")
		} else {
			b.WriteString(s.Text)
		}
	}
	return b.String()
}

func TestKeywordSearch(t *testing.T) {
	e := newEnv(t)
	e.fixture()
	list := func(js string, sort string) Page {
		t.Helper()
		p, err := e.svc.List(e.ctx, e.scope, Query{Filter: mustFilter(t, js), Sort: sort, Desc: true})
		if err != nil {
			t.Fatalf("%s: %v", js, err)
		}
		return p
	}
	p := list(`{"text":"押金"}`, "")
	if ids(p.Items) != "3" || show(p.Items[0].Highlight) != "[押金] 3000 元已退还 第二条" {
		t.Errorf("note match = %s %q", ids(p.Items), show(p.Items[0].Highlight))
	}
	p = list(`{"text":"apple"}`, "")
	if ids(p.Items) != "5" || show(p.Items[0].Highlight) != "正文：[Apple] 发票" {
		t.Errorf("title match = %s %q", ids(p.Items), show(p.Items[0].Highlight))
	}
	// Keywords combine with structured conditions; the other workspace and
	// the trash stay out.
	if p = list(`{"all":[{"text":"电费账单"},{"field":"type","op":"in","value":[10]}]}`, ""); ids(p.Items) != "2 1" {
		t.Errorf("keyword + type = %s", ids(p.Items))
	}
	if p = list(`{"all":[{"text":"电费"},{"field":"created","op":"between","value":["2024-03-01","2024-03-31"]}]}`, ""); ids(p.Items) != "1" {
		t.Errorf("keyword + date = %s", ids(p.Items))
	}
	if p = list(`{"not":{"text":"电费"}}`, ""); ids(p.Items) != "4 5 3" {
		t.Errorf("not keyword = %s", ids(p.Items))
	}
	// Relevance: 2024年3月电费账单 has the words in its title and text.
	p = list(`{"text":"3月 电费"}`, SortRelevance)
	if ids(p.Items) != "1" {
		t.Errorf("relevance = %s", ids(p.Items))
	}
	p = list(`{"text":"账单"}`, SortRelevance)
	if p.Total != 3 || len(p.Items) != 3 {
		t.Errorf("账单 = %s (total %d)", ids(p.Items), p.Total)
	}
	paged, err := e.svc.List(e.ctx, e.scope, Query{Filter: mustFilter(t, `{"text":"账单"}`), Sort: SortRelevance, Limit: 2})
	if err != nil || len(paged.Items) != 2 || paged.NextCursor == "" {
		t.Fatalf("first page = %+v, %v", paged, err)
	}
	rest, err := e.svc.List(e.ctx, e.scope, Query{Filter: mustFilter(t, `{"text":"账单"}`), Sort: SortRelevance, Limit: 2, Cursor: paged.NextCursor})
	if err != nil || ids(append(paged.Items, rest.Items...)) != ids(p.Items) {
		t.Errorf("paged relevance = %s + %s, want %s (%v)", ids(paged.Items), ids(rest.Items), ids(p.Items), err)
	}
	// Relevance without a keyword is newest first.
	if p = list(`{}`, SortRelevance); ids(p.Items) != "2 4 5 1 3" {
		t.Errorf("relevance without keyword = %s", ids(p.Items))
	}
	// Prefix: search-as-you-type.
	if p = list(`{"text":"房屋 租","prefix":true}`, ""); ids(p.Items) != "3" {
		t.Errorf("prefix = %s", ids(p.Items))
	}

	// Trash: gone after deletion, back after restoring.
	e.svc.Delete(e.ctx, e.scope, Selection{IDs: []int64{3}})
	e.index()
	if p = list(`{"text":"押金"}`, ""); len(p.Items) != 0 {
		t.Error("a deleted document must not be found")
	}
	e.svc.Restore(e.ctx, e.scope, 3)
	e.index()
	if p = list(`{"text":"押金"}`, ""); ids(p.Items) != "3" {
		t.Error("a restored document must be found again")
	}

	// Without an index, keywords fail clearly.
	noIndex := NewService(e.st.DB, Options{})
	if _, err := noIndex.List(e.ctx, e.scope, Query{Filter: mustFilter(t, `{"text":"电费"}`)}); err != filter.ErrSearchUnavailable {
		t.Errorf("no index: %v", err)
	}
}

func TestFacetsIgnoreTheirOwnGroup(t *testing.T) {
	e := newEnv(t)
	e.fixture()
	f := mustFilter(t, `{"all":[{"field":"type","op":"in","value":[10]},{"field":"created","op":"between","value":["2024-01-01","2024-12-31"]}]}`)
	got, err := e.svc.Facets(e.ctx, e.scope, f, []string{FacetType, FacetCorrespondent, FacetTag, FacetYear, FacetMonth})
	if err != nil {
		t.Fatal(err)
	}
	str := func(bs []Bucket) string {
		var out []string
		for _, b := range bs {
			out = append(out, fmt.Sprintf("%s:%d", b.Key, b.Count))
		}
		return strings.Join(out, " ")
	}
	want := map[string]string{
		FacetType:          "10:3 none:1",    // 2024 documents, any type
		FacetCorrespondent: "20:2 21:1",      // 账单 in 2024
		FacetTag:           "33:2 31:1 32:1", // 财务 counts 发票's document once
		FacetYear:          "2024:3",         // 账单 in any year; the 2023 contract is not a bill
		FacetMonth:         "2024-03:1 2024-04:1 2024-10:1",
	}
	for g, w := range want {
		if s := str(got[g]); s != w {
			t.Errorf("facet %s = %s, want %s", g, s, w)
		}
	}
	if _, err := e.svc.Facets(e.ctx, e.scope, filter.Cond{}, []string{"folder"}); err == nil {
		t.Error("an unknown group should be rejected")
	}
}

func TestGetDetail(t *testing.T) {
	e := newEnv(t)
	e.fixture()
	d, err := e.svc.GetDetail(e.ctx, e.scope, 1)
	if err != nil {
		t.Fatal(err)
	}
	if d.Content == "" || fmt.Sprint(d.TagIDs) != "[32 33]" || len(d.Fields) != 3 {
		t.Fatalf("detail = %+v", d)
	}
	if f := d.Fields[0]; f.Key != "amount" || f.Value != "326.40" {
		t.Errorf("amount field = %+v", f)
	}
	if f := d.Fields[2]; f.Key != "" || f.Value != "a" || f.DataType != "select" {
		t.Errorf("select field = %+v", f)
	}
	for _, id := range []int64{6, 7, 99} {
		if _, err := e.svc.GetDetail(e.ctx, e.scope, id); err != ErrNotFound {
			t.Errorf("document %d: err = %v, want not found", id, err)
		}
	}
}
