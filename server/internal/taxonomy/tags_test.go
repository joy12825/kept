package taxonomy

import (
	"errors"
	"strings"
	"testing"

	"kept/server/internal/apperr"
)

func ptr[T any](v T) *T { return &v }

func (e *env) tag(name string, parent int64) TagItem {
	e.t.Helper()
	t, err := e.s.CreateTag(ctx, home, TagInput{Name: name, ParentID: parent})
	if err != nil {
		e.t.Fatalf("CreateTag(%s): %v", name, err)
	}
	return t
}

func TestTagsBasics(t *testing.T) {
	e := newEnv(t)
	tax, err := e.s.CreateTag(ctx, home, TagInput{Name: "税务", Color: "#3A6EA5"})
	if err != nil {
		t.Fatal(err)
	}
	if tax.Color != "#3A6EA5" || tax.ParentID != 0 || tax.Initial != "S" || tax.Rule.Algorithm != "auto" {
		t.Errorf("created = %+v", tax)
	}
	// Without a colour, one of the presets is picked.
	auto := e.tag("保修", 0)
	if !TagColors[auto.Color] {
		t.Errorf("default colour %q is not a preset", auto.Color)
	}
	if _, err := e.s.CreateTag(ctx, home, TagInput{Name: "x", Color: "#FF0000"}); !isKind(err, apperr.KindInvalid) {
		t.Errorf("non-preset colour err = %v", err)
	}
	if _, err := e.s.CreateTag(ctx, home, TagInput{Name: "x", ParentID: 9999}); !isKind(err, apperr.KindInvalid) {
		t.Errorf("unknown parent err = %v", err)
	}
	if _, err := e.s.CreateTag(ctx, home, TagInput{Name: "税务"}); !isKind(err, apperr.KindConflict) {
		t.Errorf("duplicate name err = %v", err)
	}
	// A parent from another workspace does not exist here.
	foreign, err := e.s.CreateTag(ctx, other, TagInput{Name: "别处"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.CreateTag(ctx, home, TagInput{Name: "y", ParentID: foreign.ID}); !isKind(err, apperr.KindInvalid) {
		t.Errorf("foreign parent err = %v", err)
	}
	list, _ := e.s.ListTags(ctx, home)
	if len(list) != 2 {
		t.Errorf("home tags = %+v", list)
	}
}

func TestTagHierarchyRules(t *testing.T) {
	e := newEnv(t)
	// 财务 > 发票 > 增值税 > 专票 > 2024
	l1 := e.tag("财务", 0)
	l2 := e.tag("发票", l1.ID)
	l3 := e.tag("增值税", l2.ID)
	l4 := e.tag("专票", l3.ID)
	l5 := e.tag("2024", l4.ID)
	if _, err := e.s.CreateTag(ctx, home, TagInput{Name: "第六层", ParentID: l5.ID}); !isKind(err, apperr.KindInvalid) {
		t.Errorf("sixth level err = %v, want invalid", err)
	}
	// Moving a subtree counts its height: 增值税 (3 levels) under a level-3 tag would reach 6.
	side := e.tag("报销", l2.ID) // level 3
	if _, err := e.s.UpdateTag(ctx, home, l3.ID, TagPatch{ParentID: &side.ID}); !isKind(err, apperr.KindInvalid) {
		t.Errorf("moving a deep subtree err = %v, want invalid", err)
	}
	// No cycles: not under itself, not under a descendant.
	if _, err := e.s.UpdateTag(ctx, home, l2.ID, TagPatch{ParentID: &l2.ID}); !isKind(err, apperr.KindInvalid) {
		t.Errorf("parent = self err = %v", err)
	}
	if _, err := e.s.UpdateTag(ctx, home, l2.ID, TagPatch{ParentID: &l4.ID}); !isKind(err, apperr.KindInvalid) {
		t.Errorf("parent = descendant err = %v", err)
	}
	// Moving to the top level and back.
	got, err := e.s.UpdateTag(ctx, home, l4.ID, TagPatch{ParentID: ptr(int64(0))})
	if err != nil || got.ParentID != 0 {
		t.Fatalf("move to top = %+v, %v", got, err)
	}
	if got, err := e.s.UpdateTag(ctx, home, l4.ID, TagPatch{ParentID: &l1.ID, Color: ptr("#2F6B4F")}); err != nil || got.ParentID != l1.ID || got.Color != "#2F6B4F" {
		t.Errorf("move under 财务 = %+v, %v", got, err)
	}
}

func TestDeletingATagMovesItsChildrenUp(t *testing.T) {
	e := newEnv(t)
	root := e.tag("财务", 0)
	mid := e.tag("发票", root.ID)
	a := e.tag("专票", mid.ID)
	b := e.tag("普票", mid.ID)
	c := e.tag("电子", a.ID)

	if err := e.s.DeleteTag(ctx, home, mid.ID); err != nil {
		t.Fatal(err)
	}
	parents := map[string]int64{}
	list, _ := e.s.ListTags(ctx, home)
	for _, tg := range list {
		parents[tg.Name] = tg.ParentID
	}
	if _, ok := parents["发票"]; ok {
		t.Error("the tag was not deleted")
	}
	if parents["专票"] != root.ID || parents["普票"] != root.ID || parents["电子"] != a.ID {
		t.Errorf("parents after delete = %v (want 专票, 普票 under 财务; 电子 still under 专票)", parents)
	}
	_ = b
	_ = c
	// Deleting a top-level tag makes its children top-level.
	if err := e.s.DeleteTag(ctx, home, root.ID); err != nil {
		t.Fatal(err)
	}
	list, _ = e.s.ListTags(ctx, home)
	for _, tg := range list {
		if (tg.Name == "专票" || tg.Name == "普票") && tg.ParentID != 0 {
			t.Errorf("%s parent = %d, want top level", tg.Name, tg.ParentID)
		}
	}
	got := e.actions()
	if !strings.Contains(strings.Join(got, "\n"), "删除了标签“发票”，它的 2 个子标签已上移一层") {
		t.Errorf("audit = %v", got)
	}
	if err := e.s.DeleteTag(ctx, other, a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting from another workspace err = %v", err)
	}
}
