package filter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/store"
	"kept/server/internal/tenant"
)

// fixture: workspace 1 with documents 1–6, workspace 2 with document 7.
//
//	id  type  corr  tags          created     added       asn  inbox  amount   due         title / content / note
//	1   10    20    财务>发票(32)   2024-03-05  2024-03-06  1    0      326.40   2024-12-31  电费账单 / 国家电网 本期电费
//	2   10    21    水电(33)        2024-04-03  2024-04-04  2    0      120.00               燃气账单
//	3   11    —     财务(31)        2023-09-01  2023-09-02  —    0                           租赁合同 / note: 押金
//	4   —     20    发票(32),水电    2024-10-05  2024-10-06  —    1      236.40               2024年10月电费账单
//	5   11    21    —              2022-01-01  2022-01-02  —    0      O'Brien  (select a)  报告 'x' OR 1=1 --
//	6   10    20    —              2024-05-01  2024-05-01  —    0                           deleted (in the trash)
//	7   ws 2: 10 is not its type
func fixture(t *testing.T) (*store.Store, *Catalog) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := st.DB.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'a', 'family', 1), (2, 'b', 'family', 1)`)
	exec(`INSERT INTO document_types (id, workspace_id, name, sort_key, created_at, updated_at) VALUES (10, 1, '账单', 'zhangdan', 1, 1), (11, 1, '合同', 'hetong', 1, 1), (12, 2, '账单', 'zhangdan', 1, 1)`)
	exec(`INSERT INTO correspondents (id, workspace_id, name, sort_key, created_at, updated_at) VALUES (20, 1, '国家电网', 'g', 1, 1), (21, 1, '北京燃气', 'b', 1, 1)`)
	exec(`INSERT INTO tags (id, workspace_id, name, sort_key, color, parent_id, created_at, updated_at) VALUES
		(31, 1, '财务', 'caiwu', 'blue', NULL, 1, 1), (32, 1, '发票', 'fapiao', 'blue', 31, 1, 1), (33, 1, '水电', 'shuidian', 'blue', NULL, 1, 1), (34, 2, '其他', 'qita', 'blue', NULL, 1, 1)`)
	exec(`INSERT INTO custom_fields (id, workspace_id, name, sort_key, data_type, extra, created_at, updated_at) VALUES
		(40, 1, '备注码', 'beizhuma', 'text', '{}', 1, 1),
		(41, 1, '等级', 'dengji', 'select', '{"options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}', 1, 1),
		(42, 2, '别人的', 'bierende', 'text', '{}', 1, 1)`)
	// Workspaces inserted directly get no built-in fields; add them.
	exec(`INSERT INTO custom_fields (id, workspace_id, key, name, sort_key, data_type, extra, created_at, updated_at) VALUES
		(50, 1, 'amount', '金额', 'jine', 'monetary', '{"currency":"CNY"}', 1, 1), (51, 1, 'due_date', '到期日', 'daoqiri', 'date', '{}', 1, 1)`)
	const amount, due = 50, 51
	day := func(s string) int64 {
		tm, _ := time.ParseInLocation("2006-01-02", s, time.Local)
		return tm.Add(10 * time.Hour).UnixMilli()
	}
	doc := func(id, ws int64, typ, corr any, created, added string, asn any, inbox int, title, content string) {
		exec(`INSERT INTO documents (id, workspace_id, title, content, created_date, added_at, modified_at, document_type_id, correspondent_id, asn, inbox,
			mime_type, original_filename, original_ext, checksum) VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, 'application/pdf', 'x.pdf', 'pdf', ?)`,
			id, ws, title, content, created, day(added), typ, corr, asn, inbox, fmt.Sprint(id))
	}
	doc(1, 1, 10, 20, "2024-03-05", "2024-03-06", 1, 0, "电费账单", "国家电网 本期电费")
	doc(2, 1, 10, 21, "2024-04-03", "2024-04-04", 2, 0, "燃气账单", "")
	doc(3, 1, 11, nil, "2023-09-01", "2023-09-02", nil, 0, "租赁合同", "")
	doc(4, 1, nil, 20, "2024-10-05", "2024-10-06", nil, 1, "2024年10月电费账单", "")
	doc(5, 1, 11, 21, "2022-01-01", "2022-01-02", nil, 0, "报告 'x' OR 1=1 --", "100%")
	doc(6, 1, 10, 20, "2024-05-01", "2024-05-01", nil, 0, "deleted", "")
	doc(7, 2, 12, nil, "2024-03-05", "2024-03-06", nil, 0, "电费账单", "")
	exec(`UPDATE documents SET deleted_at = 1 WHERE id = 6`)
	exec(`INSERT INTO document_tags (document_id, tag_id) VALUES (1, 32), (2, 33), (3, 31), (4, 32), (4, 33), (7, 34)`)
	exec(`INSERT INTO notes (workspace_id, document_id, body, created_at) VALUES (1, 3, '押金已退', 1)`)
	exec(`INSERT INTO custom_field_values (document_id, field_id, value_monetary) VALUES (1, ?, 32640), (2, ?, 12000), (4, ?, 23640)`, amount, amount, amount)
	exec(`INSERT INTO custom_field_values (document_id, field_id, value_date) VALUES (1, ?, '2024-12-31')`, due)
	exec(`INSERT INTO custom_field_values (document_id, field_id, value_text) VALUES (5, 40, 'O''Brien')`)
	exec(`INSERT INTO custom_field_values (document_id, field_id, value_select) VALUES (5, 41, 'a')`)
	cat, err := LoadCatalog(ctx, st.Queries, tenant.New(1, tenant.RoleOwner))
	if err != nil {
		t.Fatal(err)
	}
	cat.Text = fakeSearch(st.DB)
	return st, cat
}

// fakeSearch stands in for the full-text index: a case-insensitive substring
// match on title, text and notes (the real index is tested in package search).
func fakeSearch(db *sql.DB) TextResolver {
	return func(text string, prefix bool) ([]int64, bool, error) {
		text = strings.ToLower(strings.TrimSpace(text))
		if text == "" {
			return nil, true, nil
		}
		rows, err := db.Query(`SELECT d.id FROM documents d WHERE instr(lower(d.title), ?) > 0 OR instr(lower(d.content), ?) > 0
			OR EXISTS (SELECT 1 FROM notes n WHERE n.document_id = d.id AND instr(lower(n.body), ?) > 0)`, text, text, text)
		if err != nil {
			return nil, false, err
		}
		defer rows.Close()
		var ids []int64
		for rows.Next() {
			var id int64
			rows.Scan(&id)
			ids = append(ids, id)
		}
		return ids, false, rows.Err()
	}
}

func run(t *testing.T, db *sql.DB, cat *Catalog, js string) ([]int64, error) {
	t.Helper()
	c, err := ParseString(js)
	if err != nil {
		return nil, err
	}
	comp, err := Compile(cat, tenant.New(1, tenant.RoleViewer), c)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(context.Background(), "SELECT d.id FROM documents d WHERE "+comp.Where+" ORDER BY d.id", comp.Args...)
	if err != nil {
		t.Fatalf("%s\n%s: %v", js, comp.Where, err)
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func TestCompileMatches(t *testing.T) {
	st, cat := fixture(t)
	amount := "cf:50"
	cases := []struct{ filter, want string }{
		{``, "1 2 3 4 5"},
		{`{}`, "1 2 3 4 5"},
		{`{"all": []}`, "1 2 3 4 5"},
		{`{"field":"type","op":"in","value":[10]}`, "1 2"},
		{`{"field":"type","op":"not_in","value":[10]}`, "3 4 5"},
		{`{"field":"type","op":"is_null"}`, "4"},
		{`{"field":"correspondent","op":"not_null"}`, "1 2 4 5"},
		{`{"field":"tag","op":"has_any","value":[31]}`, "3"},
		{`{"field":"tag","op":"has_any","value":[31],"descendants":true}`, "1 3 4"},
		{`{"field":"tag","op":"has_all","value":[32,33]}`, "4"},
		{`{"field":"tag","op":"has_all","value":[31,33],"descendants":true}`, "4"},
		{`{"field":"tag","op":"has_none","value":[31],"descendants":true}`, "2 5"},
		{`{"field":"created","op":"between","value":["2024-01-01","2024-12-31"]}`, "1 2 4"},
		{`{"field":"created","op":"lt","value":"2024-03-05"}`, "3 5"},
		{`{"field":"created","op":"lte","value":"2024-03-05"}`, "1 3 5"},
		{`{"field":"added","op":"eq","value":"2024-04-04"}`, "2"},
		{`{"field":"added","op":"gt","value":"2024-04-04"}`, "4"},
		{`{"field":"added","op":"between","value":["2023-01-01","2024-03-06"]}`, "1 3"},
		{`{"field":"asn","op":"eq","value":2}`, "2"},
		{`{"field":"asn","op":"is_null"}`, "3 4 5"},
		{`{"field":"mime","op":"in","value":["application/pdf"]}`, "1 2 3 4 5"},
		{`{"field":"inbox","op":"eq","value":true}`, "4"},
		{`{"field":"` + amount + `","op":"gt","value":"200"}`, "1 4"},
		{`{"field":"` + amount + `","op":"between","value":["120.00","236.40"]}`, "2 4"},
		{`{"field":"` + amount + `","op":"is_null"}`, "3 5"},
		{`{"field":"cf:51","op":"eq","value":"2024-12-31"}`, "1"},
		{`{"field":"cf:40","op":"contains","value":"o'b"}`, "5"},
		{`{"field":"cf:41","op":"in","value":["a"]}`, "5"},
		{`{"text":"电费"}`, "1 4"},
		{`{"text":"押金"}`, "3"},
		{`{"text":"100%"}`, "5"},
		{`{"text":"' or 1=1 --"}`, "5"},
		{`{"text":"   "}`, "1 2 3 4 5"},
		{`{"all":[{"field":"type","op":"in","value":[10]},{"text":"燃气"}]}`, "2"},
		{`{"any":[{"field":"type","op":"is_null"},{"field":"correspondent","op":"is_null"}]}`, "3 4"},
		{`{"not":{"field":"type","op":"in","value":[10]}}`, "3 4 5"},
		{`{"all":[{"any":[{"not":{"field":"inbox","op":"eq","value":true}}]}]}`, "1 2 3 5"},
	}
	for _, c := range cases {
		got, err := run(t, st.DB, cat, c.filter)
		if err != nil {
			t.Errorf("%s: %v", c.filter, err)
			continue
		}
		if s := strings.Trim(fmt.Sprint(got), "[]"); s != c.want {
			t.Errorf("%s = %s, want %s", c.filter, s, c.want)
		}
	}
}

func TestCompileRejects(t *testing.T) {
	st, cat := fixture(t)
	cases := map[string]string{
		`{"field":"type","op":"in","value":[12]}`:                                "类型不存在", // another workspace's type
		`{"field":"tag","op":"has_any","value":[34]}`:                            "标签不存在",
		`{"field":"cf:42","op":"contains","value":"x"}`:                          "自定义字段不存在",
		`{"field":"cf:x","op":"eq","value":"x"}`:                                 "自定义字段不存在",
		`{"field":"folder","op":"is_null"}`:                                      "不支持的筛选字段",
		`{"field":"type","op":"gt","value":[10]}`:                                "不支持运算",
		`{"field":"type","op":"in","value":[]}`:                                  "筛选值不正确",
		`{"field":"created","op":"eq","value":"2024-13-01"}`:                     "筛选值不正确",
		`{"field":"created","op":"between","value":["2024-02-01","2024-01-01"]}`: "筛选值不正确",
		`{"field":"cf:50","op":"gt","value":"1.234"}`:                            "筛选值不正确",
		`{"field":"cf:41","op":"in","value":["zzz"]}`:                            "筛选值不正确",
		`{"field":"cf:41","op":"gt","value":"a"}`:                                "不支持运算",
		`{"field":"type"}`:                                                       "缺少运算",
		`{"field":"type","op":"is_null","prefix":true}`:                          "prefix 只能用于文字条件",
		`{"op":"in"}`: "缺少字段",
		`{"field":"type","op":"is_null","text":"x"}`: "之一",
		`{"bogus":1}`: "格式不正确",
		`{"all":[{"all":[{"all":[{"all":[]}]}]}]}`: "嵌套 3 层",
		`{"not":{}}`: "not 里缺少条件",
		`[1]`:        "格式不正确",
		`{} {}`:      "格式不正确",
	}
	var many []string
	for i := 0; i < 51; i++ {
		many = append(many, `{"text":"x"}`)
	}
	cases[`{"all":[`+strings.Join(many, ",")+`]}`] = "最多 50 个"
	for js, want := range cases {
		_, err := run(t, st.DB, cat, js)
		var ae *apperr.Error
		if !errors.As(err, &ae) || ae.Kind != apperr.KindInvalid || !strings.Contains(ae.Message, want) {
			t.Errorf("%s: err = %v, want KindInvalid containing %q", js, err, want)
		}
	}
}

func TestWithoutAndTexts(t *testing.T) {
	c, err := ParseString(`{"all":[
		{"any":[{"field":"type","op":"in","value":[10]},{"field":"type","op":"is_null"}]},
		{"field":"type","op":"not_null"},
		{"field":"tag","op":"has_any","value":[31]},
		{"any":[{"field":"type","op":"in","value":[10]},{"field":"tag","op":"has_any","value":[1]}]},
		{"text":"电费"},
		{"not":{"text":"燃气"}}]}`)
	if err != nil {
		t.Fatal(err)
	}
	w := c.Without("type")
	if len(w.All) != 4 || w.All[0].Field != "tag" {
		t.Errorf("Without(type) kept %d conditions: %+v", len(w.All), w.All)
	}
	if got := c.Texts(); len(got) != 1 || *got[0].Text != "电费" {
		t.Errorf("Texts = %v (text under not must not count)", got)
	}
	single, _ := ParseString(`{"field":"type","op":"in","value":[10]}`)
	if !single.Without("type").IsZero() {
		t.Error("a lone condition on the field should be dropped")
	}
}

func TestMoney(t *testing.T) {
	for in, want := range map[string]int64{"326.40": 32640, "326.4": 32640, "326": 32600, "0.05": 5, "-12.5": -1250, ".5": 50} {
		if got, err := ParseMoney(in); err != nil || got != want {
			t.Errorf("ParseMoney(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"1.234", "abc", "1.2.3", "", "1.-5"} {
		if _, err := ParseMoney(in); err == nil {
			t.Errorf("ParseMoney(%q) should fail", in)
		}
	}
	for n, want := range map[int64]string{32640: "326.40", 5: "0.05", -1250: "-12.50", 0: "0.00"} {
		if got := FormatMoney(n); got != want {
			t.Errorf("FormatMoney(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestTextWithoutSearch(t *testing.T) {
	st, cat := fixture(t)
	cat.Text = nil
	if _, err := run(t, st.DB, cat, `{"text":"电费"}`); err != ErrSearchUnavailable {
		t.Errorf("err = %v, want ErrSearchUnavailable", err)
	}
}

func TestSubstringTextAndDrawer(t *testing.T) {
	st, _ := fixture(t)
	ctx := context.Background()
	if _, err := st.DB.Exec(`INSERT INTO drawers (id, workspace_id, name, created_at, updated_at) VALUES (60, 1, '房子', 1, 1), (61, 2, '别人的', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	st.DB.Exec(`UPDATE documents SET drawer_id = 60 WHERE id IN (1, 2)`)
	cat, err := LoadCatalog(ctx, st.Queries, tenant.New(1, tenant.RoleOwner))
	if err != nil {
		t.Fatal(err)
	}
	cat.Substring = true // no index needed
	for _, c := range []struct{ filter, want string }{
		{`{"text":"电费"}`, "1 4"},
		{`{"text":"国家电网 本期"}`, "1"},
		{`{"text":"押金"}`, ""}, // notes are not part of substring matching
		{`{"any":[{"text":"燃气"},{"text":"租赁"}]}`, "2 3"},
		{`{"field":"drawer","op":"in","value":[60]}`, "1 2"},
		{`{"field":"drawer","op":"is_null"}`, "3 4 5"},
	} {
		ids, err := run(t, st.DB, cat, c.filter)
		if err != nil {
			t.Fatalf("%s: %v", c.filter, err)
		}
		if got := strings.Trim(fmt.Sprint(ids), "[]"); got != c.want {
			t.Errorf("%s = %s, want %s", c.filter, got, c.want)
		}
	}
	if _, err := run(t, st.DB, cat, `{"field":"drawer","op":"in","value":[61]}`); err == nil {
		t.Error("another workspace's drawer must be rejected")
	}
	c, _ := ParseString(`{"all":[{"field":"type","op":"in","value":[10]},{"not":{"field":"drawer","op":"is_null"}}]}`)
	if !c.Uses("drawer") || c.Uses("tag") {
		t.Error("Uses")
	}
}

