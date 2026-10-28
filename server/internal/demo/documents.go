package demo

import (
	"fmt"
	"sort"
)

// docData is one sample document of the prototype (app-orgin/src/data/mock.ts).
type docData struct {
	title, docType, correspondent, date string // date: YYYY-MM-DD
	tags                                []string
	amount                              float64 // 0 when none
	inbox                               bool    // still waiting in the inbox
	asn                                 int64   // 原件编号; 0 when none
	deletedDaysAgo                      int     // > 0: in the trash, deleted that many days ago
}

// numbered gives archived documents the prototype's ASNs: 1, 2, 3, … in
// date order (finish(d, i + 1) after sorting in buildFamilyDocs).
// The order of docs is kept.
func numbered(docs []docData) []docData {
	order := make([]int, len(docs))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return docs[order[a]].date < docs[order[b]].date })
	for rank, i := range order {
		docs[i].asn = int64(rank + 1)
	}
	return docs
}

// familyDocuments mirrors buildFamilyDocs and FAMILY_INBOX.
func familyDocuments() []docData {
	var out []docData
	add := func(title, typ, corr, date string, tags []string, amount float64) {
		out = append(out, docData{title: title, docType: typ, correspondent: corr, date: date, tags: tags, amount: amount})
	}
	electricity := []float64{212.4, 238.1, 196.7, 175.3, 188.9, 296.2, 402.5, 436.8, 318.6, 202.3, 226.0, 264.4}
	gas := []float64{168, 152, 120, 86, 54, 38, 32, 30, 36, 62, 118, 160}
	for _, y := range []int{2023, 2024} {
		for m := 1; m <= 12; m++ {
			if y == 2024 && m > 9 {
				break
			}
			extra := 0.0
			if y == 2024 {
				extra = 1
			}
			add(fmt.Sprintf("%d年%d月电费账单", y, m), "账单", "国家电网", fmt.Sprintf("%d-%02d-05", y, m), []string{"水电"}, round2(electricity[m-1]+11.6*extra))
			add(fmt.Sprintf("%d年%d月燃气账单", y, m), "账单", "北京燃气", fmt.Sprintf("%d-%02d-03", y, m), []string{"水电"}, gas[m-1]+6*extra)
			if !(y == 2024 && m == 9) {
				add(fmt.Sprintf("招商银行信用卡对账单 %d年%d月", y, m), "账单", "招商银行", fmt.Sprintf("%d-%02d-28", y, m), nil, float64(1800+(m*613+y)%2400)+0.5)
			}
		}
	}
	add("房屋租赁合同", "合同", "链家", "2023-09-01", nil, 0)
	add("租金发票 2024年第一季度", "发票", "链家", "2024-03-20", []string{"税务"}, 18000)
	add("热水器保修卡", "保修卡", "京东", "2023-01-15", []string{"保修"}, 0)
	add("车险保单 2024", "保单", "平安保险", "2024-07-18", []string{"保险"}, 4260)
	add("车险保单 2023", "保单", "平安保险", "2023-07-16", []string{"保险"}, 4480)
	add("机动车检验合格证明", "证件", "北京市车管所", "2023-02-11", nil, 0)
	add("汽车保养发票", "发票", "某某汽车服务中心", "2024-05-09", nil, 860)
	add("2024年度体检报告", "报告", "协和医院", "2024-04-12", nil, 0)
	add("2023年度体检报告", "报告", "协和医院", "2023-04-18", nil, 0)
	add("门诊医疗票据", "发票", "协和医院", "2024-01-22", []string{"报销"}, 2130)
	add("商业医疗保险保单", "保单", "平安保险", "2024-02-01", []string{"保险"}, 1280)
	add("劳动合同", "合同", "某某科技有限公司", "2023-06-01", nil, 0)
	add("MacBook Pro 保修卡", "保修卡", "Apple", "2023-06-20", []string{"保修"}, 0)
	add("增值税专用发票-办公用品", "发票", "某某科技有限公司", "2024-08-28", []string{"报销", "待报销"}, 1280)
	add("差旅发票-北京至上海", "发票", "中国铁路", "2024-05-16", []string{"报销"}, 553)
	add("儿童疫苗接种记录", "证件", "社区卫生服务中心", "2024-06-15", nil, 0)
	add("幼儿园保育协议", "合同", "某某幼儿园", "2024-08-25", nil, 0)
	add("护照（孩子）", "证件", "公安局", "2019-10-29", nil, 0)
	add("居民身份证复印件", "证件", "公安局", "2022-05-10", nil, 0)
	add("户口本首页", "证件", "公安局", "2022-05-10", nil, 0)
	add("京东购物发票-显示器", "发票", "京东", "2023-09-11", nil, 1899)
	add("电费缴费凭证 2022年12月", "账单", "国网北京", "2022-12-06", []string{"水电"}, 238.5)
	add("智能电表更换通知", "账单", "国网北京", "2022-11-18", nil, 0)
	add("个税年度汇算回执", "证件", "国家税务总局", "2024-04-02", []string{"税务"}, 0)

	out = numbered(out)
	inboxDoc := func(title, typ, corr, date string, tags []string, amount float64) docData {
		return docData{title: title, docType: typ, correspondent: corr, date: date, tags: tags, amount: amount, inbox: true}
	}
	inbox := []docData{
		inboxDoc("2024年10月电费账单", "账单", "国家电网", "2024-10-05", []string{"水电"}, 236.4),
		inboxDoc("房屋租赁合同-补充协议", "合同", "链家", "2024-09-30", nil, 0),
		inboxDoc("空气净化器保修卡", "保修卡", "京东", "2024-09-26", []string{"保修"}, 0),
		inboxDoc("增值税普通发票-打印机", "发票", "某某科技有限公司", "2024-09-24", []string{"报销", "待报销"}, 1699),
		inboxDoc("招商银行信用卡对账单 2024年9月", "账单", "招商银行", "2024-09-28", nil, 2410.5),
		inboxDoc("居民身份证复印件（配偶）", "证件", "公安局", "2024-09-20", nil, 0),
	}
	// The prototype's trash: deleted 12 and 29 days ago (还剩 18 天、1 天).
	trash := []docData{
		{title: "2022年12月燃气账单（重复）", docType: "账单", correspondent: "北京燃气", date: "2022-12-03", deletedDaysAgo: 12},
		{title: "扫描件 2024-08-01", docType: "报告", date: "2024-08-01", deletedDaysAgo: 29},
	}
	return append(append(out, inbox...), trash...)
}

