// Package drawer keeps drawers (design doc 3.6, 5.7): each document sits in
// at most one drawer, placed by the first drawer (in order) whose filing
// rule matches, or by hand. It also computes the drawers' facts, raises
// alerts from them and builds the yearbook.
package drawer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"kept/server/internal/i18n"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/events"
	"kept/server/internal/files"
	"kept/server/internal/filter"
	"kept/server/internal/jobs"
	"kept/server/internal/privacy"
	"kept/server/internal/store"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

const maxNameLen = 20

// Icons and colours a drawer may use (the web app's icon set and drawer palette).
var (
	Icons  = []string{"home", "car", "heart", "briefcase", "baby", "idcard", "paw", "wallet", "plane", "book", "users", "file", "kanban", "building"}
	Colors = []string{"blue", "cyan", "green", "orange", "red", "purple", "pink", "gray"}
)

var (
	ErrNotFound = apperr.New(apperr.KindNotFound, "找不到这个抽屉")
	errName     = apperr.New(apperr.KindInvalid, "名称不能为空，最多 20 个字")
	errTaken    = apperr.WithCode(apperr.KindConflict, "name_taken", "已有同名的抽屉")
	errIcon     = apperr.New(apperr.KindInvalid, "图标不正确")
	errColor    = apperr.New(apperr.KindInvalid, "颜色不正确")
	errRuleSelf = apperr.New(apperr.KindInvalid, "收纳规则不能以“所在抽屉”为条件")
	errOrder    = apperr.New(apperr.KindInvalid, "抽屉的顺序应包含这个空间的全部抽屉")
)

// Drawer is one drawer. Rule is nil when it has no filing rule (it then
// files nothing automatically).
type Drawer struct {
	ID          int64
	WorkspaceID int64
	Name        string
	Icon        string
	Color       string
	Description string
	TemplateKey string
	Rule        *filter.Cond
	SortOrder   int64
	Kind        string // normal; library is the 书房, which files every book
	Sensitive   bool   // every document in it is sensitive (v1.6.4)
	// SameNameVersions: a file uploaded under the name of a document in
	// the drawer becomes its new version without asking (v1.7.13).
	SameNameVersions bool
	CreatedAt        time.Time
}

type Options struct {
	Queue *jobs.Queue // reassignment jobs
	Bus   *events.Bus // drawers.changed
	// Files, when set, lets the yearbook add up the storage documents take.
	Files *files.Store
	Now   func() time.Time
}

type Service struct {
	db      *sql.DB
	q       *sqlc.Queries
	opt     Options
	metrics MetricSource
}

func NewService(db *sql.DB, opt Options) *Service {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	return &Service{db: db, q: sqlc.New(db), opt: opt}
}

func toDrawer(r sqlc.Drawer) Drawer {
	d := Drawer{ID: r.ID, WorkspaceID: r.WorkspaceID, Name: r.Name, Icon: r.Icon, Color: r.Color, Description: r.Description,
		TemplateKey: r.TemplateKey.String, SortOrder: r.SortOrder, Kind: r.Kind, CreatedAt: time.UnixMilli(r.CreatedAt), Sensitive: r.Sensitive == 1,
		SameNameVersions: r.SameNameVersions == 1}
	if r.Rule != "" {
		if c, err := filter.ParseString(r.Rule); err == nil {
			d.Rule = &c
		}
	}
	return d
}

func ruleString(c *filter.Cond) string {
	if c == nil {
		return ""
	}
	b, _ := json.Marshal(c)
	return string(b)
}

