package taxonomy

import (
	"context"
	"database/sql"
	"errors"
	"hash/fnv"
	"kept/server/internal/i18n"

	"kept/server/internal/apperr"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// TagColorList is the 12 preset tag colours (UI spec 3.1), in display order.
var TagColorList = []string{
	"#4F6D7A", "#3A6EA5", "#4B4F9C", "#7A4E8C", "#A34D6D", "#A5523A",
	"#9A6A12", "#6B7A2E", "#2F6B4F", "#2C7A7B", "#555B63", "#8A6F4E",
}

// TagColors is TagColorList as a set.
var TagColors = func() map[string]bool {
	m := map[string]bool{}
	for _, c := range TagColorList {
		m[c] = true
	}
	return m
}()

// MaxTagDepth is how deep the tag hierarchy may go (design doc 3.2).
const MaxTagDepth = 5

var (
	errTagColor  = apperr.New(apperr.KindInvalid, "请从预设的颜色中选择")
	errTagParent = apperr.New(apperr.KindInvalid, "找不到上级标签")
	errTagCycle  = apperr.New(apperr.KindInvalid, "不能把标签移到它自己或它的子标签下")
	errTagDepth  = apperr.New(apperr.KindInvalid, "标签最多 5 层")
)

type TagItem struct {
	Item
	Color    string
	ParentID int64 // 0 for a top-level tag
	IsInbox  bool
	// ShowInYearbook: the yearbook sums up this tag's documents (用途标签).
	ShowInYearbook bool
}

type TagInput struct {
	Name     string
	Color    string // "" picks a preset
	ParentID int64  // 0 for top level
	Rule     *MatchRule
	// ShowInYearbook: summed up in the yearbook.
	ShowInYearbook bool
}

// TagPatch changes a tag; nil fields stay. ParentID 0 moves it to the top.
type TagPatch struct {
	Name     *string
	Color    *string
	ParentID *int64
	Rule     *MatchRule
	// ShowInYearbook: summed up in the yearbook.
	ShowInYearbook *bool
}

func toTag(r sqlc.Tag) TagItem {
	_, initial := sortFields(r.Name)
	return TagItem{
		Item: Item{ID: r.ID, Name: r.Name, SortKey: r.SortKey, Initial: initial,
			Rule: MatchRule{Algorithm: r.Algorithm, Match: r.Match, CaseInsensitive: r.CaseInsensitive == 1}},
		Color: r.Color, ParentID: r.ParentID.Int64, IsInbox: r.IsInbox == 1, ShowInYearbook: r.ShowInYearbook == 1,
	}
}

// defaultColor spreads tags over the presets by name, so the same name
// always gets the same colour.
func defaultColor(name string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return TagColorList[h.Sum32()%uint32(len(TagColorList))]
}

func (s *Service) ListTags(ctx context.Context, scope tenant.Scope) ([]TagItem, error) {
	rows, err := s.q.ListTags(ctx, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	out := make([]TagItem, len(rows))
	for i, r := range rows {
		out[i] = toTag(r)
	}
	return out, nil
}

func (s *Service) CreateTag(ctx context.Context, scope tenant.Scope, in TagInput) (TagItem, error) {
	name, err := normalizeName(in.Name)
	if err != nil {
		return TagItem{}, err
	}
	color := in.Color
	if color == "" {
		color = defaultColor(name)
	}
	if !TagColors[color] {
		return TagItem{}, errTagColor
	}
	rule := DefaultRule
	if in.Rule != nil {
		if rule, err = in.Rule.normalized(); err != nil {
			return TagItem{}, err
		}
	}
	var tag TagItem
	err = s.inTx(ctx, func(q *sqlc.Queries) error {
		ws := scope.WorkspaceID()
		if err := ensureFree(ctx, findTag, q, ws, name, 0, Tag); err != nil {
			return err
		}
		if in.ParentID != 0 {
			tree, err := loadTree(ctx, q, ws)
			if err != nil {
				return err
			}
			if err := tree.checkPlacement(0, in.ParentID); err != nil {
				return err
			}
		}
		sortKey, _ := sortFields(name)
		now := s.now().UnixMilli()
		row, err := q.InsertTag(ctx, sqlc.InsertTagParams{
			WorkspaceID: ws, Name: name, SortKey: sortKey, Color: color, ParentID: nullID(in.ParentID),
			Match: rule.Match, Algorithm: rule.Algorithm, CaseInsensitive: boolInt(rule.CaseInsensitive), ShowInYearbook: boolInt(in.ShowInYearbook),
			CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			return err
		}
		tag = toTag(row)
		return s.record(ctx, q, ws, Tag, "create", tag.ID, nil, kindSummaries[Tag].create, name)
	})
	return tag, err
}

func (s *Service) UpdateTag(ctx context.Context, scope tenant.Scope, id int64, p TagPatch) (TagItem, error) {
	var tag TagItem
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		ws := scope.WorkspaceID()
		row, err := q.GetTag(ctx, sqlc.GetTagParams{WorkspaceID: ws, ID: id})
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		before := toTag(row)
		tag = before
		if p.Name != nil {
			if tag.Name, err = normalizeName(*p.Name); err != nil {
				return err
			}
		}
		if p.Color != nil {
			if !TagColors[*p.Color] {
				return errTagColor
			}
			tag.Color = *p.Color
		}
		if p.Rule != nil {
			if tag.Rule, err = p.Rule.normalized(); err != nil {
				return err
			}
		}
		if p.ParentID != nil {
			tag.ParentID = *p.ParentID
		}
		if p.ShowInYearbook != nil {
			tag.ShowInYearbook = *p.ShowInYearbook
		}
		changes := map[string][2]any{}
		if tag.Name != before.Name {
			changes["name"] = [2]any{before.Name, tag.Name}
			if err := ensureFree(ctx, findTag, q, ws, tag.Name, id, Tag); err != nil {
				return err
			}
		}
		if tag.Color != before.Color {
			changes["color"] = [2]any{before.Color, tag.Color}
		}
		if tag.ParentID != before.ParentID {
			tree, err := loadTree(ctx, q, ws)
			if err != nil {
				return err
			}
			if err := tree.checkPlacement(id, tag.ParentID); err != nil {
				return err
			}
			changes["parent"] = [2]any{tree.name(before.ParentID), tree.name(tag.ParentID)}
		}
		if tag.ShowInYearbook != before.ShowInYearbook {
			changes["show_in_yearbook"] = [2]any{before.ShowInYearbook, tag.ShowInYearbook}
		}
		before.Rule.changes(tag.Rule, changes)
		if len(changes) == 0 {
			return nil
		}
		tag.SortKey, tag.Initial = sortFields(tag.Name)
		if tag.Name != before.Name || tag.ParentID != before.ParentID {
			if err := bumpTagTree(ctx, q, ws, id); err != nil { // the paths of the whole subtree change
				return err
			}
		}
		if err := q.UpdateTag(ctx, sqlc.UpdateTagParams{
			Name: tag.Name, SortKey: tag.SortKey, Color: tag.Color, ParentID: nullID(tag.ParentID),
			Match: tag.Rule.Match, Algorithm: tag.Rule.Algorithm, CaseInsensitive: boolInt(tag.Rule.CaseInsensitive),
			ShowInYearbook: boolInt(tag.ShowInYearbook), UpdatedAt: s.now().UnixMilli(), WorkspaceID: ws, ID: id,
		}); err != nil {
			return err
		}
		sf, sa := renameSummary(Tag, before.Name, tag.Name)
		return s.record(ctx, q, ws, Tag, "update", id, changes, sf, sa...)
	})
	return tag, err
}

// DeleteTag deletes a tag; its children move up to its own parent.
func (s *Service) DeleteTag(ctx context.Context, scope tenant.Scope, id int64) error {
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		ws := scope.WorkspaceID()
		row, err := q.GetTag(ctx, sqlc.GetTagParams{WorkspaceID: ws, ID: id})
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := bumpTagTree(ctx, q, ws, id); err != nil {
			return err
		}
		moved, err := q.ReparentTagChildren(ctx, sqlc.ReparentTagChildrenParams{
			NewParentID: row.ParentID, UpdatedAt: s.now().UnixMilli(), WorkspaceID: ws, OldParentID: nullID(id),
		})
		if err != nil {
			return err
		}
		if err := q.DeleteTag(ctx, sqlc.DeleteTagParams{WorkspaceID: ws, ID: id}); err != nil {
			return err
		}
		if moved > 0 {
			return s.record(ctx, q, ws, Tag, "delete", id, nil, i18n.N("%s 删除了标签“%s”，它的 %d 个子标签已上移一层"), row.Name, moved)
		}
		return s.record(ctx, q, ws, Tag, "delete", id, nil, kindSummaries[Tag].delete, row.Name)
	})
}

