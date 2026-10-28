package demo

import "kept/server/internal/taxonomy"

// The sample data of the web prototype (app-orgin/src/data/mock.ts), for the
// parts that have database tables so far. Each later stage adds what its new
// tables can hold (documents, drawers, reminders, ...).

type spaceData struct {
	name, kind, color string
	correspondents    []string
	types             []string
	tags              []tagData
	documents         func() []docData
	drawers           []drawerData
}

type tagData struct{ name, color string }

// rules are the prototype's MATCH_RULE entries; the keywords are the name
// itself. Everything else learns automatically.
var rules = map[string]string{
	"国家电网": "any", "链家": "any", "北京燃气": "any",
	"招商银行": "literal", "Apple": "literal",
	"公安局": "fuzzy",
}

func ruleFor(name string) *taxonomy.MatchRule {
	alg, ok := rules[name]
	if !ok {
		return nil // taxonomy.DefaultRule
	}
	return &taxonomy.MatchRule{Algorithm: alg, Match: name, CaseInsensitive: true}
}

var documentTypes = []string{"账单", "合同", "证件", "保修卡", "发票", "报告", "保单"}

// Colours as in the prototype's TAG_COLORS (all among the 12 presets).
var purposeTags = []tagData{
	{"税务", "#3A6EA5"}, {"保修", "#2F6B4F"}, {"报销", "#9A6A12"},
	{"水电", "#2C7A7B"}, {"待报销", "#A5523A"}, {"保险", "#7A4E8C"},
}

var spaces = []spaceData{
	{
		name: "王家", kind: "family", color: "green",
		correspondents: []string{
			"国家电网", "国网北京", "北京燃气", "链家", "京东", "招商银行", "平安保险", "北京市车管所",
			"某某汽车服务中心", "协和医院", "社区卫生服务中心", "某某幼儿园", "公安局", "国家税务总局",
			"中国铁路", "某某科技有限公司", "Apple",
		},
		types: documentTypes, tags: purposeTags, documents: familyDocuments, drawers: familyDrawers,
	},
	{
		name: "某某工作室", kind: "team", color: "orange",
		correspondents: []string{"星河科技", "蓝鲸传媒", "某某物业", "李明", "张婷", "社保中心", "阿里云"},
		types:          documentTypes, tags: purposeTags, documents: studioDocuments, drawers: studioDrawers,
	},
}
