package parser

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kept/server/internal/docxtest"
)

func writeDocx(t *testing.T, d docxtest.Doc) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "doc.docx")
	if err := os.WriteFile(p, d.Build(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func texts(b WordBlock) string {
	var s strings.Builder
	for _, r := range b.Runs {
		s.WriteString(r.Text)
	}
	return s.String()
}

const styles = `<w:docDefaults><w:rPrDefault><w:rPr><w:sz w:val="21"/></w:rPr></w:rPrDefault></w:docDefaults>` +
	`<w:style w:type="paragraph" w:default="1" w:styleId="a"><w:name w:val="Normal"/></w:style>` +
	`<w:style w:type="paragraph" w:styleId="1"><w:name w:val="heading 1"/><w:basedOn w:val="a"/><w:rPr><w:b/><w:sz w:val="44"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="2"><w:name w:val="heading 2"/><w:basedOn w:val="1"/><w:pPr><w:jc w:val="right"/></w:pPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Loop1"><w:name w:val="Loop"/><w:basedOn w:val="Loop2"/></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Loop2"><w:name w:val="Loop"/><w:basedOn w:val="Loop1"/></w:style>` +
	`<w:style w:type="character" w:styleId="Strong"><w:name w:val="Strong"/><w:rPr><w:b/><w:color w:val="C00000"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="ListBullet"><w:name w:val="List Bullet"/><w:pPr><w:numPr><w:numId w:val="2"/></w:numPr></w:pPr></w:style>`

const numberingXML = `<w:abstractNum w:abstractNumId="0">` +
	`<w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="chineseCounting"/><w:lvlText w:val="%1、"/></w:lvl>` +
	`<w:lvl w:ilvl="1"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1.%2"/></w:lvl>` +
	`<w:lvl w:ilvl="2"><w:start w:val="1"/><w:numFmt w:val="decimalEnclosedCircle"/><w:lvlText w:val="%3"/></w:lvl></w:abstractNum>` +
	`<w:abstractNum w:abstractNumId="1"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:lvlText w:val="` + "" + `"/></w:lvl></w:abstractNum>` +
	`<w:abstractNum w:abstractNumId="2"><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="upperRoman"/><w:lvlText w:val="(%1)"/></w:lvl></w:abstractNum>` +
	`<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num><w:num w:numId="2"><w:abstractNumId w:val="1"/></w:num>` +
	`<w:num w:numId="3"><w:abstractNumId w:val="2"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="4"/></w:lvlOverride></w:num>`

func item(num, lvl int, text string) string {
	return `<w:p><w:pPr><w:numPr><w:ilvl w:val="` + string(rune('0'+lvl)) + `"/><w:numId w:val="` + string(rune('0'+num)) + `"/></w:numPr></w:pPr><w:r><w:t>` + text + `</w:t></w:r></w:p>`
}

func TestWordParagraphsAndLists(t *testing.T) {
	src := writeDocx(t, docxtest.Doc{
		Styles: styles, Numbering: numberingXML,
		Body: `<w:p><w:pPr><w:pStyle w:val="1"/></w:pPr><w:r><w:t>家庭应急手册</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:pStyle w:val="2"/><w:jc w:val="center"/></w:pPr><w:r><w:t>第二级</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:pStyle w:val="2"/></w:pPr><w:r><w:t>样式里的对齐</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:pStyle w:val="Loop1"/><w:outlineLvl w:val="2"/></w:pPr><w:r><w:t>大纲三级</w:t></w:r></w:p>` +
			`<w:p/><w:p/>` +
			item(1, 0, "停电") + item(1, 1, "查电闸") + item(1, 1, "打电话") + item(1, 2, "带手电") + item(1, 0, "停水") + item(1, 1, "储水") +
			`<w:p><w:pPr><w:pStyle w:val="ListBullet"/></w:pPr><w:r><w:t>项目符号</w:t></w:r></w:p>` +
			item(3, 0, "从四开始") + item(3, 0, "接着") +
			`<w:p><w:r><w:t xml:space="preserve">普通 </w:t></w:r><w:r><w:rPr><w:rStyle w:val="Strong"/></w:rPr><w:t>重要</w:t></w:r>` +
			`<w:r><w:rPr><w:i/><w:u w:val="single"/><w:strike/><w:highlight w:val="yellow"/><w:sz w:val="32"/></w:rPr><w:t>标记</w:t></w:r>` +
			`<w:r><w:rPr><w:vertAlign w:val="superscript"/></w:rPr><w:t>2</w:t></w:r><w:r><w:rPr><w:vanish/></w:rPr><w:t>隐藏</w:t></w:r>` +
			`<w:r><w:t>甲</w:t><w:tab/><w:t>乙</w:t><w:br/><w:t>丙</w:t></w:r></w:p>`,
	})
	d, err := ReadWord(src)
	if err != nil {
		t.Fatal(err)
	}
	if d.BaseSize != 10.5 {
		t.Errorf("base size %v", d.BaseSize)
	}
	b := d.Blocks
	if b[0].Heading != 1 || texts(b[0]) != "家庭应急手册" || b[0].Runs[0].Size != 22 || !b[0].Runs[0].Bold {
		t.Errorf("heading 1: %+v", b[0])
	}
	if b[1].Heading != 2 || b[1].Align != "center" || b[2].Align != "right" {
		t.Errorf("alignment, direct and from the style: %+v / %+v", b[1], b[2])
	}
	b = append(b[:2], b[3:]...)
	if b[2].Heading != 3 {
		t.Errorf("heading 3: %+v", b[2])
	}
	if b[3].Kind != "paragraph" || len(b[3].Runs) != 0 || b[4].List == nil {
		t.Fatalf("two empty paragraphs should be one: %+v %+v", b[3], b[4])
	}
	var markers []string
	for _, x := range b[4:13] {
		if x.List == nil {
			t.Fatalf("not a list item: %+v", x)
		}
		markers = append(markers, x.List.Marker)
	}
	// the level above keeps its own format, as in Word (unless isLgl)
	want := []string{"一、", "一.1", "一.2", "①", "二、", "二.1", "•", "(IV)", "(V)"}
	if strings.Join(markers, " ") != strings.Join(want, " ") {
		t.Errorf("markers %q, want %q", markers, want)
	}
	if b[5].List.Level != 1 || b[7].List.Level != 2 {
		t.Errorf("levels %d %d", b[5].List.Level, b[7].List.Level)
	}
	p := b[13]
	if texts(p) != "普通 重要标记2甲\t乙\n丙" {
		t.Fatalf("runs %q", texts(p))
	}
	strong, mark, sup := p.Runs[1], p.Runs[2], p.Runs[3]
	if !strong.Bold || strong.Color != "C00000" {
		t.Errorf("character style: %+v", strong)
	}
	if !mark.Italic || !mark.Underline || !mark.Strike || mark.Highlight != "FFFF00" || mark.Size != 16 {
		t.Errorf("direct formatting: %+v", mark)
	}
	if sup.Vert != "super" {
		t.Errorf("superscript: %+v", sup)
	}
}

func TestWordLinksFieldsRevisions(t *testing.T) {
	src := writeDocx(t, docxtest.Doc{
		Rels: []docxtest.Rel{{ID: "rLink", Type: "hyperlink", Target: "https://example.com/a?b=1&c=2", External: true}, {ID: "rBad", Type: "hyperlink", Target: "javascript:alert(1)", External: true}},
		Body: `<w:p><w:hyperlink r:id="rLink"><w:r><w:t>官网</w:t></w:r></w:hyperlink><w:hyperlink r:id="rBad"><w:r><w:t>坏链接</w:t></w:r></w:hyperlink>` +
			`<w:hyperlink w:anchor="sec"><w:r><w:t>本文</w:t></w:r></w:hyperlink></w:p>` +
			`<w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> HYPERLINK "mailto:wang@example.com" </w:instrText></w:r>` +
			`<w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>写信</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>` +
			`<w:r><w:t xml:space="preserve"> 第 </w:t></w:r><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText>PAGE</w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>3</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r><w:r><w:t xml:space="preserve"> 页</w:t></w:r></w:p>` +
			`<w:p><w:r><w:t>保留</w:t></w:r><w:ins w:id="1"><w:r><w:t>新增</w:t></w:r></w:ins><w:del w:id="2"><w:r><w:delText>删去</w:delText></w:r></w:del>` +
			`<w:sdt><w:sdtContent><w:r><w:t>控件</w:t></w:r></w:sdtContent></w:sdt><m:oMath><m:r><m:t>x²+1</m:t></m:r></m:oMath></w:p>` +
			`<w:sdt><w:sdtContent><w:p><w:r><w:t>块级控件</w:t></w:r></w:p></w:sdtContent></w:sdt>`,
	})
	d, err := ReadWord(src)
	if err != nil {
		t.Fatal(err)
	}
	r := d.Blocks[0].Runs
	// the unsafe link and the link within the document are plain text, run together
	if len(r) != 2 || r[0].Text != "官网" || r[0].Link != "https://example.com/a?b=1&c=2" || r[1].Text != "坏链接本文" || r[1].Link != "" {
		t.Errorf("links: %+v", r)
	}
	if texts(d.Blocks[1]) != "写信 第 3 页" || d.Blocks[1].Runs[0].Link != "mailto:wang@example.com" || d.Blocks[1].Runs[1].Link != "" {
		t.Errorf("fields: %+v", d.Blocks[1].Runs)
	}
	if texts(d.Blocks[2]) != "保留新增控件x²+1" {
		t.Errorf("revisions: %q", texts(d.Blocks[2]))
	}
	if texts(d.Blocks[3]) != "块级控件" {
		t.Errorf("block content control: %+v", d.Blocks[3])
	}
}

func TestWordTablesImagesBoxesNotes(t *testing.T) {
	tc := func(props, text string) string {
		return `<w:tc><w:tcPr>` + props + `</w:tcPr>` + docxtest.P(text) + `</w:tc>`
	}
	nested := `<w:tbl><w:tr><w:tc>` + docxtest.P("内层") + `</w:tc></w:tr></w:tbl>`
	src := writeDocx(t, docxtest.Doc{
		Rels:  []docxtest.Rel{{ID: "rImg", Type: "image", Target: "media/flower.png"}, {ID: "rEmf", Type: "image", Target: "media/old.emf"}},
		Media: map[string][]byte{"flower.png": []byte("\x89PNG fake"), "old.emf": []byte("emf")},
		Footnotes: `<w:footnote w:type="separator" w:id="-1"><w:p><w:r><w:separator/></w:r></w:p></w:footnote>` +
			`<w:footnote w:id="1"><w:p><w:r><w:footnoteRef/></w:r><w:r><w:t xml:space="preserve"> 以物业通知为准。</w:t></w:r></w:p></w:footnote>`,
		Body: `<w:tbl><w:tr><w:trPr><w:tblHeader/></w:trPr>` + tc(`<w:gridSpan w:val="2"/>`, "联系人") + tc("", "电话") + `</w:tr>` +
			`<w:tr>` + tc(`<w:vMerge w:val="restart"/>`, "物业") + tc("", "前台") + tc("", "8888") + `</w:tr>` +
			`<w:tr>` + tc(`<w:vMerge/>`, "") + tc("", "保安") + `<w:tc>` + nested + `</w:tc></w:tr></w:tbl>` +
			`<w:p><w:r><w:t>见图</w:t></w:r>` + docxtest.Image("rImg", 320, 240) + docxtest.Image("rEmf", 10, 10) + docxtest.Image("rImg", 320, 240) +
			`<w:r><w:footnoteReference w:id="1"/></w:r>` +
			`<w:r><mc:AlternateContent><mc:Choice Requires="wps"><w:drawing><wp:anchor><wps:wsp><wps:txbx><w:txbxContent>` + docxtest.P("文本框里的提醒") + `</w:txbxContent></wps:txbx></wps:wsp></wp:anchor></w:drawing></mc:Choice>` +
			`<mc:Fallback><w:pict>` + docxtest.P("不该重复") + `</w:pict></mc:Fallback></mc:AlternateContent></w:r></w:p>` +
			`<w:p><w:r><w:footnoteReference w:id="1"/></w:r></w:p>`,
	})
	d, err := ReadWord(src)
	if err != nil {
		t.Fatal(err)
	}
	tb := d.Blocks[0]
	if tb.Kind != "table" || tb.HeaderRows != 1 || len(tb.Rows) != 3 {
		t.Fatalf("table: %+v", tb)
	}
	if c := tb.Rows[0][0]; c.ColSpan != 2 || texts(c.Blocks[0]) != "联系人" {
		t.Errorf("column span: %+v", c)
	}
	if c := tb.Rows[1][0]; c.RowSpan != 2 || texts(c.Blocks[0]) != "物业" {
		t.Errorf("row span: %+v", c)
	}
	if len(tb.Rows[2]) != 2 || texts(tb.Rows[2][0].Blocks[0]) != "保安" || tb.Rows[2][1].Blocks[0].Kind != "table" {
		t.Errorf("merged-away cell and nested table: %+v", tb.Rows[2])
	}
	p := d.Blocks[1]
	var imgs []int
	for _, r := range p.Runs {
		if r.Image > 0 {
			imgs = append(imgs, r.Image)
		}
	}
	if len(imgs) != 2 || imgs[0] != 1 || imgs[1] != 1 || len(d.Images) != 1 || d.Images[0].Width != 320 || d.Images[0].Part != "word/media/flower.png" {
		t.Errorf("images %v %+v", imgs, d.Images)
	}
	if !strings.Contains(texts(p), "[图片]") {
		t.Errorf("an EMF is a placeholder: %q", texts(p))
	}
	if last := p.Runs[len(p.Runs)-1]; last.Note != 1 || last.Vert != "super" {
		t.Errorf("note reference: %+v", last)
	}
	if box := d.Blocks[2]; box.Kind != "box" || texts(box.Blocks[0]) != "文本框里的提醒" || len(d.Blocks) != 4 {
		t.Errorf("text box: %+v (%d blocks)", box, len(d.Blocks))
	}
	if d.Blocks[3].Runs[0].Note != 1 || len(d.Notes) != 1 || texts(d.Notes[0].Blocks[0]) != " 以物业通知为准。" {
		t.Errorf("notes: %+v / %+v", d.Blocks[3], d.Notes)
	}
	b, mime, err := ReadWordImage(src, 1)
	if err != nil || mime != "image/png" || string(b) != "\x89PNG fake" {
		t.Errorf("image: %q %q %v", b, mime, err)
	}
	if _, _, err := ReadWordImage(src, 2); err == nil {
		t.Error("image 2 does not exist")
	}
}

func TestWordRefusesOtherFiles(t *testing.T) {
	dir := t.TempDir()
	txt := filepath.Join(dir, "a.txt")
	os.WriteFile(txt, []byte("plain"), 0o644)
	if _, err := ReadWord(txt); err != ErrNotWord {
		t.Errorf("text: %v", err)
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("xl/workbook.xml")
	w.Write([]byte("<workbook/>"))
	z.Close()
	xlsx := filepath.Join(dir, "a.xlsx")
	os.WriteFile(xlsx, b.Bytes(), 0o644)
	if _, err := ReadWord(xlsx); err != ErrNotWord {
		t.Errorf("workbook: %v", err)
	}
	broken := filepath.Join(dir, "b.docx")
	b.Reset()
	z = zip.NewWriter(&b)
	w, _ = z.Create("word/document.xml")
	w.Write([]byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>没有结束`))
	z.Close()
	os.WriteFile(broken, b.Bytes(), 0o644)
	if _, err := ReadWord(broken); err == nil {
		t.Error("a damaged document is an error, not half of it")
	}
}

func TestChineseNumbers(t *testing.T) {
	for n, want := range map[int]string{1: "一", 10: "十", 11: "十一", 20: "二十", 21: "二十一", 100: "一百", 105: "一百零五", 110: "一百一十"} {
		if got := chineseNumber(n); got != want {
			t.Errorf("%d: %s, want %s", n, got, want)
		}
	}
	if letters(27, false) != "aa" || roman(14, true) != "XIV" {
		t.Error("letters or roman")
	}
}
