package parser

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"

	"kept/server/internal/ofdtest"
	"kept/server/internal/watermark"
)

var invoice = ofdtest.Invoice{Number: "24312000000123456789", Date: "2024年03月05日", Seller: "上海某某餐饮有限公司", Buyer: "王小明", Item: "*餐饮服务*餐费", Amount: "286.00", Pages: 2}

// v1.9.3: an OFD's text in reading order, and its pages drawn as a PDF.
func TestOFD(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "invoice")
	os.WriteFile(src, invoice.Build(), 0o644)
	mime, ext, err := Detect(src, "发票.ofd")
	if err != nil || mime != ofdType || ext != "ofd" {
		t.Fatalf("Detect = %s %s %v", mime, ext, err)
	}
	d, err := readOFD(src)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	text := d.Text()
	for _, want := range []string{"电子发票（普通发票）", "发票号码：24312000000123456789", "购买方： 王小明", "*餐饮服务*餐费 286.00", "价税合计（小写） ¥286.00", "第 2 页"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	// the label comes before the value on its line, lines top to bottom
	if strings.Index(text, "电子发票") > strings.Index(text, "购买方") {
		t.Errorf("order:\n%s", text)
	}
	if !watermark.Available() {
		t.Skip("no Chinese font")
	}
	res, err := Parse(context.Background(), cfg, src, "发票.ofd", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("OFD_LOOK") != "" {
		exec.Command("cp", res.ArchivePath, os.Getenv("OFD_LOOK")).Run()
	}
	if res.ArchivePath == "" || res.PageCount != 2 || res.ThumbnailPath == "" || !strings.Contains(res.Content, "王小明") {
		t.Fatalf("parse = %+v", res)
	}
	out, err := exec.Command("pdfinfo", res.ArchivePath).Output()
	if err != nil || !regexp.MustCompile(`Pages:\s+2\n`).Match(out) || !strings.Contains(string(out), "595.28 x 396.85") {
		t.Errorf("pdfinfo: %s", out)
	}
	layer, _ := exec.Command("pdftotext", res.ArchivePath, "-").Output()
	if !strings.Contains(strings.ReplaceAll(string(layer), " ", ""), "上海某某餐饮有限公司") {
		t.Errorf("pdf text: %s", layer)
	}

	// not an OFD at all
	bad := filepath.Join(dir, "bad.ofd")
	writeZip(t, bad, map[string]string{"OFD.xml": "<ofd:OFD/>"})
	if _, err := readOFD(bad); err != ErrOFD {
		t.Errorf("broken: %v", err)
	}
}

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, body := range files {
		w, _ := z.Create(name)
		io.WriteString(w, body)
	}
	z.Close()
	os.WriteFile(path, b.Bytes(), 0o644)
}

