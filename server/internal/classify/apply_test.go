package classify

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"kept/server/internal/document"
	"kept/server/internal/store"
	"kept/server/internal/tenant"
)

func src(s string) *document.Source { return &document.Source{Source: s} }

func TestDecide(t *testing.T) {
	rule := document.Source{Source: document.SourceRule, Basis: "关键词“国家电网”"}
	sug := Suggestion{TypeID: 1, CorrespondentID: 10, TagIDs: []int64{100, 101},
		Sources: document.FieldSources{Type: &rule, Correspondent: &rule, Tags: map[int64]document.Source{100: rule, 101: rule}}}
	for _, c := range []struct {
		name               string
		doc                document.Document
		have               []int64
		mode               string
		wantType, wantCorr *int64
		wantTags           []int64 // nil: unchanged
	}{
		{name: "fill an empty document", doc: document.Document{}, mode: ModeFill,
			wantType: ptr(1), wantCorr: ptr(10), wantTags: []int64{100, 101}},
		{name: "fill keeps values", doc: document.Document{DocumentTypeID: 2, CorrespondentID: 11}, have: []int64{100, 101}, mode: ModeFill},
		{name: "overwrite replaces values", doc: document.Document{DocumentTypeID: 2, CorrespondentID: 11}, have: []int64{100, 101}, mode: ModeOverwrite,
			wantType: ptr(1), wantCorr: ptr(10)},
		{name: "overwrite leaves user fields", mode: ModeOverwrite, have: []int64{100, 101},
			doc: document.Document{DocumentTypeID: 2, CorrespondentID: 0, FieldSources: document.FieldSources{Type: src("user"), Correspondent: src("user")}}},
		{name: "a tag the user removed stays removed", mode: ModeFill, have: []int64{100},
			doc: document.Document{DocumentTypeID: 1, CorrespondentID: 10, FieldSources: document.FieldSources{Tags: map[int64]document.Source{101: {Source: "user"}}}}},
		{name: "overwrite drops automatic tags no longer suggested, keeps others", mode: ModeOverwrite, have: []int64{100, 101, 102, 103},
			doc: document.Document{DocumentTypeID: 1, CorrespondentID: 10, FieldSources: document.FieldSources{Tags: map[int64]document.Source{
				102: {Source: "model"}, 103: {Source: "user"}}}},
			wantTags: []int64{100, 101, 103}},
	} {
		p, changed := decide(c.doc, c.have, sug, c.mode)
		if !eqPtr(p.TypeID, c.wantType) || !eqPtr(p.CorrespondentID, c.wantCorr) {
			t.Errorf("%s: type %v corr %v", c.name, deref(p.TypeID), deref(p.CorrespondentID))
		}
		var tags []int64
		if p.TagIDs != nil {
			tags = slices.Sorted(slices.Values(*p.TagIDs))
		}
		if !slices.Equal(tags, c.wantTags) {
			t.Errorf("%s: tags %v, want %v", c.name, tags, c.wantTags)
		}
		if changed != (c.wantType != nil || c.wantCorr != nil || c.wantTags != nil) {
			t.Errorf("%s: changed = %v", c.name, changed)
		}
	}
}

func ptr(v int64) *int64 { return &v }
func eqPtr(a, b *int64) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}
func deref(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestSuggestAndReapply(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	db := st.DB
	mustExec(t, db, `INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'a', 'family', 1), (2, 'b', 'family', 1)`)
	mustExec(t, db, `INSERT INTO document_types (id, workspace_id, name, sort_key, algorithm, match, created_at, updated_at) VALUES (1, 1, '账单', 'z', 'any', '账单 通知单', 1, 1)`)
	mustExec(t, db, `INSERT INTO correspondents (id, workspace_id, name, sort_key, algorithm, match, created_at, updated_at)
		VALUES (10, 1, '国家电网', 'g', 'literal', '国家电网', 1, 1), (11, 2, '别人的', 'b', 'any', '国家电网', 1, 1)`)
	mustExec(t, db, `INSERT INTO tags (id, workspace_id, name, sort_key, color, algorithm, match, created_at, updated_at)
		VALUES (100, 1, '水电', 's', 'blue', 'any', '电费 水费', 1, 1), (101, 1, '税务', 's', 'blue', 'regex', '发票\d+', 1, 1)`)
	mustExec(t, db, `INSERT INTO documents (id, workspace_id, title, content, created_date, added_at, modified_at, mime_type, original_filename, original_ext, checksum, inbox)
		VALUES (1, 1, '三月', '国家电网 电费通知单', '2024-01-01', 1, 1, 'text/plain', 'a.txt', 'txt', 'a', 0),
		       (2, 1, '四月', '国家电网 电费通知单', '2024-01-01', 1, 1, 'text/plain', 'b.txt', 'txt', 'b', 0)`)
	docs := document.NewService(db, document.Options{})
	s := NewService(db, Options{Documents: docs})

	sug, err := s.Suggest(ctx, 1, "三月", "国家电网 电费通知单")
	if err != nil {
		t.Fatal(err)
	}
	if sug.TypeID != 1 || sug.CorrespondentID != 10 || !slices.Equal(sug.TagIDs, []int64{100}) {
		t.Fatalf("suggestion = %+v (the other workspace's rule must not apply)", sug)
	}
	if b := sug.Sources.Type.Basis; b != "关键词“通知单”" {
		t.Errorf("type basis = %q", b)
	}

	// A person cleared the type of document 2: re-applying must leave it empty.
	scope := tenant.New(1, tenant.RoleEditor)
	zero := int64(0)
	one := int64(1)
	if _, err := docs.Update(ctx, scope, 2, document.Patch{TypeID: &one}); err != nil {
		t.Fatal(err)
	}
	if _, err := docs.Update(ctx, scope, 2, document.Patch{TypeID: &zero}); err != nil {
		t.Fatal(err)
	}
	suggest, _ := s.Suggester(ctx, 1)
	for _, id := range []int64{1, 2} {
		if _, err := s.reapply(ctx, scope, id, suggest, nil, ModeOverwrite); err != nil {
			t.Fatal(err)
		}
	}
	d1, _ := docs.Get(ctx, scope, 1)
	d2, _ := docs.Get(ctx, scope, 2)
	if d1.DocumentTypeID != 1 || d1.CorrespondentID != 10 || d1.FieldSources.Type == nil || d1.FieldSources.Type.Source != "rule" {
		t.Errorf("document 1 = %+v", d1)
	}
	if d1.FieldSources.Tags[100].Source != "rule" {
		t.Errorf("tag source = %+v", d1.FieldSources.Tags)
	}
	if d2.DocumentTypeID != 0 || !d2.FieldSources.Type.UserSet() || d2.CorrespondentID != 10 {
		t.Errorf("document 2 = type %d sources %+v", d2.DocumentTypeID, d2.FieldSources)
	}
	// Running again changes nothing.
	if changed, _ := s.reapply(ctx, scope, 1, suggest, nil, ModeOverwrite); changed {
		t.Error("a second run should change nothing")
	}
	// A user edit of an automatic field makes it user-set.
	other := int64(0)
	if _, err := docs.Update(ctx, scope, 1, document.Patch{CorrespondentID: &other, ExpectedModifiedAt: ptrTime(d1.ModifiedAt)}); err != nil {
		t.Fatal(err)
	}
	d1, _ = docs.Get(ctx, scope, 1)
	if !d1.FieldSources.Correspondent.UserSet() || d1.FieldSources.Type.Source != "rule" {
		t.Errorf("after the edit: %+v", d1.FieldSources)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
