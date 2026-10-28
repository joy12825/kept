package parser

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"kept/server/internal/i18n"
)

// Prop is one property a file carries, labelled for display. Labels and
// values made here are marked with i18n.N; translate them where they are sent.
type Prop struct{ Label, Value string }

// PDFProps reads the document information of a PDF with pdfinfo (v1.3.7):
// title, author, the programs that made it, its dates, version and page
// size. An encrypted PDF yields ErrEncrypted.
func PDFProps(ctx context.Context, pdf string) ([]Prop, error) {
	out, err := run(ctx, Timeout(1), "pdfinfo", "-isodates", "-enc", "UTF-8", pdf)
	if err != nil {
		if strings.Contains(out.stderr, "Incorrect password") {
			return nil, ErrEncrypted
		}
		return nil, err
	}
	return parsePDFInfo(out.stdout), nil
}

// pdfinfo's keys, in the order shown.
var pdfKeys = []struct{ key, label string }{
	{"Title", i18n.N("标题")}, {"Author", i18n.N("作者")}, {"Subject", i18n.N("主题")}, {"Keywords", i18n.N("关键词")},
	{"Creator", i18n.N("创建软件")}, {"Producer", i18n.N("生成工具")},
	{"CreationDate", i18n.N("创建时间")}, {"ModDate", i18n.N("修改时间")},
	{"PDF version", i18n.N("PDF 版本")}, {"Page size", i18n.N("页面大小")}, {"Encrypted", i18n.N("加密")},
}

func parsePDFInfo(out string) []Prop {
	vals := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if _, seen := vals[k]; !seen {
			vals[k] = strings.TrimSpace(v)
		}
	}
	var props []Prop
	for _, k := range pdfKeys {
		v := vals[k.key]
		switch k.key {
		case "CreationDate", "ModDate":
			v = isoDate(v)
		case "Page size":
			v = pageSize(v)
		case "Encrypted":
			if strings.HasPrefix(v, "no") || v == "" {
				continue
			}
			v = i18n.N("是")
		}
		if v != "" {
			props = append(props, Prop{k.label, v})
		}
	}
	return props
}

// isoDate shows pdfinfo's -isodates value (2024-03-05T10:00:00+08) as
// “2024-03-05 10:00”.
func isoDate(v string) string {
	for _, layout := range []string{"2006-01-02T15:04:05-07", "2006-01-02T15:04:05-07:00", "2006-01-02T15:04:05Z", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.Format("2006-01-02 15:04")
		}
	}
	return v
}

var sizeRE = regexp.MustCompile(`^([\d.]+) x ([\d.]+) pts(?: \((\w+)\))?`)

// pageSize shows “595.276 x 841.89 pts (A4)” as “210 × 297 毫米（A4）”.
func pageSize(v string) string {
	m := sizeRE.FindStringSubmatch(v)
	if m == nil {
		return v
	}
	mm := func(s string) string {
		f, _ := strconv.ParseFloat(s, 64)
		return strconv.Itoa(int(f*25.4/72 + 0.5))
	}
	if m[3] != "" {
		return fmt.Sprintf(i18n.N("%s × %s 毫米（%s）"), mm(m[1]), mm(m[2]), m[3])
	}
	return fmt.Sprintf(i18n.N("%s × %s 毫米"), mm(m[1]), mm(m[2]))
}
