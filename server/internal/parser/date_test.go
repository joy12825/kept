package parser

import (
	"strings"
	"testing"
	"time"
)

// v1.4.4: the words before a date say whether it is the document's own.
func TestFindDateReadsTheWordsBefore(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	cases := []struct{ name, text, want string }{
		{"id card: not the birth date", "居民身份证\n姓名 王小明\n出生 2015年4月12日\n签发日期 2022年5月10日\n有效期限 2022.05.10-2032.05.10", "2022-05-10"},
		{"passport: issue, not expiry", "护照\n出生日期 2019-10-29\n签发日期 2024-01-08\n有效期至 2029-01-07", "2024-01-08"},
		{"policy: the signing date", "保险单\n保险期间 2024年1月1日 至 2025年1月1日\n签单日期：2023年12月20日", "2023-12-20"},
		{"policy: not the validity's end", "保险单\n保险期间 2024年1月1日 至 2025年1月1日", "2024-01-01"},
		{"policy with an issue date", "保险单\n保险期间 2024-01-01 至 2025-01-01\n出具日期 2023-12-20", "2023-12-20"},
		{"statement: the bill date", "信用卡对账单\n账单周期 2024/02/06 - 2024/03/05\n账单日 2024/03/05\n到期还款日 2024/03/25", "2024-03-05"},
		{"statement without a bill date: the period's end", "账单周期 2024/02/06 - 2024/03/05\n到期还款日 2024/03/25", "2024-03-05"},
		{"invoice", "购买方 xx\n开票日期：2024年03月05日\n校验码 1234", "2024-03-05"},
		{"due date only", "缴费截止 2024-03-25", ""},
		{"plain: the first one", "先 2022/1/1 再 2023年1月1日", "2022-01-01"},
	}
	for _, c := range cases {
		got, _ := FindDate(c.text, now)
		if got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDateCandidatesAndBasis(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	c := DateCandidates("合同编号 9\n生效 2024-01-02，补充 2024-02-03\n开票日期：2024年3月5日\n再写一次 2024-01-02", now)
	if len(c) != 3 || c[0].Date != "2024-03-05" || c[0].Matched != "开票日期：2024年3月5日" || c[1].Date != "2024-01-02" || c[2].Date != "2024-02-03" {
		t.Errorf("candidates: %+v", c)
	}
	// a signing date at the end of a long contract
	long := "租赁合同\n" + strings.Repeat("第 1 条 甲方应当按时交付房屋，乙方应当按时支付租金。", 120) + "\n签订日期：2023年9月1日"
	if got, _ := FindDate(long, now); got != "2023-09-01" {
		t.Errorf("signed at the end: %q", got)
	}
}

func TestFileNameDate(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	for name, want := range map[string]string{
		"IMG_20240305_123456.jpg": "2024-03-05", "微信图片_20240305120000.jpg": "2024-03-05", "Scan20240305.pdf": "2024-03-05",
		"扫描 2024-03-05.pdf": "2024-03-05", "2024_3_5 发票.pdf": "2024-03-05", "2024年3月5日体检.pdf": "2024-03-05",
		"发票号12345678.pdf": "", "IMG_20241399.jpg": "", "合同.pdf": "", "202403051234567890.pdf": "",
	} {
		got, ok := FileNameDate(name, now)
		if got != want || ok != (want != "") {
			t.Errorf("%s: %q", name, got)
		}
	}
}

// v1.5.5: dates in Chinese numerals and with English month names.
func TestMoreDateForms(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	cases := []struct{ name, text, want string }{
		{"contract in Chinese numerals", "房屋租赁合同\n甲方（签章）\n签订日期：二〇二四年三月五日", "2024-03-05"},
		{"other zero, tens", "证书\n二○二三年十二月三十一日", "2023-12-31"},
		{"零 and 二十", "二零二五年一月二十日", "2025-01-20"},
		{"ten alone", "二〇二四年十月十日", "2024-10-10"},
		{"spaces from OCR", "开具日期: 二 〇 二 四 年 三 月 五 日", "2024-03-05"},
		{"invalid numeral day", "二〇二四年二月三十日", ""},
		{"US receipt", "Apple Store\nOrder Date: March 5, 2024\nTotal $129.00", "2024-03-05"},
		{"abbreviated with a dot", "Invoice\nDate: Sept. 3rd, 2025", "2025-09-03"},
		{"day first", "Booking reference XYZ\nIssued 5 Mar 2024", "2024-03-05"},
		{"day first with hyphens", "Statement 05-Mar-2024", "2024-03-05"},
		{"not the due date", "Invoice Date: 1 February 2024\nDue Date: 1 March 2024", "2024-02-01"},
		{"not an expiry", "Visa\nExpiry Date 12 Jan 2030\nDate of Issue 12 Jan 2025", "2025-01-12"},
		{"ambiguous all-digit date is not read", "Date 05/03/2024", ""},
		{"a word that is not a month", "Mayday 5 2024", ""},
	}
	for _, c := range cases {
		got, _ := FindDate(c.text, now)
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	if n := cnNumber("十十"); n != 0 {
		t.Errorf("十十 = %d", n)
	}
	_, matched, _ := FindDateText("Order Date: March 5, 2024", now)
	if matched != "Order Date: March 5, 2024" {
		t.Errorf("basis = %q", matched)
	}
}
