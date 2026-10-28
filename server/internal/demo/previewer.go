package demo

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"strings"
	"time"

	"kept/server/internal/doctest"
	"kept/server/internal/ingest"
	"kept/server/internal/msgtest"
	"kept/server/internal/ofdtest"
	"kept/server/internal/tenant"
)

// The previewer (v1.9.1): a note written in Markdown, a short recording of
// 乐乐 singing and a photo, each shown the way it is: typeset, played,
// looked at. Generated here, so the demo needs no files.

const networkNote = `# 家里的网络和设备

记下来，免得每次都要重新找。**密码不写在这里**，在“爸爸”的手机里。

## 路由器

- 型号：小米 AX3000，放在客厅电视柜里
- 管理地址：192.168.31.1
- 宽带：中国电信 500M，账号见宽带合同

## 常见问题

1. 网络慢：先重启光猫，再重启路由器
2. 打印机找不到：确认它连的是 *家里的 5G* 网络
3. 电视投屏不了：手机和电视要在同一个网络

> 光猫在门口弱电箱里，别拔错电源。

| 设备 | 位置 | 备注 |
| --- | --- | --- |
| 光猫 | 弱电箱 | 电信提供 |
| 路由器 | 客厅 | 2023 年买的 |
| NAS | 书房 | 存照片 |

售后电话见 [小米服务](https://www.mi.com/service)。

本文件为“收好”的演示数据，仅用于演示。
`

// demoPreviewer adds them once; it returns how many.
func demoPreviewer(ctx context.Context, d Deps, scope tenant.Scope) (int, error) {
	if d.Ingest == nil {
		return 0, nil
	}
	ws := scope.WorkspaceID()
	now := time.Now()
	var kid int64
	_ = d.DB.QueryRowContext(ctx, `SELECT id FROM persons WHERE workspace_id = ? AND name = '乐乐'`, ws).Scan(&kid)
	items := []struct {
		name, title string
		data        []byte
		person      int64
		next        []byte // a second version (v1.9.7: Word side by side)
		nextLabel   string
	}{
		{"家里的网络和设备.md", "家里的网络和设备", []byte(networkNote), 0, nil, ""},
		{"乐乐唱歌.wav", "乐乐唱《小星星》", melody(), kid, nil, ""},
		{"阳台的花.jpg", "阳台的花", photo(), 0, nil, ""},
		// v1.9.3: an e-invoice, a workbook and a zip
		{"餐饮发票.ofd", "餐饮电子发票", ofdtest.Invoice{Number: "24312000000123456789", Date: now.AddDate(0, 0, -6).Format("2006年01月02日"),
			Seller: "上海某某餐饮有限公司", Buyer: "王小明", Item: "*餐饮服务*餐费", Amount: "286.00"}.Build(), 0, nil, ""},
		{"家庭账本.xlsx", "家庭账本", ledger(now), 0, nil, ""},
		{"报销收据.zip", "报销收据（压缩包）", receipts(), 0, nil, ""},
		// v1.9.7: an Outlook e-mail, a Word draft with a revision, slides
		{"物业费缴费通知.msg", "物业费缴费通知", outlookNotice(now), 0, nil, ""},
		{"装修合同.docx", "装修合同", wordDoc(renovation(68000, 45)), 0, wordDoc(renovation(65000, 50)), "砍价后的第二稿"},
		{"暑假旅行计划.pptx", "暑假家庭旅行计划", presentation(tripSlides), 0, nil, ""}, // 一张一张地看幻灯片
		// v1.9.8: a Word document for the reading view
		{"家庭应急手册.docx", "家庭应急手册", handbook(), 0, nil, ""},
		// v1.11.1: the same handbook as an old Word document and an OpenDocument text
		{"家庭应急手册（旧版）.doc", "家庭应急手册（旧版 Word）", doctest.HandbookDoc(), 0, nil, ""},
		{"家庭应急手册.odt", "家庭应急手册（OpenDocument）", doctest.HandbookOdt(), 0, nil, ""},
		// v1.11.3: e-invoices with an electronic seal: one as signed, one changed afterwards
		{"打车发票（带电子签章）.ofd", "打车电子发票（签章有效）", signedInvoice(now, ""), 0, nil, ""},
		{"打车发票（签章后被改）.ofd", "打车电子发票（签章后被改）", signedInvoice(now, "amount"), 0, nil, ""},
	}
	added := 0
	for _, it := range items {
		var done int
		if err := d.DB.QueryRowContext(ctx, `SELECT count(*) FROM documents WHERE workspace_id = ? AND original_filename = ?`, ws, it.name).Scan(&done); err != nil {
			return added, err
		}
		if done > 0 {
			continue
		}
		id, err := d.Ingest.Import(ctx, scope, it.name, bytes.NewReader(it.data), ingest.Overrides{Title: it.title, PersonID: it.person}, ingest.Upload{})
		if err != nil {
			return added, err
		}
		if it.next != nil {
			if _, err := d.Ingest.AddVersion(ctx, scope, id, it.name, bytes.NewReader(it.next), it.nextLabel); err != nil {
				return added, err
			}
		}
		added++
	}
	return added, nil
}

