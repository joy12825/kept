// Package views keeps saved views (常用筛选): named filters shared by the
// members of a workspace, listed in the sidebar.
package views

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"kept/server/internal/i18n"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/document"
	"kept/server/internal/filter"
	"kept/server/internal/pinyin"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

const maxNameLen = 32

var (
	errName      = apperr.New(apperr.KindInvalid, "名称不能为空，最多 32 个字")
	errSort      = apperr.New(apperr.KindInvalid, "排序方式不正确")
	errTaken     = apperr.WithCode(apperr.KindConflict, "name_taken", "已有同名的常用筛选")
	errNotFound  = apperr.New(apperr.KindNotFound, "找不到这个常用筛选")
	errNotAuthor = apperr.New(apperr.KindForbidden, "只有创建者或空间管理员可以修改这个常用筛选")
)

// View is a saved view.
type View struct {
	ID          int64
	Name        string
	Filter      json.RawMessage // the filter JSON (design doc 11.9)
	Sort        string          // "created:desc"; "" for the list's default
	Display     Display
	CreatedBy   int64 // 0 once the account is gone
	CreatorName string
	CreatedAt   time.Time
}

// Input creates or replaces a view's settings; for an update, nil fields
// are left alone.
type Input struct {
	Name    *string
	Filter  *json.RawMessage
	Sort    *string
	Display *Display
}

// Display is how the list shows a view: list or grid ("" is the list's
// default) and the columns after the title (nil: the default columns).
type Display struct {
	View    string   `json:"view,omitempty"`
	Columns []string `json:"columns,omitempty"`
}

// Columns a list can show besides the title.
var columns = map[string]bool{"correspondent": true, "person": true, "location": true, "type": true, "drawer": true, "date": true, "tags": true, "asn": true, "added": true, "amount": true, "dueDate": true, "notes": true, "pages": true}

// fieldColumn is a custom field's column (v1.5.6). A field deleted later
// is simply not shown.
var fieldColumn = regexp.MustCompile(`^field:[1-9][0-9]{0,17}$`)

var errDisplay = apperr.New(apperr.KindInvalid, "视图类型或显示的列不正确")

func (d Display) encode() (string, error) {
	if d.View != "" && d.View != "list" && d.View != "grid" {
		return "", errDisplay
	}
	seen := map[string]bool{}
	for _, c := range d.Columns {
		if !(columns[c] || fieldColumn.MatchString(c)) || seen[c] {
			return "", errDisplay
		}
		seen[c] = true
	}
	b, _ := json.Marshal(d)
	return string(b), nil
}

type Service struct {
	db  *sql.DB
	q   *sqlc.Queries
	now func() time.Time
}

func NewService(db *sql.DB) *Service { return &Service{db: db, q: sqlc.New(db), now: time.Now} }

// List returns the workspace's views: in the order set by dragging
// (v1.6.5), or in pinyin order when none was set.
func (s *Service) List(ctx context.Context, scope tenant.Scope) ([]View, error) {
	rows, err := s.q.ListSavedViews(ctx, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	out := make([]View, len(rows))
	for i, r := range rows {
		out[i] = View{ID: r.ID, Name: r.Name, Filter: json.RawMessage(r.Filter), Sort: r.Sort, CreatedBy: r.CreatedBy.Int64,
			CreatorName: r.CreatorName, CreatedAt: time.UnixMilli(r.CreatedAt)}
		_ = json.Unmarshal([]byte(r.Display), &out[i].Display)
	}
	return out, nil
}

func (s *Service) get(ctx context.Context, scope tenant.Scope, id int64) (View, error) {
	views, err := s.List(ctx, scope)
	if err != nil {
		return View{}, err
	}
	for _, v := range views {
		if v.ID == id {
			return v, nil
		}
	}
	return View{}, errNotFound
}

// Create saves a view by the caller.
func (s *Service) Create(ctx context.Context, scope tenant.Scope, in Input) (View, error) {
	if in.Name == nil || in.Filter == nil {
		return View{}, errName
	}
	name, f, sort, err := s.validate(ctx, scope, *in.Name, *in.Filter, deref(in.Sort))
	if err != nil {
		return View{}, err
	}
	var display Display
	if in.Display != nil {
		display = *in.Display
	}
	disp, err := display.encode()
	if err != nil {
		return View{}, err
	}
	actor := audit.ActorFrom(ctx)
	now := s.now().UnixMilli()
	var id int64
	err = s.inTx(ctx, func(q *sqlc.Queries) error {
		if err := s.checkName(ctx, q, scope, name, 0); err != nil {
			return err
		}
		id, err = q.InsertSavedView(ctx, sqlc.InsertSavedViewParams{
			WorkspaceID: scope.WorkspaceID(), Name: name, SortKey: pinyin.SortKey(name), Filter: f, Sort: sort, Display: disp,
			CreatedBy: sql.NullInt64{Int64: actor.UserID, Valid: actor.UserID != 0}, CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			return err
		}
		return s.record(ctx, q, scope, "create", id, nil, i18n.N("%s 新建了常用筛选“%s”"), name)
	})
	if err != nil {
		return View{}, err
	}
	return s.get(ctx, scope, id)
}

// Update changes a view; only its creator or a workspace owner may.
func (s *Service) Update(ctx context.Context, scope tenant.Scope, id int64, in Input) (View, error) {
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		cur, err := s.editable(ctx, q, scope, id)
		if err != nil {
			return err
		}
		name, f, sort := cur.Name, json.RawMessage(cur.Filter), cur.Sort
		if in.Name != nil {
			name = *in.Name
		}
		if in.Filter != nil {
			f = *in.Filter
		}
		if in.Sort != nil {
			sort = *in.Sort
		}
		disp := cur.Display
		if in.Display != nil {
			if disp, err = in.Display.encode(); err != nil {
				return err
			}
		}
		name, fs, sort, err := s.validate(ctx, scope, name, f, sort)
		if err != nil {
			return err
		}
		if err := s.checkName(ctx, q, scope, name, id); err != nil {
			return err
		}
		if name == cur.Name && fs == cur.Filter && sort == cur.Sort && disp == cur.Display {
			return nil
		}
		if err := q.UpdateSavedView(ctx, sqlc.UpdateSavedViewParams{
			Name: name, SortKey: pinyin.SortKey(name), Filter: fs, Sort: sort, Display: disp, UpdatedAt: s.now().UnixMilli(), WorkspaceID: scope.WorkspaceID(), ID: id,
		}); err != nil {
			return err
		}
		changes := map[string][2]any{}
		if name != cur.Name {
			changes["name"] = [2]any{cur.Name, name}
			return s.record(ctx, q, scope, "update", id, changes, i18n.N("%s 将常用筛选“%s”改名为“%s”"), cur.Name, name)
		}
		return s.record(ctx, q, scope, "update", id, changes, i18n.N("%s 修改了常用筛选“%s”"), name)
	})
	if err != nil {
		return View{}, err
	}
	return s.get(ctx, scope, id)
}