func (s *Service) List(ctx context.Context, scope tenant.Scope) ([]Drawer, error) {
	rows, err := s.q.ListDrawers(ctx, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	out := make([]Drawer, len(rows))
	for i, r := range rows {
		out[i] = toDrawer(r)
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, scope tenant.Scope, id int64) (Drawer, error) {
	r, err := s.q.GetDrawer(ctx, sqlc.GetDrawerParams{WorkspaceID: scope.WorkspaceID(), ID: id})
	if errors.Is(err, sql.ErrNoRows) {
		return Drawer{}, ErrNotFound
	}
	if err != nil {
		return Drawer{}, err
	}
	return toDrawer(r), nil
}

// Counts returns the documents in each drawer; key 0 counts those in none.
func (s *Service) Counts(ctx context.Context, scope tenant.Scope) (map[int64]int64, error) {
	rows, err := s.q.CountDocumentsByDrawer(ctx, sqlc.CountDocumentsByDrawerParams{WorkspaceID: scope.WorkspaceID(), Viewer: scope.Viewer()})
	if err != nil {
		return nil, err
	}
	out := map[int64]int64{}
	for _, r := range rows {
		out[r.DrawerID.Int64] = r.N
	}
	return out, nil
}

// Stats are the drawer page's subtitle numbers.
type Stats struct {
	Manual              int // placed by hand
	FirstYear, LastYear int // of its documents' dates; 0 without documents
}

func (s *Service) Stats(ctx context.Context, scope tenant.Scope, id int64) (Stats, error) {
	var st Stats
	vis, vargs := privacy.Visible("d", scope)
	err := s.db.QueryRowContext(ctx, `SELECT coalesce(sum(d.drawer_manual), 0), coalesce(CAST(min(substr(d.created_date, 1, 4)) AS INTEGER), 0),
			coalesce(CAST(max(substr(d.created_date, 1, 4)) AS INTEGER), 0)
		FROM documents d WHERE d.workspace_id = ? AND d.drawer_id = ? AND d.deleted_at IS NULL AND `+vis,
		append([]any{scope.WorkspaceID(), id}, vargs...)...).Scan(&st.Manual, &st.FirstYear, &st.LastYear)
	return st, err
}

// Input creates a drawer.
type Input struct {
	Name, Icon, Color, Description string
	Rule                           *filter.Cond
	TemplateKey                    string
}

// Patch changes a drawer; nil fields stay. SetRule replaces the rule with
// Rule (nil: no rule).
type Patch struct {
	Name, Icon, Color, Description *string
	SetRule                        bool
	Rule                           *filter.Cond
	Sensitive                      *bool // v1.6.4
	SameNameVersions               *bool // v1.7.13
}

func normalizeName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if n := utf8.RuneCountInString(s); n < 1 || n > maxNameLen {
		return "", errName
	}
	return s, nil
}

func oneOf(v string, set []string, err error) error {
	for _, x := range set {
		if x == v {
			return nil
		}
	}
	return err
}

// checkRule validates a rule against the workspace's classifications.
func (s *Service) checkRule(ctx context.Context, q *sqlc.Queries, scope tenant.Scope, c *filter.Cond) error {
	if c == nil {
		return nil
	}
	if c.Uses("drawer") {
		return errRuleSelf
	}
	cat, err := filter.LoadCatalog(ctx, q, scope)
	if err != nil {
		return err
	}
	cat.Substring = true
	_, err = filter.Compile(cat, scope, *c)
	return err
}

// Create adds a drawer at the end of the order. A drawer with a rule
// starts a reassignment job (its ID is returned, 0 otherwise).
func (s *Service) Create(ctx context.Context, scope tenant.Scope, in Input) (Drawer, int64, error) {
	name, err := normalizeName(in.Name)
	if err != nil {
		return Drawer{}, 0, err
	}
	if in.Icon == "" {
		in.Icon = "file"
	}
	if in.Color == "" {
		in.Color = "blue"
	}
	if err := oneOf(in.Icon, Icons, errIcon); err != nil {
		return Drawer{}, 0, err
	}
	if err := oneOf(in.Color, Colors, errColor); err != nil {
		return Drawer{}, 0, err
	}
	var d Drawer
	err = s.inTx(ctx, func(q *sqlc.Queries) error {
		if err := s.checkRule(ctx, q, scope, in.Rule); err != nil {
			return err
		}
		var err error
		d, err = s.insert(ctx, q, scope, name, in)
		return err
	})
	if err != nil {
		return Drawer{}, 0, err
	}
	var job int64
	if in.Rule != nil {
		job, err = s.EnqueueReassign(ctx, scope)
	}
	s.changed(scope.WorkspaceID())
	return d, job, err
}

func (s *Service) insert(ctx context.Context, q *sqlc.Queries, scope tenant.Scope, name string, in Input) (Drawer, error) {
	ws := scope.WorkspaceID()
	last, err := q.MaxDrawerOrder(ctx, ws)
	if err != nil {
		return Drawer{}, err
	}
	now := s.opt.Now().UnixMilli()
	tpl := sql.NullString{String: in.TemplateKey, Valid: in.TemplateKey != ""}
	id, err := q.InsertDrawer(ctx, sqlc.InsertDrawerParams{
		WorkspaceID: ws, Name: name, Icon: in.Icon, Color: in.Color, Description: strings.TrimSpace(in.Description), TemplateKey: tpl,
		Rule: ruleString(in.Rule), SortOrder: last + 1, CreatedAt: now, UpdatedAt: now,
	})
	if isUnique(err) {
		return Drawer{}, errTaken
	}
	if err != nil {
		return Drawer{}, err
	}
	if err := s.record(ctx, q, ws, "create", id, nil, i18n.N("%s 新建了抽屉“%s”"), name); err != nil {
		return Drawer{}, err
	}
	r, err := q.GetDrawer(ctx, sqlc.GetDrawerParams{WorkspaceID: ws, ID: id})
	return toDrawer(r), err
}

// Update changes a drawer. A changed rule starts a reassignment job.
func (s *Service) Update(ctx context.Context, scope tenant.Scope, id int64, p Patch) (Drawer, int64, error) {
	var d Drawer
	ruleChanged := false
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		ws := scope.WorkspaceID()
		r, err := q.GetDrawer(ctx, sqlc.GetDrawerParams{WorkspaceID: ws, ID: id})
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		before := toDrawer(r)
		d = before
		if p.Name != nil {
			if d.Name, err = normalizeName(*p.Name); err != nil {
				return err
			}
		}
		if p.Icon != nil {
			if err := oneOf(*p.Icon, Icons, errIcon); err != nil {
				return err
			}
			d.Icon = *p.Icon
		}
		if p.Color != nil {
			if err := oneOf(*p.Color, Colors, errColor); err != nil {
				return err
			}
			d.Color = *p.Color
		}
		if p.Description != nil {
			d.Description = strings.TrimSpace(*p.Description)
		}
		if p.SetRule {
			if before.Kind == KindLibrary {
				return errLibraryRule
			}
			if err := s.checkRule(ctx, q, scope, p.Rule); err != nil {
				return err
			}
			d.Rule = p.Rule
		}
		changes := map[string][2]any{}
		if d.Name != before.Name {
			changes["名称"] = [2]any{before.Name, d.Name}
		}
		if d.Icon != before.Icon || d.Color != before.Color {
			changes["图标"] = [2]any{before.Icon + " " + before.Color, d.Icon + " " + d.Color}
		}
		if d.Description != before.Description {
			changes["说明"] = [2]any{before.Description, d.Description}
		}
		if ruleString(d.Rule) != ruleString(before.Rule) {
			changes["收纳规则"] = [2]any{ruleString(before.Rule), ruleString(d.Rule)}
			ruleChanged = true
		}
		if p.Sensitive != nil && *p.Sensitive != before.Sensitive {
			changes["敏感"] = [2]any{before.Sensitive, *p.Sensitive}
			d.Sensitive = *p.Sensitive
			if err := q.SetDrawerSensitive(ctx, sqlc.SetDrawerSensitiveParams{Sensitive: boolInt(d.Sensitive), UpdatedAt: s.opt.Now().UnixMilli(), WorkspaceID: ws, ID: id}); err != nil {
				return err
			}
			// so the embedder drops (or makes again) their vectors
			if err := q.BumpIndexRevByDrawer(ctx, sqlc.BumpIndexRevByDrawerParams{WorkspaceID: ws, DrawerID: sql.NullInt64{Int64: id, Valid: true}}); err != nil {
				return err
			}
		}
		if p.SameNameVersions != nil && *p.SameNameVersions != before.SameNameVersions {
			changes["同名作为新版本"] = [2]any{before.SameNameVersions, *p.SameNameVersions}
			d.SameNameVersions = *p.SameNameVersions
			if err := q.SetDrawerSameNameVersions(ctx, sqlc.SetDrawerSameNameVersionsParams{SameNameVersions: boolInt(d.SameNameVersions), UpdatedAt: s.opt.Now().UnixMilli(), WorkspaceID: ws, ID: id}); err != nil {
				return err
			}
		}
		if len(changes) == 0 {
			return nil
		}
		err = q.UpdateDrawer(ctx, sqlc.UpdateDrawerParams{Name: d.Name, Icon: d.Icon, Color: d.Color, Description: d.Description,
			Rule: ruleString(d.Rule), UpdatedAt: s.opt.Now().UnixMilli(), WorkspaceID: ws, ID: id})
		if isUnique(err) {
			return errTaken
		}
		if err != nil {
			return err
		}
		return s.record(ctx, q, ws, "update", id, changes, i18n.N("%s 修改了抽屉“%s”"), d.Name)
	})
	if err != nil {
		return Drawer{}, 0, err
	}
	var job int64
	if ruleChanged {
		job, err = s.EnqueueReassign(ctx, scope)
	}
	s.changed(scope.WorkspaceID())
	return d, job, err
}

