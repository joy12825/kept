package classify

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"kept/server/internal/document"
	"kept/server/internal/store"
	"kept/server/internal/tenant"
)

func TestSimilarDocumentsFillGaps(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	db := st.DB
	mustExec(t, db, `INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'a', 'family', 1)`)
	mustExec(t, db, `INSERT INTO document_types (id, workspace_id, name, sort_key, created_at, updated_at) VALUES (1, 1, '账单', 'z', 1, 1), (2, 1, '合同', 'h', 1, 1)`)
	// 燃气公司 is matched by a rule: similar documents must not vote for it.
	mustExec(t, db, `INSERT INTO correspondents (id, workspace_id, name, sort_key, algorithm, match, created_at, updated_at)
		VALUES (10, 1, '自来水公司', 'z', 'auto', '', 1, 1), (11, 1, '燃气公司', 'r', 'literal', '燃气公司', 1, 1)`)
	mustExec(t, db, `INSERT INTO tags (id, workspace_id, name, sort_key, color, created_at, updated_at) VALUES (100, 1, '水电', 's', 'blue', 1, 1), (101, 1, '房子', 'f', 'blue', 1, 1)`)
	doc := func(id int64, title string, inbox int, typ, corr any) {
		mustExec(t, db, `INSERT INTO documents (id, workspace_id, title, content, created_date, added_at, modified_at, mime_type, original_filename, original_ext, checksum, inbox, document_type_id, correspondent_id)
			VALUES (?, 1, ?, '缴费', '2024-01-01', 1, 1, 'text/plain', 'a.txt', 'txt', ?, ?, ?, ?)`, id, title, title, inbox, typ, corr)
	}
	doc(1, "一月水费", 0, 1, 10)
	doc(2, "二月水费", 0, 1, 10)
	doc(3, "三月水费", 0, 1, 10)
	doc(4, "租房合同", 0, 2, nil)
	doc(5, "待整理的四月水费", 1, nil, nil)
	doc(6, "另一份待整理", 1, nil, nil)
	mustExec(t, db, `INSERT INTO document_tags (document_id, tag_id) VALUES (1, 100), (2, 100), (3, 100), (1, 101)`)

	near := map[int64]map[int64]float64{
		5: {1: 0.92, 2: 0.9, 3: 0.88, 4: 0.7, 6: 0.95}, // 6 is in the inbox: no vote
		6: {1: 0.65, 4: 0.64, 2: 0.4},                  // too few close enough
	}
	docs := document.NewService(db, document.Options{})
	s := NewService(db, Options{Documents: docs})
	s.SetNeighbors(func(_ context.Context, _ tenant.Scope, id int64, _ int) (map[int64]float64, error) {
		return near[id], nil
	})
	s.FillFromNeighbors(ctx, 1, []int64{5, 6, 1})

	scope := tenant.New(1, tenant.RoleEditor)
	d5, _ := docs.Get(ctx, scope, 5)
	if d5.DocumentTypeID != 1 || d5.CorrespondentID != 10 {
		t.Fatalf("document 5 = type %d corr %d", d5.DocumentTypeID, d5.CorrespondentID)
	}
	ts := d5.FieldSources.Type
	if ts.Source != document.SourceSimilar || ts.Confidence < 0.75 || ts.Confidence > 0.8 || !strings.Contains(ts.Basis, "一月水费") || !strings.Contains(ts.Basis, "3 份") {
		t.Errorf("type source = %+v", ts)
	}
	tags, _ := document.NewService(db, document.Options{}).GetDetail(ctx, scope, 5)
	if !slices.Equal(tags.TagIDs, []int64{100}) || tags.FieldSources.Tags[100].Source != document.SourceSimilar {
		t.Errorf("tags = %v %+v (房子 is on 1 of 4 only)", tags.TagIDs, tags.FieldSources.Tags)
	}
	if d6, _ := docs.Get(ctx, scope, 6); d6.DocumentTypeID != 0 || d6.CorrespondentID != 0 {
		t.Errorf("document 6 = %+v", d6)
	}
	// Filed documents are left alone.
	if d1, _ := docs.Get(ctx, scope, 1); d1.FieldSources.Type != nil {
		t.Errorf("document 1 touched: %+v", d1.FieldSources)
	}

	// A rule-matched correspondent does not get votes.
	mustExec(t, db, `UPDATE documents SET correspondent_id = 11 WHERE id IN (1, 2, 3)`)
	mustExec(t, db, `UPDATE documents SET correspondent_id = NULL, field_sources = '{}' WHERE id = 5`)
	_, learn, _ := s.Rules(ctx, 1)
	sug := Suggestion{}
	if err := s.supplement(ctx, scope, 5, learn, &sug); err != nil || sug.CorrespondentID != 0 {
		t.Errorf("rule-matched correspondent voted: %+v %v", sug, err)
	}
}
