package api

import (
	"context"
	"encoding/json"
	"kept/server/internal/i18n"
	"strings"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"kept/server/internal/api/gen"
	"kept/server/internal/drawer"
	"kept/server/internal/filter"
	"kept/server/internal/tenant"
)

// convert copies src into dst through JSON (for generated types with
// nested anonymous structs).
func convert(src, dst any) {
	b, _ := json.Marshal(src)
	_ = json.Unmarshal(b, dst)
}

func condMap(c *filter.Cond) *map[string]any {
	if c == nil {
		return nil
	}
	m := map[string]any{}
	convert(c, &m)
	return &m
}

func mapCond(m *map[string]any) (*filter.Cond, error) {
	if m == nil {
		return nil, nil
	}
	raw, err := json.Marshal(*m)
	if err != nil {
		return nil, err
	}
	c, err := filter.Parse(raw)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func dateOf(s string) *openapi_types.Date {
	if s == "" {
		return nil
	}
	return apiDate(s)
}

func toAPIFact(f drawer.Fact) gen.Fact {
	c := f.Config
	cfg := gen.FactConfig{Filter: condMap(c.Filter), Period: strPtr(c.Period)}
	if len(c.TagIDs) > 0 {
		cfg.TagIds = &c.TagIDs
	}
	if c.FieldID != 0 {
		cfg.FieldId = &c.FieldID
	}
	if c.AdvanceDays != 0 {
		cfg.AdvanceDays = &c.AdvanceDays
	}
	if c.GraceDays != 0 {
		cfg.GraceDays = &c.GraceDays
	}
	if len(c.DocumentIDs) > 0 {
		cfg.DocumentIds = &c.DocumentIDs
	}
	cfg.MetricId, cfg.PersonId = idPtr(c.MetricID), idPtr(c.PersonID)
	return gen.Fact{Id: f.ID, DrawerId: f.DrawerID, Kind: gen.FactKind(f.Kind), Label: f.Label, Config: cfg}
}

func fromAPIFactConfig(c gen.FactConfig) (drawer.FactConfig, error) {
	f, err := mapCond(c.Filter)
	if err != nil {
		return drawer.FactConfig{}, err
	}
	return drawer.FactConfig{Filter: f, TagIDs: derefIDs(c.TagIds), FieldID: derefID(c.FieldId), AdvanceDays: derefInt(c.AdvanceDays),
		Period: deref(c.Period), GraceDays: derefInt(c.GraceDays), DocumentIDs: derefIDs(c.DocumentIds), MetricID: derefID(c.MetricId), PersonID: derefID(c.PersonId)}, nil
}

func docRef(d drawer.DocRef) gen.DocRef {
	return gen.DocRef{Id: d.ID, Title: d.Title, Date: *apiDate(d.Date)}
}

// toAPIFactValue translates what the server wrote (a period, yes or no)
// to the request's language (v1.10.1).
func toAPIFactValue(ctx context.Context, v drawer.Value) gen.FactValue {
	out := gen.FactValue{Fact: toAPIFact(v.Fact), Warn: v.Warn, Filter: condMap(v.Filter), FieldValue: v.FieldValue}
	if v.FieldValue != nil && v.FieldType == "boolean" {
		out.FieldValue = strPtr(i18n.T(ctx, *v.FieldValue))
	}
	if v.Document != nil {
		d := docRef(*v.Document)
		out.Document = &d
	}
	switch v.Fact.Kind {
	case drawer.KindLatest:
		if v.FieldType != "" {
			out.FieldType = &v.FieldType
		}
	case drawer.KindDue:
		if v.DueDate != "" {
			out.DueDate, out.DaysLeft = dateOf(v.DueDate), &v.DaysLeft
		}
	case drawer.KindSum:
		out.Total, out.Count, out.Money = strPtr(v.Total), &v.Count, &v.Money
		convert(v.Months, &out.Months)
	case drawer.KindExpect:
		label := i18n.T(ctx, v.PeriodLabel)
		out.PeriodLabel, out.Present = &label, &v.Present
	case drawer.KindPinned:
		docs := make([]gen.DocRef, len(v.Documents))
		for i, d := range v.Documents {
			docs[i] = docRef(d)
		}
		out.Documents = &docs
	case drawer.KindMetric:
		if v.Metric != nil {
			if v.Metric.Values == nil {
				v.Metric.Values = []float64{}
			}
			convert(v.Metric, &out.Metric)
		}
	}
	return out
}

func (s *Server) toAPIDrawer(ctx context.Context, d drawer.Drawer, count, alerts int, primary *drawer.Value, shared ...int) gen.Drawer {
	out := gen.Drawer{Id: d.ID, Name: d.Name, Kind: gen.DrawerKind(d.Kind), Icon: gen.DrawerIcon(d.Icon), Color: gen.DrawerColor(d.Color), Description: d.Description,
		TemplateKey: strPtr(d.TemplateKey), Rule: condMap(d.Rule), DocumentCount: count, AlertCount: alerts, CreatedAt: d.CreatedAt, Sensitive: optTrue(d.Sensitive),
		SameNameVersions: optTrue(d.SameNameVersions)}
	if len(shared) > 0 && shared[0] > 0 {
		out.SharedCount = &shared[0]
	}
	if primary != nil {
		v := toAPIFactValue(ctx, *primary)
		out.Primary = &v
	}
	return out
}

// drawerExtras loads what the drawer list shows besides the drawers.
func (s *Server) drawerExtras(ctx context.Context, scope tenant.Scope) (map[int64]int64, map[int64]int, map[int64]drawer.Value, error) {
	counts, err := s.drawers.Counts(ctx, scope)
	if err != nil {
		return nil, nil, nil, err
	}
	alerts, err := s.drawers.Alerts(ctx, scope)
	if err != nil {
		return nil, nil, nil, err
	}
	perDrawer := map[int64]int{}
	for _, a := range alerts {
		perDrawer[a.DrawerID]++
	}
	prim, err := s.drawers.PrimaryValues(ctx, scope)
	return counts, perDrawer, prim, err
}

func (s *Server) savedDrawer(ctx context.Context, scope tenant.Scope, d drawer.Drawer, job int64) (gen.DrawerSaved, error) {
	counts, alerts, prim, err := s.drawerExtras(ctx, scope)
	if err != nil {
		return gen.DrawerSaved{}, err
	}
	var p *drawer.Value
	if v, ok := prim[d.ID]; ok {
		p = &v
	}
	shares, err := s.drawers.ShareCounts(ctx, scope)
	if err != nil {
		return gen.DrawerSaved{}, err
	}
	out := gen.DrawerSaved{Drawer: s.toAPIDrawer(ctx, d, int(counts[d.ID]), alerts[d.ID], p, shares[d.ID])}
	if job != 0 {
		out.JobId = &job
	}
	return out, nil
}

func (s *Server) ListDrawers(ctx context.Context, _ gen.ListDrawersRequestObject) (gen.ListDrawersResponseObject, error) {
	scope := tenant.MustFrom(ctx)
	ds, err := s.drawers.List(ctx, scope)
	if err != nil {
		return nil, err
	}
	counts, alerts, prim, err := s.drawerExtras(ctx, scope)
	if err != nil {
		return nil, err
	}
	shares, err := s.drawers.ShareCounts(ctx, scope)
	if err != nil {
		return nil, err
	}
	out := gen.DrawerList{Items: make([]gen.Drawer, len(ds)), Unfiled: int(counts[0])}
	for i, d := range ds {
		var p *drawer.Value
		if v, ok := prim[d.ID]; ok {
			p = &v
		}
		out.Items[i] = s.toAPIDrawer(ctx, d, int(counts[d.ID]), alerts[d.ID], p, shares[d.ID])
	}
	return gen.ListDrawers200JSONResponse(out), nil
}

func (s *Server) CreateDrawer(ctx context.Context, req gen.CreateDrawerRequestObject) (gen.CreateDrawerResponseObject, error) {
	b := req.Body
	scope := tenant.MustFrom(ctx)
	rule, err := mapCond(b.Rule)
	if err != nil {
		return nil, err
	}
	in := drawer.Input{Name: b.Name, Description: deref(b.Description), Rule: rule}
	if b.Icon != nil {
		in.Icon = string(*b.Icon)
	}
	if b.Color != nil {
		in.Color = string(*b.Color)
	}
	d, job, err := s.drawers.Create(ctx, scope, in)
	if err != nil {
		return nil, err
	}
	out, err := s.savedDrawer(ctx, scope, d, job)
	if err != nil {
		return nil, err
	}
	return gen.CreateDrawer201JSONResponse(out), nil
}

func (s *Server) GetDrawer(ctx context.Context, req gen.GetDrawerRequestObject) (gen.GetDrawerResponseObject, error) {
	scope := tenant.MustFrom(ctx)
	d, err := s.drawers.Get(ctx, scope, req.Id)
	if err != nil {
		return nil, err
	}
	saved, err := s.savedDrawer(ctx, scope, d, 0)
	if err != nil {
		return nil, err
	}
	vals, err := s.drawers.ComputeAll(ctx, scope, d.ID)
	if err != nil {
		return nil, err
	}
	var out gen.DrawerDetail
	convert(saved.Drawer, &out)
	out.Facts = make([]gen.FactValue, len(vals))
	for i, v := range vals {
		out.Facts[i] = toAPIFactValue(ctx, v)
	}
	st, err := s.drawers.Stats(ctx, scope, d.ID)
	if err != nil {
		return nil, err
	}
	out.ManualCount, out.FirstYear, out.LastYear = st.Manual, st.FirstYear, st.LastYear
	return gen.GetDrawer200JSONResponse(out), nil
}

func (s *Server) UpdateDrawer(ctx context.Context, req gen.UpdateDrawerRequestObject) (gen.UpdateDrawerResponseObject, error) {
	b := req.Body
	scope := tenant.MustFrom(ctx)
	p := drawer.Patch{Name: b.Name, Description: b.Description, Sensitive: b.Sensitive, SameNameVersions: b.SameNameVersions}
	if b.Icon != nil {
		v := string(*b.Icon)
		p.Icon = &v
	}
	if b.Color != nil {
		v := string(*b.Color)
		p.Color = &v
	}
	if b.ClearRule != nil && *b.ClearRule {
		p.SetRule = true
	} else if b.Rule != nil {
		rule, err := mapCond(b.Rule)
		if err != nil {
			return nil, err
		}
		p.SetRule, p.Rule = true, rule
	}
	d, job, err := s.drawers.Update(ctx, scope, req.Id, p)
	if err != nil {
		return nil, err
	}
	out, err := s.savedDrawer(ctx, scope, d, job)
	if err != nil {
		return nil, err
	}
	return gen.UpdateDrawer200JSONResponse(out), nil
}

func jobStarted(id int64) gen.JobStarted {
	if id == 0 {
		return gen.JobStarted{}
	}
	return gen.JobStarted{JobId: &id}
}

func (s *Server) DeleteDrawer(ctx context.Context, req gen.DeleteDrawerRequestObject) (gen.DeleteDrawerResponseObject, error) {
	job, err := s.drawers.Delete(ctx, tenant.MustFrom(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	return gen.DeleteDrawer200JSONResponse(jobStarted(job)), nil
}

func (s *Server) ReorderDrawers(ctx context.Context, req gen.ReorderDrawersRequestObject) (gen.ReorderDrawersResponseObject, error) {
	job, err := s.drawers.Reorder(ctx, tenant.MustFrom(ctx), req.Body.Ids)
	if err != nil {
		return nil, err
	}
	return gen.ReorderDrawers200JSONResponse(jobStarted(job)), nil
}

func (s *Server) PreviewDrawers(ctx context.Context, req gen.PreviewDrawersRequestObject) (gen.PreviewDrawersResponseObject, error) {
	b := req.Body
	in := drawer.PreviewInput{DrawerID: derefID(b.DrawerId)}
	if b.Order != nil {
		in.Order = *b.Order
	}
	if b.ClearRule != nil && *b.ClearRule {
		in.SetRule = true
	} else if b.Rule != nil {
		rule, err := mapCond(b.Rule)
		if err != nil {
			return nil, err
		}
		in.SetRule, in.Rule = true, rule
	}
	if in.SetRule && in.DrawerID == 0 {
		return nil, invalid("预览新规则需要指定抽屉")
	}
	if !in.SetRule && in.Order == nil {
		return nil, invalid("请给出新的规则或新的顺序")
	}
	p, err := s.drawers.Preview(ctx, tenant.MustFrom(ctx), in)
	if err != nil {
		return nil, err
	}
	out := gen.DrawerPreview{Total: p.Total, Manual: p.Manual, In: p.In, New: p.New, Out: p.Out, Unfiled: p.Unfiled, Moves: p.Moves,
		Samples: make([]gen.DrawerMove, len(p.Samples))}
	for i, m := range p.Samples {
		out.Samples[i] = gen.DrawerMove{DocumentId: m.DocumentID, Title: m.Title, From: idPtr(m.From), To: idPtr(m.To)}
	}
	return gen.PreviewDrawers200JSONResponse(out), nil
}

func (s *Server) CreateDrawersFromTemplates(ctx context.Context, req gen.CreateDrawersFromTemplatesRequestObject) (gen.CreateDrawersFromTemplatesResponseObject, error) {
	scope := tenant.MustFrom(ctx)
	ds, job, err := s.drawers.FromTemplates(ctx, scope, req.Body.Keys)
	if err != nil {
		return nil, err
	}
	counts, alerts, prim, err := s.drawerExtras(ctx, scope)
	if err != nil {
		return nil, err
	}
	out := gen.DrawersCreated{Drawers: make([]gen.Drawer, len(ds))}
	for i, d := range ds {
		var p *drawer.Value
		if v, ok := prim[d.ID]; ok {
			p = &v
		}
		out.Drawers[i] = s.toAPIDrawer(ctx, d, int(counts[d.ID]), alerts[d.ID], p)
	}
	if job != 0 {
		out.JobId = &job
	}
	return gen.CreateDrawersFromTemplates201JSONResponse(out), nil
}

func (s *Server) ListDrawerTemplates(ctx context.Context, _ gen.ListDrawerTemplatesRequestObject) (gen.ListDrawerTemplatesResponseObject, error) {
	type tpl struct {
		Key         string   `json:"key"`
		Name        string   `json:"name"`
		Icon        string   `json:"icon"`
		Color       string   `json:"color"`
		Description string   `json:"description"`
		Facts       []string `json:"facts"`
	}
	type scene struct {
		Key         string   `json:"key"`
		Name        string   `json:"name"`
		Icon        string   `json:"icon"`
		Description string   `json:"description"`
		Templates   []string `json:"templates"`
	}
	var body struct {
		Scenes    []scene `json:"scenes"`
		Templates []tpl   `json:"templates"`
	}
	for _, sc := range drawer.Scenes {
		body.Scenes = append(body.Scenes, scene{sc.Key, drawer.TemplateText(ctx, sc.Name), sc.Icon, drawer.TemplateText(ctx, sc.Description), sc.Templates})
	}
	for _, t := range drawer.Templates {
		var facts []string
		for _, f := range t.Facts {
			facts = append(facts, drawer.TemplateText(ctx, f.Label))
		}
		body.Templates = append(body.Templates, tpl{t.Key, drawer.TemplateText(ctx, t.Name), t.Icon, t.Color, drawer.TemplateText(ctx, t.Description), facts})
	}
	var out gen.DrawerTemplates
	convert(body, &out)
	return gen.ListDrawerTemplates200JSONResponse(out), nil
}

func (s *Server) CreateDrawerFact(ctx context.Context, req gen.CreateDrawerFactRequestObject) (gen.CreateDrawerFactResponseObject, error) {
	scope := tenant.MustFrom(ctx)
	cfg, err := fromAPIFactConfig(req.Body.Config)
	if err != nil {
		return nil, err
	}
	f, err := s.drawers.AddFact(ctx, scope, req.Id, drawer.Fact{Kind: string(req.Body.Kind), Label: req.Body.Label, Config: cfg})
	if err != nil {
		return nil, err
	}
	v, err := s.drawers.Compute(ctx, scope, f)
	if err != nil {
		return nil, err
	}
	return gen.CreateDrawerFact201JSONResponse(toAPIFactValue(ctx, v)), nil
}

func (s *Server) UpdateDrawerFact(ctx context.Context, req gen.UpdateDrawerFactRequestObject) (gen.UpdateDrawerFactResponseObject, error) {
	scope := tenant.MustFrom(ctx)
	cfg, err := fromAPIFactConfig(req.Body.Config)
	if err != nil {
		return nil, err
	}
	f, err := s.drawers.UpdateFact(ctx, scope, req.Id, req.Body.Label, cfg)
	if err != nil {
		return nil, err
	}
	v, err := s.drawers.Compute(ctx, scope, f)
	if err != nil {
		return nil, err
	}
	return gen.UpdateDrawerFact200JSONResponse(toAPIFactValue(ctx, v)), nil
}

func (s *Server) DeleteDrawerFact(ctx context.Context, req gen.DeleteDrawerFactRequestObject) (gen.DeleteDrawerFactResponseObject, error) {
	if err := s.drawers.DeleteFact(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	return gen.DeleteDrawerFact204Response{}, nil
}

func (s *Server) ReorderDrawerFacts(ctx context.Context, req gen.ReorderDrawerFactsRequestObject) (gen.ReorderDrawerFactsResponseObject, error) {
	scope := tenant.MustFrom(ctx)
	if _, err := s.drawers.Get(ctx, scope, req.Id); err != nil {
		return nil, err
	}
	if err := s.drawers.ReorderFacts(ctx, scope, req.Id, req.Body.Ids); err != nil {
		return nil, err
	}
	return gen.ReorderDrawerFacts204Response{}, nil
}

func (s *Server) ListAlerts(ctx context.Context, _ gen.ListAlertsRequestObject) (gen.ListAlertsResponseObject, error) {
	as, err := s.drawers.Alerts(ctx, tenant.MustFrom(ctx))
	if err != nil {
		return nil, err
	}
	out := make(gen.ListAlerts200JSONResponse, len(as))
	notes := s.stewardOn(ctx, tenant.MustFrom(ctx).WorkspaceID())
	for i, a := range as {
		x := gen.Alert{Id: a.ID, DrawerId: a.DrawerID, FactId: a.FactID, Kind: gen.AlertKind(a.Kind), Text: i18n.T(ctx, a.Text), Warn: a.Warn,
			CreatedAt: a.CreatedAt, DocumentId: idPtr(a.DocumentID), DueDate: dateOf(a.DueDate), PeriodLabel: strPtr(i18n.T(ctx, a.PeriodLabel))}
		if notes {
			x.Note = strPtr(a.Note)
		}
		if a.Kind == "due" {
			x.DaysLeft = &as[i].DaysLeft
		}
		out[i] = x
	}
	return out, nil
}

func (s *Server) DismissAlert(ctx context.Context, req gen.DismissAlertRequestObject) (gen.DismissAlertResponseObject, error) {
	if err := s.drawers.SetAlertDismissed(ctx, tenant.MustFrom(ctx), req.Id, true); err != nil {
		return nil, err
	}
	return gen.DismissAlert204Response{}, nil
}

func (s *Server) RestoreAlert(ctx context.Context, req gen.RestoreAlertRequestObject) (gen.RestoreAlertResponseObject, error) {
	if err := s.drawers.SetAlertDismissed(ctx, tenant.MustFrom(ctx), req.Id, false); err != nil {
		return nil, err
	}
	return gen.RestoreAlert204Response{}, nil
}

func (s *Server) ListYearbookYears(ctx context.Context, _ gen.ListYearbookYearsRequestObject) (gen.ListYearbookYearsResponseObject, error) {
	ys, err := s.drawers.Years(ctx, tenant.MustFrom(ctx))
	if err != nil {
		return nil, err
	}
	return gen.ListYearbookYears200JSONResponse(ys), nil
}

func (s *Server) GetYearbook(ctx context.Context, req gen.GetYearbookRequestObject) (gen.GetYearbookResponseObject, error) {
	y, err := s.drawers.Yearbook(ctx, tenant.MustFrom(ctx), req.Year)
	if err != nil {
		return nil, err
	}
	var out gen.Yearbook
	convert(y, &out)
	for i := range out.Events { // after the narrative's basis: that is of the Chinese (v1.10.1)
		out.Events[i].Text = yearEventText(ctx, out.Events[i].Text)
	}
	if err := s.withNarrative(ctx, req.Year, y, &out); err != nil {
		return nil, err
	}
	return gen.GetYearbook200JSONResponse(out), nil
}

// withRuleDrawer adds what only the detail page needs: for a document placed
// by hand, the drawer the rules would give it; a book's details; whether it
// has recognised results (v1.9.11).
func (s *Server) withRuleDrawer(ctx context.Context, d gen.Document) gen.Document {
	if d.IsBook && s.books != nil {
		if b, err := s.books.Get(ctx, tenant.MustFrom(ctx), d.Id); err == nil && b != nil {
			ab := toAPIBook(*b)
			d.Book = &ab
		}
	}
	if s.hasLab(ctx, d.Id) {
		has := true
		d.HasLab = &has
	}
	if !d.DrawerManual || s.drawers == nil {
		return d
	}
	if id, err := s.drawers.RuleDrawer(ctx, tenant.MustFrom(ctx), d.Id); err == nil {
		d.RuleDrawerId = idPtr(id)
	}
	return d
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// yearEventText translates a 大事记 entry: “新建抽屉 名称”, or a rule's
// verb and the document (“签订 租房合同”), whose default verbs are in the
// catalog.
func yearEventText(ctx context.Context, text string) string {
	if en := i18n.T(ctx, text); en != text {
		return en
	}
	if verb, rest, ok := strings.Cut(text, " "); ok {
		if en := i18n.T(ctx, verb); en != verb {
			return en + " " + rest
		}
	}
	return text
}

func (s *Server) milestoneRules(ctx context.Context) (gen.MilestoneRules, error) {
	rules, def, err := s.drawers.MilestoneRules(ctx, tenant.MustFrom(ctx))
	if err != nil {
		return gen.MilestoneRules{}, err
	}
	out := gen.MilestoneRules{Default: def}
	convert(rules, &out.Rules)
	if def { // the default verbs, in the request's language
		for i := range out.Rules {
			out.Rules[i].Verb = i18n.T(ctx, out.Rules[i].Verb)
		}
	}
	if out.Rules == nil {
		convert([]any{}, &out.Rules)
	}
	return out, nil
}

func (s *Server) GetMilestoneRules(ctx context.Context, _ gen.GetMilestoneRulesRequestObject) (gen.GetMilestoneRulesResponseObject, error) {
	out, err := s.milestoneRules(ctx)
	if err != nil {
		return nil, err
	}
	return gen.GetMilestoneRules200JSONResponse(out), nil
}

func (s *Server) SetMilestoneRules(ctx context.Context, req gen.SetMilestoneRulesRequestObject) (gen.SetMilestoneRulesResponseObject, error) {
	var rules []drawer.MilestoneRule
	if req.Body.Rules != nil {
		convert(*req.Body.Rules, &rules)
	}
	if err := s.drawers.SetMilestoneRules(ctx, tenant.MustFrom(ctx), rules, req.Body.Reset != nil && *req.Body.Reset); err != nil {
		return nil, err
	}
	out, err := s.milestoneRules(ctx)
	if err != nil {
		return nil, err
	}
	return gen.SetMilestoneRules200JSONResponse(out), nil
}