// signedInvoice is a taxi e-invoice signed with a demo seal (a self-signed
// certificate); tamper changes it after signing (ofdtest.Signer).
func signedInvoice(now time.Time, tamper string) []byte {
	s, err := ofdtest.NewSigner("上海某某出租汽车有限公司", "上海某某出租汽车有限公司", "上海某某出租汽车有限公司发票专用章（演示）")
	if err != nil {
		return nil
	}
	s.Vector, s.Tamper, s.Time = true, tamper, now.AddDate(0, 0, -3)
	number := "24312000000555000001"
	if tamper != "" {
		number = "24312000000555000002"
	}
	return ofdtest.Invoice{Number: number, Date: now.AddDate(0, 0, -3).Format("2006年01月02日"), Seller: "上海某某出租汽车有限公司",
		Buyer: "王小明", Item: "*运输服务*客运服务费", Amount: "58.00", Sign: s}.Build()
}

// melody is the first line of 小星星 as a WAV: 8 kHz, 16-bit mono, a soft
// sine with a short fade on each note.
func melody() []byte {
	const rate = 8000
	notes := []float64{261.6, 261.6, 392.0, 392.0, 440.0, 440.0, 392.0, 0, 349.2, 349.2, 329.6, 329.6, 293.7, 293.7, 261.6}
	var pcm []int16
	for _, f := range notes {
		n := rate * 2 / 5 // 0.4 s
		for i := 0; i < n; i++ {
			v := 0.0
			if f > 0 {
				env := math.Min(1, math.Min(float64(i)/400, float64(n-i)/800))
				v = math.Sin(2*math.Pi*f*float64(i)/rate) * env * 0.3
			}
			pcm = append(pcm, int16(v*math.MaxInt16))
		}
	}
	var b bytes.Buffer
	size := uint32(len(pcm) * 2)
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, 36+size)
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(rate), uint32(rate * 2), uint16(2), uint16(16)} {
		binary.Write(&b, binary.LittleEndian, v)
	}
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, size)
	binary.Write(&b, binary.LittleEndian, pcm)
	return b.Bytes()
}

// photo is a picture of flowers on a balcony, drawn: sky, railing, a pot
// and a few blooms.
func photo() []byte {
	const w, h = 960, 720
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			t := float64(y) / h
			c := color.RGBA{uint8(150 + 80*t), uint8(195 + 40*t), uint8(235 - 20*t), 255} // sky
			if y > h*3/5 {
				c = color.RGBA{196, 178, 150, 255} // the floor
			}
			if y > h/2 && y < h*3/5 || (y > h/2 && x%80 < 8 && y < h*3/5+40) {
				c = color.RGBA{90, 90, 95, 255} // the railing
			}
			img.Set(x, y, c)
		}
	}
	disc := func(cx, cy, r int, c color.RGBA) {
		for y := cy - r; y <= cy+r; y++ {
			for x := cx - r; x <= cx+r; x++ {
				if (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r && x >= 0 && y >= 0 && x < w && y < h {
					img.Set(x, y, c)
				}
			}
		}
	}
	for y := h*3/5 - 20; y < h-60; y++ { // the pot
		in := (y - h*3/5) / 6
		for x := 380 + in; x < 580-in; x++ {
			img.Set(x, y, color.RGBA{184, 98, 62, 255})
		}
	}
	for _, s := range [][2]int{{420, 300}, {480, 250}, {540, 310}, {455, 360}, {520, 380}} {
		for y := s[1]; y < h*3/5-20; y++ { // stems
			img.Set(s[0], y, color.RGBA{60, 130, 70, 255})
			img.Set(s[0]+1, y, color.RGBA{60, 130, 70, 255})
		}
	}
	for i, s := range [][2]int{{420, 300}, {480, 250}, {540, 310}, {455, 360}, {520, 380}} {
		petal := []color.RGBA{{230, 90, 120, 255}, {245, 190, 60, 255}, {200, 110, 210, 255}}[i%3]
		for a := 0; a < 6; a++ {
			ang := float64(a) * math.Pi / 3
			disc(s[0]+int(22*math.Cos(ang)), s[1]+int(22*math.Sin(ang)), 16, petal)
		}
		disc(s[0], s[1], 12, color.RGBA{250, 230, 120, 255})
	}
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, &jpeg.Options{Quality: 85})
	return b.Bytes()
}

