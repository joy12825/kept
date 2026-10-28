package document

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/events"
	"kept/server/internal/files"
)

func ptr[T any](v T) *T { return &v }

func kindOf(err error) apperr.Kind {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Kind
	}
	return 0
}

// history returns the audit events recorded for a document, oldest first.
func (e *env) history(id int64) []string {
	e.t.Helper()
	rows, err := e.st.DB.QueryContext(e.ctx, `SELECT action, summary, changes FROM audit_events WHERE target_type = 'document' AND target_id = ? ORDER BY id`, fmt.Sprint(id))
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a, s, c string
		_ = rows.Scan(&a, &s, &c)
		out = append(out, a+" | "+s+" | "+c)
	}
	return out
}

func TestUpdateChangesFieldsAndRecordsHistory(t *testing.T) {
	e := newEnv(t)
	e.fixture()
	ctx := audit.WithActor(e.ctx, audit.Actor{UserID: 0, Name: "小王", Via: "session"})
	bus := events.NewBus()
	sub, cancel := bus.Subscribe(1)
	defer cancel()
	e.svc.opt.Bus = bus

	d, err := e.svc.Update(ctx, e.scope, 1, Patch{
		Title: ptr("  3月电费  "), CreatedDate: ptr("2024-03-06"), TypeID: ptr(int64(11)), CorrespondentID: ptr(int64(0)),
		TagIDs: &[]int64{33, 31, 31}, ASN: ptr(int64(123)), Inbox: ptr(true),
		CustomFields: []FieldInput{
			{FieldID: 50, Value: json.RawMessage(`"88.5"`)},
			{FieldID: 51, Value: json.RawMessage(`null`)},
			{FieldID: 52, Value: json.RawMessage(`"a"`)}, // unchanged
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "3月电费" || d.CreatedDate != "2024-03-06" || d.DocumentTypeID != 11 || d.CorrespondentID != 0 ||
		fmt.Sprint(d.TagIDs) != "[31 33]" || *d.ASN != 123 || !d.Inbox || *d.Amount != 8850 || d.DueDate != "" {
		t.Fatalf("updated = %+v", d.Summary)
	}
	h := e.history(1)
	if len(h) != 1 {
		t.Fatalf("history = %v", h)
	}
	for _, want := range []string{"document.update", "小王 修改了“3月电费”的", `"标题":["2024年3月电费账单","3月电费"]`, `"类型":["账单","合同"]`,
		`"往来方":["国家电网",""]`, `"标签":["发票、水电","水电、财务"]`, `"原件编号":["","#000123"]`, `"金额":["326.40","88.50"]`, `"到期日":["2024-12-31",""]`, `"待整理":[false,true]`} {
		if !strings.Contains(h[0], want) {
			t.Errorf("history %s lacks %s", h[0], want)
		}
	}
	if strings.Contains(h[0], "级别") {
		t.Error("an unchanged field should not be recorded")
	}
	got := map[string]bool{}
	for range 2 {
		select {
		case ev := <-sub:
			got[ev.Type] = true
		case <-time.After(time.Second):
		}
	}
	if !got[events.DocumentUpdated] || !got[events.InboxChanged] {
		t.Errorf("events = %v", got)
	}

	// The same patch again changes nothing and records nothing.
	if _, err := e.svc.Update(ctx, e.scope, 1, Patch{Title: ptr("3月电费"), TagIDs: &[]int64{31, 33}}); err != nil {
		t.Fatal(err)
	}
	if len(e.history(1)) != 1 {
		t.Error("a no-op update should not be recorded")
	}

	// Completing alone reads as such.
	if _, err := e.svc.Update(ctx, e.scope, 1, Patch{Inbox: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	if h := e.history(1); !strings.Contains(h[1], "小王 整理了“3月电费”") {
		t.Errorf("complete summary = %s", h[1])
	}
}

func TestUpdateValidates(t *testing.T) {
	e := newEnv(t)
	e.fixture()
	e.exec(`INSERT INTO document_types (id, workspace_id, name, sort_key, created_at, updated_at) VALUES (90, 2, '别人的', 'x', 1, 1)`)
	e.exec(`UPDATE documents SET asn = 7 WHERE id = 6`) // in the trash
	d1, _ := e.svc.GetDetail(e.ctx, e.scope, 1)
	cases := []struct {
		name  string
		patch Patch
		kind  apperr.Kind
		msg   string
	}{
		{"empty title", Patch{Title: ptr("  ")}, apperr.KindInvalid, "标题"},
		{"bad date", Patch{CreatedDate: ptr("2024/3/5")}, apperr.KindInvalid, "日期"},
		{"other workspace's type", Patch{TypeID: ptr(int64(90))}, apperr.KindInvalid, "类型不存在"},
		{"missing tag", Patch{TagIDs: &[]int64{999}}, apperr.KindInvalid, "标签不存在"},
		{"negative asn", Patch{ASN: ptr(int64(-1))}, apperr.KindInvalid, "原件编号"},
		{"asn in the trash", Patch{ASN: ptr(int64(7))}, apperr.KindConflict, "#000007 已被《已删除》（在回收站中）使用"},
		{"money", Patch{CustomFields: []FieldInput{{FieldID: 50, Value: json.RawMessage(`"1.234"`)}}}, apperr.KindInvalid, "金额"},
		{"select option", Patch{CustomFields: []FieldInput{{FieldID: 52, Value: json.RawMessage(`"zzz"`)}}}, apperr.KindInvalid, "级别"},
		{"missing field", Patch{CustomFields: []FieldInput{{FieldID: 999, Value: json.RawMessage(`1`)}}}, apperr.KindInvalid, "自定义字段不存在"},
		{"stale", Patch{Title: ptr("x"), ExpectedModifiedAt: ptr(d1.ModifiedAt.Add(-time.Second))}, apperr.KindConflict, "刚被其他人修改过"},
	}
	for _, c := range cases {
		_, err := e.svc.Update(e.ctx, e.scope, 1, c.patch)
		if kindOf(err) != c.kind || !strings.Contains(err.Error(), c.msg) {
			t.Errorf("%s: err = %v", c.name, err)
		}
	}
	if _, err := e.svc.Update(e.ctx, e.scope, 7, Patch{Title: ptr("x")}); err != ErrNotFound {
		t.Errorf("another workspace's document: %v", err)
	}
	if _, err := e.svc.Update(e.ctx, e.scope, 1, Patch{Title: ptr("x"), ExpectedModifiedAt: ptr(d1.ModifiedAt)}); err != nil {
		t.Errorf("a current modifiedAt should pass: %v", err)
	}
	if len(e.history(1)) != 1 {
		t.Error("failed updates must not be recorded")
	}
}

func TestDocumentLinkField(t *testing.T) {
	e := newEnv(t)
	e.fixture()
	e.exec(`INSERT INTO custom_fields (id, workspace_id, name, sort_key, data_type, extra, created_at, updated_at) VALUES (53, 1, '关联', 'guanlian', 'document_link', '{}', 1, 1)`)
	link := func(target string) error {
		_, err := e.svc.Update(e.ctx, e.scope, 1, Patch{CustomFields: []FieldInput{{FieldID: 53, Value: json.RawMessage(target)}}})
		return err
	}
	if err := link("1"); err == nil || !strings.Contains(err.Error(), "自己") {
		t.Errorf("self link: %v", err)
	}
	for _, target := range []string{"6", "7", "999"} {
		if err := link(target); err == nil {
			t.Errorf("link to %s should fail", target)
		}
	}
	if err := link("3"); err != nil {
		t.Fatal(err)
	}
	if h := e.history(1); !strings.Contains(h[len(h)-1], `"关联":["","《房屋租赁合同》"]`) {
		t.Errorf("history = %v", h)
	}
}

func TestBulkEditsManyDocumentsAtOnce(t *testing.T) {
	e := newEnv(t)
	for i := int64(100); i < 200; i++ {
		e.doc(i, 1, fmt.Sprintf("账单 %d", i), "2024-01-01", i, 10, nil, 33)
	}
	e.exec(`UPDATE documents SET inbox = 1`)
	ids := make([]int64, 0, 100)
	for i := int64(100); i < 200; i++ {
		ids = append(ids, i)
	}
	n, err := e.svc.Bulk(e.ctx, e.scope, Selection{IDs: append(ids, 7, 999)}, BulkEdit{AddTagIDs: []int64{31}, RemoveTagIDs: []int64{33}, CorrespondentID: ptr(int64(20))})
	if err != nil || n != 100 {
		t.Fatalf("bulk = %d, %v", n, err)
	}
	p, _ := e.svc.List(e.ctx, e.scope, Query{Filter: mustFilter(t, `{"all":[{"field":"tag","op":"has_all","value":[31]},{"field":"correspondent","op":"in","value":[20]}]}`), Limit: 1})
	if p.Total != 100 || fmt.Sprint(p.Items[0].TagIDs) != "[31]" {
		t.Errorf("after bulk: total %d, tags %v", p.Total, p.Items[0].TagIDs)
	}
	if h := e.history(150); len(h) != 1 || !strings.Contains(h[0], "批量修改了“账单 150”") {
		t.Errorf("history = %v", h)
	}

	f := mustFilter(t, `{"field":"inbox","op":"eq","value":true}`)
	n, err = e.svc.Bulk(e.ctx, e.scope, Selection{Filter: &f}, BulkEdit{Inbox: ptr(false)})
	if err != nil || n != 100 {
		t.Fatalf("complete = %d, %v", n, err)
	}
	if h := e.history(150); !strings.Contains(h[1], "整理了“账单 150”") {
		t.Errorf("complete history = %v", h)
	}
	if _, err := e.svc.Bulk(e.ctx, e.scope, Selection{}, BulkEdit{}); kindOf(err) != apperr.KindInvalid {
		t.Errorf("empty selection: %v", err)
	}
	if _, err := e.svc.Bulk(e.ctx, e.scope, Selection{IDs: ids}, BulkEdit{AddTagIDs: []int64{999}}); kindOf(err) != apperr.KindInvalid {
		t.Errorf("bad tag: %v", err)
	}
}

func TestTrash(t *testing.T) {
	e := newEnv(t)
	e.fixture()
	dir := t.TempDir()
	fs := files.NewStore(dir)
	e.svc.opt.Files = fs
	now := time.Date(2024, 10, 8, 12, 0, 0, 0, time.Local)
	e.svc.opt.Now = func() time.Time { return now }
	for _, p := range []string{fs.Path(files.Original(1, 3, "pdf")), fs.ThumbnailPath(1, 3)} {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("x"), 0o644)
	}
	e.exec(`INSERT INTO custom_fields (id, workspace_id, name, sort_key, data_type, extra, created_at, updated_at) VALUES (53, 1, '关联', 'guanlian', 'document_link', '{}', 1, 1)`)
	e.exec(`INSERT INTO custom_field_values (document_id, field_id, value_doclink) VALUES (1, 53, 3)`)

	if n, err := e.svc.Delete(e.ctx, e.scope, Selection{IDs: []int64{3, 4, 7}}); err != nil || n != 2 {
		t.Fatalf("delete = %d, %v", n, err)
	}
	if _, err := e.svc.GetDetail(e.ctx, e.scope, 3); err != ErrNotFound {
		t.Error("a deleted document should be gone from the list and details")
	}
	now = now.Add(29*24*time.Hour + time.Hour)
	trash, err := e.svc.ListTrash(e.ctx, e.scope)
	if err != nil || len(trash) != 3 { // 3, 4 and the fixture's 6
		t.Fatalf("trash = %v, %v", trash, err)
	}
	if trash[0].ID != 4 || trash[0].DaysLeft != 1 {
		t.Errorf("first in trash = %d with %d days left", trash[0].ID, trash[0].DaysLeft)
	}

	if err := e.svc.Restore(e.ctx, e.scope, 4); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Restore(e.ctx, e.scope, 4); kindOf(err) != apperr.KindNotFound {
		t.Errorf("restoring a document not in the trash: %v", err)
	}
	if err := e.svc.Purge(e.ctx, e.scope, []int64{1}); kindOf(err) != apperr.KindNotFound {
		t.Errorf("purging a document outside the trash: %v", err)
	}

	if err := e.svc.Purge(e.ctx, e.scope, []int64{3}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fs.Path(files.Original(1, 3, "pdf"))); !os.IsNotExist(err) {
		t.Error("purging should remove the files")
	}
	d1, _ := e.svc.GetDetail(e.ctx, e.scope, 1)
	for _, f := range d1.Fields {
		if f.FieldID == 53 {
			t.Error("a link to a purged document should be removed")
		}
	}
	if h := e.history(3); len(h) != 2 || !strings.HasPrefix(h[1], "document.purge") {
		t.Errorf("history = %v", h)
	}

	// The fixture's document 6 was deleted at time 1: long expired.
	n, err := e.svc.PurgeExpired(e.ctx, now.Add(-TrashRetention))
	if err != nil || n != 1 {
		t.Errorf("purge expired = %d, %v", n, err)
	}
	if n, err := e.svc.EmptyTrash(e.ctx, e.scope); err != nil || n != 0 {
		t.Errorf("empty trash = %d, %v", n, err)
	}
}

func (e *env) rev(id int64) int64 {
	e.t.Helper()
	var r int64
	if err := e.st.DB.QueryRow(`SELECT index_rev FROM documents WHERE id = ?`, id).Scan(&r); err != nil {
		e.t.Fatal(err)
	}
	return r
}

func TestChangesRaiseTheIndexRevision(t *testing.T) {
	e := newEnv(t)
	e.fixture()
	steps := []struct {
		name string
		do   func() error
	}{
		{"title", func() error { _, err := e.svc.Update(e.ctx, e.scope, 1, Patch{Title: ptr("新标题")}); return err }},
		{"tags", func() error { _, err := e.svc.Update(e.ctx, e.scope, 1, Patch{TagIDs: &[]int64{31}}); return err }},
		{"field", func() error {
			_, err := e.svc.Update(e.ctx, e.scope, 1, Patch{CustomFields: []FieldInput{{FieldID: 50, Value: json.RawMessage(`"1.00"`)}}})
			return err
		}},
		{"delete", func() error { _, err := e.svc.Delete(e.ctx, e.scope, Selection{IDs: []int64{1}}); return err }},
		{"restore", func() error { return e.svc.Restore(e.ctx, e.scope, 1) }},
	}
	for _, s := range steps {
		before := e.rev(1)
		if err := s.do(); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		if e.rev(1) <= before {
			t.Errorf("%s did not raise index_rev", s.name)
		}
	}
	before := e.rev(1)
	if _, err := e.svc.Update(e.ctx, e.scope, 1, Patch{Title: ptr("新标题")}); err != nil || e.rev(1) != before {
		t.Errorf("a no-op update should leave index_rev alone: %v", err)
	}
}

func TestUpdateContentMarksItEditedAndKeepsSizesInHistory(t *testing.T) {
	e := newEnv(t)
	e.fixture()
	ctx := audit.WithActor(e.ctx, audit.Actor{UserID: 0, Name: "小王", Via: "session"})
	before, err := e.svc.GetDetail(ctx, e.scope, 1)
	if err != nil {
		t.Fatal(err)
	}
	if before.ContentEditedAt != nil {
		t.Fatal("a recognised text is not edited")
	}
	d, err := e.svc.Update(ctx, e.scope, 1, Patch{Content: ptr("国家电网 2024 年 3 月电费 326.40 元")})
	if err != nil {
		t.Fatal(err)
	}
	if d.Content != "国家电网 2024 年 3 月电费 326.40 元" || d.ContentEditedAt == nil {
		t.Fatalf("content = %q, edited = %v", d.Content, d.ContentEditedAt)
	}
	h := e.history(1)
	if len(h) != 1 || !strings.Contains(h[0], `"识别文字":[`) || strings.Contains(h[0], "326.40 元") {
		t.Fatalf("history = %v", h)
	}
	var rev, indexed int64
	_ = e.st.DB.QueryRowContext(e.ctx, `SELECT index_rev, indexed_rev FROM documents WHERE id = 1`).Scan(&rev, &indexed)
	if rev <= indexed {
		t.Error("an edited text should be reindexed")
	}
	if _, err := e.svc.Update(ctx, e.scope, 1, Patch{Content: ptr(strings.Repeat("字", maxContent+1))}); kindOf(err) != apperr.KindInvalid {
		t.Errorf("too long: %v", err)
	}
}

func TestTextSize(t *testing.T) {
	for in, want := range map[string]string{"": "（空）", "  ": "（空）", "电费": "约 2 字", strings.Repeat("a", 1234567): "约 1,234,567 字"} {
		if got := textSize(in); got != want {
			t.Errorf("textSize(%d) = %s, want %s", len(in), got, want)
		}
	}
}

// v1.4.4: picking another date keeps the others on offer, the old one too.
func TestChangingTheDateKeepsTheOtherDates(t *testing.T) {
	e := newEnv(t)
	e.fixture()
	fs := FieldSources{Date: &Source{Source: SourceText, Basis: "账单日 2024/03/05", Alternatives: []DateOption{
		{Date: "2024-02-06", Basis: "账单周期 2024/02/06"}, {Date: "2024-03-01", Basis: "文件名“IMG_20240301.jpg”"}}}}
	e.exec(`UPDATE documents SET created_date = '2024-03-05', field_sources = ? WHERE id = 1`, fs.String())
	d, err := e.svc.Update(e.ctx, e.scope, 1, Patch{CreatedDate: ptr("2024-02-06")})
	if err != nil {
		t.Fatal(err)
	}
	got := d.FieldSources.Date
	if got == nil || got.Source != SourceUser || len(got.Alternatives) != 2 || got.Alternatives[0] != (DateOption{"2024-03-05", "账单日 2024/03/05"}) ||
		got.Alternatives[1].Date != "2024-03-01" {
		t.Fatalf("date source = %+v", got)
	}
}
