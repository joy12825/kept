package demo

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"kept/server/internal/drawer"
	"kept/server/internal/filter"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// The prototype's drawers (FAMILY_DRAWERS, STUDIO_DRAWERS in mock.ts): their
// filing rules written with names, facts, documents put in by hand, and the
// dates of the yearbook's 大事记.

type ruleData struct {
	join   string // "" (first), and, or
	field  string // type, correspondent, text
	not    bool
	values []string
}

type factData struct {
	kind, label, field, period string // field: amount | due_date
	tags                       []string
	types, corrs               []string // narrow by type or correspondent
	grace                      int
	pinned                     []string // document titles
}

type drawerData struct {
	name, icon, color, created string // created: YYYY-MM-DD, "" for today
	rule                       []ruleData
	facts                      []factData
	manual                     []string // documents placed by hand
}

var familyDrawers = []drawerData{
	{name: "房子", icon: "home", color: "blue", created: "2023-01-15",
		rule: []ruleData{{field: "type", values: []string{"账单", "合同", "保修卡", "发票"}}, {join: "and", field: "correspondent", values: []string{"国家电网", "北京燃气", "链家", "京东"}}},
		facts: []factData{
			{kind: drawer.KindDue, label: "租约到期", field: "due_date", types: []string{"合同"}},
			{kind: drawer.KindSum, label: "水电费合计", field: "amount", tags: []string{"水电"}, period: "all"},
			{kind: drawer.KindExpect, label: "电费账单", corrs: []string{"国家电网"}, period: "monthly", grace: 7},
			{kind: drawer.KindPinned, label: "固定文档", pinned: []string{"房屋租赁合同", "热水器保修卡"}},
			{kind: drawer.KindDue, label: "保修到期", field: "due_date", tags: []string{"保修"}},
		},
		manual: []string{"热水器保修卡"}},
	{name: "车", icon: "car", color: "green", created: "2023-02-11",
		rule: []ruleData{{field: "correspondent", values: []string{"北京市车管所", "某某汽车服务中心"}}, {join: "or", field: "text", values: []string{"车险"}}},
		facts: []factData{
			{kind: drawer.KindDue, label: "年检到期", field: "due_date", corrs: []string{"北京市车管所"}},
			{kind: drawer.KindLatest, label: "最近车险", field: "amount", types: []string{"保单"}},
			{kind: drawer.KindPinned, label: "固定文档", pinned: []string{"机动车检验合格证明", "车险保单 2024"}},
		}},
	{name: "健康", icon: "heart", color: "red", created: "2023-04-18",
		rule: []ruleData{{field: "type", values: []string{"报告"}}, {join: "or", field: "correspondent", values: []string{"协和医院"}}},
		facts: []factData{
			{kind: drawer.KindLatest, label: "最近体检", types: []string{"报告"}},
			{kind: drawer.KindLatest, label: "最近医疗票据", field: "amount", types: []string{"发票"}},
			{kind: drawer.KindPinned, label: "固定文档", pinned: []string{"商业医疗保险保单"}},
		},
		manual: []string{"商业医疗保险保单"}},
	{name: "证件", icon: "idcard", color: "purple", created: "2022-05-10",
		rule: []ruleData{{field: "type", values: []string{"证件"}}, {join: "and", field: "text", not: true, values: []string{"儿童", "疫苗"}}},
		facts: []factData{
			{kind: drawer.KindDue, label: "证件到期", field: "due_date"},
			{kind: drawer.KindPinned, label: "固定文档", pinned: []string{"居民身份证复印件", "户口本首页"}},
		}},
	{name: "工作", icon: "briefcase", color: "orange", created: "2023-06-01",
		rule: []ruleData{{field: "correspondent", values: []string{"某某科技有限公司", "Apple", "中国铁路"}}},
		facts: []factData{
			{kind: drawer.KindSum, label: "待报销", field: "amount", tags: []string{"待报销"}, period: "all"},
			{kind: drawer.KindPinned, label: "固定文档", pinned: []string{"劳动合同", "MacBook Pro 保修卡"}},
		}},
	{name: "孩子", icon: "baby", color: "pink", created: "2024-06-15",
		rule:  []ruleData{{field: "text", values: []string{"儿童", "幼儿园", "疫苗"}}},
		facts: []factData{{kind: drawer.KindPinned, label: "固定文档", pinned: []string{"幼儿园保育协议", "儿童疫苗接种记录"}}}},
}