// v1.9.3: the cells of a workbook.
func TestXLSX(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "账本.xlsx")
	writeZip(t, src, map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/></Types>`,
		"xl/workbook.xml": `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
			<sheets><sheet name="支出" sheetId="1" r:id="rId1"/><sheet name="藏起来" sheetId="2" state="hidden" r:id="rId2"/><sheet name="收入" sheetId="3" r:id="rId3"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
			<Relationship Id="rId1" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Target="worksheets/sheet2.xml"/><Relationship Id="rId3" Target="/xl/worksheets/sheet3.xml"/></Relationships>`,
		"xl/sharedStrings.xml": `<sst><si><t>日期</t></si><si><t>项目</t></si><si><r><t>电</t></r><r><t>费</t></r></si><si><t>2024 年支出</t></si></sst>`,
		"xl/styles.xml":        `<styleSheet><numFmts><numFmt numFmtId="164" formatCode="yyyy&quot;年&quot;m&quot;月&quot;d&quot;日&quot;"/></numFmts><cellXfs><xf numFmtId="0"/><xf numFmtId="14"/><xf numFmtId="164"/><xf numFmtId="10"/></cellXfs></styleSheet>`,
		"xl/worksheets/sheet1.xml": `<worksheet><sheetData>
			<row r="1"><c r="A1" t="s"><v>3</v></c></row>
			<row r="2"><c r="A2" t="s"><v>0</v></c><c r="B2" t="s"><v>1</v></c><c r="C2" t="inlineStr"><is><t>金额</t></is></c><c r="D2" t="b"><v>1</v></c></row>
			<row r="3"><c r="A3" s="1"><v>45356</v></c><c r="B3" t="s"><v>2</v></c><c r="C3"><f>SUM(1,2)</f><v>128.5</v></c><c r="E3" s="2"><v>45357</v></c></row>
			<row r="5" s="1"/></sheetData><mergeCells><mergeCell ref="A1:D1"/></mergeCells></worksheet>`,
		"xl/worksheets/sheet2.xml": `<worksheet><sheetData/></worksheet>`,
		"xl/worksheets/sheet3.xml": `<worksheet><sheetData><row r="1"><c r="B1"><v>0.1</v></c><c r="C1" t="str"><v>文字公式</v></c></row></sheetData></worksheet>`,
	})
	sheets, err := ReadSheets(src, "xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if len(sheets) != 2 || sheets[0].Name != "支出" || sheets[1].Name != "收入" {
		t.Fatalf("sheets = %+v", sheets)
	}
	s := sheets[0]
	want := [][]string{{"2024 年支出"}, {"日期", "项目", "金额", "TRUE"}, {"2024-03-05", "电费", "128.5", "", "2024-03-06"}}
	if len(s.Rows) != 3 {
		t.Fatalf("rows = %q", s.Rows)
	}
	for i, r := range want {
		if strings.Join(s.Rows[i], "|") != strings.Join(r, "|") {
			t.Errorf("row %d = %q, want %q", i, s.Rows[i], r)
		}
	}
	if len(s.Merges) != 1 || s.Merges[0] != [4]int{0, 0, 0, 3} {
		t.Errorf("merges = %v", s.Merges)
	}
	if got := sheets[1].Rows[0]; strings.Join(got, "|") != "|0.1|文字公式" {
		t.Errorf("收入 = %q", got)
	}
}

func TestCSV(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.csv")
	os.WriteFile(p, []byte("名称,金额,备注\n电费,128.5,\"含\n换行, 和逗号\"\n"), 0o644)
	sheets, err := ReadSheets(p, "csv")
	if err != nil || len(sheets[0].Rows) != 2 || sheets[0].Rows[1][2] != "含\n换行, 和逗号" {
		t.Fatalf("csv = %+v %v", sheets, err)
	}
	gbk, _ := simplifiedchinese.GBK.NewEncoder().String("日期\t读数\n10月\t5230\n")
	q := filepath.Join(dir, "b.tsv")
	os.WriteFile(q, []byte(gbk), 0o644)
	sheets, err = ReadSheets(q, "txt")
	if err != nil || sheets[0].Rows[1][1] != "5230" || sheets[0].Rows[0][0] != "日期" {
		t.Fatalf("gbk tsv = %+v %v", sheets, err)
	}
	var big strings.Builder
	for i := 0; i < SheetMaxRows+5; i++ {
		big.WriteString("a,b\n")
	}
	r := filepath.Join(dir, "big.csv")
	os.WriteFile(r, []byte(big.String()), 0o644)
	if s, _ := ReadSheets(r, "csv"); len(s[0].Rows) != SheetMaxRows || !s[0].Truncated {
		t.Errorf("big: %d %v", len(s[0].Rows), s[0].Truncated)
	}
}

// v1.9.3: a zip's files, with GBK names and unsafe paths cleaned.
func TestZip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.zip")
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	add := func(name string, body string, nonUTF8 bool) {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate, NonUTF8: nonUTF8}
		w, _ := z.CreateHeader(h)
		io.WriteString(w, body)
	}
	gbkName, _ := simplifiedchinese.GBK.NewEncoder().String("收据/停车费.txt")
	add(gbkName, "停车费 20 元", true)
	add("../../etc/passwd.txt", "x", false)
	add("收据/", "", false)
	add("__MACOSX/._停车费.txt", "junk", false)
	add("报告.pdf", "%PDF-1.4", false)
	z.Close()
	os.WriteFile(src, b.Bytes(), 0o644)
	if mime, ext, _ := Detect(src, "a.zip"); mime != zipType || ext != "zip" {
		t.Fatalf("Detect = %s %s", mime, ext)
	}
	entries, truncated, err := ListZip(src)
	if err != nil || truncated {
		t.Fatal(err)
	}
	var paths []string
	for _, e := range entries {
		paths = append(paths, e.Path)
	}
	if strings.Join(paths, ",") != "etc/passwd.txt,报告.pdf,收据,收据/停车费.txt" {
		t.Errorf("paths = %v", paths)
	}
	rc, e, err := OpenZipEntry(src, "收据/停车费.txt", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(rc)
	rc.Close()
	if string(body) != "停车费 20 元" || e.Size != int64(len("停车费 20 元")) {
		t.Errorf("entry = %q %+v", body, e)
	}
	if _, _, err := OpenZipEntry(src, "报告.pdf", 3); err == nil {
		t.Error("too large should fail")
	}
	res, err := Parse(context.Background(), cfg, src, "a.zip", dir, nil)
	if err != nil || !strings.Contains(res.Content, "收据/停车费.txt") || res.ArchivePath != "" {
		t.Errorf("parse = %+v %v", res, err)
	}
}
