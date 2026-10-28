package paperless

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kept/server/internal/files"
	"kept/server/internal/parser"
	"kept/server/internal/pdftest"
	"kept/server/internal/store"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/taxonomy"
	"kept/server/internal/tenant"
)

func rec(model string, pk int64, fields map[string]any, extra ...string) map[string]any {
	r := map[string]any{"model": model, "pk": pk, "fields": fields}
	for i := 0; i+1 < len(extra); i += 2 {
		r[extra[i]] = extra[i+1]
	}
	return r
}

// export writes a Paperless export directory.
func export(t *testing.T, manifest []map[string]any, files map[string][]byte, split []map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, b []byte) {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := json.Marshal(manifest)
	write("manifest.json", b)
	if split != nil {
		b, _ = json.Marshal(split)
		write("0000004-manifest.json", b)
	}
	for name, content := range files {
		write(name, content)
	}
	return dir
}

func TestImport(t *testing.T) {
	if len(parser.MissingTools()) > 0 {
		t.Skip("needs poppler")
	}
	ctx := context.Background()
	data := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(data, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.DB.Exec(`INSERT INTO users (id, username, display_name, password_hash, password_changed_at, created_at, updated_at) VALUES (1, 'wang', '小王', 'x', 1, 1, 1)`)
	st.DB.Exec(`INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, '家', 'family', 1)`)
	scope := tenant.New(1, tenant.RoleOwner)
	tax := taxonomy.NewService(st.DB, taxonomy.Options{})
	existing, err := tax.Create(ctx, scope, taxonomy.Correspondent, taxonomy.Input{Name: "国家电网"})
	if err != nil {
		t.Fatal(err)
	}
	fs := files.NewStore(data)
	// A document already in the workspace: the same file in the export is skipped.
	dup := pdftest.Text("ALREADY HERE")
	sum := sha256.Sum256(dup)
	if _, err := sqlc.New(st.DB).InsertDocument(ctx, sqlc.InsertDocumentParams{WorkspaceID: 1, Title: "旧", TitleSort: "jiu", CreatedDate: "2020-01-01",
		MimeType: "application/pdf", OriginalFilename: "old.pdf", OriginalExt: "pdf", PageCount: 1, Checksum: hex.EncodeToString(sum[:]), FieldSources: "{}", Source: "web", Storage: files.Local}); err != nil {
		t.Fatal(err)
	}

	manifest := []map[string]any{
		rec("documents.correspondent", 1, map[string]any{"name": "国家电网", "match": "", "matching_algorithm": 1, "is_insensitive": true}),
		rec("documents.correspondent", 2, map[string]any{"name": "Landlord", "match": "lease rent", "matching_algorithm": 2, "is_insensitive": true}),
		rec("documents.correspondent", 3, map[string]any{"name": "No keywords", "match": "", "matching_algorithm": 1, "is_insensitive": true}),
		rec("documents.documenttype", 1, map[string]any{"name": "合同", "match": "", "matching_algorithm": 6, "is_insensitive": true}),
		rec("documents.tag", 1, map[string]any{"name": "Inbox", "color": "#a6cee3", "is_inbox_tag": true, "matching_algorithm": 0}),
		rec("documents.tag", 3, map[string]any{"name": "专票", "color": "#1f78b4", "tn_parent": 2, "matching_algorithm": 0}),
		rec("documents.tag", 2, map[string]any{"name": "发票", "color": "#33a02c", "matching_algorithm": 0}),
		rec("documents.customfield", 1, map[string]any{"name": "Rent", "data_type": "monetary", "extra_data": map[string]any{"default_currency": "EUR"}}),
		rec("documents.customfield", 2, map[string]any{"name": "Room", "data_type": "select", "extra_data": map[string]any{"select_options": []map[string]string{{"id": "aa", "label": "Bedroom"}, {"id": "bb", "label": "Kitchen"}}}}),
		rec("documents.customfield", 3, map[string]any{"name": "Related", "data_type": "documentlink", "extra_data": map[string]any{}}),
		rec("documents.document", 1, map[string]any{"title": "Lease 2023", "content": "HOUSE LEASE AGREEMENT", "created": "2023-09-01T00:00:00+08:00",
			"added": "2023-09-02T10:00:00Z", "modified": "2023-09-03T10:00:00Z", "correspondent": 2, "document_type": 1, "tags": []int{1, 3},
			"mime_type": "application/pdf", "archive_serial_number": 5, "original_filename": "lease.pdf"},
			"__exported_file_name__", "0000001.pdf", "__exported_archive_name__", "archive/0000001.pdf"),
		rec("documents.document", 2, map[string]any{"title": "Bill", "content": "POWER", "created": "2024-03-05", "correspondent": 1, "tags": []int{},
			"mime_type": "application/pdf", "original_filename": "bill.pdf"}, "__exported_file_name__", "0000002.pdf"),
		rec("documents.document", 3, map[string]any{"title": "Lost", "created": "2024-01-01", "mime_type": "application/pdf"}, "__exported_file_name__", "0000003.pdf"),
		rec("documents.note", 1, map[string]any{"note": "押金已付", "created": "2023-09-04T08:00:00Z", "document": 1}),
		rec("documents.customfieldinstance", 1, map[string]any{"document": 1, "field": 1, "value_monetary": "EUR1200.50"}),
		rec("documents.customfieldinstance", 2, map[string]any{"document": 1, "field": 2, "value_select": "bb"}),
		rec("documents.customfieldinstance", 3, map[string]any{"document": 1, "field": 3, "value_document_ids": []int{2}}),
	}
	split := []map[string]any{ // --split-manifest: one more document beside its file
		rec("documents.document", 4, map[string]any{"title": "Dup", "created": "2020-01-01", "mime_type": "application/pdf", "tags": []int{}}, "__exported_file_name__", "0000004.pdf"),
	}
	dir := export(t, manifest, map[string][]byte{
		"0000001.pdf": pdftest.Text("LEASE ORIGINAL", "PAGE TWO"), "archive/0000001.pdf": pdftest.Text("LEASE ARCHIVE", "PAGE TWO"),
		"0000002.pdf": pdftest.Text("POWER BILL"), "0000004.pdf": dup,
	}, split)

	rep, err := Import(ctx, Deps{DB: st.DB, Taxonomy: tax, Files: fs}, scope, 1, dir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Correspondents != 2 || rep.Types != 1 || rep.Tags != 2 || rep.Fields != 3 || rep.Documents != 2 || rep.Skipped != 1 ||
		rep.Notes != 1 || len(rep.Missing) != 1 || rep.Missing[0] != "Lost" {
		t.Fatalf("report = %+v", rep)
	}

	type row struct {
		id, pages             int64
		title, date, source   string
		inbox, archive, thumb int
		corr, typ, asn        sql.NullInt64
	}
	get := func(title string) row {
		var r row
		if err := st.DB.QueryRow(`SELECT id, title, created_date, source, inbox, has_archive, has_thumbnail, page_count, correspondent_id, document_type_id, asn FROM documents WHERE title = ?`, title).
			Scan(&r.id, &r.title, &r.date, &r.source, &r.inbox, &r.archive, &r.thumb, &r.pages, &r.corr, &r.typ, &r.asn); err != nil {
			t.Fatal(title, err)
		}
		return r
	}
	lease := get("Lease 2023")
	if lease.date != "2023-09-01" || lease.source != "import" || lease.inbox != 1 || lease.archive != 1 || lease.thumb != 1 || lease.pages != 2 ||
		!lease.typ.Valid || lease.asn.Int64 != 5 {
		t.Errorf("lease = %+v", lease)
	}
	if b, _ := os.ReadFile(fs.Path(files.Archive(1, lease.id))); !strings.Contains(string(b), "LEASE ARCHIVE") {
		t.Error("the archive version was not kept")
	}
	if b, _ := os.ReadFile(fs.Path(files.Original(1, lease.id, "pdf"))); !strings.Contains(string(b), "LEASE ORIGINAL") {
		t.Error("the original was not kept")
	}
	bill := get("Bill")
	if bill.corr.Int64 != existing.ID || bill.inbox != 0 || bill.archive != 0 {
		t.Errorf("bill reuses 国家电网 and is filed: %+v", bill)
	}
	// Tags: 专票 under 发票, the inbox tag not created.
	tags, _ := tax.ListTags(ctx, scope)
	var parent, child taxonomy.TagItem
	for _, tg := range tags {
		switch tg.Name {
		case "发票":
			parent = tg
		case "专票":
			child = tg
		case "Inbox":
			t.Error("the inbox tag should not be created")
		}
	}
	if child.ParentID != parent.ID || parent.ID == 0 {
		t.Errorf("tags = %+v", tags)
	}
	var n int
	st.DB.QueryRow(`SELECT count(*) FROM document_tags WHERE document_id = ? AND tag_id = ?`, lease.id, child.ID).Scan(&n)
	if n != 1 {
		t.Error("the lease lost its tag")
	}
	// Custom fields: money in cents, the select option, the link to the bill.
	var cents, link sql.NullInt64
	var opt sql.NullString
	st.DB.QueryRow(`SELECT v.value_monetary FROM custom_field_values v JOIN custom_fields f ON f.id = v.field_id WHERE v.document_id = ? AND f.name = 'Rent'`, lease.id).Scan(&cents)
	st.DB.QueryRow(`SELECT v.value_select FROM custom_field_values v JOIN custom_fields f ON f.id = v.field_id WHERE v.document_id = ? AND f.name = 'Room'`, lease.id).Scan(&opt)
	st.DB.QueryRow(`SELECT v.value_doclink FROM custom_field_values v JOIN custom_fields f ON f.id = v.field_id WHERE v.document_id = ? AND f.name = 'Related'`, lease.id).Scan(&link)
	fields, _ := tax.ListFields(ctx, scope)
	var kitchen string
	for _, f := range fields {
		if f.Name == "Room" {
			kitchen = f.Extra.Options[1].ID
		}
		if f.Name == "Rent" && f.Extra.Currency != "EUR" {
			t.Errorf("currency = %q", f.Extra.Currency)
		}
	}
	if cents.Int64 != 120050 || opt.String != kitchen || link.Int64 != bill.id {
		t.Errorf("values: money=%v select=%v (want %s) link=%v (want %d)", cents, opt, kitchen, link, bill.id)
	}
	var note string
	st.DB.QueryRow(`SELECT body FROM notes WHERE document_id = ?`, lease.id).Scan(&note)
	if note != "押金已付" {
		t.Errorf("note = %q", note)
	}

	// Importing again adds nothing.
	again, err := Import(ctx, Deps{DB: st.DB, Taxonomy: tax, Files: fs}, scope, 1, dir)
	if err != nil || again.Documents != 0 || again.Skipped != 3 || again.Correspondents+again.Tags+again.Types+again.Fields != 0 {
		t.Errorf("second import = %+v, %v", again, err)
	}
}
