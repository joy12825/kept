package drawer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"kept/server/internal/files"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kept/server/internal/document"
	"kept/server/internal/filter"
	"kept/server/internal/store"
	"kept/server/internal/tenant"
)

// env: workspace 1 with types 账单 (10), 合同 (11), 证件 (12), correspondents
// 国家电网 (20), 链家 (21), tag 水电 (30), built-in fields amount (50) and
// due_date (51). Today is 2024-10-08.
type env struct {
	t     *testing.T
	db    *sql.DB
	s     *Service
	docs  *document.Service
	scope tenant.Scope
	ctx   context.Context
	next  int64
}

var today = time.Date(2024, 10, 8, 9, 0, 0, 0, time.Local)

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e := &env{t: t, db: st.DB, scope: tenant.New(1, tenant.RoleOwner), ctx: context.Background(), next: 100}
	e.s = NewService(st.DB, Options{Now: func() time.Time { return today }})
	e.docs = document.NewService(st.DB, document.Options{Placer: e.s, Now: func() time.Time { return today }})
	for _, q := range []string{
		`INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'a', 'family', 1), (2, 'b', 'family', 1)`,
		`INSERT INTO document_types (id, workspace_id, name, sort_key, created_at, updated_at) VALUES (10, 1, '账单', 'z', 1, 1), (11, 1, '合同', 'h', 1, 1), (12, 1, '证件', 'zj', 1, 1)`,
		`INSERT INTO correspondents (id, workspace_id, name, sort_key, created_at, updated_at) VALUES (20, 1, '国家电网', 'g', 1, 1), (21, 1, '链家', 'l', 1, 1)`,
		`INSERT INTO tags (id, workspace_id, name, sort_key, color, created_at, updated_at) VALUES (30, 1, '水电', 's', 'blue', 1, 1)`,
		`INSERT INTO custom_fields (id, workspace_id, key, name, sort_key, data_type, extra, created_at, updated_at) VALUES
			(50, 1, 'amount', '金额', 'jine', 'monetary', '{"currency":"CNY"}', 1, 1), (51, 1, 'due_date', '到期日', 'daoqiri', 'date', '{}', 1, 1)`,
	} {
		e.exec(q)
	}
	return e
}

func (e *env) exec(q string, args ...any) {
	e.t.Helper()
	if _, err := e.db.Exec(q, args...); err != nil {
		e.t.Fatalf("%s: %v", q, err)
	}
}