var studioDrawers = []drawerData{
	{name: "客户", icon: "users", color: "blue", created: "2024-02-20",
		rule:  []ruleData{{field: "type", values: []string{"证件"}}, {join: "and", field: "correspondent", values: []string{"星河科技", "蓝鲸传媒"}}},
		facts: []factData{{kind: drawer.KindPinned, label: "固定文档", pinned: []string{"营业执照副本-星河科技", "营业执照副本-蓝鲸传媒"}}}},
	{name: "合同", icon: "file", color: "green", created: "2024-01-01",
		rule: []ruleData{{field: "type", values: []string{"合同"}}, {join: "and", field: "correspondent", not: true, values: []string{"李明", "张婷"}}},
		facts: []factData{
			{kind: drawer.KindDue, label: "最近到期", field: "due_date"},
			{kind: drawer.KindSum, label: "合同金额", field: "amount", period: "all"},
		}},
	{name: "财务", icon: "wallet", color: "cyan", created: "2024-01-01",
		rule: []ruleData{{field: "type", values: []string{"发票"}}},
		facts: []factData{
			{kind: drawer.KindLatest, label: "最近开票", field: "amount", tags: []string{"税务"}},
			{kind: drawer.KindSum, label: "报销合计", field: "amount", tags: []string{"报销"}, period: "all"},
		}},
	{name: "人事", icon: "idcard", color: "purple", created: "2024-03-11",
		rule:  []ruleData{{field: "correspondent", values: []string{"李明", "张婷", "社保中心"}}},
		facts: []factData{{kind: drawer.KindExpect, label: "社保缴费明细", corrs: []string{"社保中心"}, period: "monthly", grace: 15}}},
	{name: "项目", icon: "kanban", color: "orange", created: "2024-04-18",
		rule:  []ruleData{{field: "type", values: []string{"报告"}}},
		facts: []factData{{kind: drawer.KindLatest, label: "最近验收", types: []string{"报告"}}}},
}

// dueIn: the built-in 到期日 of these documents, in days from loading, so
// that the home page has alerts (the samples are from 2022–2024).
var dueIn = map[string]int{
	"护照（孩子）": 21, "房屋租赁合同": 327, "热水器保修卡": 95, "机动车检验合格证明": 126, "服务合同-蓝鲸传媒": 43,
}

// milestones are the prototype's 大事记 entries that are not contracts or
// certificates (those appear by themselves).
var milestones = []string{"商业医疗保险保单", "租金发票 2024年第一季度", "车险保单 2024", "车险保单 2023"}

// yearbookTags are shown in the yearbook (用途标签).
var yearbookTags = []string{"税务", "报销"}

