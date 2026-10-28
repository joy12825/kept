package filter

import (
	"context"
	"encoding/json"
	"fmt"
	"kept/server/internal/i18n"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/parser"
	"kept/server/internal/privacy"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// SensitiveSQL holds for a sensitive document (v1.6.4): marked itself, or
// in a drawer marked sensitive. Other packages use it to leave such
// documents out (AI, embeddings).
const SensitiveSQL = "(d.sensitive = 1 OR EXISTS (SELECT 1 FROM drawers AS sdr WHERE sdr.id = d.drawer_id AND sdr.sensitive = 1))"

// Catalog is what the compiler needs to know about a workspace: which
// classification IDs exist, the tag tree and the custom fields.
type Catalog struct {
	// Text answers text conditions from the full-text index (design doc
	// 5.1): the matching document IDs, or all = true when the text holds
	// nothing to search for. Without it text conditions fail.
	Text TextResolver
	// Substring answers text conditions with plain substring matching over
	// the title and text instead of the index (drawer rules and facts): a
	// document not yet indexed is matched too, and the result does not
	// depend on word segmentation.
	Substring bool
	// UserID is the person whose own data (reading status, v0.4) a
	// condition may use; 0 where the filter is shared (drawer rules,
	// workflows, facts), which then refuses such conditions.
	UserID int64

	types, correspondents, drawers map[int64]bool
	persons, locations             map[int64]bool  // v1.6.3
	tagParent                      map[int64]int64 // tag ID → parent ID (0 for a root)
	fields                         map[int64]field
	retention                      map[int64]int // document type → months to keep (v1.6.5)
	pinned                         []int64       // documents pinned in drawer facts
}

type field struct {
	dataType string
	options  map[string]bool // select
}

// TextResolver finds the documents of the compiled scope matching a text
// condition.
type TextResolver func(text string, prefix bool) (ids []int64, all bool, err error)

// ErrSearchUnavailable: the full-text index cannot answer yet.
var ErrSearchUnavailable = apperr.WithCode(apperr.KindUnavailable, "search_unavailable", "搜索索引正在准备，请稍后再试")

// LoadCatalog reads a workspace's classifications.
func LoadCatalog(ctx context.Context, q *sqlc.Queries, scope tenant.Scope) (*Catalog, error) {
	ws := scope.WorkspaceID()
	c := &Catalog{types: map[int64]bool{}, correspondents: map[int64]bool{}, drawers: map[int64]bool{}, persons: map[int64]bool{}, locations: map[int64]bool{}, tagParent: map[int64]int64{}, fields: map[int64]field{}}
	types, err := q.ListDocumentTypes(ctx, ws)
	if err != nil {
		return nil, err
	}
	for _, t := range types {
		c.types[t.ID] = true
	}
	corrs, err := q.ListCorrespondents(ctx, ws)
	if err != nil {
		return nil, err
	}
	for _, t := range corrs {
		c.correspondents[t.ID] = true
	}
	persons, err := q.ListPersons(ctx, ws)
	if err != nil {
		return nil, err
	}
	for _, t := range persons {
		c.persons[t.ID] = true
	}
	locations, err := q.ListLocations(ctx, ws)
	if err != nil {
		return nil, err
	}
	for _, t := range locations {
		c.locations[t.ID] = true
	}
	drawers, err := q.ListDrawers(ctx, ws)
	if err != nil {
		return nil, err
	}
	for _, d := range drawers {
		c.drawers[d.ID] = true
	}
	tags, err := q.ListTags(ctx, ws)
	if err != nil {
		return nil, err
	}
	for _, t := range tags {
		c.tagParent[t.ID] = t.ParentID.Int64
	}
	ret, err := q.ListTypeRetention(ctx, ws)
	if err != nil {
		return nil, err
	}
	c.retention = map[int64]int{}
	for _, r := range ret {
		c.retention[r.DocumentTypeID] = int(r.Months)
	}
	if len(c.retention) > 0 { // pinned documents matter only then
		cfgs, err := q.ListPinnedFactConfigs(ctx, ws)
		if err != nil {
			return nil, err
		}
		for _, raw := range cfgs {
			var cfg struct {
				DocumentIDs []int64 `json:"documentIds"`
			}
			_ = json.Unmarshal([]byte(raw), &cfg)
			c.pinned = append(c.pinned, cfg.DocumentIDs...)
		}
	}
	fields, err := q.ListCustomFields(ctx, ws)
	if err != nil {
		return nil, err
	}
	for _, f := range fields {
		fd := field{dataType: f.DataType}
		if f.DataType == "select" {
			var extra struct {
				Options []struct {
					ID string `json:"id"`
				} `json:"options"`
			}
			_ = json.Unmarshal([]byte(f.Extra), &extra)
			fd.options = map[string]bool{}
			for _, o := range extra.Options {
				fd.options[o.ID] = true
			}
		}
		c.fields[f.ID] = fd
	}
	return c, nil
}

// Ancestors returns tag id and its ancestors, nearest first.
func (c *Catalog) Ancestors(id int64) []int64 {
	var out []int64
	for seen := map[int64]bool{}; id != 0 && !seen[id]; id = c.tagParent[id] {
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// descendants returns ids plus all their descendant tags.
func (c *Catalog) descendants(ids []int64) []int64 {
	children := map[int64][]int64{}
	for id, p := range c.tagParent {
		if p != 0 {
			children[p] = append(children[p], id)
		}
	}
	seen := map[int64]bool{}
	var out []int64
	var add func(int64)
	add = func(id int64) {
		if seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
		for _, ch := range children[id] {
			add(ch)
		}
	}
	for _, id := range ids {
		add(id)
	}
	return out
}

// Compiled is a WHERE clause over the documents table aliased "d", with its
// arguments in order.
type Compiled struct {
	Where string
	Args  []any
}

// Compile turns c into a WHERE clause. The clause always restricts to the
// scope's workspace, to documents outside the trash, and to what the
// scope's viewer may see (private documents, v1.8.0); callers cannot drop
// any of them.
// Conditions that always or never hold, written so both SQLite and
// Postgres (which wants a boolean, not 1 or 0) accept them.
const (
	sqlTrue  = "1 = 1"
	sqlFalse = "1 = 0"
)

func Compile(cat *Catalog, scope tenant.Scope, c Cond) (Compiled, error) {
	b := &builder{cat: cat}
	expr, err := b.cond(c)
	if err != nil {
		return Compiled{}, err
	}
	vis, vargs := privacy.Visible("d", scope)
	args := append([]any{scope.WorkspaceID()}, vargs...)
	args = append(args, b.args...)
	return Compiled{Where: "d.workspace_id = ? AND d.deleted_at IS NULL AND " + vis + " AND (" + expr + ")", Args: args}, nil
}

type builder struct {
	cat  *Catalog
	args []any
}

func (b *builder) arg(v any) string { b.args = append(b.args, v); return "?" }

func (b *builder) list(vs []any) string {
	marks := make([]string, len(vs))
	for i, v := range vs {
		marks[i] = b.arg(v)
	}
	return "(" + strings.Join(marks, ", ") + ")"
}

func (b *builder) cond(c Cond) (string, error) {
	switch {
	case c.All != nil:
		return b.group(c.All, " AND ", sqlTrue)
	case c.Any != nil:
		return b.group(c.Any, " OR ", sqlFalse)
	case c.Not != nil:
		inner, err := b.cond(*c.Not)
		if err != nil {
			return "", err
		}
		// A condition on a NULL column is unknown, and NOT unknown is still
		// unknown: “not type 10” must include documents without a type.
		return "NOT coalesce((" + inner + "), " + sqlFalse + ")", nil
	case c.Text != nil:
		return b.text(c)
	case c.Field != "":
		return b.field(c)
	}
	return sqlTrue, nil
}

func (b *builder) group(cs []Cond, sep, empty string) (string, error) {
	if len(cs) == 0 {
		return empty, nil
	}
	parts := make([]string, len(cs))
	for i, ch := range cs {
		p, err := b.cond(ch)
		if err != nil {
			return "", err
		}
		parts[i] = "(" + p + ")"
	}
	return strings.Join(parts, sep), nil
}

// text is answered by the full-text index: the matching IDs come in as a
// JSON array, so any number of them is one parameter.
func (b *builder) text(c Cond) (string, error) {
	if b.cat.Substring {
		words := strings.Fields(strings.ToLower(*c.Text))
		if len(words) == 0 {
			return sqlTrue, nil
		}
		parts := make([]string, len(words))
		for i, w := range words {
			parts[i] = "instr(lower(d.title || char(10) || d.content), " + b.arg(w) + ") > 0"
		}
		return strings.Join(parts, " AND "), nil
	}
	if b.cat.Text == nil {
		return "", ErrSearchUnavailable
	}
	ids, all, err := b.cat.Text(*c.Text, c.Prefix)
	if err != nil {
		return "", err
	}
	if all {
		return sqlTrue, nil
	}
	if ids == nil {
		ids = []int64{}
	}
	js, _ := json.Marshal(ids)
	return "d.id IN (SELECT CAST(value AS INTEGER) FROM json_each(" + b.arg(string(js)) + "))", nil
}

var fieldNames = map[string]string{"type": i18n.N("类型"), "correspondent": i18n.N("往来方"), "person": i18n.N("家人"), "location": i18n.N("存放位置"), "tag": i18n.N("标签"), "created": i18n.N("文档日期"), "added": i18n.N("添加日期"), "asn": i18n.N("原件编号"), "mime": i18n.N("文件格式"), "inbox": i18n.N("待整理"), "sensitive": i18n.N("敏感"), "retention": i18n.N("保留期限"), "drawer": i18n.N("抽屉"), "book": i18n.N("书籍"), "reading": i18n.N("阅读状态")}

func badOp(c Cond) error {
	name := fieldNames[c.Field]
	if name == "" {
		name = i18n.N("这个字段")
	}
	return invalid(fmt.Sprintf("%s不支持运算 %s", name, c.Op))
}

func badValue(c Cond) error {
	name := fieldNames[c.Field]
	if name == "" {
		name = i18n.N("自定义字段")
	}
	return invalid(fmt.Sprintf("%s的筛选值不正确", name))
}

func (b *builder) field(c Cond) (string, error) {
	switch c.Field {
	case "type":
		return b.classification(c, "d.document_type_id", b.cat.types, i18n.N("筛选条件中的类型不存在"))
	case "correspondent":
		return b.classification(c, "d.correspondent_id", b.cat.correspondents, i18n.N("筛选条件中的往来方不存在"))
	case "person":
		return b.classification(c, "d.person_id", b.cat.persons, i18n.N("筛选条件中的家人不存在"))
	case "location":
		return b.classification(c, "d.location_id", b.cat.locations, i18n.N("筛选条件中的存放位置不存在"))
	case "tag":
		return b.tag(c)
	case "drawer":
		return b.classification(c, "d.drawer_id", b.cat.drawers, i18n.N("筛选条件中的抽屉不存在"))
	case "created":
		return b.date(c, "d.created_date", false)
	case "added":
		return b.date(c, "d.added_at", true)
	case "asn":
		switch c.Op {
		case "is_null":
			return "d.asn IS NULL", nil
		case "not_null":
			return "d.asn IS NOT NULL", nil
		case "between": // v1.6.1
			var r []int64
			if json.Unmarshal(c.Value, &r) != nil || len(r) != 2 || r[0] > r[1] {
				return "", badValue(c)
			}
			return "d.asn >= " + b.arg(r[0]) + " AND d.asn <= " + b.arg(r[1]), nil
		}
		op := map[string]string{"eq": "=", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}[c.Op]
		if op == "" {
			return "", badOp(c)
		}
		var n int64
		if json.Unmarshal(c.Value, &n) != nil {
			return "", badValue(c)
		}
		return "d.asn " + op + " " + b.arg(n), nil
	case "mime":
		if c.Op == "kind" {
			return b.mimeKind(c)
		}
		if c.Op != "in" {
			return "", badOp(c)
		}
		var vs []string
		if json.Unmarshal(c.Value, &vs) != nil || len(vs) == 0 {
			return "", badValue(c)
		}
		return "d.mime_type IN " + b.list(anys(vs)), nil
	case "book":
		var v bool
		if c.Op != "eq" {
			return "", badOp(c)
		}
		if json.Unmarshal(c.Value, &v) != nil {
			return "", badValue(c)
		}
		if v {
			return "EXISTS (SELECT 1 FROM books AS bk WHERE bk.document_id = d.id)", nil
		}
		return "NOT EXISTS (SELECT 1 FROM books AS bk WHERE bk.document_id = d.id)", nil
	case "reading":
		if b.cat.UserID == 0 {
			return "", invalid("阅读状态是个人数据，不能用在共享的规则里")
		}
		if c.Op != "in" {
			return "", badOp(c)
		}
		var vs []string
		if json.Unmarshal(c.Value, &vs) != nil || len(vs) == 0 {
			return "", badValue(c)
		}
		var states []any
		none := false
		for _, v := range vs {
			switch v {
			case "want", "reading", "finished":
				states = append(states, v)
			case "none":
				none = true
			default:
				return "", badValue(c)
			}
		}
		var parts []string
		if len(states) > 0 {
			parts = append(parts, "EXISTS (SELECT 1 FROM reading_progress AS rp WHERE rp.document_id = d.id AND rp.user_id = "+b.arg(b.cat.UserID)+" AND rp.status IN "+b.list(states)+")")
		}
		if none {
			parts = append(parts, "NOT EXISTS (SELECT 1 FROM reading_progress AS rp WHERE rp.document_id = d.id AND rp.user_id = "+b.arg(b.cat.UserID)+" AND rp.status != '')")
		}
		return strings.Join(parts, " OR "), nil
	case "collection", "id":
		// Internal (v1.0 stage 37): the documents of collections, or given
		// documents; not offered in the filter editor.
		if c.Op != "in" {
			return "", badOp(c)
		}
		var ids []int64
		if json.Unmarshal(c.Value, &ids) != nil || len(ids) == 0 {
			return "", badValue(c)
		}
		vs := make([]any, len(ids))
		for i, id := range ids {
			vs[i] = id
		}
		if c.Field == "id" {
			return "d.id IN " + b.list(vs), nil
		}
		return "EXISTS (SELECT 1 FROM collection_documents AS cd WHERE cd.document_id = d.id AND cd.collection_id IN " + b.list(vs) + ")", nil
	case "inbox":
		var v bool
		if c.Op != "eq" {
			return "", badOp(c)
		}
		if json.Unmarshal(c.Value, &v) != nil {
			return "", badValue(c)
		}
		return "d.inbox = " + b.arg(boolInt(v)), nil
	case "retention":
		return b.retention(c)
	case "sensitive":
		var v bool
		if c.Op != "eq" {
			return "", badOp(c)
		}
		if json.Unmarshal(c.Value, &v) != nil {
			return "", badValue(c)
		}
		if v {
			return SensitiveSQL, nil
		}
		return "NOT " + SensitiveSQL, nil
	}
	if id, ok := strings.CutPrefix(c.Field, "cf:"); ok {
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return "", invalid("筛选条件中的自定义字段不存在")
		}
		return b.custom(c, n)
	}
	return "", invalid(fmt.Sprintf("不支持的筛选字段 %s", c.Field))
}

func (b *builder) ids(c Cond, known map[int64]bool, missing string) ([]int64, error) {
	var ids []int64
	if json.Unmarshal(c.Value, &ids) != nil || len(ids) == 0 {
		return nil, badValue(c)
	}
	for _, id := range ids {
		if !known[id] {
			return nil, invalid(missing)
		}
	}
	return ids, nil
}

func (b *builder) classification(c Cond, col string, known map[int64]bool, missing string) (string, error) {
	switch c.Op {
	case "is_null":
		return col + " IS NULL", nil
	case "not_null":
		return col + " IS NOT NULL", nil
	case "in", "not_in":
		ids, err := b.ids(c, known, missing)
		if err != nil {
			return "", err
		}
		if c.Op == "in" {
			return col + " IN " + b.list(anys(ids)), nil
		}
		return col + " IS NULL OR " + col + " NOT IN " + b.list(anys(ids)), nil
	}
	return "", badOp(c)
}

func (b *builder) tag(c Cond) (string, error) {
	// v1.6.1: documents without any tag, or with some
	switch c.Op {
	case "is_null":
		return "NOT EXISTS (SELECT 1 FROM document_tags dt WHERE dt.document_id = d.id)", nil
	case "not_null":
		return "EXISTS (SELECT 1 FROM document_tags dt WHERE dt.document_id = d.id)", nil
	}
	known := map[int64]bool{}
	for id := range b.cat.tagParent {
		known[id] = true
	}
	ids, err := b.ids(c, known, i18n.N("筛选条件中的标签不存在"))
	if err != nil {
		return "", err
	}
	set := func(ids []int64) []int64 {
		if c.Descendants {
			return b.cat.descendants(ids)
		}
		return ids
	}
	has := func(ids []int64) string {
		return "EXISTS (SELECT 1 FROM document_tags dt WHERE dt.document_id = d.id AND dt.tag_id IN " + b.list(anys(ids)) + ")"
	}
	switch c.Op {
	case "has_any":
		return has(set(ids)), nil
	case "has_none":
		return "NOT " + has(set(ids)), nil
	case "has_all":
		parts := make([]string, len(ids))
		for i, id := range ids {
			parts[i] = has(set([]int64{id}))
		}
		return strings.Join(parts, " AND "), nil
	}
	return "", badOp(c)
}

// date compares a YYYY-MM-DD document date as text, or the added time
// (Unix milliseconds) against local-midnight boundaries.
// Periods (v1.5.2) are dates relative to today, so a saved filter like
// “上个月” always means the month before the current one.
var periods = map[string]func(today time.Time) (from, to time.Time){
	"this_month": func(t time.Time) (time.Time, time.Time) { f := monthStart(t); return f, f.AddDate(0, 1, -1) },
	"last_month": func(t time.Time) (time.Time, time.Time) {
		f := monthStart(t).AddDate(0, -1, 0)
		return f, f.AddDate(0, 1, -1)
	},
	"last_7_days":  func(t time.Time) (time.Time, time.Time) { return t.AddDate(0, 0, -6), t },
	"last_30_days": func(t time.Time) (time.Time, time.Time) { return t.AddDate(0, 0, -29), t },
	"last_90_days": func(t time.Time) (time.Time, time.Time) { return t.AddDate(0, 0, -89), t },
	"this_year": func(t time.Time) (time.Time, time.Time) {
		return time.Date(t.Year(), 1, 1, 0, 0, 0, 0, t.Location()), time.Date(t.Year(), 12, 31, 0, 0, 0, 0, t.Location())
	},
	"last_year": func(t time.Time) (time.Time, time.Time) {
		return time.Date(t.Year()-1, 1, 1, 0, 0, 0, 0, t.Location()), time.Date(t.Year()-1, 12, 31, 0, 0, 0, 0, t.Location())
	},
}

func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

// Now is today for periods; tests set it.
var Now = time.Now

func (b *builder) date(c Cond, col string, millis bool) (string, error) {
	parse := func(s string) (time.Time, bool) {
		t, err := time.ParseInLocation("2006-01-02", s, time.Local)
		return t, err == nil
	}
	if c.Op == "period" {
		var name string
		if json.Unmarshal(c.Value, &name) != nil || periods[name] == nil {
			return "", badValue(c)
		}
		n := Now().In(time.Local)
		from, to := periods[name](time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.Local))
		v, _ := json.Marshal([]string{from.Format("2006-01-02"), to.Format("2006-01-02")})
		c.Op, c.Value = "between", v
	}
	if c.Op == "between" {
		var vs []string
		if json.Unmarshal(c.Value, &vs) != nil || len(vs) != 2 {
			return "", badValue(c)
		}
		from, ok1 := parse(vs[0])
		to, ok2 := parse(vs[1])
		if !ok1 || !ok2 || to.Before(from) {
			return "", badValue(c)
		}
		if millis {
			return col + " >= " + b.arg(from.UnixMilli()) + " AND " + col + " < " + b.arg(to.AddDate(0, 0, 1).UnixMilli()), nil
		}
		return col + " >= " + b.arg(vs[0]) + " AND " + col + " <= " + b.arg(vs[1]), nil
	}
	var s string
	if json.Unmarshal(c.Value, &s) != nil {
		return "", badValue(c)
	}
	day, ok := parse(s)
	if !ok {
		return "", badValue(c)
	}
	if !millis {
		op := map[string]string{"eq": "=", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}[c.Op]
		if op == "" {
			return "", badOp(c)
		}
		return col + " " + op + " " + b.arg(s), nil
	}
	start, next := day.UnixMilli(), day.AddDate(0, 0, 1).UnixMilli()
	switch c.Op {
	case "eq":
		return col + " >= " + b.arg(start) + " AND " + col + " < " + b.arg(next), nil
	case "gt":
		return col + " >= " + b.arg(next), nil
	case "gte":
		return col + " >= " + b.arg(start), nil
	case "lt":
		return col + " < " + b.arg(start), nil
	case "lte":
		return col + " < " + b.arg(next), nil
	}
	return "", badOp(c)
}

var fieldColumn = map[string]string{
	"text": "value_text", "url": "value_text", "integer": "value_int", "float": "value_float",
	"monetary": "value_monetary", "date": "value_date", "boolean": "value_bool",
	"select": "value_select", "document_link": "value_doclink",
}

var comparable = map[string]bool{"integer": true, "float": true, "monetary": true, "date": true}

func (b *builder) custom(c Cond, id int64) (string, error) {
	f, ok := b.cat.fields[id]
	if !ok {
		return "", invalid("筛选条件中的自定义字段不存在")
	}
	col := "v." + fieldColumn[f.dataType]
	// exists matches documents with a value for the field satisfying pred,
	// whose placeholders take args.
	exists := func(pred string, args ...any) string {
		s := "EXISTS (SELECT 1 FROM custom_field_values v WHERE v.document_id = d.id AND v.field_id = " + b.arg(id) + " AND " + pred + ")"
		b.args = append(b.args, args...)
		return s
	}
	switch c.Op {
	case "is_null":
		return "NOT " + exists(col+" IS NOT NULL"), nil
	case "not_null":
		return exists(col + " IS NOT NULL"), nil
	}
	switch {
	case c.Op == "contains" && (f.dataType == "text" || f.dataType == "url"):
		var s string
		if json.Unmarshal(c.Value, &s) != nil || strings.TrimSpace(s) == "" {
			return "", badValue(c)
		}
		return exists("instr(lower("+col+"), ?) > 0", strings.ToLower(s)), nil
	case c.Op == "in" && f.dataType == "select":
		var vs []string
		if json.Unmarshal(c.Value, &vs) != nil || len(vs) == 0 {
			return "", badValue(c)
		}
		for _, v := range vs {
			if !f.options[v] {
				return "", badValue(c)
			}
		}
		marks := strings.TrimSuffix(strings.Repeat("?, ", len(vs)), ", ")
		return exists(col+" IN ("+marks+")", anys(vs)...), nil
	case c.Op == "within_days" && f.dataType == "date":
		// v1.6.1: N > 0 is from today to N days ahead (a warranty ending
		// within three months), N < 0 the last -N days up to today
		var n int
		if json.Unmarshal(c.Value, &n) != nil || n == 0 || n > 36500 || n < -36500 {
			return "", badValue(c)
		}
		today := Now().In(time.Local)
		lo, hi := today, today.AddDate(0, 0, n)
		if n < 0 {
			lo, hi = hi, today
		}
		return exists(col+" >= ? AND "+col+" <= ?", lo.Format("2006-01-02"), hi.Format("2006-01-02")), nil
	case c.Op == "between" && comparable[f.dataType]:
		var raw []json.RawMessage
		if json.Unmarshal(c.Value, &raw) != nil || len(raw) != 2 {
			return "", badValue(c)
		}
		lo, err1 := customValue(f.dataType, raw[0])
		hi, err2 := customValue(f.dataType, raw[1])
		if err1 != nil || err2 != nil {
			return "", badValue(c)
		}
		return exists(col+" >= ? AND "+col+" <= ?", lo, hi), nil
	}
	op := map[string]string{"eq": "=", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}[c.Op]
	if op == "" || (op != "=" && !comparable[f.dataType]) || f.dataType == "select" {
		return "", badOp(c)
	}
	v, err := customValue(f.dataType, c.Value)
	if err != nil {
		return "", badValue(c)
	}
	return exists(col+" "+op+" ?", v), nil
}

// fileKinds are the file format groups of the list's filter (v1.6.1).
var fileKinds = map[string]bool{"pdf": true, "image": true, "office": true, "email": true, "ebook": true, "text": true}

func (b *builder) mimeKind(c Cond) (string, error) {
	var kinds []string
	if json.Unmarshal(c.Value, &kinds) != nil || len(kinds) == 0 {
		return "", badValue(c)
	}
	var parts []string
	for _, k := range kinds {
		if !fileKinds[k] {
			return "", badValue(c)
		}
		switch k {
		case "pdf":
			parts = append(parts, "d.mime_type = 'application/pdf'")
		case "image":
			parts = append(parts, "d.mime_type LIKE 'image/%'")
		case "office":
			parts = append(parts, "d.mime_type IN "+b.list(anys(parser.OfficeTypes())))
		case "email":
			parts = append(parts, "d.mime_type IN "+b.list(anys(parser.EmailTypes())))
		case "ebook":
			parts = append(parts, "d.mime_type = 'application/epub+zip'")
		case "text":
			parts = append(parts, "(d.mime_type LIKE 'text/%' AND d.mime_type <> 'text/rtf')")
		}
	}
	return "(" + strings.Join(parts, " OR ") + ")", nil
}

// customValue converts a JSON value to what the field's column stores.
func customValue(dataType string, raw json.RawMessage) (any, error) {
	switch dataType {
	case "text", "url":
		var s string
		err := json.Unmarshal(raw, &s)
		return s, err
	case "integer", "document_link":
		var n int64
		err := json.Unmarshal(raw, &n)
		return n, err
	case "float":
		var f float64
		err := json.Unmarshal(raw, &f)
		return f, err
	case "boolean":
		var v bool
		err := json.Unmarshal(raw, &v)
		return boolInt(v), err
	case "date":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		_, err := time.Parse("2006-01-02", s)
		return s, err
	case "monetary":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return ParseMoney(s)
	}
	return nil, invalid("不支持的字段类型")
}

// ParseMoney turns "326.40" (at most two decimals, optional minus sign) into
// minor units (32640).
func ParseMoney(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return 0, invalid("金额的格式不正确")
	}
	whole, frac, _ := strings.Cut(s, ".")
	if len(frac) > 2 {
		return 0, invalid("金额最多两位小数")
	}
	frac += strings.Repeat("0", 2-len(frac))
	neg := strings.HasPrefix(whole, "-")
	whole = strings.TrimPrefix(whole, "-")
	if whole == "" {
		whole = "0"
	}
	w, err1 := strconv.ParseInt(whole, 10, 64)
	f, err2 := strconv.ParseInt(frac, 10, 64)
	if err1 != nil || err2 != nil || f < 0 || w > math.MaxInt64/100-1 {
		return 0, invalid("金额的格式不正确")
	}
	n := w*100 + f
	if neg {
		n = -n
	}
	return n, nil
}

