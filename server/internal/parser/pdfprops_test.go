package parser

import (
	"context"
	"fmt"
	"testing"
)

func TestParsePDFInfo(t *testing.T) {
	out := `Title:           2024 年 3 月电费账单
Author:          国家电网
Creator:         Microsoft Word
Producer:        macOS 版本14.4 Quartz PDFContext
CreationDate:    2024-03-05T10:00:00+08
ModDate:         2024-03-06T09:30:00Z
Custom Metadata: no
Tagged:          no
Pages:           2
Encrypted:       no
Page size:       595.276 x 841.89 pts (A4)
Page rot:        0
PDF version:     1.7
`
	got := fmt.Sprint(parsePDFInfo(out))
	want := "[{标题 2024 年 3 月电费账单} {作者 国家电网} {创建软件 Microsoft Word} {生成工具 macOS 版本14.4 Quartz PDFContext} {创建时间 2024-03-05 10:00} {修改时间 2024-03-06 09:30} {PDF 版本 1.7} {页面大小 210 × 297 毫米（A4）}]"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got := fmt.Sprint(parsePDFInfo("Encrypted:       yes (print:yes copy:no)\nPage size: 612 x 792 pts (letter)\n")); got != "[{页面大小 216 × 279 毫米（letter）} {加密 是}]" {
		t.Errorf("encrypted: %s", got)
	}
}

func TestPDFPropsReadsARealPDF(t *testing.T) {
	needTools(t)
	props, err := PDFProps(context.Background(), textPDF(t, t.TempDir(), "电费账单"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range props {
		found = found || p.Label == "PDF 版本"
	}
	if !found {
		t.Errorf("props = %v", props)
	}
}