// loadDrawers creates the drawers (skipping names that exist), files the
// documents, applies the hand placements, due dates and marks, and checks
// the alerts. It returns how many drawers it created.
func loadDrawers(ctx context.Context, d Deps, scope tenant.Scope, specs []drawerData) (int, error) {
	if d.Drawers == nil {
		return 0, nil
	}
	ws := scope.WorkspaceID()
	q := sqlc.New(d.DB)
	ids := func(list func() ([]idName, error)) (map[string]int64, error) {
		rows, err := list()
		m := map[string]int64{}
		for _, r := range rows {
			m[r.name] = r.id
		}
		return m, err
	}
	types, err := ids(func() ([]idName, error) {
		rows, err := q.ListDocumentTypes(ctx, ws)
		out := make([]idName, len(rows))
		for i, r := range rows {
			out[i] = idName{r.ID, r.Name}
		}
		return out, err
	})
	if err != nil {
		return 0, err
	}
	corrs, err := ids(func() ([]idName, error) {
		rows, err := q.ListCorrespondents(ctx, ws)
		out := make([]idName, len(rows))
		for i, r := range rows {
			out[i] = idName{r.ID, r.Name}
		}
		return out, err
	})
	if err != nil {
		return 0, err
	}
	tags, err := ids(func() ([]idName, error) {
		rows, err := q.ListTags(ctx, ws)
		out := make([]idName, len(rows))
		for i, r := range rows {
			out[i] = idName{r.ID, r.Name}
		}
		return out, err
	})
	if err != nil {
		return 0, err
	}
	docID := func(title string) int64 {
		id, err := q.FindDocumentByTitle(ctx, sqlc.FindDocumentByTitleParams{WorkspaceID: ws, Title: title})
		if err != nil {
			return 0
		}
		return id
	}
	pick := func(m map[string]int64, names []string) []int64 {
		var out []int64
		for _, n := range names {
			if id, ok := m[n]; ok {
				out = append(out, id)
			}
		}
		return out
	}
	field := func(key string) int64 {
		id, _ := q.GetBuiltInField(ctx, sqlc.GetBuiltInFieldParams{WorkspaceID: ws, Key: sql.NullString{String: key, Valid: true}})
		return id
	}

	existing, err := d.Drawers.List(ctx, scope)
	if err != nil {
		return 0, err
	}
	have := map[string]bool{}
	for _, x := range existing {
		have[x.Name] = true
	}
	created := 0
	for _, spec := range specs {
		if have[spec.name] {
			continue
		}
		rule := buildRule(spec.rule, types, corrs)
		dr, _, err := d.Drawers.Create(ctx, scope, drawer.Input{Name: spec.name, Icon: spec.icon, Color: spec.color, Rule: rule})
		if err != nil {
			return created, err
		}
		created++
		if spec.created != "" {
			if t, err := time.ParseInLocation("2006-01-02", spec.created, time.Local); err == nil {
				if _, err := d.DB.ExecContext(ctx, `UPDATE drawers SET created_at = ? WHERE id = ?`, t.Add(9*time.Hour).UnixMilli(), dr.ID); err != nil {
					return created, err
				}
			}
		}
		for _, f := range spec.facts {
			cfg := drawer.FactConfig{Period: f.period, GraceDays: f.grace, TagIDs: pick(tags, f.tags)}
			if f.field != "" {
				cfg.FieldID = field(f.field)
			}
			var all []filter.Cond
			if ids := pick(types, f.types); len(ids) > 0 {
				all = append(all, cond("type", "in", ids))
			}
			if ids := pick(corrs, f.corrs); len(ids) > 0 {
				all = append(all, cond("correspondent", "in", ids))
			}
			if len(all) == 1 {
				cfg.Filter = &all[0]
			} else if len(all) > 1 {
				cfg.Filter = &filter.Cond{All: all}
			}
			for _, t := range f.pinned {
				if id := docID(t); id != 0 {
					cfg.DocumentIDs = append(cfg.DocumentIDs, id)
				}
			}
			if _, err := d.Drawers.AddFact(ctx, scope, dr.ID, drawer.Fact{Kind: f.kind, Label: f.label, Config: cfg}); err != nil {
				return created, err
			}
		}
	}
	if _, err := d.Drawers.Reassign(ctx, scope); err != nil {
		return created, err
	}

	// Hand placements, due dates, milestones and yearbook tags (idempotent).
	all, err := d.Drawers.List(ctx, scope)
	if err != nil {
		return created, err
	}
	byName := map[string]int64{}
	for _, x := range all {
		byName[x.Name] = x.ID
	}
	for _, spec := range specs {
		for _, t := range spec.manual {
			if id := docID(t); id != 0 {
				if err := d.Drawers.SetManual(ctx, d.DB, ws, id, byName[spec.name], false); err != nil {
					return created, err
				}
			}
		}
	}
	due := field("due_date")
	for title, days := range dueIn {
		id := docID(title)
		if id == 0 || due == 0 {
			continue
		}
		date := time.Now().AddDate(0, 0, days).Format("2006-01-02")
		if _, err := d.DB.ExecContext(ctx, `INSERT INTO custom_field_values (document_id, field_id, value_date) VALUES (?, ?, ?)
			ON CONFLICT (document_id, field_id) DO UPDATE SET value_date = excluded.value_date`, id, due, date); err != nil {
			return created, err
		}
	}
	for _, t := range milestones {
		if id := docID(t); id != 0 {
			if err := q.SetDocumentMilestone(ctx, sqlc.SetDocumentMilestoneParams{IsMilestone: 1, WorkspaceID: ws, ID: id}); err != nil {
				return created, err
			}
		}
	}
	for _, t := range yearbookTags {
		if _, err := d.DB.ExecContext(ctx, `UPDATE tags SET show_in_yearbook = 1 WHERE workspace_id = ? AND name = ?`, ws, t); err != nil {
			return created, err
		}
	}
	_, err = d.Drawers.RefreshAlerts(ctx, scope)
	return created, err
}

type idName struct {
	id   int64
	name string
}

func cond(field, op string, v any) filter.Cond {
	b, _ := json.Marshal(v)
	return filter.Cond{Field: field, Op: op, Value: b}
}

// buildRule turns the prototype's flat conditions into filter JSON, the
// way the web app's rule editor does (and binds tighter than or).
func buildRule(rows []ruleData, types, corrs map[string]int64) *filter.Cond {
	var groups [][]filter.Cond
	for i, r := range rows {
		var c filter.Cond
		switch r.field {
		case "text":
			var any []filter.Cond
			for _, w := range r.values {
				w := w
				any = append(any, filter.Cond{Text: &w})
			}
			c = filter.Cond{Any: any}
			if len(any) == 1 {
				c = any[0]
			}
			if r.not {
				inner := c
				c = filter.Cond{Not: &inner}
			}
		default:
			m := types
			if r.field == "correspondent" {
				m = corrs
			}
			var ids []int64
			for _, n := range r.values {
				if id, ok := m[n]; ok {
					ids = append(ids, id)
				}
			}
			if len(ids) == 0 {
				continue
			}
			op := "in"
			if r.not {
				op = "not_in"
			}
			c = cond(r.field, op, ids)
		}
		if i == 0 || r.join == "or" || len(groups) == 0 {
			groups = append(groups, []filter.Cond{c})
		} else {
			groups[len(groups)-1] = append(groups[len(groups)-1], c)
		}
	}
	var parts []filter.Cond
	for _, g := range groups {
		if len(g) == 1 {
			parts = append(parts, g[0])
		} else {
			parts = append(parts, filter.Cond{All: g})
		}
	}
	switch len(parts) {
	case 0:
		return nil
	case 1:
		return &parts[0]
	}
	return &filter.Cond{Any: parts}
}
