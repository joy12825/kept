package search

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestAnalyzer(t *testing.T) {
	cases := map[string]struct{ index, query string }{
		"国家电网北京市电力公司":     {"国家电网 国家 电网 北京市 北京 电力公司 电力 公司", "国家电网 北京市 电力公司"},
		"增值税专用发票":         {"增值税 增值 专用发票 专用 发票", "增值税 专用发票"},
		"MacBook Pro 保修卡": {"macbook pro 保修卡 保修", "macbook pro 保修卡"},
		"ＡＢＣ２０２４年":        {"abc2024 年", "abc2024 年"}, // full-width folded
	}
	for in, want := range cases {
		if got := strings.Join(Terms(in, true), " "); got != want.index {
			t.Errorf("index %q = %q, want %q", in, got, want.index)
		}
		if got := strings.Join(Terms(in, false), " "); got != want.query {
			t.Errorf("query %q = %q, want %q", in, got, want.query)
		}
	}
}

func day(s string) time.Time { t, _ := time.ParseInLocation("2006-01-02", s, time.Local); return t }

// fixture: workspace 1 holds documents 1–5, workspace 2 holds 6.
func fixture(t *testing.T) *Index {
	t.Helper()
	ix, rebuilt, err := Open(t.TempDir())
	if err != nil || !rebuilt {
		t.Fatalf("Open = %v, rebuilt %v", err, rebuilt)
	}
	t.Cleanup(func() { ix.Close() })
	docs := []Doc{
		{ID: 1, WorkspaceID: 1, Title: "2024年3月电费账单", Content: "国家电网北京市电力公司\n本期电费 128.50 元\n户号 0012345678", Tags: []string{"财务 / 发票", "水电"},
			Correspondent: "国家电网", DocumentType: "账单", ASN: 123, Created: day("2024-03-05"), Added: day("2024-03-06")},
		{ID: 2, WorkspaceID: 1, Title: "增值税专用发票-办公用品", Content: "购买方 某某科技有限公司 增值税专用发票", Tags: []string{"财务 / 发票"},
			DocumentType: "发票", Created: day("2024-08-28"), Added: day("2024-08-29")},
		{ID: 3, WorkspaceID: 1, Title: "房屋租赁合同", Content: "出租方 链家 租赁期限两年", Notes: []string{"押金已退还"}, Correspondent: "链家",
			DocumentType: "合同", Created: day("2023-09-01"), Added: day("2023-09-02")},
		{ID: 4, WorkspaceID: 1, Title: "合同草稿", Content: "租赁合同 草稿 未签字", DocumentType: "合同", CustomFields: []string{"HT-2023-001"},
			Created: day("2023-08-20"), Added: day("2023-08-20")},
		{ID: 5, WorkspaceID: 1, Title: "家电网购发票", Content: "京东 网购 空调", Created: day("2024-06-01"), Added: day("2024-06-01")},
		{ID: 6, WorkspaceID: 2, Title: "别人的电费账单", Content: "国家电网 电费", Created: day("2024-03-05"), Added: day("2024-03-05")},
	}
	if err := ix.Put(docs); err != nil {
		t.Fatal(err)
	}
	return ix
}

func find(t *testing.T, ix *Index, ws int64, q string, prefix bool) string {
	t.Helper()
	pq := Parse(q, QueryOptions{Prefix: prefix})
	if pq == nil {
		return "nil"
	}
	hits, total, err := ix.Search(ws, pq, 100)
	if err != nil {
		t.Fatalf("%q: %v", q, err)
	}
	ids := make([]int, len(hits))
	for i, h := range hits {
		ids[i] = int(h.ID)
	}
	sort.Ints(ids)
	if int(total) != len(ids) {
		t.Errorf("%q: total %d, hits %d", q, total, len(ids))
	}
	return strings.Trim(fmt.Sprint(ids), "[]")
}

