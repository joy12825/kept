package dedupe

import "testing"

const invoice = `电子发票（普通发票）
发票代码：011002300111 发票号码：24871634
开票日期：2024年03月05日 校验码：12345 67890 11223 34455
购买方 名称：王小明 纳税人识别号：
销售方 名称：北京京东世纪贸易有限公司 纳税人识别号：911101087921245832
货物或应税劳务名称 规格型号 单位 数量 单价 金额 税率 税额
*计算机外部设备*罗技 MX Master 3S 无线鼠标 台 1 646.02 646.02 13% 83.98
合计 646.02 83.98
价税合计（大写）柒佰叁拾圆整 （小写）730.00
收款人：张三 复核：李四 开票人：王五`

// The same invoice as OCR reads a scan: spacing, a misread character, a
// line break in another place.
const scanned = `电子发票 (普通发票)
发票代码:011002300111 发票号码:24871634
开票日期:2024年03月05日 校验码:12345 67890 11223 34455
购买方名称:王小明 纳税人识别号:
销售方名称:北京京东世纪贸易有限公司 纳税人识别号:911101087921245832
货物或应税劳务名称 规格型号 单位 数量 单价 金额 税率 税额
*计算机外部设备*罗技 MX Master 3S 无线鼠标 台 1 646.02
646.02 13% 83.98 合计 646.02 83.98
价税合计(大写)柒佰叁拾园整 (小写)730.00
收款人:张三 复核:李四 开票人:王五`

func bill(month, used, amount, due string) string {
	return `国网北京市电力公司 电费缴费通知单
户号：1100 2233 4455 户名：王小明 地址：北京市朝阳区某某小区 3 号楼 2 单元 501
计费周期：2024年` + month + `月01日至2024年` + month + `月31日
本期示数：` + used + ` 上期示数：12000 用电量（千瓦时）：` + used + `
电价（元/千瓦时）：0.4883 本期电费（元）：` + amount + `
请于2024年` + due + `前缴纳，逾期将按日收取万分之一的违约金。
缴费方式：网上国网 App、支付宝、微信、银行代扣、营业厅。服务热线 95598`
}

func TestNearDuplicates(t *testing.T) {
	a, ok := Fingerprint(invoice)
	b, ok2 := Fingerprint(scanned)
	if !ok || !ok2 {
		t.Fatal("no fingerprint")
	}
	if d := Distance(a, b); d > MaxDistance {
		t.Errorf("scan vs e-invoice: distance %d", d)
	}
	m3, _ := Fingerprint(bill("03", "12312", "152.35", "04月15日"))
	m4, _ := Fingerprint(bill("04", "12588", "134.77", "05月15日"))
	if d := Distance(m3, m4); d <= MaxDistance {
		t.Errorf("two months' bills: distance %d", d)
	}
	t.Logf("invoice %d, bills %d, invoice vs bill %d", Distance(a, b), Distance(m3, m4), Distance(a, m3))
	if !Confirm(invoice, scanned, Distance(a, b)) {
		t.Error("scan vs e-invoice: numbers differ")
	}
	// Two statements from one template: near by the fingerprint, told
	// apart by their numbers.
	tpl := func(month, amount, card string) string {
		return "招商银行信用卡对账单 2023年" + month + "月 本文件为演示数据，由原型的示例内容生成，仅用于展示收好的功能，没有真实的金额和个人信息。卡号尾号 " + card + " 本期应还 " + amount + " 元 最低还款 " + amount + " 元"
	}
	if Confirm(tpl("10", "2753.50", "6688"), tpl("12", "3979.50", "6688"), 4) {
		t.Error("two statements: same numbers")
	}
	// Few numbers: only very close fingerprints count.
	if Confirm("北京燃气 2023年6月燃气账单 金额 38.00", "北京燃气 2024年7月燃气账单 金额 38.00", 4) {
		t.Error("few numbers, distance 4 confirmed")
	}
	if _, ok := Fingerprint("只有几个字的便条 123"); ok {
		t.Error("short text fingerprinted")
	}
}
