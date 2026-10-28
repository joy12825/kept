package parser

import (
	"errors"
	"strings"
	"testing"

	"kept/server/internal/doctest"
)

// TestOdtFromAnotherApplication: an .odt textutil wrote, the same
// content as the .doc: the two read alike.
func TestOdtFromAnotherApplication(t *testing.T) {
	o, err := ReadWord(writeFile(t, "a.odt", doctest.HandbookOdt()))
	if err != nil {
		t.Fatal(err)
	}
	d, err := ReadWord(writeFile(t, "a.doc", doctest.HandbookDoc()))
	if err != nil {
		t.Fatal(err)
	}
	// textutil writes the .doc's list numbers as text, the .odt's as lists
	plain := func(b WordBlock) string {
		s := texts(b)
		if b.List != nil {
			s = "\t" + b.List.Marker + "\t" + s
		}
		return s
	}
	var a, b []string
	for _, x := range o.Blocks {
		if x.Kind == "paragraph" {
			a = append(a, plain(x))
		}
	}
	for _, x := range d.Blocks {
		if x.Kind == "paragraph" {
			b = append(b, plain(x))
		}
	}
	if strings.Join(a, "|") != strings.Join(b, "|") {
		t.Errorf("odt:\n%s\ndoc:\n%s", strings.Join(a, "\n"), strings.Join(b, "\n"))
	}
	if l := o.Blocks[len(o.Blocks)-1].Runs; l[1].Link != "https://www.example.com/safety" || !l[1].Underline {
		t.Errorf("link = %+v", l)
	}
	if o.Blocks[0].Runs[0].Size != 24 || !o.Blocks[0].Runs[0].Bold {
		t.Errorf("title = %+v", o.Blocks[0])
	}
	if tbl := o.Blocks[4]; tbl.Kind != "table" || len(tbl.Rows) != 4 || texts(tbl.Rows[1][2].Blocks[0]) != "白天优先" {
		t.Errorf("table = %+v", tbl)
	}
}

const odtStyles = `<style:default-style style:family="paragraph"><style:text-properties fo:font-size="10.5pt"/></style:default-style>` +
	`<style:style style:name="Standard" style:family="paragraph"/>` +
	`<style:style style:name="Heading" style:family="paragraph" style:parent-style-name="Standard"><style:text-properties fo:font-weight="bold"/></style:style>` +
	`<style:style style:name="Heading_20_1" style:display-name="Heading 1" style:family="paragraph" style:parent-style-name="Heading" style:default-outline-level="1"><style:text-properties fo:font-size="18pt"/></style:style>` +
	`<style:style style:name="Title" style:family="paragraph" style:parent-style-name="Heading"><style:paragraph-properties fo:text-align="center"/></style:style>` +
	`<style:style style:name="Loop1" style:family="paragraph" style:parent-style-name="Loop2"/>` +
	`<style:style style:name="Loop2" style:family="paragraph" style:parent-style-name="Loop1"/>` +
	`<style:style style:name="Strong" style:family="text"><style:text-properties fo:font-weight="bold" fo:color="#c00000"/></style:style>` +
	`<text:list-style style:name="Numbers">` +
	`<text:list-level-style-number text:level="1" style:num-format="一, 二, 三, ..." style:num-suffix="、"/>` +
	`<text:list-level-style-number text:level="2" style:num-format="1" style:num-prefix="(" style:num-suffix=")" text:display-levels="1"/>` +
	`<text:list-level-style-number text:level="3" style:num-format="a" style:num-suffix="." text:display-levels="2"/>` +
	`</text:list-style>` +
	`<text:list-style style:name="Bullets"><text:list-level-style-bullet text:level="1" text:bullet-char="` + "" + `"/></text:list-style>`

const odtOutline = `<text:outline-style style:name="Outline">` +
	`<text:outline-level-style text:level="1" style:num-format="1" style:num-prefix="第" style:num-suffix="章"/>` +
	`<text:outline-level-style text:level="2" style:num-format="1" text:display-levels="2"/>` +
	`</text:outline-style>`

const odtAutomatic = `<style:style style:name="T1" style:family="text"><style:text-properties fo:font-style="italic" style:text-underline-style="solid" fo:background-color="#ffff00"/></style:style>` +
	`<style:style style:name="T2" style:family="text"><style:text-properties text:display="none"/></style:style>` +
	`<style:style style:name="T3" style:family="text"><style:text-properties style:text-position="33% 58%"/></style:style>` +
	`<style:style style:name="T4" style:family="text"><style:text-properties style:text-line-through-style="solid" fo:font-size="14pt"/></style:style>` +
	`<style:style style:name="P1" style:family="paragraph" style:parent-style-name="Standard"><style:paragraph-properties fo:text-align="end"/></style:style>`

