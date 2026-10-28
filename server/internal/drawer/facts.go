package drawer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"kept/server/internal/i18n"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"kept/server/internal/apperr"
	"kept/server/internal/filter"
	"kept/server/internal/privacy"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// Fact kinds (design doc 3.6).
const (
	KindLatest = "latest" // the newest matching document, with one field
	KindDue    = "due"    // the next date in a date field
	KindSum    = "sum"    // a number or money field added up over a period
	KindExpect = "expect" // one matching document per period
	KindPinned = "pinned" // chosen documents
	KindMetric = "metric" // a health metric's latest value (v1.8.3)
)

const (
	defaultAdvance = 30
	maxPinned      = 10
	maxLabelLen    = 20
	// overdueWindow: a due date this many days past still shows and alerts;
	// older ones (an expired document long replaced) are left alone.
	overdueWindow = 30
)

// FactConfig is a fact's settings; which fields apply depends on the kind.
type FactConfig struct {
	TagIDs      []int64      `json:"tagIds,omitempty"`      // any kind: only documents with one of these tags (or a sub-tag)
	Filter      *filter.Cond `json:"filter,omitempty"`      // any kind but pinned: only matching documents
	FieldID     int64        `json:"fieldId,omitempty"`     // latest: shown; due: a date field; sum: a number or money field
	AdvanceDays int          `json:"advanceDays,omitempty"` // due: alert this many days ahead (default 30)
	Period      string       `json:"period,omitempty"`      // sum: this_year | last_12_months | all; expect: monthly | quarterly | yearly
	GraceDays   int          `json:"graceDays,omitempty"`   // expect: days into a period before its document is missed
	DocumentIDs []int64      `json:"documentIds,omitempty"` // pinned
	MetricID    int64        `json:"metricId,omitempty"`    // metric
	PersonID    int64        `json:"personId,omitempty"`    // metric: whose; 0 whoever was measured last
}

// MetricCard is a metric's latest value on a fact card (package health
// fills it in, v1.8.3).
type MetricCard struct {
	MetricID     int64     `json:"metricId"`
	PersonID     int64     `json:"personId,omitempty"`
	Name         string    `json:"name"`
	Unit         string    `json:"unit"`
	Date         string    `json:"date"`
	Text         string    `json:"text"`
	Flag         string    `json:"flag"`
	Value        *float64  `json:"value,omitempty"`
	DocumentID   int64     `json:"documentId"`
	Previous     *float64  `json:"previous,omitempty"`
	PreviousText string    `json:"previousText,omitempty"`
	Values       []float64 `json:"values"`
}

// MetricSource reads metric cards and checks a metric and person exist.
type MetricSource interface {
	MetricCard(ctx context.Context, scope tenant.Scope, metricID, personID int64) (*MetricCard, error)
	CheckMetric(ctx context.Context, ws, metricID, personID int64) error
}

// SetMetrics lets facts show health metrics.
func (s *Service) SetMetrics(m MetricSource) { s.metrics = m }

// Fact is one fact of a drawer.
type Fact struct {
	ID       int64
	DrawerID int64
	Kind     string
	Label    string
	Config   FactConfig
}

var (
	ErrFactNotFound = apperr.New(apperr.KindNotFound, "找不到这个要点")
	errFactLabel    = apperr.New(apperr.KindInvalid, "要点的名称不能为空，最多 20 个字")
	errFactKind     = apperr.New(apperr.KindInvalid, "要点的种类不正确")
	errFactOrder    = apperr.New(apperr.KindInvalid, "要点的顺序应包含这个抽屉的全部要点")
)

func invalid(msg string) error { return apperr.New(apperr.KindInvalid, msg) }

func toFact(r sqlc.DrawerFact) Fact {
	f := Fact{ID: r.ID, DrawerID: r.DrawerID, Kind: r.Kind, Label: r.Label}
	_ = json.Unmarshal([]byte(r.Config), &f.Config)
	return f
}