// Delete removes a view; only its creator or a workspace owner may.
func (s *Service) Delete(ctx context.Context, scope tenant.Scope, id int64) error {
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		cur, err := s.editable(ctx, q, scope, id)
		if err != nil {
			return err
		}
		if err := q.DeleteSavedView(ctx, sqlc.DeleteSavedViewParams{WorkspaceID: scope.WorkspaceID(), ID: id}); err != nil {
			return err
		}
		return s.record(ctx, q, scope, "delete", id, nil, i18n.N("%s 删除了常用筛选“%s”"), cur.Name)
	})
}

func (s *Service) editable(ctx context.Context, q *sqlc.Queries, scope tenant.Scope, id int64) (sqlc.SavedView, error) {
	cur, err := q.GetSavedView(ctx, sqlc.GetSavedViewParams{WorkspaceID: scope.WorkspaceID(), ID: id})
	if errors.Is(err, sql.ErrNoRows) {
		return cur, errNotFound
	}
	if err != nil {
		return cur, err
	}
	if scope.Role() != tenant.RoleOwner && (!cur.CreatedBy.Valid || cur.CreatedBy.Int64 != audit.ActorFrom(ctx).UserID) {
		return cur, errNotAuthor
	}
	return cur, nil
}

// validate normalizes the name, checks the filter compiles in this
// workspace (its classifications exist) and checks the sort.
func (s *Service) validate(ctx context.Context, scope tenant.Scope, name string, raw json.RawMessage, sort string) (string, string, string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > maxNameLen {
		return "", "", "", errName
	}
	c, err := filter.Parse(raw)
	if err != nil {
		return "", "", "", err
	}
	cat, err := filter.LoadCatalog(ctx, s.q, scope)
	if err != nil {
		return "", "", "", err
	}
	cat.UserID = audit.ActorFrom(ctx).UserID // views are personal
	// Saving checks the shape and the classifications; keywords are only
	// searched when the view is used.
	cat.Text = func(string, bool) ([]int64, bool, error) { return nil, true, nil }
	if _, err := filter.Compile(cat, scope, c); err != nil {
		return "", "", "", err
	}
	compact, _ := json.Marshal(c)
	if sort != "" {
		key, dir, ok := strings.Cut(sort, ":")
		valid := map[string]bool{document.SortCreated: true, document.SortAdded: true, document.SortTitle: true, document.SortCorrespondent: true, document.SortRelevance: true}
		if !ok || !valid[key] || (dir != "asc" && dir != "desc") {
			return "", "", "", errSort
		}
	}
	return name, string(compact), sort, nil
}

func (s *Service) checkName(ctx context.Context, q *sqlc.Queries, scope tenant.Scope, name string, self int64) error {
	id, err := q.FindSavedViewByName(ctx, sqlc.FindSavedViewByNameParams{WorkspaceID: scope.WorkspaceID(), Name: name})
	if err == nil && id != self {
		return errTaken
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return nil
}

func (s *Service) record(ctx context.Context, q *sqlc.Queries, scope tenant.Scope, verb string, id int64, changes map[string][2]any, format string, args ...any) error {
	return audit.Record(ctx, q, audit.Event{
		WorkspaceID: scope.WorkspaceID(), Action: "view." + verb, TargetType: "view", TargetID: strconv.FormatInt(id, 10),
		Summary: fmt.Sprintf(format, append([]any{audit.ActorFrom(ctx).Name}, args...)...), Changes: changes, At: s.now(),
	})
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

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

var errOrder = apperr.New(apperr.KindInvalid, "顺序里要包含全部常用筛选，每个一次")

// Reorder sets the order of the workspace's views (v1.6.5); ids must be
// every view, each once.
func (s *Service) Reorder(ctx context.Context, scope tenant.Scope, ids []int64) error {
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		have, err := q.ListSavedViews(ctx, scope.WorkspaceID())
		if err != nil {
			return err
		}
		seen := map[int64]bool{}
		for _, id := range ids {
			seen[id] = true
		}
		if len(ids) != len(have) || len(seen) != len(ids) {
			return errOrder
		}
		for i, id := range ids {
			n, err := q.SetSavedViewPosition(ctx, sqlc.SetSavedViewPositionParams{Position: int64(i + 1), WorkspaceID: scope.WorkspaceID(), ID: id})
			if err != nil {
				return err
			}
			if n == 0 {
				return errOrder
			}
		}
		return s.record(ctx, q, scope, "reorder", 0, nil, i18n.N("%s 调整了常用筛选的顺序"))
	})
}