// v1.5.2: periods relative to today.
func TestPeriods(t *testing.T) {
	st, cat := fixture(t)
	Now = func() time.Time { return time.Date(2024, 4, 10, 15, 0, 0, 0, time.Local) }
	t.Cleanup(func() { Now = time.Now })
	for p, want := range map[string]string{
		`{"field":"created","op":"period","value":"this_month"}`:   "2",
		`{"field":"created","op":"period","value":"last_month"}`:   "1",
		`{"field":"created","op":"period","value":"last_30_days"}`: "2",
		`{"field":"created","op":"period","value":"last_90_days"}`: "1 2",
		`{"field":"created","op":"period","value":"this_year"}`:    "1 2 4",
		`{"field":"created","op":"period","value":"last_year"}`:    "3",
		`{"field":"added","op":"period","value":"this_month"}`:     "2",
	} {
		got, err := run(t, st.DB, cat, p)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		if s := strings.Trim(fmt.Sprint(got), "[]"); s != want {
			t.Errorf("%s = %s, want %s", p, s, want)
		}
	}
	if _, err := run(t, st.DB, cat, `{"field":"created","op":"period","value":"next_week"}`); err == nil {
		t.Error("an unknown period should be refused")
	}
	// January: last month is December of the year before
	Now = func() time.Time { return time.Date(2025, 1, 15, 9, 0, 0, 0, time.Local) }
	from, to := periods["last_month"](time.Date(2025, 1, 15, 0, 0, 0, 0, time.Local))
	if from.Format("2006-01-02") != "2024-12-01" || to.Format("2006-01-02") != "2024-12-31" {
		t.Errorf("last month in January: %s – %s", from, to)
	}
}