// FormatMoney is the inverse of ParseMoney.
func FormatMoney(n int64) string {
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}
	return sign + strconv.FormatInt(n/100, 10) + "." + strconv.FormatInt(n%100/10, 10) + strconv.FormatInt(n%10, 10)
}

func anys[T any](vs []T) []any {
	out := make([]any, len(vs))
	for i, v := range vs {
		out[i] = v
	}
	return out
}

func boolInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

// retention compiles {"field":"retention","op":"expired","value":true}
// (v1.6.5): filed documents older than their type's retention period, by
// document date, that nothing holds on to: not a milestone, not pinned in
// a drawer's facts, not linked from another document. These are suggested
// for clean-up.
func (b *builder) retention(c Cond) (string, error) {
	var v bool
	if c.Op != "expired" {
		return "", badOp(c)
	}
	if json.Unmarshal(c.Value, &v) != nil || !v {
		return "", badValue(c)
	}
	types := make([]int64, 0, len(b.cat.retention))
	for id := range b.cat.retention {
		types = append(types, id)
	}
	if len(types) == 0 {
		return "1 = 0", nil
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })
	today := Now()
	var old []string
	for _, id := range types {
		cutoff := today.AddDate(0, -b.cat.retention[id], 0).Format("2006-01-02")
		old = append(old, "(d.document_type_id = "+b.arg(id)+" AND d.created_date < "+b.arg(cutoff)+")")
	}
	parts := []string{"(" + strings.Join(old, " OR ") + ")", "d.inbox = 0", "d.is_milestone = 0",
		"NOT EXISTS (SELECT 1 FROM custom_field_values AS lv JOIN documents AS ld ON ld.id = lv.document_id WHERE lv.value_doclink = d.id AND ld.deleted_at IS NULL)"}
	if len(b.cat.pinned) > 0 {
		args := make([]any, len(b.cat.pinned))
		for i, id := range b.cat.pinned {
			args[i] = id
		}
		parts = append(parts, "d.id NOT IN "+b.list(args))
	}
	return "(" + strings.Join(parts, " AND ") + ")", nil
}