// Facts lists a drawer's facts in order.
func (s *Service) Facts(ctx context.Context, scope tenant.Scope, drawerID int64) ([]Fact, error) {
	if _, err := s.Get(ctx, scope, drawerID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListDrawerFacts(ctx, sqlc.ListDrawerFactsParams{WorkspaceID: scope.WorkspaceID(), DrawerID: drawerID})
	if err != nil {
		return nil, err
	}
	out := make([]Fact, len(rows))
	for i, r := range rows {
		out[i] = toFact(r)
	}
	return out, nil
}

// checkFact validates a fact against the workspace and tidies its config.
func checkFact(ctx context.Context, q *sqlc.Queries, scope tenant.Scope, f *Fact) error {
	f.Label = strings.TrimSpace(f.Label)
	if n := utf8.RuneCountInString(f.Label); n < 1 || n > maxLabelLen {
		return errFactLabel
	}
	c := &f.Config
	ws := scope.WorkspaceID()
	cat, err := filter.LoadCatalog(ctx, q, scope)
	if err != nil {
		return err
	}
	cat.Substring = true
	if len(c.TagIDs) > 0 {
		if _, err := filter.Compile(cat, scope, tagCond(c.TagIDs)); err != nil {
			return err
		}
	}
	if c.Filter != nil {
		if c.Filter.Uses("drawer") {
			return invalid("要点的条件不能以“所在抽屉”为条件")
		}
		if _, err := filter.Compile(cat, scope, *c.Filter); err != nil {
			return err
		}
	}
	fieldType := func() (string, error) {
		fd, err := q.GetCustomField(ctx, sqlc.GetCustomFieldParams{WorkspaceID: ws, ID: c.FieldID})
		if errors.Is(err, sql.ErrNoRows) {
			return "", invalid("要点用到的字段不存在")
		}
		return fd.DataType, err
	}
	switch f.Kind {
	case KindLatest:
		if c.FieldID != 0 {
			if _, err := fieldType(); err != nil {
				return err
			}
		}
	case KindDue:
		t, err := fieldType()
		if err != nil {
			return err
		}
		if t != "date" {
			return invalid("到期要点需要一个日期字段")
		}
		if c.AdvanceDays == 0 {
			c.AdvanceDays = defaultAdvance
		}
		if c.AdvanceDays < 1 || c.AdvanceDays > 366 {
			return invalid("提前提醒的天数应在 1 到 366 之间")
		}
	case KindSum:
		t, err := fieldType()
		if err != nil {
			return err
		}
		if t != "monetary" && t != "integer" && t != "float" {
			return invalid("汇总要点需要一个金额或数字字段")
		}
		if c.Period == "" {
			c.Period = "this_year"
		}
		if c.Period != "this_year" && c.Period != "last_12_months" && c.Period != "all" {
			return invalid("汇总的周期不正确")
		}
	case KindExpect:
		if c.Period == "" {
			c.Period = "monthly"
		}
		if c.Period != "monthly" && c.Period != "quarterly" && c.Period != "yearly" {
			return invalid("周期检查的周期不正确")
		}
		if c.GraceDays < 0 || c.GraceDays > 60 {
			return invalid("宽限天数应在 0 到 60 之间")
		}
	case KindPinned:
		if len(c.DocumentIDs) > maxPinned {
			return invalid(fmt.Sprintf("固定文档最多 %d 份", maxPinned))
		}
		for _, id := range c.DocumentIDs {
			if _, err := q.GetDocumentTitle(ctx, sqlc.GetDocumentTitleParams{WorkspaceID: ws, ID: id, Viewer: scope.Viewer()}); err != nil {
				return invalid("固定的文档不存在")
			}
		}
	case KindMetric:
		if c.MetricID <= 0 || c.PersonID < 0 {
			return invalid("指标要点需要选一个指标")
		}
	default:
		return errFactKind
	}
	if f.Kind == KindPinned {
		c.Filter = nil
	} else {
		c.DocumentIDs = nil
	}
	if f.Kind != KindMetric {
		c.MetricID, c.PersonID = 0, 0
	}
	return nil
}

// checkMetric checks that a metric fact's metric and person exist.
func (s *Service) checkMetric(ctx context.Context, scope tenant.Scope, f Fact) error {
	if f.Kind != KindMetric {
		return nil
	}
	if s.metrics == nil {
		return errFactKind
	}
	return s.metrics.CheckMetric(ctx, scope.WorkspaceID(), f.Config.MetricID, f.Config.PersonID)
}

// AddFact appends a fact to a drawer.
func (s *Service) AddFact(ctx context.Context, scope tenant.Scope, drawerID int64, f Fact) (Fact, error) {
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		d, err := q.GetDrawer(ctx, sqlc.GetDrawerParams{WorkspaceID: scope.WorkspaceID(), ID: drawerID})
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		f.DrawerID = drawerID
		if err := checkFact(ctx, q, scope, &f); err != nil {
			return err
		}
		if err := s.checkMetric(ctx, scope, f); err != nil {
			return err
		}
		f.ID, err = s.insertFact(ctx, q, f)
		if err != nil {
			return err
		}
		return s.record(ctx, q, scope.WorkspaceID(), "fact.create", drawerID, nil, i18n.N("%s 为抽屉“%s”添加了要点“%s”"), d.Name, f.Label)
	})
	if err == nil {
		s.changed(scope.WorkspaceID())
	}
	return f, err
}