// Delete removes a drawer; its documents become unfiled (also those put
// there by hand) and are handed to the rules again.
func (s *Service) Delete(ctx context.Context, scope tenant.Scope, id int64) (int64, error) {
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		ws := scope.WorkspaceID()
		r, err := q.GetDrawer(ctx, sqlc.GetDrawerParams{WorkspaceID: ws, ID: id})
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if r.Kind == KindLibrary && !libraryOK(ctx) {
			return errLibraryDelete
		}
		if err := q.ClearManualDrawer(ctx, sqlc.ClearManualDrawerParams{WorkspaceID: ws, DrawerID: sql.NullInt64{Int64: id, Valid: true}}); err != nil {
			return err
		}
		if _, err := q.DeleteDrawer(ctx, sqlc.DeleteDrawerParams{WorkspaceID: ws, ID: id}); err != nil {
			return err
		}
		return s.record(ctx, q, ws, "delete", id, nil, i18n.N("%s 删除了抽屉“%s”"), r.Name)
	})
	if err != nil {
		return 0, err
	}
	job, err := s.EnqueueReassign(ctx, scope)
	s.changed(scope.WorkspaceID())
	return job, err
}

// Reorder sets the order of all the workspace's drawers. Since the first
// matching drawer wins, this starts a reassignment job.
func (s *Service) Reorder(ctx context.Context, scope tenant.Scope, ids []int64) (int64, error) {
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		ws := scope.WorkspaceID()
		rows, err := q.ListDrawers(ctx, ws)
		if err != nil {
			return err
		}
		if !sameSet(rows, ids) {
			return errOrder
		}
		// The 书房 stays first, so books are filed there before other rules.
		for _, r := range rows {
			if r.Kind == KindLibrary {
				ids = append([]int64{r.ID}, slices.DeleteFunc(slices.Clone(ids), func(id int64) bool { return id == r.ID })...)
			}
		}
		for i, id := range ids {
			if err := q.SetDrawerOrder(ctx, sqlc.SetDrawerOrderParams{SortOrder: int64(i + 1), WorkspaceID: ws, ID: id}); err != nil {
				return err
			}
		}
		return s.record(ctx, q, ws, "reorder", 0, nil, i18n.N("%s 调整了抽屉的顺序"))
	})
	if err != nil {
		return 0, err
	}
	job, err := s.EnqueueReassign(ctx, scope)
	s.changed(scope.WorkspaceID())
	return job, err
}

func sameSet(rows []sqlc.Drawer, ids []int64) bool {
	if len(rows) != len(ids) {
		return false
	}
	have := map[int64]bool{}
	for _, r := range rows {
		have[r.ID] = true
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if !have[id] || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

// DrawersChanged tells clients to reload drawers, their counts and facts.
const DrawersChanged = "drawers.changed"

func (s *Service) changed(ws int64) {
	if s.opt.Bus != nil {
		s.opt.Bus.Publish(events.Event{Type: DrawersChanged, WorkspaceID: ws})
	}
}

func (s *Service) inTx(ctx context.Context, fn func(*sqlc.Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) record(ctx context.Context, q *sqlc.Queries, ws int64, verb string, id int64, changes map[string][2]any, format string, args ...any) error {
	target := ""
	if id != 0 {
		target = strconv.FormatInt(id, 10)
	}
	return audit.Record(ctx, q, audit.Event{
		WorkspaceID: ws, Action: "drawer." + verb, TargetType: "drawer", TargetID: target,
		Summary: fmt.Sprintf(format, append([]any{audit.ActorFrom(ctx).Name}, args...)...), Changes: changes, At: s.opt.Now(),
	})
}

func isUnique(err error) bool { return store.IsUniqueViolation(err) }