func richOdt() doctest.Odt {
	p := func(s string) string { return `<text:p>` + s + `</text:p>` }
	cell := func(attrs, s string) string { return `<table:table-cell` + attrs + `>` + p(s) + `</table:table-cell>` }
	nested := `<table:table><table:table-row>` + cell("", "第四层") + `</table:table-row></table:table>`
	for i := 0; i < 3; i++ {
		nested = `<table:table><table:table-row><table:table-cell>` + nested + `</table:table-cell></table:table-row></table:table>`
	}
	return doctest.Odt{
		Styles: odtStyles, Outline: odtOutline, Automatic: odtAutomatic,
		Pictures: map[string][]byte{"valve.png": pngBytes},
		Body: `<text:sequence-decls/>` +
			`<text:p text:style-name="Title">家庭应急手册</text:p>` +
			`<text:h text:outline-level="1">停电</text:h>` +
			`<text:h text:outline-level="2">先看范围</text:h>` +
			`<text:h text:outline-level="2">再合闸</text:h>` +
			`<text:h text:outline-level="1">停水</text:h>` +
			`<text:p text:style-name="Heading_20_1">按样式的标题</text:p>` +
			`<text:p text:style-name="Loop1">样式成环</text:p>` +
			`<text:p text:style-name="P1">先保证<text:span text:style-name="Strong">人身安全</text:span>，` +
			`<text:span text:style-name="T1">每年</text:span>核对<text:s text:c="3"/>一次<text:tab/>H<text:span text:style-name="T3">2</text:span>` +
			`<text:span text:style-name="T2">看不见</text:span><text:hidden-text text:string-value="x">也看不见</text:hidden-text>` +
			`<text:change text:change-id="c1"/><text:span text:style-name="T4">旧的</text:span><text:line-break/>第二行` +
			`<text:note text:note-class="footnote"><text:note-citation>1</text:note-citation><text:note-body>` + p("脚注内容") + `</text:note-body></text:note>` +
			`。</text:p>` +
			`<text:tracked-changes><text:changed-region text:id="c1"><text:deletion>` + p("删掉的段落") + `</text:deletion></text:changed-region></text:tracked-changes>` +
			`<text:list text:style-name="Numbers"><text:list-item>` + p("查看停电范围") +
			`<text:list><text:list-item>` + p("整栋楼") + `<text:list><text:list-item>` + p("走廊灯") + `</text:list-item></text:list></text:list-item>` +
			`<text:list-item>` + p("只有家里") + `</text:list-item></text:list></text:list-item>` +
			`<text:list-item>` + p("合上总闸") + `</text:list-item></text:list>` +
			`<text:p>插在中间</text:p>` +
			`<text:list text:style-name="Numbers" text:continue-numbering="true"><text:list-item>` + p("接着编号") + `</text:list-item></text:list>` +
			`<text:list text:style-name="Numbers"><text:list-item text:start-value="7">` + p("从七开始") + `</text:list-item></text:list>` +
			`<text:list text:style-name="Bullets"><text:list-item>` + p("手电筒") + `</text:list-item><text:list-header>` + p("不是条目") + `</text:list-header></text:list>` +
			`<table:table><table:table-column/><table:table-column/><table:table-column/>` +
			`<table:table-header-rows><table:table-row>` + cell("", "称呼") + cell("", "电话") + cell("", "备注") + `</table:table-row></table:table-header-rows>` +
			`<table:table-row>` + cell("", "爸爸") + cell("", "138") + cell(` table:number-rows-spanned="2"`, "白天优先") + `</table:table-row>` +
			`<table:table-row>` + cell("", "妈妈") + cell("", "139") + `<table:covered-table-cell/></table:table-row>` +
			`<table:table-row>` + cell(` table:number-columns-spanned="2"`, "物业") + `<table:covered-table-cell/>` + cell("", "24 小时") + `</table:table-row>` +
			`</table:table>` +
			nested +
			`<text:p>看<text:a xlink:href="https://example.com/safety">安全指南</text:a>，<text:a xlink:href="javascript:alert(1)">别点</text:a>` +
			`<draw:frame svg:width="2.54cm" svg:height="1in"><draw:image xlink:href="Pictures/valve.png"/></draw:frame>` +
			`<draw:frame><draw:image xlink:href="Pictures/chart.emf"/></draw:frame>` +
			`<draw:frame><draw:text-box>` + p("框里的字") + `</draw:text-box></draw:frame>` +
			`<text:note text:note-class="endnote"><text:note-body>` + p("尾注") + `</text:note-body></text:note></text:p>` +
			`<text:table-of-content><text:index-title>` + p("不显示的标题") + `</text:index-title><text:index-body>` + p("目录 1") + `</text:index-body></text:table-of-content>` +
			`<text:section>` + p("节里的段落") + `</text:section>` +
			p("") + p(""),
	}
}

