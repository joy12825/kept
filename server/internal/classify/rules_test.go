package classify

import (
	"reflect"
	"strings"
	"testing"

	"kept/server/internal/taxonomy"
)

func TestRules(t *testing.T) {
	const bill = "国家电网 电费通知单\n户号 HH-0012345 本期电费 128.50 元\nState Grid Invoice, see the TAX section"
	for _, c := range []struct {
		algo, match string
		ci          bool
		want        bool
		basis       string
	}{
		{"any", "国家电网 供电局", true, true, "关键词“国家电网”"},
		{"any", "自来水，燃气", true, false, ""},
		{"any", "tax", true, true, "关键词“tax”"},
		{"any", "tax", false, false, ""},     // case matters
		{"any", "invoic", true, false, ""},   // part of a longer Latin word
		{"any", "电网", true, true, "关键词“电网”"}, // Chinese matches anywhere
		{"all", "电费 户号", true, true, "关键词“电费”“户号”"},
		{"all", "电费 水费", true, false, ""},
		{"any", `"state grid" 水务`, true, true, "关键词“state grid”"},
		{"literal", "本期电费 128.50", true, true, "包含“本期电费 128.50”"},
		{"literal", "本期 128.50", true, false, ""},
		{"regex", `HH-\d+`, false, true, "正则匹配“HH-0012345”"},
		{"regex", `hh-\d+`, false, false, ""},
		{"regex", `[`, false, false, ""},
		{"fuzzy", "state grid invoise", true, true, "近似匹配“state grid invoice”"},
		{"fuzzy", "国家电力", true, false, ""}, // short: no edits allowed
		{"none", "电费", true, false, ""},
		{"auto", "", true, false, ""},
	} {
		m := Test(taxonomy.MatchRule{Algorithm: c.algo, Match: c.match, CaseInsensitive: c.ci}, bill)
		if m.Matched != c.want || m.Basis != c.basis {
			t.Errorf("%s %q (ci %v) = %v %q, want %v %q", c.algo, c.match, c.ci, m.Matched, m.Basis, c.want, c.basis)
		}
	}
}

func TestFullWidthAndSpacesFold(t *testing.T) {
	m := Test(taxonomy.MatchRule{Algorithm: "literal", Match: "abc 2024", CaseInsensitive: true}, "编号ＡＢＣ　 ２０２４号")
	if !m.Matched {
		t.Error("full-width letters and spaces should fold")
	}
}

func TestKeywords(t *testing.T) {
	got := Keywords(`国家电网, 供电局；"state grid"  “南方 电网”`)
	want := []string{"国家电网", "供电局", "state grid", "南方 电网"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Keywords = %q, want %q", got, want)
	}
}

func TestFeatures(t *testing.T) {
	f := Features("国家电网的电费通知单 2024 年 3 月，金额 128.50 元")
	joined := strings.Join(f, " ")
	for _, w := range []string{"国家电网", "电费"} {
		if !strings.Contains(joined, w) {
			t.Errorf("features %q lack %q", f, w)
		}
	}
	for _, w := range []string{"2024", "128.50", "的", "年"} {
		for _, x := range f {
			if x == w {
				t.Errorf("features %q should drop %q", f, w)
			}
		}
	}
}

func TestTextIsCut(t *testing.T) {
	s := Text("标题", strings.Repeat("字", MaxText*2))
	if n := len([]rune(s)); n != MaxText {
		t.Errorf("Text length %d, want %d", n, MaxText)
	}
}