func zipFiles(files [][2]string) []byte {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, f := range files {
		w, _ := z.Create(f[0])
		w.Write([]byte(f[1]))
	}
	z.Close()
	return b.Bytes()
}

// ledger is a workbook with two sheets: this year's spending by month (a
// date column, a merged title) and the income.
func ledger(now time.Time) []byte {
	var strs []string
	idx := map[string]int{}
	str := func(v string) string {
		if _, ok := idx[v]; !ok {
			idx[v] = len(strs)
			strs = append(strs, v)
		}
		return fmt.Sprintf(`t="s"><v>%d</v>`, idx[v])
	}
	cell := func(ref, body string) string { return `<c r="` + ref + `" ` + body + `</c>` }
	num := func(ref string, v float64, style int) string {
		return fmt.Sprintf(`<c r="%s" s="%d"><v>%s</v></c>`, ref, style, strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), "."))
	}
	var rows strings.Builder
	rows.WriteString(`<row r="1">` + cell("A1", str(fmt.Sprintf("%d 年家庭支出", now.Year()))) + `</row>`)
	rows.WriteString(`<row r="2">` + cell("A2", str("日期")) + cell("B2", str("类别")) + cell("C2", str("项目")) + cell("D2", str("金额")) + `</row>`)
	items := [][3]string{{"水电", "电费", "212.40"}, {"水电", "水费", "58.00"}, {"餐饮", "家庭聚餐", "286.00"}, {"孩子", "幼儿园学费", "3200.00"}, {"交通", "加油", "350.00"}, {"医疗", "体检", "680.00"}}
	excel := func(t time.Time) float64 { return t.Sub(time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)).Hours() / 24 }
	for i, it := range items {
		r := i + 3
		day := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -i, 4)
		var amount float64
		fmt.Sscan(it[2], &amount)
		rows.WriteString(fmt.Sprintf(`<row r="%d">`, r) + num(fmt.Sprintf("A%d", r), math.Floor(excel(day)), 1) +
			cell(fmt.Sprintf("B%d", r), str(it[0])) + cell(fmt.Sprintf("C%d", r), str(it[1])) + num(fmt.Sprintf("D%d", r), amount, 0) + `</row>`)
	}
	total := len(items) + 3
	rows.WriteString(fmt.Sprintf(`<row r="%d">`, total) + cell(fmt.Sprintf("C%d", total), str("合计")) +
		fmt.Sprintf(`<c r="D%d"><f>SUM(D3:D%d)</f><v>4786.4</v></c>`, total, total-1) + `</row>`)
	spend := `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` + rows.String() + `</sheetData><mergeCells count="1"><mergeCell ref="A1:D1"/></mergeCells></worksheet>`
	income := `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` +
		`<row r="1">` + cell("A1", str("来源")) + cell("B1", str("月份")) + cell("C1", str("金额")) + `</row>` +
		`<row r="2">` + cell("A2", str("工资")) + cell("B2", str("每月")) + num("C2", 18000, 0) + `</row>` +
		`<row r="3">` + cell("A3", str("房租收入")) + cell("B3", str("每月")) + num("C3", 3200, 0) + `</row></sheetData></worksheet>`
	var sst strings.Builder
	for _, v := range strs {
		sst.WriteString("<si><t>" + v + "</t></si>")
	}
	main := "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	return zipFiles([][2]string{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/worksheets/sheet2.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/sharedStrings.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="` + main + `" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="支出" sheetId="1" r:id="rId1"/><sheet name="收入" sheetId="2" r:id="rId2"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet2.xml"/><Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings" Target="sharedStrings.xml"/><Relationship Id="rId4" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`},
		{"xl/styles.xml", `<?xml version="1.0" encoding="UTF-8"?><styleSheet xmlns="` + main + `"><fonts count="1"><font><sz val="11"/><name val="Calibri"/></font></fonts><fills count="1"><fill><patternFill patternType="none"/></fill></fills><borders count="1"><border/></borders><cellStyleXfs count="1"><xf numFmtId="0"/></cellStyleXfs><cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/><xf numFmtId="14" fontId="0" fillId="0" borderId="0" applyNumberFormat="1"/></cellXfs></styleSheet>`},
		{"xl/sharedStrings.xml", `<?xml version="1.0" encoding="UTF-8"?><sst xmlns="` + main + `">` + sst.String() + `</sst>`},
		{"xl/worksheets/sheet1.xml", `<?xml version="1.0" encoding="UTF-8"?>` + spend},
		{"xl/worksheets/sheet2.xml", `<?xml version="1.0" encoding="UTF-8"?>` + income},
	})
}