func TestOdtBlocks(t *testing.T) {
	src := writeFile(t, "rich.odt", richOdt().Build())
	d, err := ReadWord(src)
	if err != nil {
		t.Fatal(err)
	}
	if d.BaseSize != 10.5 {
		t.Errorf("base size = %v", d.BaseSize)
	}
	var lines []string
	for _, b := range d.Blocks {
		s := b.Kind + ":" + texts(b)
		if b.Heading > 0 {
			s += " h" + string(rune('0'+b.Heading))
		}
		if b.List != nil {
			s += " [" + b.List.Marker + "]"
		}
		if b.Align != "" {
			s += " " + b.Align
		}
		lines = append(lines, s)
	}
	want := []string{
		"paragraph:家庭应急手册 h1 center",
		"paragraph:停电 h1 [第1章]", "paragraph:先看范围 h2 [1.1]", "paragraph:再合闸 h2 [1.2]", "paragraph:停水 h1 [第2章]",
		"paragraph:按样式的标题 h1", "paragraph:样式成环",
		"paragraph:先保证人身安全，每年核对   一次\tH2旧的\n第二行。 right",
		"paragraph:查看停电范围 [一、]", "paragraph:整栋楼 [(1)]", "paragraph:走廊灯 [1.a.]", "paragraph:只有家里 [(2)]", "paragraph:合上总闸 [二、]",
		"paragraph:插在中间", "paragraph:接着编号 [三、]", "paragraph:从七开始 [七、]",
		"paragraph:手电筒 [•]", "paragraph:不是条目",
		"table:", "table:",
		"paragraph:看安全指南，别点[图片]", "box:",
		"paragraph:目录 1", "paragraph:节里的段落",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("blocks:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	runs := d.Blocks[7].Runs
	find := func(text string) WordRun {
		for _, r := range runs {
			if r.Text == text {
				return r
			}
		}
		t.Fatalf("no run %q in %+v", text, runs)
		return WordRun{}
	}
	if r := find("人身安全"); !r.Bold || r.Color != "C00000" {
		t.Errorf("character style = %+v", r)
	}
	if r := find("每年"); !r.Italic || !r.Underline || r.Highlight != "FFFF00" {
		t.Errorf("automatic style = %+v", r)
	}
	if r := find("2"); r.Vert != "super" {
		t.Errorf("raised = %+v", r)
	}
	if r := find("旧的"); !r.Strike || r.Size != 14 {
		t.Errorf("struck, bigger = %+v", r)
	}
	if r := runs[len(runs)-2]; r.Note != 1 {
		t.Errorf("footnote = %+v", runs)
	}
	tbl := d.Blocks[18]
	if tbl.HeaderRows != 1 || len(tbl.Rows) != 4 || tbl.Rows[1][2].RowSpan != 2 || len(tbl.Rows[2]) != 2 || tbl.Rows[3][0].ColSpan != 2 || len(tbl.Rows[3]) != 2 {
		t.Errorf("table = %+v", tbl)
	}
	deep := d.Blocks[19]
	for i := 0; i < 3; i++ {
		deep = deep.Rows[0][0].Blocks[0]
	}
	if deep.Kind != "paragraph" || texts(deep) != "第四层" {
		t.Errorf("a table four deep is its text: %+v", deep)
	}
	link := d.Blocks[20].Runs
	if link[1].Link != "https://example.com/safety" || link[3].Link != "" {
		t.Errorf("links = %+v", link)
	}
	var pics []WordRun
	for _, r := range link {
		if r.Image > 0 || r.Text == wordPicture {
			pics = append(pics, r)
		}
	}
	if len(pics) != 2 || pics[0].Image != 1 || pics[1].Text != wordPicture {
		t.Errorf("pictures = %+v", pics)
	}
	if len(d.Images) != 1 || d.Images[0].Width != 96 || d.Images[0].Height != 96 || d.Images[0].Part != "Pictures/valve.png" {
		t.Errorf("images = %+v", d.Images)
	}
	if texts(d.Blocks[21].Blocks[0]) != "框里的字" {
		t.Errorf("text box = %+v", d.Blocks[21])
	}
	if len(d.Notes) != 2 || texts(d.Notes[0].Blocks[0]) != "脚注内容" || texts(d.Notes[1].Blocks[0]) != "尾注" {
		t.Errorf("notes = %+v", d.Notes)
	}
	img, mime, err := ReadWordImage(src, 1)
	if err != nil || mime != "image/png" || string(img) != string(pngBytes) {
		t.Errorf("picture = %s %v", mime, err)
	}
}

func TestOdtRefused(t *testing.T) {
	o := richOdt()
	o.Body = `<text:p>x</text:p>`
	// another OpenDocument package (a spreadsheet) is no text
	sheet := strings.Replace(string(o.Build()), "opendocument.text", "opendocument.spreadsheet", 1)
	if _, err := ReadWord(writeFile(t, "s.odt", []byte(sheet))); !errors.Is(err, ErrNotWord) {
		t.Errorf("a spreadsheet: %v", err)
	}
	if _, err := ReadWord(writeFile(t, "x.odt", []byte("PK\x03\x04 broken"))); !errors.Is(err, ErrNotWord) {
		t.Errorf("broken: %v", err)
	}
}
