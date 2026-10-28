package demo

import (
	"fmt"
	"html"
	"strings"
)

// A presentation for the slides view (v1.9.7): the family's summer trip
// plan, four 16:9 slides with a title, a coloured bar and a few lines each.
// Written as a minimal but complete PowerPoint file (presentation, master,
// layout, theme, slides), so LibreOffice converts it like a real one.

type slide struct {
	title string
	lines []string
}

var tripSlides = []slide{
	{"暑假家庭旅行计划", []string{"青岛 · 5 天 4 晚", "2026 年 7 月 18 日—22 日", "爸爸、妈妈、乐乐", "本文件为“收好”的演示数据，仅用于演示。"}},
	{"行程", []string{"第 1 天：高铁到青岛，栈桥、中山路", "第 2 天：八大关、第二海水浴场", "第 3 天：崂山（早出发，带好干粮）", "第 4 天：海洋馆、啤酒博物馆", "第 5 天：上午自由活动，下午返程"}},
	{"预算", []string{"交通：高铁往返 3 人 约 1,860 元", "住宿：4 晚 约 2,400 元", "餐饮：约 1,500 元", "门票和其他：约 800 元", "合计：约 6,560 元"}},
	{"出发前准备", []string{"身份证、乐乐的户口本复印件", "防晒霜、泳衣、雨伞", "常用药：晕车药、创可贴", "提前 15 天订高铁票和崂山门票"}},
}

const (
	nsA = `xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"`
	nsR = `xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
	nsP = `xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"`
	rel = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
	ct  = "application/vnd.openxmlformats-officedocument.presentationml."
	// 16:9, in EMU
	slideW, slideH = 12192000, 6858000
)

const emptyTree = `<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>`

func rels(items ...[2]string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i, it := range items {
		fmt.Fprintf(&b, `<Relationship Id="rId%d" Type="%s%s" Target="%s"/>`, i+1, rel, it[0], it[1])
	}
	b.WriteString(`</Relationships>`)
	return b.String()
}

// box is a shape: a text box, or a filled rectangle when text is empty.
func box(id int, x, y, w, h int, fill string, paras []string, size int, bold bool, color string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="形状 %d"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom>`, id, id, x, y, w, h)
	if fill != "" {
		fmt.Fprintf(&b, `<a:solidFill><a:srgbClr val="%s"/></a:solidFill>`, fill)
	} else {
		b.WriteString(`<a:noFill/>`)
	}
	b.WriteString(`</p:spPr><p:txBody><a:bodyPr wrap="square" lIns="0" rIns="0"><a:normAutofit/></a:bodyPr><a:lstStyle/>`)
	if len(paras) == 0 {
		b.WriteString(`<a:p><a:endParaRPr lang="zh-CN"/></a:p>`)
	}
	for _, p := range paras {
		bb := "0"
		if bold {
			bb = "1"
		}
		fmt.Fprintf(&b, `<a:p><a:pPr><a:spcBef><a:spcPts val="900"/></a:spcBef></a:pPr><a:r><a:rPr lang="zh-CN" altLang="en-US" sz="%d" b="%s"><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:latin typeface="+mn-lt"/><a:ea typeface="+mn-ea"/></a:rPr><a:t>%s</a:t></a:r></a:p>`,
			size, bb, color, html.EscapeString(p))
	}
	b.WriteString(`</p:txBody></p:sp>`)
	return b.String()
}

func slideXML(i int, s slide) string {
	var shapes string
	if i == 0 { // the cover: a band across, the title large
		shapes = box(2, 0, 2200000, slideW, 2000000, "1F4E79", nil, 0, false, "") +
			box(3, 900000, 2450000, 10400000, 900000, "", []string{s.title}, 4400, true, "FFFFFF") +
			box(4, 900000, 3300000, 10400000, 700000, "", s.lines[:1], 2400, false, "DCE9F5") +
			box(5, 900000, 4600000, 10400000, 1500000, "", s.lines[1:], 1800, false, "404040")
	} else {
		shapes = box(2, 0, 0, slideW, 260000, "1F4E79", nil, 0, false, "") +
			box(3, 900000, 600000, 10400000, 900000, "", []string{s.title}, 3600, true, "1F4E79") +
			box(4, 900000, 1700000, 10400000, 4400000, "", s.lines, 2200, false, "333333")
	}
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><p:sld ` + nsA + ` ` + nsR + ` ` + nsP + `><p:cSld><p:spTree>` + emptyTree + shapes +
		`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`
}

const themeXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><a:theme ` + nsA + ` name="收好"><a:themeElements>` +
	`<a:clrScheme name="收好"><a:dk1><a:srgbClr val="000000"/></a:dk1><a:lt1><a:srgbClr val="FFFFFF"/></a:lt1><a:dk2><a:srgbClr val="1F4E79"/></a:dk2><a:lt2><a:srgbClr val="E7E6E6"/></a:lt2>` +
	`<a:accent1><a:srgbClr val="4472C4"/></a:accent1><a:accent2><a:srgbClr val="ED7D31"/></a:accent2><a:accent3><a:srgbClr val="A5A5A5"/></a:accent3><a:accent4><a:srgbClr val="FFC000"/></a:accent4>` +
	`<a:accent5><a:srgbClr val="5B9BD5"/></a:accent5><a:accent6><a:srgbClr val="70AD47"/></a:accent6><a:hlink><a:srgbClr val="0563C1"/></a:hlink><a:folHlink><a:srgbClr val="954F72"/></a:folHlink></a:clrScheme>` +
	`<a:fontScheme name="收好"><a:majorFont><a:latin typeface="Arial"/><a:ea typeface="Noto Sans CJK SC"/><a:cs typeface=""/></a:majorFont><a:minorFont><a:latin typeface="Arial"/><a:ea typeface="Noto Sans CJK SC"/><a:cs typeface=""/></a:minorFont></a:fontScheme>` +
	`<a:fmtScheme name="收好"><a:fillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:fillStyleLst>` +
	`<a:lnStyleLst><a:ln w="6350"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln><a:ln w="12700"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln><a:ln w="19050"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln></a:lnStyleLst>` +
	`<a:effectStyleLst><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle></a:effectStyleLst>` +
	`<a:bgFillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:bgFillStyleLst></a:fmtScheme>` +
	`</a:themeElements></a:theme>`

const clrMap = `<p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/>`

// presentation writes slides as a .pptx file.
func presentation(slides []slide) []byte {
	types := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>` +
		`<Override PartName="/ppt/presentation.xml" ContentType="` + ct + `presentation.main+xml"/>` +
		`<Override PartName="/ppt/slideMasters/slideMaster1.xml" ContentType="` + ct + `slideMaster+xml"/>` +
		`<Override PartName="/ppt/slideLayouts/slideLayout1.xml" ContentType="` + ct + `slideLayout+xml"/>` +
		`<Override PartName="/ppt/theme/theme1.xml" ContentType="application/vnd.openxmlformats-officedocument.theme+xml"/>`
	presRels := [][2]string{{"slideMaster", "slideMasters/slideMaster1.xml"}}
	var ids strings.Builder
	files := [][2]string{}
	for i, s := range slides {
		n := i + 1
		types += fmt.Sprintf(`<Override PartName="/ppt/slides/slide%d.xml" ContentType="%sslide+xml"/>`, n, ct)
		presRels = append(presRels, [2]string{"slide", fmt.Sprintf("slides/slide%d.xml", n)})
		fmt.Fprintf(&ids, `<p:sldId id="%d" r:id="rId%d"/>`, 255+n, n+1)
		files = append(files,
			[2]string{fmt.Sprintf("ppt/slides/slide%d.xml", n), slideXML(i, s)},
			[2]string{fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", n), rels([2]string{"slideLayout", "../slideLayouts/slideLayout1.xml"})})
	}
	presRels = append(presRels, [2]string{"theme", "theme/theme1.xml"})
	types += `</Types>`
	pres := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><p:presentation ` + nsA + ` ` + nsR + ` ` + nsP + `>` +
		`<p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst><p:sldIdLst>` + ids.String() + `</p:sldIdLst>` +
		fmt.Sprintf(`<p:sldSz cx="%d" cy="%d"/><p:notesSz cx="6858000" cy="9144000"/></p:presentation>`, slideW, slideH)
	master := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><p:sldMaster ` + nsA + ` ` + nsR + ` ` + nsP + `><p:cSld><p:bg><p:bgPr><a:solidFill><a:srgbClr val="FFFFFF"/></a:solidFill><a:effectLst/></p:bgPr></p:bg><p:spTree>` + emptyTree + `</p:spTree></p:cSld>` +
		clrMap + `<p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst></p:sldMaster>`
	layout := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><p:sldLayout ` + nsA + ` ` + nsR + ` ` + nsP + ` type="blank"><p:cSld name="空白"><p:spTree>` + emptyTree + `</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`
	return zipFiles(append([][2]string{
		{"[Content_Types].xml", types},
		{"_rels/.rels", rels([2]string{"officeDocument", "ppt/presentation.xml"})},
		{"ppt/presentation.xml", pres},
		{"ppt/_rels/presentation.xml.rels", rels(presRels...)},
		{"ppt/slideMasters/slideMaster1.xml", master},
		{"ppt/slideMasters/_rels/slideMaster1.xml.rels", rels([2]string{"slideLayout", "../slideLayouts/slideLayout1.xml"}, [2]string{"theme", "../theme/theme1.xml"})},
		{"ppt/slideLayouts/slideLayout1.xml", layout},
		{"ppt/slideLayouts/_rels/slideLayout1.xml.rels", rels([2]string{"slideMaster", "../slideMasters/slideMaster1.xml"})},
		{"ppt/theme/theme1.xml", themeXML},
	}, files...))
}