// receipts is a zip of a few receipts, in a folder, with a note.
func receipts() []byte {
	return zipFiles([][2]string{
		{"报销/说明.txt", "十月出差的报销收据，交给财务前先核对一遍。\n本文件为“收好”的演示数据，仅用于演示。"},
		{"报销/停车费收据.txt", "停车费收据\n上海虹桥机场 P9 停车场\n停车 2 天，金额 120 元\n本文件为“收好”的演示数据，仅用于演示。"},
		{"报销/出租车票.txt", "上海市出租汽车专用发票\n上车 08:40 下车 09:25\n金额 86.00 元\n本文件为“收好”的演示数据，仅用于演示。"},
		{"报销/酒店/住宿发票.txt", "增值税电子普通发票\n某某酒店（北京）有限公司\n住宿费 2 晚 金额 980.00 元\n本文件为“收好”的演示数据，仅用于演示。"},
	})
}

// outlookNotice is the property office's notice as Outlook saves it: an
// HTML body and the bill attached.
func outlookNotice(now time.Time) []byte {
	return msgtest.Build(msgtest.Message{
		Subject: "2026 年第四季度物业费缴费通知", FromName: "阳光花园物业服务中心", FromEmail: "service@example.com",
		To:   []msgtest.Recipient{{Name: "王小明", Email: "wang@example.com"}},
		Date: now.AddDate(0, 0, -3),
		Body: "王先生您好：\n\n您家（3 栋 1202 室）第四季度物业费 1,260.00 元，请于 10 月 31 日前缴纳，可在物业 App 或前台办理。\n\n阳光花园物业服务中心\n\n本文件为“收好”的演示数据，仅用于演示。",
		HTML: `<p>王先生您好：</p><p>您家（3 栋 1202 室）第四季度物业费 <b>1,260.00 元</b>，请于 <b>10 月 31 日</b>前缴纳，可在物业 App 或前台办理。</p>` +
			`<p>阳光花园物业服务中心</p><p style="color:#888">本文件为“收好”的演示数据，仅用于演示。</p>`,
		Attachments: []msgtest.Attachment{{Name: "物业费缴费单.txt", MIME: "text/plain",
			Data: []byte("物业费缴费单\n3 栋 1202 室\n2026 年 10–12 月 物业费 1,260.00 元\n本文件为“收好”的演示数据，仅用于演示。")}},
	})
}

// renovation is the renovation contract's paragraphs at a price and a
// number of working days.
func renovation(price, days int) []string {
	return []string{
		"家庭装修施工合同",
		"甲方（业主）：王小明　　乙方（施工方）：某某装饰工程有限公司",
		"工程地址：阳光花园 3 栋 1202 室",
		fmt.Sprintf("一、工程总价：人民币 %d 元（含人工、辅材）。", price),
		fmt.Sprintf("二、工期：%d 个工作日，自开工之日起计算。", days),
		"三、付款：开工付 30%，水电验收付 40%，竣工验收付 25%，质保金 5% 一年后付清。",
		"四、质保：水电工程五年，其余两年。",
		"本文件为“收好”的演示数据，仅用于演示。",
	}
}

// wordDoc is a small Word document of paragraphs.
func wordDoc(paras []string) []byte {
	var body strings.Builder
	for _, p := range paras {
		body.WriteString("<w:p><w:r><w:t>" + p + "</w:t></w:r></w:p>")
	}
	return zipFiles([][2]string{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`},
		{"word/document.xml", `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + body.String() + `</w:body></w:document>`},
	})
}