// studioDocuments mirrors buildStudioDocs.
func studioDocuments() []docData {
	d := func(title, typ, corr, date string, tags []string, amount float64) docData {
		return docData{title: title, docType: typ, correspondent: corr, date: date, tags: tags, amount: amount}
	}
	return numbered([]docData{
		d("服务合同-星河科技", "合同", "星河科技", "2024-08-12", nil, 120000),
		d("服务合同-蓝鲸传媒", "合同", "蓝鲸传媒", "2024-05-20", nil, 86000),
		d("办公室租赁合同", "合同", "某某物业", "2024-01-01", nil, 0),
		d("增值税专用发票-星河科技", "发票", "星河科技", "2024-09-15", []string{"税务"}, 60000),
		d("增值税专用发票-蓝鲸传媒", "发票", "蓝鲸传媒", "2024-06-30", []string{"税务"}, 43000),
		d("2024年9月房租发票", "发票", "某某物业", "2024-09-01", []string{"报销"}, 18500),
		d("员工劳动合同-李明", "合同", "李明", "2024-03-11", nil, 0),
		d("员工劳动合同-张婷", "合同", "张婷", "2024-07-01", nil, 0),
		d("社保缴费明细 2024年9月", "账单", "社保中心", "2024-09-10", nil, 21360),
		d("项目验收报告-官网改版", "报告", "星河科技", "2024-08-30", nil, 0),
		d("项目需求说明书-小程序", "报告", "蓝鲸传媒", "2024-04-18", nil, 0),
		d("营业执照副本-星河科技", "证件", "星河科技", "2024-02-20", nil, 0),
		d("营业执照副本-蓝鲸传媒", "证件", "蓝鲸传媒", "2024-02-21", nil, 0),
		d("云服务器账单 2024年9月", "账单", "阿里云", "2024-09-02", []string{"报销"}, 1320),
		d("云服务器账单 2024年8月", "账单", "阿里云", "2024-08-02", []string{"报销"}, 1280),
	})
}

func round2(f float64) float64 { return float64(int64(f*100+0.5)) / 100 }
