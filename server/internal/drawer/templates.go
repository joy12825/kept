package drawer

import (
	"context"
	"database/sql"
	"errors"
	"kept/server/internal/i18n"

	"kept/server/internal/apperr"
	"kept/server/internal/filter"
	"kept/server/internal/pinyin"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// Template is a built-in drawer (design doc 3.6): copied when a drawer is
// created from it, unrelated afterwards. Its filing rule is mostly
// keywords found in the text; type names are matched against the
// workspace's types and left out when missing. Purpose tags it needs are
// created when missing.
type Template struct {
	Key, Name, Icon, Color, Description string
	Keywords                            []string // any of them in the title or text
	Types                               []string // or a document of one of these types
	Tags                                []PurposeTag
	Facts                               []FactTemplate
}

// PurposeTag is a tag a template relies on (税务, 保修, …).
type PurposeTag struct{ Name, Color string }

// FactTemplate is a fact of a template. Field is a built-in field key
// (amount, due_date); Tag a purpose tag name; Keywords narrow it by text.
type FactTemplate struct {
	Kind, Label, Field, Tag, Period string
	Keywords                        []string
	GraceDays                       int
}

var (
	tagTax       = PurposeTag{"税务", "#3A6EA5"}
	tagWarranty  = PurposeTag{"保修", "#2F6B4F"}
	tagExpense   = PurposeTag{"报销", "#9A6A12"}
	tagUtilities = PurposeTag{"水电", "#2C7A7B"}
	tagInsurance = PurposeTag{"保险", "#7A4E8C"}
)

var pinned = FactTemplate{Kind: KindPinned, Label: "固定文档"}

// Templates are the built-in drawers.
var Templates = []Template{
	{Key: "house", Name: "房子", Icon: "home", Color: "blue", Description: "租房或买房的合同、物业、水电燃气账单、家电保修",
		Keywords: []string{"租赁", "租房", "房租", "物业", "房产", "购房", "电费", "水费", "燃气", "供暖", "宽带", "装修"},
		Tags:     []PurposeTag{tagUtilities, tagWarranty},
		Facts: []FactTemplate{{Kind: KindDue, Label: "合同到期", Field: "due_date"},
			{Kind: KindSum, Label: "今年水电费", Field: "amount", Tag: "水电", Period: "this_year"}, pinned}},
	{Key: "car", Name: "车", Icon: "car", Color: "green", Description: "车险、年检、保养和维修",
		Keywords: []string{"机动车", "车险", "行驶证", "年检", "保养", "车辆", "维修", "停车", "加油"},
		Tags:     []PurposeTag{tagInsurance},
		Facts: []FactTemplate{{Kind: KindDue, Label: "下次到期", Field: "due_date"},
			{Kind: KindSum, Label: "今年用车花费", Field: "amount", Period: "this_year"}, pinned}},
	{Key: "health", Name: "健康", Icon: "heart", Color: "red", Description: "体检报告、病历、医疗票据和医疗保险",
		Keywords: []string{"医院", "体检", "门诊", "病历", "处方", "检验", "医疗"},
		Tags:     []PurposeTag{tagExpense, tagInsurance},
		Facts: []FactTemplate{{Kind: KindLatest, Label: "最近体检", Keywords: []string{"体检"}},
			{Kind: KindSum, Label: "今年医疗费用", Field: "amount", Period: "this_year"}, pinned}},
	{Key: "kids", Name: "孩子", Icon: "baby", Color: "pink", Description: "出生、疫苗、入园入学和学费",
		Keywords: []string{"儿童", "幼儿园", "疫苗", "出生", "学校", "学费", "托育"},
		Facts:    []FactTemplate{{Kind: KindDue, Label: "下次到期", Field: "due_date"}, pinned}},
	{Key: "ids", Name: "证件", Icon: "idcard", Color: "purple", Description: "身份证、护照、户口本等证件及复印件",
		Keywords: []string{"身份证", "护照", "户口", "签证", "结婚证", "通行证", "居住证"}, Types: []string{"证件"},
		Facts: []FactTemplate{{Kind: KindDue, Label: "最近到期", Field: "due_date"}, pinned}},
	{Key: "pets", Name: "宠物", Icon: "paw", Color: "orange", Description: "宠物的疫苗、体检和医疗",
		Keywords: []string{"宠物", "犬", "猫", "兽医", "动物医院"},
		Facts:    []FactTemplate{{Kind: KindDue, Label: "下次疫苗", Field: "due_date"}, pinned}},
	{Key: "finance", Name: "财务", Icon: "wallet", Color: "cyan", Description: "银行、信用卡、个税、社保和公积金",
		Keywords: []string{"银行", "信用卡", "对账单", "个税", "个人所得税", "社保", "公积金", "理财", "保险单"},
		Tags:     []PurposeTag{tagTax},
		Facts:    []FactTemplate{{Kind: KindSum, Label: "今年税务", Field: "amount", Tag: "税务", Period: "this_year"}, pinned}},
	{Key: "work", Name: "工作", Icon: "briefcase", Color: "orange", Description: "劳动合同、工资、报销和差旅",
		Keywords: []string{"劳动合同", "工资", "入职", "离职", "差旅", "报销"},
		Tags:     []PurposeTag{tagExpense},
		Facts:    []FactTemplate{{Kind: KindSum, Label: "今年报销", Field: "amount", Tag: "报销", Period: "this_year"}, pinned}},
	{Key: "travel", Name: "出行", Icon: "plane", Color: "blue", Description: "机票、火车票、酒店和行程单",
		Keywords: []string{"机票", "火车票", "登机牌", "酒店", "行程单", "航班"},
		Facts:    []FactTemplate{{Kind: KindLatest, Label: "最近出行"}, {Kind: KindSum, Label: "今年出行花费", Field: "amount", Period: "this_year"}}},
	{Key: "study", Name: "学习", Icon: "book", Color: "green", Description: "学历、证书、成绩和培训",
		Keywords: []string{"毕业", "学位", "学历", "证书", "成绩", "培训", "课程"},
		Facts:    []FactTemplate{pinned}},
	{Key: "clients", Name: "客户", Icon: "users", Color: "blue", Description: "客户的资料、营业执照和往来文件",
		Keywords: []string{"营业执照", "客户", "统一社会信用代码"},
		Facts:    []FactTemplate{{Kind: KindLatest, Label: "最近往来"}, pinned}},
	{Key: "contracts", Name: "合同", Icon: "file", Color: "green", Description: "与客户、供应商签订的合同和协议",
		Keywords: []string{"合同", "协议"}, Types: []string{"合同"},
		Facts: []FactTemplate{{Kind: KindDue, Label: "最近到期", Field: "due_date"},
			{Kind: KindSum, Label: "今年合同金额", Field: "amount", Period: "this_year"}}},
	{Key: "accounts", Name: "财务", Icon: "wallet", Color: "cyan", Description: "发票、账单、银行回单和税务",
		Keywords: []string{"发票", "回单", "税务", "纳税"}, Types: []string{"发票"},
		Tags:  []PurposeTag{tagTax, tagExpense},
		Facts: []FactTemplate{{Kind: KindSum, Label: "今年开票", Field: "amount", Tag: "税务", Period: "this_year"}, {Kind: KindSum, Label: "今年报销", Field: "amount", Tag: "报销", Period: "this_year"}}},
	{Key: "hr", Name: "人事", Icon: "idcard", Color: "purple", Description: "员工合同、社保、入离职",
		Keywords: []string{"劳动合同", "员工", "社保", "入职", "离职", "工资表"},
		Facts:    []FactTemplate{{Kind: KindExpect, Label: "社保缴费明细", Keywords: []string{"社保"}, Period: "monthly", GraceDays: 15}}},
	{Key: "projects", Name: "项目", Icon: "kanban", Color: "orange", Description: "项目的需求、方案和验收",
		Keywords: []string{"项目", "需求", "验收", "方案"},
		Facts:    []FactTemplate{{Kind: KindLatest, Label: "最近文件"}}},
	{Key: "office", Name: "行政", Icon: "building", Color: "gray", Description: "办公室租赁、物业、采购和证照",
		Keywords: []string{"办公室", "物业", "采购", "租赁"},
		Facts:    []FactTemplate{{Kind: KindDue, Label: "合同到期", Field: "due_date"}, pinned}},
}

// Scene groups templates for first use (UI design spec 5.2).
type Scene struct {
	Key, Name, Icon, Description string
	Templates                    []string
}

var Scenes = []Scene{
	{Key: "family", Name: "家庭", Icon: "home", Description: "房子、车、孩子、健康等家庭事务", Templates: []string{"house", "car", "health", "kids", "ids", "pets"}},
	{Key: "personal", Name: "个人", Icon: "user", Description: "证件、财务、工作、学习等个人文件", Templates: []string{"ids", "health", "finance", "work", "travel", "study"}},
	{Key: "company", Name: "工作室或公司", Icon: "building", Description: "客户、合同、财务、人事等业务文件", Templates: []string{"clients", "contracts", "accounts", "hr", "projects", "office"}},
}

// TemplateByKey finds a built-in template.
func TemplateByKey(key string) (Template, bool) {
	for _, t := range Templates {
		if t.Key == key {
			return t, true
		}
	}
	return Template{}, false
}

var errTemplate = apperr.New(apperr.KindInvalid, "没有这个抽屉模板")

func textAny(words []string) filter.Cond {
	var any []filter.Cond
	for _, w := range words {
		w := w
		any = append(any, filter.Cond{Text: &w})
	}
	if len(any) == 1 {
		return any[0]
	}
	return filter.Cond{Any: any}
}

// FromTemplates creates drawers from templates (in the given order) with
// their rules and facts, and starts one reassignment. A template whose name
// is already a drawer of the workspace is skipped. Names, descriptions,
// facts and purpose tags are in the language of the request (v1.10.1);
// keywords and type names stay Chinese, they match Chinese documents.
func (s *Service) FromTemplates(ctx context.Context, scope tenant.Scope, keys []string) ([]Drawer, int64, error) {
	var tpls []Template
	for _, k := range keys {
		t, ok := TemplateByKey(k)
		if !ok {
			return nil, 0, errTemplate
		}
		tpls = append(tpls, t)
	}
	ws := scope.WorkspaceID()
	var out []Drawer
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		existing, err := q.ListDrawers(ctx, ws)
		if err != nil {
			return err
		}
		taken := map[string]bool{}
		for _, d := range existing {
			taken[d.Name] = true
		}
		types, err := q.ListDocumentTypes(ctx, ws)
		if err != nil {
			return err
		}
		typeID := map[string]int64{}
		for _, t := range types {
			typeID[t.Name] = t.ID
		}
		fieldID := func(key string) int64 {
			id, err := q.GetBuiltInField(ctx, sqlc.GetBuiltInFieldParams{WorkspaceID: ws, Key: sql.NullString{String: key, Valid: true}})
			if err != nil {
				return 0
			}
			return id
		}
		for _, t := range tpls {
			name := TemplateText(ctx, t.Name)
			if taken[name] {
				continue
			}
			taken[name] = true
			tagIDs := map[string]int64{}
			for _, pt := range t.Tags {
				id, err := ensureTag(ctx, q, ws, PurposeTag{Name: TemplateText(ctx, pt.Name), Color: pt.Color}, s.opt.Now().UnixMilli())
				if err != nil {
					return err
				}
				tagIDs[pt.Name] = id
			}
			var rule []filter.Cond
			var ids []int64
			for _, name := range t.Types {
				if id, ok := typeID[name]; ok {
					ids = append(ids, id)
				}
			}
			if len(ids) > 0 {
				rule = append(rule, fieldCond("type", "in", ids))
			}
			if len(t.Keywords) > 0 {
				rule = append(rule, textAny(t.Keywords))
			}
			var c *filter.Cond
			switch len(rule) {
			case 0:
			case 1:
				c = &rule[0]
			default:
				c = &filter.Cond{Any: rule}
			}
			d, err := s.insert(ctx, q, scope, name, Input{Name: name, Icon: t.Icon, Color: t.Color, Description: TemplateText(ctx, t.Description), Rule: c, TemplateKey: t.Key})
			if err != nil {
				return err
			}
			for _, ft := range t.Facts {
				f := Fact{DrawerID: d.ID, Kind: ft.Kind, Label: TemplateText(ctx, ft.Label), Config: FactConfig{Period: ft.Period, GraceDays: ft.GraceDays}}
				if ft.Field != "" {
					if f.Config.FieldID = fieldID(ft.Field); f.Config.FieldID == 0 {
						continue
					}
				}
				if ft.Tag != "" {
					f.Config.TagIDs = []int64{tagIDs[ft.Tag]}
				}
				if len(ft.Keywords) > 0 {
					kc := textAny(ft.Keywords)
					f.Config.Filter = &kc
				}
				if err := checkFact(ctx, q, scope, &f); err != nil {
					return err
				}
				if _, err := s.insertFact(ctx, q, f); err != nil {
					return err
				}
			}
			out = append(out, d)
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	var job int64
	if len(out) > 0 {
		job, err = s.EnqueueReassign(ctx, scope)
		s.changed(ws)
	}
	return out, job, err
}

// ensureTag returns the ID of the workspace's tag with the purpose tag's
// name, creating it (top level) when missing.
func ensureTag(ctx context.Context, q *sqlc.Queries, ws int64, pt PurposeTag, now int64) (int64, error) {
	id, err := q.FindTagByName(ctx, sqlc.FindTagByNameParams{WorkspaceID: ws, Name: pt.Name})
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	key := pinyin.SortKey(pt.Name)
	row, err := q.InsertTag(ctx, sqlc.InsertTagParams{WorkspaceID: ws, Name: pt.Name, SortKey: key, Color: pt.Color,
		Match: "", Algorithm: "auto", CaseInsensitive: 1, ShowInYearbook: 1, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		return 0, err
	}
	return row.ID, nil
}

// templateEN is the English of the templates, the scenes and the library
// drawer (v1.10.1). It is kept here, not in the server's catalog: those
// are everyday words (合同, 车, 税务) that are also titles and names
// people write, which the catalog would translate wherever they appear.
var templateEN = map[string]string{
	"书房":         "Library",
	"我的书房":       "My library",
	"所有书籍，按封面排列": "All books, shown by cover",
	"税务":         "Taxes",
	"保修":         "Warranty",
	"报销":         "Reimbursement",
	"水电":         "Utilities",
	"保险":         "Insurance",
	"固定文档":       "Pinned documents",
	"房子":         "House",
	"租房或买房的合同、物业、水电燃气账单、家电保修": "Rental or purchase contracts, property management, utility bills and appliance warranties",
	"合同到期":        "Contract ends",
	"今年水电费":       "Utilities this year",
	"车":           "Car",
	"车险、年检、保养和维修": "Car insurance, inspections, servicing and repairs",
	"下次到期":        "Next due",
	"今年用车花费":      "Car costs this year",
	"健康":          "Health",
	"体检报告、病历、医疗票据和医疗保险": "Checkup reports, medical records, medical receipts and health insurance",
	"最近体检":   "Latest checkup",
	"今年医疗费用": "Medical costs this year",
	"孩子":     "Kids",
	"出生、疫苗、入园入学和学费": "Birth, vaccinations, school enrollment and fees",
	"证件": "IDs",
	"身份证、护照、户口本等证件及复印件": "ID cards, passports, household registers and other IDs, with copies",
	"最近到期":        "Next to expire",
	"宠物":          "Pets",
	"宠物的疫苗、体检和医疗": "Pet vaccinations, checkups and vet care",
	"下次疫苗":        "Next vaccination",
	"财务":          "Finance",
	"银行、信用卡、个税、社保和公积金": "Banking, credit cards, income tax, social insurance and housing fund",
	"今年税务": "Taxes this year",
	"工作":   "Work",
	"劳动合同、工资、报销和差旅": "Employment contracts, pay, reimbursements and business travel",
	"今年报销": "Reimbursements this year",
	"出行":   "Travel",
	"机票、火车票、酒店和行程单": "Flights, train tickets, hotels and itineraries",
	"最近出行":        "Latest trip",
	"今年出行花费":      "Travel costs this year",
	"学习":          "Education",
	"学历、证书、成绩和培训": "Degrees, certificates, grades and training",
	"客户":          "Clients",
	"客户的资料、营业执照和往来文件": "Client details, business licenses and correspondence",
	"最近往来": "Latest correspondence",
	"合同":   "Contracts",
	"与客户、供应商签订的合同和协议": "Contracts and agreements with clients and suppliers",
	"今年合同金额":          "Contract value this year",
	"发票、账单、银行回单和税务":   "Invoices, bills, bank receipts and taxes",
	"今年开票":            "Invoiced this year",
	"人事":              "HR",
	"员工合同、社保、入离职":     "Employee contracts, social insurance, onboarding and leaving",
	"社保缴费明细":          "Social insurance statement",
	"项目":              "Projects",
	"项目的需求、方案和验收":     "Project requirements, proposals and sign-offs",
	"最近文件":            "Latest documents",
	"行政":              "Office",
	"办公室租赁、物业、采购和证照":  "Office lease, property management, purchasing and licenses",
	"家庭": "Family",
	"房子、车、孩子、健康等家庭事务": "House, car, kids, health and other family matters",
	"个人": "Personal",
	"证件、财务、工作、学习等个人文件": "IDs, finance, work, education and other personal documents",
	"工作室或公司":           "Studio or company",
	"客户、合同、财务、人事等业务文件": "Clients, contracts, finance, HR and other business documents",
}

// TemplateText is a template's (or the library drawer's) name,
// description or key fact in the language of ctx.
func TemplateText(ctx context.Context, s string) string {
	if i18n.From(ctx) != i18n.Zh {
		if en, ok := templateEN[s]; ok {
			return en
		}
	}
	return s
}
