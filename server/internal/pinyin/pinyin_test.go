package pinyin

import (
	"sort"
	"strings"
	"testing"
)

func TestSortKey(t *testing.T) {
	cases := map[string]string{
		"国家电网":     "guojiadianwang",
		"国网北京":     "guowangbeijing",
		"Apple":    "apple",
		"Apple 中国": "apple zhongguo",
		"12345":    "12345",
		"保修卡":      "baoxiuka",
		"  链家  ":   "lianjia",
		"":         "",
	}
	for in, want := range cases {
		if got := SortKey(in); got != want {
			t.Errorf("SortKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInitial(t *testing.T) {
	cases := map[string]string{
		"国家电网": "G", "链家": "L", "阿里云": "A", "张婷": "Z",
		"Apple": "#", "apple": "#", "12345": "#", "（暂存）": "#", "": "#",
		"  招商银行": "Z",
	}
	for in, want := range cases {
		if got := Initial(in); got != want {
			t.Errorf("Initial(%q) = %q, want %q", in, got, want)
		}
	}
}

// Sorting by the key must put names in the directory's pinyin order.
func TestSortKeysOrderLikeTheDirectory(t *testing.T) {
	ordered := []string{"阿里云", "北京燃气", "国家电网", "国网北京", "链家", "招商银行"}
	for i := 1; i < len(ordered); i++ {
		if SortKey(ordered[i-1]) >= SortKey(ordered[i]) {
			t.Errorf("%s (%s) should sort before %s (%s)", ordered[i-1], SortKey(ordered[i-1]), ordered[i], SortKey(ordered[i]))
		}
	}
}

func TestTitleKeyOrdersNumbersNaturally(t *testing.T) {
	titles := []string{"2024年10月电费账单", "2024年3月电费账单", "2023年12月电费账单", "Apple 发票 7", "Apple 发票 12"}
	sort.Slice(titles, func(i, j int) bool { return TitleKey(titles[i]) < TitleKey(titles[j]) })
	want := []string{"2023年12月电费账单", "2024年3月电费账单", "2024年10月电费账单", "Apple 发票 7", "Apple 发票 12"}
	if strings.Join(titles, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", titles, want)
	}
	if got := TitleKey("2024年3月"); got != "0000002024nian0000000003yue" {
		t.Errorf("TitleKey = %q", got)
	}
}