func (s *Service) insertFact(ctx context.Context, q *sqlc.Queries, f Fact) (int64, error) {
	last, err := q.MaxDrawerFactOrder(ctx, f.DrawerID)
	if err != nil {
		return 0, err
	}
	cfg, _ := json.Marshal(f.Config)
	return q.InsertDrawerFact(ctx, sqlc.InsertDrawerFactParams{DrawerID: f.DrawerID, Kind: f.Kind, Label: f.Label, Config: string(cfg),
		SortOrder: last + 1, CreatedAt: s.opt.Now().UnixMilli()})
}

// UpdateFact replaces a fact's label and config (its kind stays).
func (s *Service) UpdateFact(ctx context.Context, scope tenant.Scope, id int64, label string, cfg FactConfig) (Fact, error) {
	var f Fact
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		r, err := q.GetDrawerFact(ctx, sqlc.GetDrawerFactParams{WorkspaceID: scope.WorkspaceID(), ID: id})
		if errors.Is(err, sql.ErrNoRows) {
			return ErrFactNotFound
		}
		if err != nil {
			return err
		}
		f = toFact(r)
		f.Label, f.Config = label, cfg
		if err := checkFact(ctx, q, scope, &f); err != nil {
			return err
		}
		if err := s.checkMetric(ctx, scope, f); err != nil {
			return err
		}
		b, _ := json.Marshal(f.Config)
		if err := q.UpdateDrawerFact(ctx, sqlc.UpdateDrawerFactParams{Label: f.Label, Config: string(b), ID: id}); err != nil {
			return err
		}
		return s.record(ctx, q, scope.WorkspaceID(), "fact.update", f.DrawerID, nil, i18n.N("%s 修改了要点“%s”"), f.Label)
	})
	if err == nil {
		s.changed(scope.WorkspaceID())
	}
	return f, err
}

func (s *Service) DeleteFact(ctx context.Context, scope tenant.Scope, id int64) error {
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		r, err := q.GetDrawerFact(ctx, sqlc.GetDrawerFactParams{WorkspaceID: scope.WorkspaceID(), ID: id})
		if errors.Is(err, sql.ErrNoRows) {
			return ErrFactNotFound
		}
		if err != nil {
			return err
		}
		if err := q.DeleteDrawerFact(ctx, id); err != nil {
			return err
		}
		return s.record(ctx, q, scope.WorkspaceID(), "fact.delete", r.DrawerID, nil, i18n.N("%s 删除了要点“%s”"), r.Label)
	})
	if err == nil {
		s.changed(scope.WorkspaceID())
	}
	return err
}

// ReorderFacts sets the order of a drawer's facts; the first is its primary fact.
func (s *Service) ReorderFacts(ctx context.Context, scope tenant.Scope, drawerID int64, ids []int64) error {
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		rows, err := q.ListDrawerFacts(ctx, sqlc.ListDrawerFactsParams{WorkspaceID: scope.WorkspaceID(), DrawerID: drawerID})
		if err != nil {
			return err
		}
		have := map[int64]bool{}
		for _, r := range rows {
			have[r.ID] = true
		}
		if len(ids) != len(rows) {
			return errFactOrder
		}
		seen := map[int64]bool{}
		for _, id := range ids {
			if !have[id] || seen[id] {
				return errFactOrder
			}
			seen[id] = true
		}
		for i, id := range ids {
			if err := q.SetDrawerFactOrder(ctx, sqlc.SetDrawerFactOrderParams{SortOrder: int64(i + 1), ID: id}); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		s.changed(scope.WorkspaceID())
	}
	return err
}