func TestQuerySyntax(t *testing.T) {
	ix := fixture(t)
	cases := []struct {
		q, want string
		prefix  bool
	}{
		{"电费", "1", false},
		{"电网", "1", false},     // a sub-word of 国家电网; workspace 2 stays out
		{"电费 户号", "1", false},  // every word
		{"电费账单", "1", false},   // cut into 电费 + 账单
		{"发票", "1 2 5", false}, // tag path, title, content
		{`"专用发票"`, "2", false},
		{`"租赁合同"`, "3 4", false}, // title of 3, text of 4
		{"合同 -草稿", "3", false},
		{"押金", "3", false},          // notes
		{"HT-2023-001", "4", false}, // custom field
		{"tag:水电", "1", false},
		{`tag:"财务 / 发票"`, "1 2", false},
		{"type:合同", "3 4", false},
		{"from:链家", "3", false},
		{"from:电费", "", false}, // not a correspondent
		{"created:2023", "3 4", false},
		{"created:2024-03", "1", false},
		{"created:2023-09..2024-03", "1 3", false},
		{"created:2024-13", "", false}, // bad date → plain word, matches nothing
		{"asn:123", "1", false},
		{"asn:#000123", "1", false},
		{"nickname:x 电费", "", false}, // unknown field → plain word "nickname:x"
		{`"电费`, "1", false},          // unclosed quote ignored
		{"租", "", false},             // single characters of longer words are not indexed
		{"租", "3 4", true},           // … but match as a prefix while typing
		{"房屋 租", "3", true},
		{"   ", "nil", false},
		{"-草稿", "1 2 3 5", false}, // only exclusions
	}
	for _, c := range cases {
		if got := find(t, ix, 1, c.q, c.prefix); got != c.want {
			t.Errorf("%q (prefix %v) = %q, want %q", c.q, c.prefix, got, c.want)
		}
	}
	if got := find(t, ix, 2, "电费", false); got != "6" {
		t.Errorf("workspace 2 = %q", got)
	}
}

func TestRelevanceAndHighlight(t *testing.T) {
	ix := fixture(t)
	hits, _, err := ix.Search(1, Parse("发票", QueryOptions{}), 10)
	if err != nil || len(hits) != 3 || hits[0].ID != 2 {
		t.Fatalf("发票 = %+v, %v (the title match with the word twice should rank first)", hits, err)
	}
	hl, err := ix.Highlight(1, Parse("电费", QueryOptions{}), []int64{1, 2, 6})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := hl[6]; ok {
		t.Error("another workspace's document must not be highlighted")
	}
	if _, ok := hl[2]; ok {
		t.Error("a document that does not match has no snippet")
	}
	var b strings.Builder
	for _, s := range hl[1] {
		if s.Hit {
			b.WriteString("[" + s.Text + "]")
		} else {
			b.WriteString(s.Text)
		}
	}
	if !strings.Contains(b.String(), "本期[电费] 128.50 元") {
		t.Errorf("snippet = %q", b.String())
	}
	hl, _ = ix.Highlight(1, Parse("押金", QueryOptions{}), []int64{3})
	if len(hl[3]) == 0 || !hl[3][0].Hit && (len(hl[3]) < 2 || !hl[3][1].Hit) {
		t.Errorf("notes snippet = %+v", hl[3])
	}
}

func TestDeleteAndReopen(t *testing.T) {
	dir := t.TempDir()
	ix, _, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ix.Put([]Doc{{ID: 1, WorkspaceID: 1, Title: "电费账单"}, {ID: 2, WorkspaceID: 1, Title: "电费"}, {ID: 3, WorkspaceID: 2, Title: "电费"}})
	if err := ix.Delete([]int64{2}); err != nil {
		t.Fatal(err)
	}
	if err := ix.DeleteWorkspace(2); err != nil {
		t.Fatal(err)
	}
	if n, _ := ix.Count(); n != 1 {
		t.Errorf("count = %d, want 1", n)
	}
	ix.Close()
	if _, _, err := ix.Search(1, Parse("电费", QueryOptions{}), 10); err != ErrClosed {
		t.Errorf("search after close: %v", err)
	}

	ix, rebuilt, err := Open(dir)
	if err != nil || rebuilt {
		t.Fatalf("reopen = %v, rebuilt %v", err, rebuilt)
	}
	if n, _ := ix.Count(); n != 1 {
		t.Errorf("count after reopen = %d", n)
	}
	ix.idx.SetInternal([]byte(schemaKey), []byte("0")) // an older schema
	ix.Close()
	ix, rebuilt, err = Open(dir)
	if err != nil || !rebuilt {
		t.Fatalf("old schema: %v, rebuilt %v", err, rebuilt)
	}
	if n, _ := ix.Count(); n != 0 {
		t.Errorf("a rebuilt index starts empty, has %d", n)
	}
	ix.Close()

	// An unreadable index is recreated rather than blocking start-up.
	os.WriteFile(filepath.Join(dir, "documents.bleve", "index_meta.json"), []byte("garbage"), 0o644)
	ix, rebuilt, err = Open(dir)
	if err != nil || !rebuilt {
		t.Fatalf("corrupt index: %v, rebuilt %v", err, rebuilt)
	}
	ix.Close()
}
