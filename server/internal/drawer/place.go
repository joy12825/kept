package drawer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"kept/server/internal/i18n"
	"slices"
	"strings"

	"kept/server/internal/audit"
	"kept/server/internal/document"
	"kept/server/internal/filter"
	"kept/server/internal/jobs"
	"kept/server/internal/privacy"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// KindReassign recomputes the drawer of every document not placed by hand
// (after a rule, the order or the set of drawers changed).
const KindReassign = "drawer.reassign"

// ruleSet is a workspace's filing rules in drawer order, compiled.
type ruleSet struct {
	ids   []int64
	names map[int64]string
	expr  string // CASE WHEN <rule 1> THEN <id 1> … END over documents d
	args  []any
}

// loadRules compiles the drawers' rules; edit, when set, changes the list
// first (a preview of an unsaved rule or order). Rules file every document,
// private ones too (v1.8.0): they are compiled for the system, whoever
// asks.
func loadRules(ctx context.Context, q *sqlc.Queries, scope tenant.Scope, edit func([]Drawer) []Drawer) (*ruleSet, error) {
	scope = tenant.New(scope.WorkspaceID(), tenant.RoleOwner)
	rows, err := q.ListDrawers(ctx, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	ds := make([]Drawer, len(rows))
	for i, r := range rows {
		ds[i] = toDrawer(r)
	}
	if edit != nil {
		ds = edit(ds)
	}
	cat, err := filter.LoadCatalog(ctx, q, scope)
	if err != nil {
		return nil, err
	}
	cat.Substring = true
	rs := &ruleSet{names: map[int64]string{}}
	var b strings.Builder
	for _, d := range ds {
		rs.names[d.ID] = d.Name
		if d.Rule == nil {
			continue
		}
		comp, err := filter.Compile(cat, scope, *d.Rule)
		if err != nil {
			continue // a rule that no longer compiles (e.g. a deleted type) files nothing
		}
		rs.ids = append(rs.ids, d.ID)
		b.WriteString(" WHEN (" + comp.Where + ") THEN CAST(? AS INTEGER)")
		rs.args = append(append(rs.args, comp.Args...), d.ID)
	}
	if len(rs.ids) == 0 {
		rs.expr = "NULL"
	} else {
		rs.expr = "CASE" + b.String() + " ELSE NULL END"
	}
	return rs, nil
}

// match returns the drawer the rules give one document (0 for none).
func (rs *ruleSet) match(ctx context.Context, db sqlc.DBTX, docID int64) (int64, error) {
	var id sql.NullInt64
	err := db.QueryRowContext(ctx, "SELECT "+rs.expr+" FROM documents d WHERE d.id = ?", append(append([]any{}, rs.args...), docID)...).Scan(&id)
	return id.Int64, err
}

// assignments returns, for every document outside the trash not placed by
// hand that scope's viewer may see, its current drawer and the one the
// rules give (0 for none).
func (rs *ruleSet) assignments(ctx context.Context, db sqlc.DBTX, scope tenant.Scope) (map[int64][2]int64, error) {
	vis, vargs := privacy.Visible("d", scope)
	rows, err := db.QueryContext(ctx, "SELECT d.id, coalesce(d.drawer_id, 0), coalesce("+rs.expr+", 0) FROM documents d WHERE d.workspace_id = ? AND d.deleted_at IS NULL AND d.drawer_manual = 0 AND "+vis,
		append(append(append([]any{}, rs.args...), scope.WorkspaceID()), vargs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][2]int64{}
	for rows.Next() {
		var id, cur, next int64
		if err := rows.Scan(&id, &cur, &next); err != nil {
			return nil, err
		}
		out[id] = [2]int64{cur, next}
	}
	return out, rows.Err()
}

// ruleBasis is the basis recorded in field_sources for a drawer chosen by rule.
func ruleBasis(name string) string { return fmt.Sprintf(i18n.N("收纳规则“%s”"), name) }

// writeDrawer sets a document's drawer and records where it came from.
func writeDrawer(ctx context.Context, q *sqlc.Queries, ws, docID, drawerID int64, manual bool, src *document.Source) error {
	if err := q.SetDocumentDrawer(ctx, sqlc.SetDocumentDrawerParams{
		DrawerID: sql.NullInt64{Int64: drawerID, Valid: drawerID != 0}, DrawerManual: boolInt(manual), WorkspaceID: ws, ID: docID,
	}); err != nil {
		return err
	}
	st, err := q.GetDocumentDrawerState(ctx, sqlc.GetDocumentDrawerStateParams{WorkspaceID: ws, ID: docID})
	if err != nil {
		return err
	}
	fs := document.ParseSources(st.FieldSources)
	fs.Drawer = src
	return q.SetDocumentFieldSources(ctx, sqlc.SetDocumentFieldSourcesParams{FieldSources: fs.String(), WorkspaceID: ws, ID: docID})
}

// Placement is where Place put a document.
type Placement struct {
	DrawerID int64 // 0: no drawer
	Name     string
	Changed  bool
}

// Place files one document by the rules, unless a person placed it. db may
// be a transaction (ingest, document edits).
func (s *Service) Place(ctx context.Context, db sqlc.DBTX, ws, docID int64) (Placement, error) {
	q := sqlc.New(db)
	st, err := q.GetDocumentDrawerState(ctx, sqlc.GetDocumentDrawerStateParams{WorkspaceID: ws, ID: docID})
	if err != nil {
		return Placement{}, err
	}
	if st.DrawerManual == 1 {
		return Placement{DrawerID: st.DrawerID.Int64}, nil
	}
	rs, err := loadRules(ctx, q, tenant.New(ws, tenant.RoleOwner), nil)
	if err != nil {
		return Placement{}, err
	}
	id, err := rs.match(ctx, db, docID)
	if err != nil {
		return Placement{}, err
	}
	p := Placement{DrawerID: id, Name: rs.names[id], Changed: id != st.DrawerID.Int64}
	src := (*document.Source)(nil)
	if id != 0 {
		src = &document.Source{Source: document.SourceRule, Basis: ruleBasis(p.Name)}
	}
	cur := document.ParseSources(st.FieldSources).Drawer
	if p.Changed || (cur == nil) != (src == nil) || (cur != nil && src != nil && !cur.Same(*src)) {
		if err := writeDrawer(ctx, q, ws, docID, id, false, src); err != nil {
			return p, err
		}
	}
	return p, nil
}

// PlaceDocument is Place for the document service (document.Placer).
func (s *Service) PlaceDocument(ctx context.Context, db sqlc.DBTX, ws, docID int64) error {
	_, err := s.Place(ctx, db, ws, docID)
	return err
}

// RuleDrawer is the drawer the rules would give a document, ignoring a
// placement by hand (the details page's “交还给规则后将放入 …”).
func (s *Service) RuleDrawer(ctx context.Context, scope tenant.Scope, docID int64) (int64, error) {
	rs, err := loadRules(ctx, s.q, scope, nil)
	if err != nil {
		return 0, err
	}
	return rs.match(ctx, s.db, docID)
}

// SetManual puts a document into a drawer by hand (0: into none) or, with
// auto, hands it back to the rules.
func (s *Service) SetManual(ctx context.Context, db sqlc.DBTX, ws, docID, drawerID int64, auto bool) error {
	q := sqlc.New(db)
	if auto {
		if err := q.SetDocumentDrawer(ctx, sqlc.SetDocumentDrawerParams{DrawerID: sql.NullInt64{}, DrawerManual: 0, WorkspaceID: ws, ID: docID}); err != nil {
			return err
		}
		_, err := s.Place(ctx, db, ws, docID)
		return err
	}
	if drawerID != 0 {
		if _, err := q.GetDrawer(ctx, sqlc.GetDrawerParams{WorkspaceID: ws, ID: drawerID}); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
	}
	return writeDrawer(ctx, q, ws, docID, drawerID, true, &document.Source{Source: document.SourceUser})
}

// Name returns a drawer's name, or "" if it does not exist.
func (s *Service) Name(ctx context.Context, db sqlc.DBTX, ws, id int64) string {
	r, err := sqlc.New(db).GetDrawer(ctx, sqlc.GetDrawerParams{WorkspaceID: ws, ID: id})
	if err != nil {
		return ""
	}
	return r.Name
}

// PreviewInput is an unsaved change: a new rule for one drawer, or a new
// order of all drawers.
type PreviewInput struct {
	DrawerID int64
	SetRule  bool
	Rule     *filter.Cond
	Order    []int64
}

// Move is one document whose drawer would change (0: none).
type Move struct {
	DocumentID int64
	Title      string
	From, To   int64
}

// Preview says what saving a change would do to documents not placed by
// hand. For a rule change the counts are about that drawer: how many it
// would hold, how many come in from other drawers or from none, how many
// leave for another drawer or for none.
type Preview struct {
	Total, Manual, In, New, Out, Unfiled int
	Moves                                int
	Samples                              []Move
}

const maxSamples = 8

func (s *Service) Preview(ctx context.Context, scope tenant.Scope, in PreviewInput) (Preview, error) {
	ws := scope.WorkspaceID()
	if in.DrawerID != 0 {
		if _, err := s.Get(ctx, scope, in.DrawerID); err != nil {
			return Preview{}, err
		}
	}
	if in.SetRule {
		if err := s.checkRule(ctx, s.q, scope, in.Rule); err != nil {
			return Preview{}, err
		}
	}
	if in.Order != nil {
		rows, err := s.q.ListDrawers(ctx, ws)
		if err != nil {
			return Preview{}, err
		}
		if !sameSet(rows, in.Order) {
			return Preview{}, errOrder
		}
	}
	rs, err := loadRules(ctx, s.q, scope, func(ds []Drawer) []Drawer {
		if in.SetRule {
			for i := range ds {
				if ds[i].ID == in.DrawerID {
					ds[i].Rule = in.Rule
				}
			}
		}
		if in.Order != nil {
			pos := map[int64]int{}
			for i, id := range in.Order {
				pos[id] = i
			}
			out := make([]Drawer, len(ds))
			for _, d := range ds {
				out[pos[d.ID]] = d
			}
			ds = out
		}
		return ds
	})
	if err != nil {
		return Preview{}, err
	}
	all, err := rs.assignments(ctx, s.db, scope)
	if err != nil {
		return Preview{}, err
	}
	var p Preview
	target := in.DrawerID
	if target != 0 {
		n, err := s.q.CountManualInDrawer(ctx, sqlc.CountManualInDrawerParams{WorkspaceID: ws, DrawerID: sql.NullInt64{Int64: target, Valid: true}, Viewer: scope.Viewer()})
		if err != nil {
			return p, err
		}
		p.Manual = int(n)
		p.Total = p.Manual
	}
	var moved []int64
	for id, a := range all {
		cur, next := a[0], a[1]
		if target != 0 && next == target {
			p.Total++
		}
		if cur == next {
			continue
		}
		p.Moves++
		if target != 0 {
			switch {
			case next == target && cur == 0:
				p.New++
			case next == target:
				p.In++
			case cur == target && next == 0:
				p.Unfiled++
			case cur == target:
				p.Out++
			}
		}
		moved = append(moved, id)
	}
	sortIDs(moved)
	for _, id := range moved {
		if len(p.Samples) == maxSamples {
			break
		}
		a := all[id]
		if target != 0 && a[0] != target && a[1] != target {
			continue
		}
		title, _ := s.q.GetDocumentTitle(ctx, sqlc.GetDocumentTitleParams{WorkspaceID: ws, ID: id, Viewer: scope.Viewer()})
		p.Samples = append(p.Samples, Move{DocumentID: id, Title: title, From: a[0], To: a[1]})
	}
	return p, nil
}

// EnqueueReassign queues a reassignment of the workspace's documents.
func (s *Service) EnqueueReassign(ctx context.Context, scope tenant.Scope) (int64, error) {
	if s.opt.Queue == nil {
		return 0, nil
	}
	ws := scope.WorkspaceID()
	return s.opt.Queue.Enqueue(ctx, KindReassign, map[string]any{"actor": audit.ActorFrom(ctx)}, jobs.EnqueueOptions{WorkspaceID: &ws, Priority: 1})
}

// Reassign files every document not placed by hand by the current rules
// and returns how many moved.
func (s *Service) Reassign(ctx context.Context, scope tenant.Scope) (int, error) {
	ws := scope.WorkspaceID()
	rs, err := loadRules(ctx, s.q, scope, nil)
	if err != nil {
		return 0, err
	}
	all, err := rs.assignments(ctx, s.db, scope)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for id, a := range all {
		if a[0] != a[1] {
			ids = append(ids, id)
		}
	}
	sortIDs(ids)
	for start := 0; start < len(ids); start += 200 {
		batch := ids[start:min(start+200, len(ids))]
		err := s.inTx(ctx, func(q *sqlc.Queries) error {
			for _, id := range batch {
				st, err := q.GetDocumentDrawerState(ctx, sqlc.GetDocumentDrawerStateParams{WorkspaceID: ws, ID: id})
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				if err != nil {
					return err
				}
				if st.DrawerManual == 1 { // placed by hand meanwhile
					continue
				}
				next := all[id][1]
				var src *document.Source
				if next != 0 {
					src = &document.Source{Source: document.SourceRule, Basis: ruleBasis(rs.names[next])}
				}
				if err := writeDrawer(ctx, q, ws, id, next, false, src); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return 0, err
		}
	}
	if len(ids) > 0 {
		s.changed(ws)
	}
	return len(ids), nil
}

// Register adds the reassignment job's handler.
func (s *Service) Register(p *jobs.Pool) {
	p.Handle(KindReassign, func(ctx context.Context, job jobs.Job) (any, error) {
		if job.WorkspaceID == nil {
			return nil, jobs.Permanent(errors.New("drawer.reassign needs a workspace"))
		}
		var pl struct{ Actor audit.Actor }
		_ = json.Unmarshal(job.Payload, &pl)
		n, err := s.Reassign(audit.WithActor(ctx, pl.Actor), tenant.New(*job.WorkspaceID, tenant.RoleOwner))
		return map[string]int{"moved": n}, err
	})
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func sortIDs(ids []int64) { slices.Sort(ids) }