// ---------- computing ----------

// DocRef names a document in a fact's value.
type DocRef struct {
	ID    int64
	Title string
	Date  string // the document date
}

// Month is one month of a sum fact.
type Month struct {
	Month string `json:"month"` // 2024-03
	Total string `json:"total"` // decimal, 2 places for money
}

// Value is a fact computed now. Which fields are set depends on the kind:
//
//	latest  Document, FieldValue (when a field is shown)
//	due     Document, DueDate, DaysLeft
//	sum     Total, Count, Months (12, oldest first), Money
//	expect  PeriodLabel (2024-10, 2024-Q4 or 2024), Present, Document
//	pinned  Documents
//	metric  Metric
//
// Warn marks a value the home page and the drawer show in the warning
// colour: a due date within the advance days (or past), a missing document,
// a metric outside its range.
// Filter selects the fact's documents in the drawer's list (without the
// drawer itself); nil when there is nothing to filter by.
type Value struct {
	Fact        Fact
	Document    *DocRef
	FieldValue  *string
	FieldType   string
	DueDate     string
	DaysLeft    int
	Total       string
	Count       int
	Months      []Month
	Money       bool
	PeriodLabel string
	Present     bool
	Documents   []DocRef
	Warn        bool
	Filter      *filter.Cond
	Metric      *MetricCard // metric; nil when there is no value to see
}

func tagCond(ids []int64) filter.Cond {
	b, _ := json.Marshal(ids)
	return filter.Cond{Field: "tag", Op: "has_any", Value: b, Descendants: true}
}

func fieldCond(field, op string, v any) filter.Cond {
	c := filter.Cond{Field: field, Op: op}
	if v != nil {
		c.Value, _ = json.Marshal(v)
	}
	return c
}

// scopeFilter is what a fact narrows the drawer to (tags and filter),
// with extra conditions.
func scopeFilter(cfg FactConfig, extra ...filter.Cond) *filter.Cond {
	var all []filter.Cond
	if len(cfg.TagIDs) > 0 {
		all = append(all, tagCond(cfg.TagIDs))
	}
	if cfg.Filter != nil {
		all = append(all, *cfg.Filter)
	}
	all = append(all, extra...)
	if len(all) == 0 {
		return nil
	}
	if len(all) == 1 {
		return &all[0]
	}
	return &filter.Cond{All: all}
}

// factEnv compiles conditions for one workspace once.
type factEnv struct {
	s     *Service
	scope tenant.Scope
	cat   *filter.Catalog
	today time.Time
}

func (s *Service) env(ctx context.Context, scope tenant.Scope) (*factEnv, error) {
	cat, err := filter.LoadCatalog(ctx, s.q, scope)
	if err != nil {
		return nil, err
	}
	cat.Substring = true
	now := s.opt.Now()
	return &factEnv{s: s, scope: scope, cat: cat, today: time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())}, nil
}

// where compiles: in the drawer, filed (not waiting in the inbox), plus the
// fact's own conditions and extra.
func (e *factEnv) where(drawerID int64, cfg FactConfig, extra ...filter.Cond) (filter.Compiled, error) {
	all := []filter.Cond{fieldCond("drawer", "in", []int64{drawerID}), fieldCond("inbox", "eq", false)}
	if f := scopeFilter(cfg, extra...); f != nil {
		all = append(all, *f)
	}
	return filter.Compile(e.cat, e.scope, filter.Cond{All: all})
}

func (e *factEnv) day(s string) time.Time {
	t, _ := time.ParseInLocation("2006-01-02", s, e.today.Location())
	return t
}

// Compute works out a fact's value now.
func (s *Service) Compute(ctx context.Context, scope tenant.Scope, f Fact) (Value, error) {
	e, err := s.env(ctx, scope)
	if err != nil {
		return Value{}, err
	}
	return e.compute(ctx, f)
}