// v1.6.1: no tags, original-number ranges, file format groups and a date
// field within the next or last N days.
func TestMoreConditions(t *testing.T) {
	st, cat := fixture(t)
	for id, mime := range map[int]string{2: "image/heic", 3: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", 4: "text/rtf", 5: "text/markdown"} {
		if _, err := st.DB.Exec(`UPDATE documents SET mime_type = ? WHERE id = ?`, mime, id); err != nil {
			t.Fatal(err)
		}
	}
	old := Now
	Now = func() time.Time { return time.Date(2024, 12, 1, 9, 0, 0, 0, time.Local) }
	defer func() { Now = old }()
	cases := []struct{ filter, want string }{
		{`{"field":"tag","op":"is_null"}`, "5"},
		{`{"field":"tag","op":"not_null"}`, "1 2 3 4"},
		{`{"field":"asn","op":"gte","value":2}`, "2"},
		{`{"field":"asn","op":"lt","value":2}`, "1"},
		{`{"field":"asn","op":"between","value":[1,2]}`, "1 2"},
		{`{"field":"mime","op":"kind","value":["pdf"]}`, "1"},
		{`{"field":"mime","op":"kind","value":["image","office"]}`, "2 3 4"}, // rtf counts as Office
		{`{"field":"mime","op":"kind","value":["text"]}`, "5"},
		{`{"field":"cf:51","op":"within_days","value":30}`, "1"}, // due 2024-12-31, today 12-01
		{`{"field":"cf:51","op":"within_days","value":20}`, ""},
		{`{"field":"cf:51","op":"within_days","value":-30}`, ""},
	}
	for _, c := range cases {
		got, err := run(t, st.DB, cat, c.filter)
		if err != nil {
			t.Errorf("%s: %v", c.filter, err)
			continue
		}
		if s := strings.Trim(fmt.Sprint(got), "[]"); s != c.want {
			t.Errorf("%s = %s, want %s", c.filter, s, c.want)
		}
	}
	for _, bad := range []string{`{"field":"mime","op":"kind","value":["zip"]}`, `{"field":"cf:51","op":"within_days","value":0}`, `{"field":"cf:40","op":"within_days","value":3}`, `{"field":"asn","op":"between","value":[1]}`} {
		if _, err := run(t, st.DB, cat, bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

// v1.6.3: family members and where originals are kept.
func TestPersonAndLocation(t *testing.T) {
	st, _ := fixture(t)
	for _, q := range []string{
		`INSERT INTO persons (id, workspace_id, name, sort_key, created_at, updated_at) VALUES (60, 1, '爸爸', 'baba', 1, 1), (61, 1, '小明', 'xiaoming', 1, 1), (62, 2, '别人', 'bieren', 1, 1)`,
		`INSERT INTO locations (id, workspace_id, name, sort_key, created_at, updated_at) VALUES (70, 1, '文件盒 A', 'wenjianhe', 1, 1), (71, 2, '别处', 'biechu', 1, 1)`,
		`UPDATE documents SET person_id = 60 WHERE id IN (1, 2)`,
		`UPDATE documents SET person_id = 61 WHERE id = 3`,
		`UPDATE documents SET location_id = 70 WHERE id IN (1, 3)`,
	} {
		if _, err := st.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	cat, err := LoadCatalog(context.Background(), st.Queries, tenant.New(1, tenant.RoleOwner))
	if err != nil {
		t.Fatal(err)
	}
	for js, want := range map[string]string{
		`{"field":"person","op":"in","value":[60]}`:                                               "1 2",
		`{"not":{"field":"person","op":"in","value":[60]}}`:                                       "3 4 5",
		`{"field":"person","op":"is_null"}`:                                                       "4 5",
		`{"field":"location","op":"in","value":[70]}`:                                             "1 3",
		`{"field":"location","op":"not_null"}`:                                                    "1 3",
		`{"all":[{"field":"person","op":"in","value":[60]},{"field":"location","op":"is_null"}]}`: "2",
	} {
		ids, err := run(t, st.DB, cat, js)
		if err != nil {
			t.Errorf("%s: %v", js, err)
			continue
		}
		if got := strings.Trim(fmt.Sprint(ids), "[]"); got != want {
			t.Errorf("%s = %s, want %s", js, got, want)
		}
	}
	for js, want := range map[string]string{
		`{"field":"person","op":"in","value":[62]}`:   "家人不存在",
		`{"field":"location","op":"in","value":[71]}`: "存放位置不存在",
	} {
		_, err := run(t, st.DB, cat, js)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", js, err, want)
		}
	}
}

// v1.6.5: documents past their type's retention, unless something holds
// on to them.
func TestRetentionExpired(t *testing.T) {
	st, _ := fixture(t)
	defer func(old func() time.Time) { Now = old }(Now)
	Now = func() time.Time { return time.Date(2026, 6, 1, 12, 0, 0, 0, time.Local) }
	exec := func(q string) {
		t.Helper()
		if _, err := st.DB.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	expired := `{"field":"retention","op":"expired","value":true}`
	check := func(want string) {
		t.Helper()
		cat, err := LoadCatalog(context.Background(), st.Queries, tenant.New(1, tenant.RoleOwner))
		if err != nil {
			t.Fatal(err)
		}
		ids, err := run(t, st.DB, cat, expired)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Trim(fmt.Sprint(ids), "[]"); got != want {
			t.Errorf("expired = %q, want %q", got, want)
		}
	}
	check("") // no type has a retention
	// 账单 (10) two years: the bills of 2024-03 and 2024-04 are past it;
	// 合同 (11) ten years: nothing
	exec(`INSERT INTO type_retention (document_type_id, workspace_id, months) VALUES (10, 1, 24), (11, 1, 120)`)
	check("1 2")
	exec(`UPDATE documents SET is_milestone = 1 WHERE id = 2`)
	check("1")
	exec(`INSERT INTO custom_fields (id, workspace_id, name, sort_key, data_type, extra, created_at, updated_at) VALUES (43, 1, '相关', 'xiangguan', 'document_link', '{}', 1, 1)`)
	exec(`INSERT INTO custom_field_values (document_id, field_id, value_doclink) VALUES (3, 43, 1)`)
	check("")
	exec(`DELETE FROM custom_field_values WHERE field_id = 43`)
	exec(`UPDATE documents SET is_milestone = 0`)
	exec(`INSERT INTO drawers (id, workspace_id, name, created_at, updated_at) VALUES (90, 1, '水电', 1, 1)`)
	exec(`INSERT INTO drawer_facts (drawer_id, kind, label, config, created_at) VALUES (90, 'pinned', '固定', '{"documentIds":[2]}', 1)`)
	check("1")

	cat, _ := LoadCatalog(context.Background(), st.Queries, tenant.New(1, tenant.RoleOwner))
	for _, bad := range []string{`{"field":"retention","op":"eq","value":true}`, `{"field":"retention","op":"expired","value":false}`} {
		if _, err := run(t, st.DB, cat, bad); err == nil {
			t.Errorf("%s should fail", bad)
		}
	}
}
