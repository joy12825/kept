package demo

import (
	"bytes"
	"image"
	"image/color"
	"image/png"

	"kept/server/internal/docxtest"
)

// The reading view (v1.9.8): a family emergency handbook in Word, with what
// the view lays out — headings, numbered steps, bullets, a table with
// merged cells, bold and coloured words, a link, a footnote, a picture and
// a text box.

const handbookStyles = `<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:eastAsia="宋体"/><w:sz w:val="21"/></w:rPr></w:rPrDefault></w:docDefaults>` +
	`<w:style w:type="paragraph" w:default="1" w:styleId="a"><w:name w:val="Normal"/></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="a"/><w:pPr><w:jc w:val="center"/></w:pPr><w:rPr><w:b/><w:sz w:val="40"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="1"><w:name w:val="heading 1"/><w:basedOn w:val="a"/><w:rPr><w:b/><w:color w:val="1F4E79"/><w:sz w:val="32"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="2"><w:name w:val="heading 2"/><w:basedOn w:val="a"/><w:rPr><w:b/><w:sz w:val="26"/></w:rPr></w:style>` +
	`<w:style w:type="character" w:styleId="Strong"><w:name w:val="Strong"/><w:rPr><w:b/></w:rPr></w:style>`

const handbookNumbering = `<w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/></w:lvl>` +
	`<w:lvl w:ilvl="1"><w:start w:val="1"/><w:numFmt w:val="lowerLetter"/><w:lvlText w:val="%2)"/></w:lvl></w:abstractNum>` +
	`<w:abstractNum w:abstractNumId="1"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:lvlText w:val="` + "" + `"/></w:lvl></w:abstractNum>` +
	`<w:abstractNum w:abstractNumId="2"><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="chineseCounting"/><w:lvlText w:val="%1、"/></w:lvl></w:abstractNum>` +
	`<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num><w:num w:numId="2"><w:abstractNumId w:val="1"/></w:num><w:num w:numId="3"><w:abstractNumId w:val="2"/></w:num>` +
	`<w:num w:numId="4"><w:abstractNumId w:val="0"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="1"/></w:lvlOverride></w:num>`

func hp(style, text string) string {
	return `<w:p><w:pPr><w:pStyle w:val="` + style + `"/></w:pPr><w:r><w:t>` + text + `</w:t></w:r></w:p>`
}

func li(num, lvl, text string) string {
	return `<w:p><w:pPr><w:numPr><w:ilvl w:val="` + lvl + `"/><w:numId w:val="` + num + `"/></w:numPr></w:pPr><w:r><w:t xml:space="preserve">` + text + `</w:t></w:r></w:p>`
}

func cell(props, body string) string {
	return `<w:tc><w:tcPr>` + props + `</w:tcPr>` + body + `</w:tc>`
}

