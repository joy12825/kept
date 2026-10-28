package taxonomy

import "testing"

// Renaming, moving or deleting a classification changes what the search
// index holds for the documents using it.
func TestTaxonomyChangesRaiseTheIndexRevision(t *testing.T) {
	e := newEnv(t)
	corr, err := e.s.Create(ctx, home, Correspondent, Input{Name: "国家电网"})
	if err != nil {
		t.Fatal(err)
	}
	parent, _ := e.s.CreateTag(ctx, home, TagInput{Name: "财务"})
	child, _ := e.s.CreateTag(ctx, home, TagInput{Name: "发票", ParentID: parent.ID})
	other, _ := e.s.CreateTag(ctx, home, TagInput{Name: "水电"})
	field, _ := e.s.CreateField(ctx, home, FieldInput{Name: "户号", DataType: "text"})
	for _, q := range []string{
		`INSERT INTO documents (id, workspace_id, title, created_date, added_at, modified_at, mime_type, original_filename, original_ext, checksum, correspondent_id)
		 VALUES (1, 1, 'a', '2024-01-01', 1, 1, 'text/plain', 'a.txt', 'txt', 'a', ?), (2, 1, 'b', '2024-01-01', 1, 1, 'text/plain', 'b.txt', 'txt', 'b', NULL)`,
	} {
		if _, err := e.st.DB.Exec(q, corr.ID); err != nil {
			t.Fatal(err)
		}
	}
	e.st.DB.Exec(`INSERT INTO document_tags (document_id, tag_id) VALUES (1, ?), (2, ?)`, child.ID, other.ID)
	e.st.DB.Exec(`INSERT INTO custom_field_values (document_id, field_id, value_text) VALUES (2, ?, '0012345')`, field.ID)
	rev := func(id int) (r int) {
		_ = e.st.DB.QueryRow(`SELECT index_rev FROM documents WHERE id = ?`, id).Scan(&r)
		return r
	}

	cases := []struct {
		name    string
		do      func() error
		changed int // document whose revision must go up
		same    int // document that must stay
	}{
		{"rename correspondent", func() error {
			n := "国网北京"
			_, err := e.s.Update(ctx, home, Correspondent, corr.ID, Patch{Name: &n})
			return err
		}, 1, 2},
		{"rename parent tag", func() error {
			n := "账务"
			_, err := e.s.UpdateTag(ctx, home, parent.ID, TagPatch{Name: &n})
			return err
		}, 1, 2},
		{"delete field", func() error { return e.s.DeleteField(ctx, home, field.ID) }, 2, 1},
		{"delete parent tag", func() error { return e.s.DeleteTag(ctx, home, parent.ID) }, 1, 2},
		{"delete correspondent", func() error { return e.s.Delete(ctx, home, Correspondent, corr.ID) }, 1, 2},
	}
	for _, c := range cases {
		a, b := rev(c.changed), rev(c.same)
		if err := c.do(); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if rev(c.changed) <= a || rev(c.same) != b {
			t.Errorf("%s: revisions %d→%d (want up), %d→%d (want same)", c.name, a, rev(c.changed), b, rev(c.same))
		}
	}
}
