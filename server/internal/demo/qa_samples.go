package demo

import (
	"fmt"
	"sort"
	"strconv"
)

// The question-answering agent's evaluation set (design doc 10.8): the
// family workspace's filed documents, and 100 questions about them with
// the document to cite and a value the answer must state, plus questions
// nothing answers.

// QADoc is a document of the evaluation workspace.
type QADoc struct {
	Title, Type, Correspondent, Date, Text string
	Amount                                 float64
}

// QAQuestion is one question; Title is the document to cite ("" when the
// answer is a total over several).
type QAQuestion struct {
	Question, Title, Want string
	NotFound              bool
}

// QAEval returns the documents and 100 questions.
func QAEval() ([]QADoc, []QAQuestion) {
	var docs []QADoc
	var filed []docData
	for _, d := range familyDocuments() {
		if d.deletedDaysAgo > 0 || d.inbox || d.correspondent == "" {
			continue
		}
		filed = append(filed, d)
		docs = append(docs, QADoc{Title: d.title, Type: d.docType, Correspondent: d.correspondent, Date: d.date, Text: sampleText(d), Amount: d.amount})
	}
	var qs []QAQuestion
	// Amounts of single documents (40).
	for _, d := range filed {
		if d.amount > 0 && len(qs) < 40 {
			qs = append(qs, QAQuestion{Question: d.title + "的金额是多少？", Title: d.title, Want: money2(d.amount)})
		}
	}
	// Dates (30), spread over the documents.
	for i := 0; i < len(filed) && len(qs) < 70; i += 2 {
		d := filed[i]
		qs = append(qs, QAQuestion{Question: fmt.Sprintf("%s的“%s”是哪天的？", d.correspondent, d.title), Title: d.title, Want: d.date})
	}
	// Totals per year and correspondent (as many as there are, up to 20).
	type key struct {
		year int
		corr string
	}
	sums := map[key]float64{}
	counts := map[key]int{}
	for _, d := range filed {
		if d.docType != "账单" || d.amount <= 0 {
			continue
		}
		y, _ := strconv.Atoi(d.date[:4])
		k := key{y, d.correspondent}
		sums[k] += d.amount
		counts[k]++
	}
	var keys []key
	for k, n := range counts {
		if n >= 2 {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].year != keys[j].year {
			return keys[i].year < keys[j].year
		}
		return keys[i].corr < keys[j].corr
	})
	for _, k := range keys {
		if len(qs) >= 80 {
			break
		}
		qs = append(qs, QAQuestion{Question: fmt.Sprintf("%d 年%s的账单一共多少钱？", k.year, k.corr), Want: money2(sums[k])})
		qs = append(qs, QAQuestion{Question: fmt.Sprintf("%d 年有几份%s的账单？", k.year, k.corr), Want: strconv.Itoa(counts[k]) + "份"})
	}
	// Questions nothing answers; then the rest are amounts again.
	for _, q := range []string{
		"宽带合同什么时候到期？", "家里的狗哪天打的疫苗？", "房产证上的面积是多少？", "我的驾驶证哪天到期？", "装修合同的总价是多少？",
		"去年交了多少物业费？", "最近一次手机话费是多少？", "最近一次水费是多少？", "2021 年的电费一共多少钱？", "结婚证是哪天领的？",
	} {
		qs = append(qs, QAQuestion{Question: q, NotFound: true})
	}
	for _, d := range filed {
		if len(qs) >= 100 {
			break
		}
		if d.amount > 0 && d.docType != "账单" {
			qs = append(qs, QAQuestion{Question: fmt.Sprintf("%s花了多少钱？", d.title), Title: d.title, Want: money2(d.amount)})
		}
	}
	for i := 0; len(qs) < 100 && i < len(filed); i++ {
		d := filed[len(filed)-1-i]
		qs = append(qs, QAQuestion{Question: fmt.Sprintf("“%s”是哪家的？", d.title), Title: d.title, Want: d.correspondent})
	}
	return docs, qs
}

func money2(f float64) string { return strconv.FormatFloat(f, 'f', 2, 64) }

// sampleText is a document's text as a receipt, contract or certificate
// of its kind would read.
func sampleText(d docData) string {
	date := chineseDate(d.date)
	var text string
	switch d.docType {
	case "账单":
		text = fmt.Sprintf("%s\n%s\n出账日期 %s\n本期应缴 %s 元\n请在到期日前缴纳，逾期将产生滞纳金。客户服务热线 95598", d.correspondent, d.title, date, money(d.amount))
	case "发票":
		text = fmt.Sprintf("电子发票（普通发票）\n开票日期：%s\n销售方名称：%s\n项目名称：%s\n价税合计（小写）¥%s\n开票人：王五", date, d.correspondent, d.title, money(d.amount))
	case "合同":
		text = fmt.Sprintf("%s\n甲方：王小明\n乙方：%s\n双方经友好协商，就相关事宜达成如下协议，共同遵守。\n签订日期：%s", d.title, d.correspondent, date)
	case "保单":
		text = fmt.Sprintf("%s\n保险人：%s\n投保人：王小明\n保险费合计：人民币 %s 元\n保单生成日期：%s", d.title, d.correspondent, money(d.amount), date)
	case "保修卡":
		text = fmt.Sprintf("%s\n销售商：%s\n购买日期：%s\n凭本卡享受一年免费保修服务，请妥善保管。", d.title, d.correspondent, date)
	case "报告":
		text = fmt.Sprintf("%s\n%s 健康管理中心\n受检者：王小明\n检查日期：%s\n结论：各项指标基本正常，建议定期复查。", d.title, d.correspondent, date)
	default: // 证件
		text = fmt.Sprintf("%s\n签发机关：%s\n签发日期：%s\n持证人：王小明", d.title, d.correspondent, date)
	}
	return text
}