func findTag(ctx context.Context, q *sqlc.Queries, ws int64, name string) (int64, error) {
	return q.FindTagByName(ctx, sqlc.FindTagByNameParams{WorkspaceID: ws, Name: name})
}

// tree is a workspace's tag hierarchy, loaded to check moves. Workspaces
// have tens to hundreds of tags, so loading them all is cheap.
type tree struct {
	parent map[int64]int64
	names  map[int64]string
}

func loadTree(ctx context.Context, q *sqlc.Queries, ws int64) (tree, error) {
	rows, err := q.ListTags(ctx, ws)
	if err != nil {
		return tree{}, err
	}
	t := tree{parent: map[int64]int64{}, names: map[int64]string{}}
	for _, r := range rows {
		t.parent[r.ID] = r.ParentID.Int64
		t.names[r.ID] = r.Name
	}
	return t, nil
}

func (t tree) name(id int64) string { return t.names[id] }

// depth is 1 for a top-level tag.
func (t tree) depth(id int64) int {
	d := 0
	for seen := 0; id != 0 && seen <= len(t.parent); seen++ {
		d++
		id = t.parent[id]
	}
	return d
}

// height is the number of levels in id's subtree, itself included.
func (t tree) height(id int64) int {
	h := 1
	for child, p := range t.parent {
		if p == id {
			h = max(h, 1+t.height(child))
		}
	}
	return h
}