// handbook is the document.
func handbook() []byte {
	b := hp("Title", "家庭应急手册") +
		`<w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:rPr><w:color w:val="7F7F7F"/></w:rPr><w:t>2026 年 10 月修订 · 贴在冰箱门上一份</w:t></w:r></w:p>` +
		hp("1", "一、停电") +
		li("1", "0", "先看是不是只有我们家停电：看看楼道灯和邻居家。") +
		li("1", "0", "只有我们家停电时，检查电闸：") +
		li("1", "1", "空气开关跳闸：关掉大功率电器后再合上；") +
		li("1", "1", "反复跳闸：不要再合，打电话给物业。") +
		li("1", "0", "整栋楼停电：打国家电网 95598 问恢复时间。") +
		`<w:p><w:r><w:t xml:space="preserve">冰箱里的东西：</w:t></w:r><w:r><w:rPr><w:rStyle w:val="Strong"/><w:color w:val="C00000"/></w:rPr><w:t>不要频繁开门</w:t></w:r>` +
		`<w:r><w:t xml:space="preserve">，关着门可以保冷 4 小时左右</w:t></w:r><w:r><w:rPr><w:vertAlign w:val="superscript"/></w:rPr><w:footnoteReference w:id="1"/></w:r><w:r><w:t>。</w:t></w:r></w:p>` +
		hp("1", "二、漏水") +
		li("4", "0", "关总水阀（在厨房水槽下面，红色把手）。") +
		li("4", "0", "拍照留证据，再通知楼下邻居。") +
		li("4", "0", "联系物业，必要时找保险公司报案。") +
		`<w:p><w:r><w:t>总水阀的位置：</w:t></w:r>` + docxtest.Image("rValve", 240, 160) +
		`<w:r><mc:AlternateContent><mc:Choice Requires="wps"><w:drawing><wp:anchor><wps:wsp><wps:txbx><w:txbxContent>` +
		`<w:p><w:r><w:rPr><w:b/></w:rPr><w:t>提醒：</w:t></w:r><w:r><w:t>出门旅行超过三天，记得关总水阀。</w:t></w:r></w:p>` +
		`</w:txbxContent></wps:txbx></wps:wsp></wp:anchor></w:drawing></mc:Choice><mc:Fallback/></mc:AlternateContent></w:r></w:p>` +
		hp("1", "三、常用电话") +
		`<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/></w:tblPr><w:tblGrid><w:gridCol w:w="2000"/><w:gridCol w:w="2400"/><w:gridCol w:w="3000"/></w:tblGrid>` +
		`<w:tr><w:trPr><w:tblHeader/></w:trPr>` + cell("", docxtest.P("类别")) + cell("", docxtest.P("名称")) + cell("", docxtest.P("电话")) + `</w:tr>` +
		`<w:tr>` + cell(`<w:vMerge w:val="restart"/>`, docxtest.P("小区")) + cell("", docxtest.P("物业前台")) + cell("", docxtest.P("021-5555 0101")) + `</w:tr>` +
		`<w:tr>` + cell(`<w:vMerge/>`, docxtest.P("")) + cell("", docxtest.P("物业维修（24 小时）")) + cell("", docxtest.P("021-5555 0102")) + `</w:tr>` +
		`<w:tr>` + cell("", docxtest.P("公共服务")) + cell("", docxtest.P("国家电网")) + cell("", docxtest.P("95598")) + `</w:tr>` +
		`<w:tr>` + cell(`<w:gridSpan w:val="2"/>`, docxtest.P("急救 / 火警 / 报警")) + cell("", docxtest.P("120 / 119 / 110")) + `</w:tr></w:tbl>` +
		hp("1", "四、家里常备") +
		li("2", "0", "手电筒和备用电池（玄关柜第二层）") +
		li("2", "0", "急救包：创可贴、碘伏、纱布、体温计") +
		li("2", "0", "每人一瓶饮用水，每半年换一次") +
		hp("2", "备注") +
		li("3", "0", "本手册的电子版存在“收好”里，手机上也能看。") +
		`<w:p><w:r><w:t xml:space="preserve">更多应急知识见 </w:t></w:r><w:hyperlink r:id="rLink"><w:r><w:rPr><w:color w:val="0563C1"/><w:u w:val="single"/></w:rPr><w:t>应急管理部网站</w:t></w:r></w:hyperlink><w:r><w:t>。</w:t></w:r></w:p>` +
		docxtest.P("本文件为“收好”的演示数据，仅用于演示。")
	return docxtest.Doc{
		Body: b, Styles: handbookStyles, Numbering: handbookNumbering,
		Footnotes: `<w:footnote w:type="separator" w:id="-1"><w:p><w:r><w:separator/></w:r></w:p></w:footnote>` +
			`<w:footnote w:id="1"><w:p><w:r><w:rPr><w:vertAlign w:val="superscript"/></w:rPr><w:footnoteRef/></w:r><w:r><w:t xml:space="preserve"> 时间因冰箱和天气而不同，夏天要短一些。</w:t></w:r></w:p></w:footnote>`,
		Rels: []docxtest.Rel{
			{ID: "rValve", Type: "image", Target: "media/valve.png"},
			{ID: "rLink", Type: "hyperlink", Target: "https://www.mem.gov.cn/", External: true},
		},
		Media: map[string][]byte{"valve.png": valvePicture()},
	}.Build()
}

// valvePicture is a drawing of the sink cabinet with the red valve handle.
func valvePicture() []byte {
	const w, h = 480, 320
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{236, 230, 220, 255} // the cabinet's inside
			if x < 20 || x > w-20 || y < 20 || y > h-20 {
				c = color.RGBA{150, 120, 90, 255} // its frame
			}
			if x > 220 && x < 260 && y > 20 && y < h-20 {
				c = color.RGBA{170, 170, 175, 255} // the pipe
			}
			if (x-240)*(x-240)+(y-150)*(y-150) < 18*18 {
				c = color.RGBA{120, 120, 125, 255} // the valve
			}
			if x > 250 && x < 340 && y > 140 && y < 160 {
				c = color.RGBA{210, 40, 40, 255} // the red handle
			}
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}