// ComputeAll works out the values of a drawer's facts.
func (s *Service) ComputeAll(ctx context.Context, scope tenant.Scope, drawerID int64) ([]Value, error) {
	facts, err := s.Facts(ctx, scope, drawerID)
	if err != nil {
		return nil, err
	}
	e, err := s.env(ctx, scope)
	if err != nil {
		return nil, err
	}
	out := make([]Value, 0, len(facts))
	for _, f := range facts {
		v, err := e.compute(ctx, f)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// PrimaryValues computes each drawer's first fact (the home page cards).
func (s *Service) PrimaryValues(ctx context.Context, scope tenant.Scope) (map[int64]Value, error) {
	rows, err := s.q.ListWorkspaceFacts(ctx, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	e, err := s.env(ctx, scope)
	if err != nil {
		return nil, err
	}
	out := map[int64]Value{}
	for _, r := range rows {
		if _, done := out[r.DrawerID]; done {
			continue
		}
		v, err := e.compute(ctx, toFact(r))
		if err != nil {
			return nil, err
		}
		out[r.DrawerID] = v
	}
	return out, nil
}

func (e *factEnv) compute(ctx context.Context, f Fact) (Value, error) {
	v := Value{Fact: f}
	c := f.Config
	switch f.Kind {
	case KindLatest:
		w, err := e.where(f.DrawerID, c)
		if err != nil {
			return v, nil // a condition that no longer compiles (a deleted tag): show nothing
		}
		v.Filter = scopeFilter(c)
		v.Document, err = e.latestDoc(ctx, w)
		if err != nil || v.Document == nil || c.FieldID == 0 {
			return v, err
		}
		v.FieldValue, v.FieldType, err = e.fieldValue(ctx, v.Document.ID, c.FieldID)
		return v, err
	case KindDue:
		return e.due(ctx, f)
	case KindSum:
		return e.sum(ctx, f)
	case KindExpect:
		return e.expect(ctx, f)
	case KindMetric:
		if e.s.metrics != nil {
			card, err := e.s.metrics.MetricCard(ctx, e.scope, c.MetricID, c.PersonID)
			if err != nil {
				return v, err
			}
			v.Metric = card
			v.Warn = card != nil && card.Flag != ""
		}
	case KindPinned:
		for _, id := range c.DocumentIDs {
			var d DocRef
			vis, vargs := privacy.Visible("d", e.scope)
			err := e.s.db.QueryRowContext(ctx, `SELECT d.id, d.title, d.created_date FROM documents d WHERE d.workspace_id = ? AND d.id = ? AND d.deleted_at IS NULL AND `+vis,
				append([]any{e.scope.WorkspaceID(), id}, vargs...)...).Scan(&d.ID, &d.Title, &d.Date)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return v, err
			}
			v.Documents = append(v.Documents, d)
		}
	}
	return v, nil
}

// Covers lists which of a drawer's filed documents a fact's conditions
// take in (the steward looks for date values no due fact covers).
func (s *Service) Covers(ctx context.Context, scope tenant.Scope, f Fact) (map[int64]bool, error) {
	e, err := s.env(ctx, scope)
	if err != nil {
		return nil, err
	}
	w, err := e.where(f.DrawerID, f.Config)
	if err != nil {
		return map[int64]bool{}, nil
	}
	rows, err := s.db.QueryContext(ctx, "SELECT d.id FROM documents d WHERE "+w.Where, w.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// LatestOf is the newest filed document a fact covers, whatever its date
// (the steward shows the last bill when one is missing).
func (s *Service) LatestOf(ctx context.Context, scope tenant.Scope, f Fact) (*DocRef, error) {
	e, err := s.env(ctx, scope)
	if err != nil {
		return nil, err
	}
	w, err := e.where(f.DrawerID, f.Config)
	if err != nil {
		return nil, nil
	}
	return e.latestDoc(ctx, w)
}

func (e *factEnv) latestDoc(ctx context.Context, w filter.Compiled) (*DocRef, error) {
	var d DocRef
	err := e.s.db.QueryRowContext(ctx, "SELECT d.id, d.title, d.created_date FROM documents d WHERE "+w.Where+" ORDER BY d.created_date DESC, d.id DESC LIMIT 1", w.Args...).
		Scan(&d.ID, &d.Title, &d.Date)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &d, err
}

// fieldValue reads one custom field of a document as text (money as a
// decimal with two places).
func (e *factEnv) fieldValue(ctx context.Context, docID, fieldID int64) (*string, string, error) {
	var typ string
	var text, date, sel sql.NullString
	var integer, boolean, money sql.NullInt64
	var float sql.NullFloat64
	err := e.s.db.QueryRowContext(ctx, `SELECT f.data_type, v.value_text, v.value_date, v.value_select, v.value_int, v.value_bool, v.value_monetary, v.value_float
		FROM custom_field_values v JOIN custom_fields f ON f.id = v.field_id WHERE v.document_id = ? AND v.field_id = ? AND f.workspace_id = ?`,
		docID, fieldID, e.scope.WorkspaceID()).Scan(&typ, &text, &date, &sel, &integer, &boolean, &money, &float)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	var out string
	switch {
	case money.Valid:
		out = filter.FormatMoney(money.Int64)
	case integer.Valid:
		out = fmt.Sprint(integer.Int64)
	case float.Valid:
		out = fmt.Sprint(float.Float64)
	case date.Valid:
		out = date.String
	case text.Valid:
		out = text.String
	case sel.Valid:
		out = sel.String
	case boolean.Valid:
		out = map[bool]string{true: i18n.N("是"), false: i18n.N("否")}[boolean.Int64 == 1]
	default:
		return nil, typ, nil
	}
	return &out, typ, nil
}

// dueDates lists the documents of a due fact with a date from
// overdueWindow days ago on, soonest first.
func (e *factEnv) dueDates(ctx context.Context, f Fact) ([]DocRef, []string, error) {
	w, err := e.where(f.DrawerID, f.Config)
	if err != nil {
		return nil, nil, nil
	}
	from := e.today.AddDate(0, 0, -overdueWindow).Format("2006-01-02")
	rows, err := e.s.db.QueryContext(ctx, `SELECT d.id, d.title, d.created_date, v.value_date FROM documents d
		JOIN custom_field_values v ON v.document_id = d.id AND v.field_id = ?
		WHERE `+w.Where+` AND v.value_date >= ? ORDER BY v.value_date, d.id`, append(append([]any{f.Config.FieldID}, w.Args...), from)...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var docs []DocRef
	var dates []string
	for rows.Next() {
		var d DocRef
		var due string
		if err := rows.Scan(&d.ID, &d.Title, &d.Date, &due); err != nil {
			return nil, nil, err
		}
		docs, dates = append(docs, d), append(dates, due)
	}
	return docs, dates, rows.Err()
}

func (e *factEnv) due(ctx context.Context, f Fact) (Value, error) {
	v := Value{Fact: f, Filter: scopeFilter(f.Config, fieldCond(fmt.Sprintf("cf:%d", f.Config.FieldID), "not_null", nil))}
	docs, dates, err := e.dueDates(ctx, f)
	if err != nil || len(docs) == 0 {
		return v, err
	}
	// The soonest date still ahead; if all are past, the most recent past one.
	pick := len(docs) - 1
	for i, d := range dates {
		if !e.day(d).Before(e.today) {
			pick = i
			break
		}
	}
	v.Document, v.DueDate = &docs[pick], dates[pick]
	v.DaysLeft = int(e.day(v.DueDate).Sub(e.today).Hours() / 24)
	v.Warn = v.DaysLeft <= f.Config.AdvanceDays
	return v, nil
}

func (e *factEnv) sum(ctx context.Context, f Fact) (Value, error) {
	c := f.Config
	first := time.Date(e.today.Year(), 1, 1, 0, 0, 0, 0, e.today.Location())
	if c.Period == "last_12_months" {
		first = time.Date(e.today.Year(), e.today.Month()-11, 1, 0, 0, 0, 0, e.today.Location())
	}
	var extra []filter.Cond
	if c.Period != "all" {
		extra = append(extra, fieldCond("created", "gte", first.Format("2006-01-02")))
	}
	v := Value{Fact: f, Filter: scopeFilter(c, extra...)}
	w, err := e.where(f.DrawerID, c, extra...)
	if err != nil {
		return v, nil
	}
	var typ string
	if err := e.s.db.QueryRowContext(ctx, `SELECT data_type FROM custom_fields WHERE id = ? AND workspace_id = ?`, c.FieldID, e.scope.WorkspaceID()).Scan(&typ); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return v, nil
		}
		return v, err
	}
	v.Money = typ == "monetary"
	col := map[string]string{"monetary": "v.value_monetary", "integer": "v.value_int", "float": "v.value_float"}[typ]
	if col == "" {
		return v, nil
	}
	rows, err := e.s.db.QueryContext(ctx, `SELECT substr(d.created_date, 1, 7), count(*), coalesce(sum(`+col+`), 0) FROM documents d
		JOIN custom_field_values v ON v.document_id = d.id AND v.field_id = ?
		WHERE `+w.Where+` AND `+col+` IS NOT NULL GROUP BY 1`, append([]any{c.FieldID}, w.Args...)...)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	byMonth := map[string]float64{}
	var total float64
	for rows.Next() {
		var m string
		var n int
		var t float64
		if err := rows.Scan(&m, &n, &t); err != nil {
			return v, err
		}
		byMonth[m] = t
		total += t
		v.Count += n
	}
	if err := rows.Err(); err != nil {
		return v, err
	}
	format := func(t float64) string {
		if v.Money {
			return filter.FormatMoney(int64(t))
		}
		return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.4f", t), "0"), ".")
	}
	if c.Period == "all" { // the bars show the last 12 months with documents
		var months []string
		for m := range byMonth {
			months = append(months, m)
		}
		sort.Strings(months)
		if len(months) > 0 {
			last, _ := time.ParseInLocation("2006-01", months[len(months)-1], e.today.Location())
			first = last.AddDate(0, -11, 0)
		}
	}
	for i := 0; i < 12; i++ {
		m := first.AddDate(0, i, 0).Format("2006-01")
		v.Months = append(v.Months, Month{Month: m, Total: format(byMonth[m])})
	}
	v.Total = format(total)
	return v, nil
}

// expectPeriod is the period an expect fact checks now: the current one
// once graceDays of it have passed, otherwise the one before.
func expectPeriod(today time.Time, period string, grace int) (start, end time.Time, key, label string) {
	y, m := today.Year(), today.Month()
	loc := today.Location()
	at := func(back int) (time.Time, time.Time) {
		switch period {
		case "yearly":
			s := time.Date(y-back, 1, 1, 0, 0, 0, 0, loc)
			return s, s.AddDate(1, 0, 0)
		case "quarterly":
			q := (int(m) - 1) / 3
			s := time.Date(y, time.Month(q*3+1), 1, 0, 0, 0, 0, loc).AddDate(0, -3*back, 0)
			return s, s.AddDate(0, 3, 0)
		}
		s := time.Date(y, m, 1, 0, 0, 0, 0, loc).AddDate(0, -back, 0)
		return s, s.AddDate(0, 1, 0)
	}
	start, end = at(0)
	if today.Before(start.AddDate(0, 0, grace)) {
		start, end = at(1)
	}
	switch period {
	case "yearly":
		key = start.Format("2006")
		label = fmt.Sprintf(i18n.N("%d 年"), start.Year())
	case "quarterly":
		q := (int(start.Month())-1)/3 + 1
		key = fmt.Sprintf("%d-Q%d", start.Year(), q)
		label = fmt.Sprintf(i18n.N("%d 年第 %d 季度"), start.Year(), q)
	default:
		key = start.Format("2006-01")
		label = monthLabels[start.Month()-1]
		if start.Year() != today.Year() {
			label = fmt.Sprintf(i18n.N("%d 年 %s"), start.Year(), label)
		}
	}
	return start, end, key, label
}

func (e *factEnv) expect(ctx context.Context, f Fact) (Value, error) {
	c := f.Config
	start, end, _, label := expectPeriod(e.today, c.Period, c.GraceDays)
	v := Value{Fact: f, PeriodLabel: label, Filter: scopeFilter(c)}
	rng := filter.Cond{All: []filter.Cond{fieldCond("created", "gte", start.Format("2006-01-02")), fieldCond("created", "lt", end.Format("2006-01-02"))}}
	w, err := e.where(f.DrawerID, c, rng)
	if err != nil {
		return v, nil
	}
	v.Document, err = e.latestDoc(ctx, w)
	v.Present = v.Document != nil
	v.Warn = !v.Present
	return v, err
}

// monthLabels name the months of an expected document's period; each is
// a catalog entry, so “3 月” reads “March” in English (v1.10.1).
var monthLabels = [12]string{i18n.N("1 月"), i18n.N("2 月"), i18n.N("3 月"), i18n.N("4 月"), i18n.N("5 月"), i18n.N("6 月"),
	i18n.N("7 月"), i18n.N("8 月"), i18n.N("9 月"), i18n.N("10 月"), i18n.N("11 月"), i18n.N("12 月")}
