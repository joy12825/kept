package demo

import (
	"bytes"
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
)

// NotoSansSC-subset.ttf holds only the characters the sample data uses
// (fonts/make-subset.sh regenerates it after the data changes). Licence:
// SIL Open Font License 1.1, fonts/OFL.txt.
//
//go:embed fonts/NotoSansSC-subset.ttf
var fontData []byte

// samplePDF renders one A4 page for a sample document: letterhead, title,
// date, type and amount, so ingesting it yields real text and a thumbnail.
func samplePDF(d docData) ([]byte, error) {
	p := fpdf.New("P", "mm", "A4", "")
	p.SetTitle(d.title, true)
	p.SetCreator("收好 演示数据", true)
	// Fixed dates keep the bytes stable from run to run.
	fixed := time.Date(2024, 10, 8, 9, 0, 0, 0, time.UTC)
	p.SetCreationDate(fixed)
	p.SetModificationDate(fixed)
	p.AddUTF8FontFromBytes("noto", "", fontData)
	p.SetMargins(24, 28, 24)
	p.AddPage()

	p.SetFont("noto", "", 13)
	p.SetTextColor(90, 98, 110)
	p.CellFormat(0, 8, d.correspondent, "", 1, "L", false, 0, "")
	p.SetDrawColor(208, 213, 221)
	p.Line(24, p.GetY()+2, 186, p.GetY()+2)
	p.Ln(10)

	p.SetFont("noto", "", 22)
	p.SetTextColor(31, 35, 40)
	p.MultiCell(0, 11, d.title, "", "L", false)
	p.Ln(6)

	p.SetFont("noto", "", 12)
	row := func(label, value string) {
		p.SetTextColor(99, 107, 120)
		p.CellFormat(28, 9, label, "", 0, "L", false, 0, "")
		p.SetTextColor(31, 35, 40)
		p.CellFormat(0, 9, value, "", 1, "L", false, 0, "")
	}
	row("日期", chineseDate(d.date))
	row("类型", d.docType)
	row("往来方", d.correspondent)
	if d.amount > 0 {
		row("金额", "¥"+money(d.amount))
	}
	p.Ln(10)
	p.SetTextColor(150, 156, 166)
	p.SetFont("noto", "", 10)
	p.MultiCell(0, 6, "本文件为“收好”的演示数据，由原型的示例内容生成，仅用于演示。", "", "L", false)

	var b bytes.Buffer
	if err := p.Output(&b); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// chineseDate turns 2024-03-05 into 2024年3月5日.
func chineseDate(iso string) string {
	parts := strings.Split(iso, "-")
	if len(parts) != 3 {
		return iso
	}
	m, _ := strconv.Atoi(parts[1])
	d, _ := strconv.Atoi(parts[2])
	return fmt.Sprintf("%s年%d月%d日", parts[0], m, d)
}

// money formats 1234.5 as 1,234.50.
func money(f float64) string {
	s := strconv.FormatFloat(f, 'f', 2, 64)
	intPart, frac := s[:len(s)-3], s[len(s)-3:]
	var b strings.Builder
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String() + frac
}