// checkPlacement validates putting tag id (0 for a new tag) under parent.
func (t tree) checkPlacement(id, parent int64) error {
	if parent == 0 {
		if id != 0 && t.height(id) > MaxTagDepth {
			return errTagDepth
		}
		return nil
	}
	if _, ok := t.parent[parent]; !ok {
		return errTagParent
	}
	for p := parent; p != 0; p = t.parent[p] {
		if p == id {
			return errTagCycle
		}
	}
	height := 1
	if id != 0 {
		height = t.height(id)
	}
	if t.depth(parent)+height > MaxTagDepth {
		return errTagDepth
	}
	return nil
}

func nullID(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id != 0} }

// bumpTagTree marks for re-indexing the documents on a tag or on any of its
// descendants: the index holds tag paths ("财务 / 发票").
func bumpTagTree(ctx context.Context, q *sqlc.Queries, ws, root int64) error {
	tags, err := q.ListTags(ctx, ws)
	if err != nil {
		return err
	}
	subtree := []int64{root}
	for i := 0; i < len(subtree); i++ {
		for _, t := range tags {
			if t.ParentID.Valid && t.ParentID.Int64 == subtree[i] {
				subtree = append(subtree, t.ID)
			}
		}
	}
	for _, id := range subtree {
		if err := q.BumpIndexRevByTag(ctx, sqlc.BumpIndexRevByTagParams{WorkspaceID: ws, ID: id}); err != nil {
			return err
		}
	}
	return nil
}