// doc adds a filed document and files it by the rules.
func (e *env) doc(title, content, date string, typ, corr int64, tags ...int64) int64 {
	e.t.Helper()
	e.next++
	id := e.next
	e.exec(`INSERT INTO documents (id, workspace_id, title, content, created_date, added_at, modified_at, document_type_id, correspondent_id,
		mime_type, original_filename, original_ext, checksum, inbox) VALUES (?, 1, ?, ?, ?, 1, 1, ?, ?, 'text/plain', 'a.txt', 'txt', ?, 0)`,
		id, title, content, date, nullable(typ), nullable(corr), fmt.Sprint(id))
	for _, t := range tags {
		e.exec(`INSERT INTO document_tags (document_id, tag_id) VALUES (?, ?)`, id, t)
	}
	if _, err := e.s.Place(e.ctx, e.db, 1, id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func nullable(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func (e *env) drawerOf(id int64) (int64, bool) {
	var d sql.NullInt64
	var manual int64
	if err := e.db.QueryRow(`SELECT drawer_id, drawer_manual FROM documents WHERE id = ?`, id).Scan(&d, &manual); err != nil {
		e.t.Fatal(err)
	}
	return d.Int64, manual == 1
}

func cond(t *testing.T, js string) *filter.Cond {
	t.Helper()
	c, err := filter.ParseString(js)
	if err != nil {
		t.Fatal(err)
	}
	return &c
}

func TestFilingFollowsOrderAndHandPlacement(t *testing.T) {
	e := newEnv(t)
	house, _, err := e.s.Create(e.ctx, e.scope, Input{Name: "房子", Icon: "home", Rule: cond(t, `{"any":[{"field":"correspondent","op":"in","value":[20,21]},{"text":"物业"}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	bills, _, err := e.s.Create(e.ctx, e.scope, Input{Name: "账单", Rule: cond(t, `{"field":"type","op":"in","value":[10]}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.s.Create(e.ctx, e.scope, Input{Name: "房子"}); err == nil {
		t.Error("a second drawer with the same name must be refused")
	}
	if _, _, err := e.s.Create(e.ctx, e.scope, Input{Name: "x", Rule: cond(t, `{"field":"drawer","op":"is_null"}`)}); err == nil {
		t.Error("a rule on the drawer itself must be refused")
	}

	elec := e.doc("电费", "国家电网 本期电费", "2024-09-05", 10, 20)
	gas := e.doc("燃气", "燃气费", "2024-09-03", 10, 0)
	fee := e.doc("物业费", "小区物业管理费", "2024-09-01", 0, 0)
	note := e.doc("笔记", "随手记", "2024-09-01", 0, 0)
	for id, want := range map[int64]int64{elec: house.ID, gas: bills.ID, fee: house.ID, note: 0} {
		if got, _ := e.drawerOf(id); got != want {
			t.Errorf("document %d in drawer %d, want %d (the first matching drawer)", id, got, want)
		}
	}
	var fs string
	e.db.QueryRow(`SELECT field_sources FROM documents WHERE id = ?`, elec).Scan(&fs)
	if !strings.Contains(fs, `"drawer":{"source":"rule","basis":"收纳规则“房子”"}`) {
		t.Errorf("field_sources = %s", fs)
	}

	// Reordering: bills first. Preview first, then the job does it.
	p, err := e.s.Preview(e.ctx, e.scope, PreviewInput{Order: []int64{bills.ID, house.ID}})
	if err != nil || p.Moves != 1 || len(p.Samples) != 1 || p.Samples[0].DocumentID != elec {
		t.Fatalf("order preview = %+v, %v", p, err)
	}
	if _, err := e.s.Reorder(e.ctx, e.scope, []int64{bills.ID, house.ID}); err != nil {
		t.Fatal(err)
	}
	if n, err := e.s.Reassign(e.ctx, e.scope); err != nil || n != 1 {
		t.Fatalf("reassign = %d, %v", n, err)
	}
	if got, _ := e.drawerOf(elec); got != bills.ID {
		t.Errorf("after reordering the bill is in %d", got)
	}

	// By hand: rules no longer move it; handing it back refiles it.
	billsID := bills.ID
	if _, err := e.docs.Update(e.ctx, e.scope, elec, document.Patch{DrawerID: &house.ID}); err != nil {
		t.Fatal(err)
	}
	if got, manual := e.drawerOf(elec); got != house.ID || !manual {
		t.Errorf("by hand: drawer %d manual %v", got, manual)
	}
	if n, _ := e.s.Reassign(e.ctx, e.scope); n != 0 {
		t.Errorf("reassign moved %d documents, including one placed by hand", n)
	}
	if rd, _ := e.s.RuleDrawer(e.ctx, e.scope, elec); rd != billsID {
		t.Errorf("rule drawer = %d, want %d", rd, billsID)
	}
	if _, err := e.docs.Update(e.ctx, e.scope, elec, document.Patch{DrawerAuto: true}); err != nil {
		t.Fatal(err)
	}
	if got, manual := e.drawerOf(elec); got != bills.ID || manual {
		t.Errorf("handed back: drawer %d manual %v", got, manual)
	}

	// Changing metadata refiles a document.
	none := int64(0)
	if _, err := e.docs.Update(e.ctx, e.scope, gas, document.Patch{TypeID: &none, CorrespondentID: ptr(21)}); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.drawerOf(gas); got != house.ID {
		t.Errorf("after the edit the gas bill is in %d, want 房子", got)
	}

	// A rule preview counts the drawer's changes.
	p, err = e.s.Preview(e.ctx, e.scope, PreviewInput{DrawerID: house.ID, SetRule: true, Rule: cond(t, `{"text":"费"}`)})
	if err != nil {
		t.Fatal(err)
	}
	// 房子 would hold 燃气 (费), 物业费; 电费 stays in 账单 (first); nothing new from none.
	if p.Total != 2 || p.In != 0 || p.Out != 0 || p.New != 0 || p.Unfiled != 0 {
		t.Errorf("rule preview = %+v", p)
	}

	// Deleting a drawer frees its documents, also those placed by hand.
	e.docs.Update(e.ctx, e.scope, note, document.Patch{DrawerID: &house.ID})
	if _, err := e.s.Delete(e.ctx, e.scope, house.ID); err != nil {
		t.Fatal(err)
	}
	if got, manual := e.drawerOf(note); got != 0 || manual {
		t.Errorf("after deleting its drawer: %d manual %v", got, manual)
	}
	// Another workspace cannot see or change the drawer.
	other := tenant.New(2, tenant.RoleOwner)
	if _, err := e.s.Get(e.ctx, other, bills.ID); err != ErrNotFound {
		t.Errorf("other workspace get = %v", err)
	}
	if _, _, err := e.s.Update(e.ctx, other, bills.ID, Patch{Name: ptrS("x")}); err != ErrNotFound {
		t.Errorf("other workspace update = %v", err)
	}
}

func ptr(v int64) *int64    { return &v }
func ptrS(s string) *string { return &s }

func TestTemplatesCreateDrawersRulesAndFacts(t *testing.T) {
	e := newEnv(t)
	ds, _, err := e.s.FromTemplates(e.ctx, e.scope, []string{"house", "ids", "car"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 3 || ds[0].Name != "房子" || ds[1].Rule == nil {
		t.Fatalf("drawers = %+v", ds)
	}
	// 证件 uses the existing type 证件 besides its keywords.
	if js, _ := json.Marshal(ds[1].Rule); !strings.Contains(string(js), `"field":"type","op":"in","value":[12]`) {
		t.Errorf("证件 rule = %s", js)
	}
	// Purpose tags were created; existing 水电 reused.
	var n int
	e.db.QueryRow(`SELECT count(*) FROM tags WHERE workspace_id = 1 AND name IN ('水电', '保修', '保险')`).Scan(&n)
	if n != 3 {
		t.Errorf("purpose tags = %d", n)
	}
	facts, _ := e.s.Facts(e.ctx, e.scope, ds[0].ID)
	if len(facts) != 3 || facts[1].Kind != KindSum || facts[1].Config.TagIDs[0] != 30 {
		t.Errorf("房子 facts = %+v", facts)
	}
	if _, _, err := e.s.FromTemplates(e.ctx, e.scope, []string{"house"}); err != nil {
		t.Fatal(err)
	}
	all, _ := e.s.List(e.ctx, e.scope)
	if len(all) != 3 {
		t.Errorf("an existing drawer name is skipped, got %d drawers", len(all))
	}
	if _, _, err := e.s.FromTemplates(e.ctx, e.scope, []string{"nope"}); err == nil {
		t.Error("unknown template")
	}
	// Every template's rule compiles.
	for _, tpl := range Templates {
		for _, ft := range tpl.Facts {
			if ft.Kind == "" || ft.Label == "" {
				t.Errorf("template %s has an empty fact", tpl.Key)
			}
		}
	}
}

func TestFactsAndAlerts(t *testing.T) {
	e := newEnv(t)
	house, _, _ := e.s.Create(e.ctx, e.scope, Input{Name: "房子", Rule: cond(t, `{"any":[{"field":"correspondent","op":"in","value":[20,21]},{"field":"type","op":"in","value":[12]}]}`)})
	var ids []int64
	for m := 1; m <= 9; m++ {
		id := e.doc(fmt.Sprintf("%d月电费", m), "电费", fmt.Sprintf("2024-%02d-05", m), 10, 20, 30)
		e.exec(`INSERT INTO custom_field_values (document_id, field_id, value_monetary) VALUES (?, 50, ?)`, id, 10000+m*100)
		ids = append(ids, id)
	}
	lease := e.doc("租赁合同", "房屋租赁", "2023-09-01", 11, 21)
	e.exec(`INSERT INTO custom_field_values (document_id, field_id, value_date) VALUES (?, 51, '2024-10-29')`, lease)
	passport := e.doc("护照", "护照", "2019-10-29", 12, 0)
	e.exec(`INSERT INTO custom_field_values (document_id, field_id, value_date) VALUES (?, 51, '2025-06-01')`, passport)

	add := func(kind, label string, cfg FactConfig) Fact {
		t.Helper()
		f, err := e.s.AddFact(e.ctx, e.scope, house.ID, Fact{Kind: kind, Label: label, Config: cfg})
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		return f
	}
	due := add(KindDue, "租约到期", FactConfig{FieldID: 51})
	sum := add(KindSum, "今年水电费", FactConfig{FieldID: 50, TagIDs: []int64{30}})
	expect := add(KindExpect, "电费账单", FactConfig{Filter: cond(t, `{"field":"correspondent","op":"in","value":[20]}`), GraceDays: 7})
	latest := add(KindLatest, "最近电费", FactConfig{FieldID: 50, Filter: cond(t, `{"text":"电费"}`)})
	pin := add(KindPinned, "固定文档", FactConfig{DocumentIDs: []int64{lease, passport}})
	if _, err := e.s.AddFact(e.ctx, e.scope, house.ID, Fact{Kind: KindDue, Label: "x", Config: FactConfig{FieldID: 50}}); err == nil {
		t.Error("a due fact needs a date field")
	}

	vals, err := e.s.ComputeAll(e.ctx, e.scope, house.ID)
	if err != nil {
		t.Fatal(err)
	}
	by := map[int64]Value{}
	for _, v := range vals {
		by[v.Fact.ID] = v
	}
	if v := by[due.ID]; v.DueDate != "2024-10-29" || v.DaysLeft != 21 || !v.Warn || v.Document.ID != lease {
		t.Errorf("due = %+v", v)
	}
	// 9 bills: 100+…+109 yuan = 945 yuan → 94500 fen.
	if v := by[sum.ID]; v.Total != "945.00" || v.Count != 9 || len(v.Months) != 12 || v.Months[2].Total != "103.00" || !v.Money {
		t.Errorf("sum = %+v", v)
	}
	if v := by[expect.ID]; v.Present || v.PeriodLabel != "10 月" || !v.Warn {
		t.Errorf("expect = %+v (October checked after 7 days; no bill)", v)
	}
	if v := by[latest.ID]; v.Document == nil || v.Document.Title != "9月电费" || *v.FieldValue != "109.00" {
		t.Errorf("latest = %+v", v)
	}
	if v := by[pin.ID]; len(v.Documents) != 2 || v.Documents[0].Title != "租赁合同" {
		t.Errorf("pinned = %+v", v)
	}
	prim, _ := e.s.PrimaryValues(e.ctx, e.scope)
	if prim[house.ID].Fact.ID != due.ID {
		t.Errorf("primary fact = %+v", prim[house.ID].Fact)
	}

	// Alerts: the lease (21 days, within 30) and the missing October bill;
	// the passport (2025) is not due yet.
	if _, err := e.s.RefreshAlerts(e.ctx, e.scope); err != nil {
		t.Fatal(err)
	}
	alerts, _ := e.s.Alerts(e.ctx, e.scope)
	if len(alerts) != 2 || alerts[0].Kind != "missing" || alerts[0].Text != "电费账单：10 月的还没有存入" ||
		alerts[1].Text != "租赁合同：到期日 2024-10-29，还剩 21 天" {
		t.Fatalf("alerts = %+v", alerts)
	}
	// Dismissed stays dismissed on the next check; filing the October bill resolves the other.
	if err := e.s.SetAlertDismissed(e.ctx, e.scope, alerts[1].ID, true); err != nil {
		t.Fatal(err)
	}
	e.doc("10月电费", "电费", "2024-10-05", 10, 20, 30)
	e.s.RefreshAlerts(e.ctx, e.scope)
	if alerts, _ = e.s.Alerts(e.ctx, e.scope); len(alerts) != 0 {
		t.Errorf("after filing the bill and dismissing the lease: %+v", alerts)
	}
	if err := e.s.SetAlertDismissed(e.ctx, tenant.New(2, tenant.RoleOwner), 1, true); err != ErrAlertNotFound {
		t.Errorf("another workspace's alert: %v", err)
	}
	// A document still in the inbox does not count.
	e.exec(`UPDATE documents SET inbox = 1 WHERE title = '10月电费'`)
	e.s.RefreshAlerts(e.ctx, e.scope)
	if alerts, _ = e.s.Alerts(e.ctx, e.scope); len(alerts) != 1 || alerts[0].Kind != "missing" {
		t.Errorf("with the bill back in the inbox: %+v", alerts)
	}
	_ = ids
}

func TestExpectPeriod(t *testing.T) {
	d := func(s string) time.Time { x, _ := time.ParseInLocation("2006-01-02", s, time.Local); return x }
	for _, c := range []struct {
		today, period string
		grace         int
		key, label    string
	}{
		{"2024-10-08", "monthly", 7, "2024-10", "10 月"},
		{"2024-10-05", "monthly", 7, "2024-09", "9 月"},
		{"2024-01-03", "monthly", 7, "2023-12", "2023 年 12 月"},
		{"2024-11-20", "quarterly", 15, "2024-Q4", "2024 年第 4 季度"},
		{"2024-10-02", "quarterly", 15, "2024-Q3", "2024 年第 3 季度"},
		{"2024-03-01", "yearly", 90, "2023", "2023 年"},
	} {
		_, _, key, label := expectPeriod(d(c.today), c.period, c.grace)
		if key != c.key || label != c.label {
			t.Errorf("%s %s +%d = %s %q, want %s %q", c.today, c.period, c.grace, key, label, c.key, c.label)
		}
	}
}

func TestYearbook(t *testing.T) {
	e := newEnv(t)
	house, _, _ := e.s.Create(e.ctx, e.scope, Input{Name: "房子", Rule: cond(t, `{"field":"correspondent","op":"in","value":[20,21]}`)})
	e.s.AddFact(e.ctx, e.scope, house.ID, Fact{Kind: KindSum, Label: "水电费", Config: FactConfig{FieldID: 50, TagIDs: []int64{30}}})
	e.exec(`UPDATE tags SET show_in_yearbook = 1 WHERE id = 30`)
	for m := 1; m <= 3; m++ {
		id := e.doc(fmt.Sprintf("%d月电费", m), "电费", fmt.Sprintf("2024-%02d-05", m), 10, 20, 30)
		e.exec(`INSERT INTO custom_field_values (document_id, field_id, value_monetary) VALUES (?, 50, 10000)`, id)
	}
	e.doc("去年电费", "电费", "2023-12-05", 10, 20, 30)
	e.doc("租赁合同", "租赁", "2024-03-01", 11, 21)
	e.doc("护照", "护照", "2024-03-01", 12, 0)
	big := e.doc("买车", "车", "2024-07-01", 0, 0)
	e.exec(`UPDATE documents SET is_milestone = 1 WHERE id = ?`, big)

	y, err := e.s.Yearbook(e.ctx, e.scope, 2024)
	if err != nil {
		t.Fatal(err)
	}
	if y.Documents != 6 || y.AsOf != "2024-10-08" || y.Months[2] != 3 || y.BusiestMonth != 3 || y.NewCorrespondents != 1 {
		t.Errorf("numbers = %+v", y)
	}
	if y.TopType == nil || y.TopType.Name != "账单" || y.TopType.Count != 3 {
		t.Errorf("top type = %+v", y.TopType)
	}
	if len(y.Drawers) != 1 || y.Drawers[0].Count != 4 || y.Unfiled != 2 || y.Drawers[0].Count+y.Unfiled != y.Documents {
		t.Errorf("drawers = %+v unfiled %d", y.Drawers, y.Unfiled)
	}
	if s := y.Drawers[0].Sums; len(s) != 1 || s[0].Total != "300.00" {
		t.Errorf("sums = %+v", s)
	}
	if len(y.Tags) != 1 || y.Tags[0].Count != 3 || y.Tags[0].Amount != "300.00" {
		t.Errorf("tags = %+v", y.Tags)
	}
	var texts []string
	for _, ev := range y.Events {
		texts = append(texts, ev.Date+" "+ev.Text)
	}
	if got := strings.Join(texts, " | "); got != "03-01 签订 租赁合同（链家） | 03-01 存入 护照 | 07-01 买车 | 10-08 新建抽屉 房子" {
		t.Errorf("events = %s", got)
	}
	years, _ := e.s.Years(e.ctx, e.scope)
	if fmt.Sprint(years) != "[2024 2023]" {
		t.Errorf("years = %v", years)
	}
	if _, err := e.s.Yearbook(e.ctx, e.scope, 3000); err == nil {
		t.Error("a far future year")
	}
}

func TestYearbookComparesRulesAndStorage(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	fs := files.NewStore(dir)
	e.s.opt.Files = fs
	a := e.doc("去年电费", "电费", "2023-12-05", 10, 20, 30)
	b := e.doc("今年电费", "电费", "2024-01-05", 10, 20, 30)
	c := e.doc("租赁合同", "租赁", "2024-03-01", 11, 21)
	e.doc("五年前", "旧", "2020-02-01", 0, 0)
	write := func(p string, n int) {
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, n), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	write(fs.Path(files.Original(1, a, "txt")), 1000)
	write(fs.Path(files.Original(1, b, "txt")), 300)
	write(fs.Path(files.Archive(1, b)), 200)
	write(fs.ThumbnailPath(1, b), 10)
	write(fs.Path(files.Original(1, c, "txt")), 50)
	write(fs.Path(files.Version(1, c, 9, "pdf")), 400)
	// what ingest records with the files (v1.13.5: sizes come from the database)
	for _, q := range []string{
		fmt.Sprintf(`UPDATE documents SET has_thumbnail = 1 WHERE id = %d`, b),
		fmt.Sprintf(`INSERT INTO document_versions (workspace_id, document_id, number, original_filename, original_ext, mime_type, checksum, size, current, created_at)
			VALUES (1, %d, 1, 'old.pdf', 'pdf', 'application/pdf', 'v1', 400, 0, 1)`, c),
	} {
		if _, err := e.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}

	y, err := e.s.Yearbook(e.ctx, e.scope, 2024)
	if err != nil {
		t.Fatal(err)
	}
	if y.Previous == nil || y.Previous.Year != 2023 || y.Previous.Documents != 1 {
		t.Errorf("previous = %+v", y.Previous)
	}
	if len(y.History) != 5 || y.History[0] != (YearCount{2020, 1}) || y.History[3] != (YearCount{2023, 1}) || y.History[4] != (YearCount{2024, 2}) {
		t.Errorf("history = %+v", y.History)
	}
	st := y.Storage
	if st == nil || st.Originals != 350 || st.Searchable != 200 || st.Thumbnails != 10 || st.Versions != 400 || st.Total != 960 || st.Workspace != 1960 {
		t.Fatalf("storage = %+v", st)
	}
	if len(st.Largest) != 2 || st.Largest[0].ID != b || st.Largest[0].Bytes != 510 || st.Largest[1].Bytes != 450 {
		t.Errorf("largest = %+v", st.Largest)
	}

	// Default rules come from the types; custom ones replace them.
	rules, def, err := e.s.MilestoneRules(e.ctx, e.scope)
	if err != nil || !def || len(rules) != 2 || rules[0].Verb != "签订" {
		t.Fatalf("defaults: %+v %v %v", rules, def, err)
	}
	if err := e.s.SetMilestoneRules(e.ctx, e.scope, []MilestoneRule{{Verb: "太长的一个动词哈哈", Filter: filter.Cond{}}}, false); err == nil {
		t.Error("long verb accepted")
	}
	if err := e.s.SetMilestoneRules(e.ctx, e.scope, []MilestoneRule{{Verb: "缴纳", Filter: *cond(t, `{"field":"correspondent","op":"in","value":[20]}`)}}, false); err != nil {
		t.Fatal(err)
	}
	y, _ = e.s.Yearbook(e.ctx, e.scope, 2024)
	if len(y.Events) < 1 || y.Events[0].Text != "缴纳 今年电费（国家电网）" || y.Events[0].Kind != "rule" {
		t.Errorf("custom events = %+v", y.Events)
	}
	for _, ev := range y.Events {
		if strings.Contains(ev.Text, "租赁合同") {
			t.Errorf("the contract rule still applies: %+v", ev)
		}
	}
	// A year without events still has a list (JSON [] rather than null).
	if empty, err := e.s.Yearbook(e.ctx, e.scope, 2022); err != nil || empty.Events == nil {
		t.Errorf("empty year events = %#v, %v", empty.Events, err)
	}
	if err := e.s.SetMilestoneRules(e.ctx, e.scope, nil, true); err != nil {
		t.Fatal(err)
	}
	if _, def, _ := e.s.MilestoneRules(e.ctx, e.scope); !def {
		t.Error("reset did not restore the defaults")
	}
}
