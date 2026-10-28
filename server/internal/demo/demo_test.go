package demo

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"kept/server/internal/auth"
	"kept/server/internal/classify"
	"kept/server/internal/drawer"
	"kept/server/internal/events"
	"kept/server/internal/files"
	"kept/server/internal/ingest"
	"kept/server/internal/jobs"
	"kept/server/internal/parser"
	"kept/server/internal/store"
	"kept/server/internal/taxonomy"
	"kept/server/internal/tenant"
	"kept/server/internal/workspace"
)

func TestLoadIsRepeatable(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	maxDocuments = 3
	t.Cleanup(func() { maxDocuments = 0 })
	fs := files.NewStore(t.TempDir())
	d := Deps{
		DB:         st.DB,
		Auth:       auth.NewService(st.DB, auth.Options{}),
		Workspaces: workspace.NewService(st.DB, workspace.Options{}),
		Taxonomy:   taxonomy.NewService(st.DB, taxonomy.Options{}),
		Ingest: ingest.NewService(st.DB, jobs.NewQueue(st.DB), fs, events.NewBus(), ingest.Options{
			Parser: parser.Config{OCRLanguages: "chi_sim+eng", OCRMode: "skip"},
		}),
		Classifier: classify.NewService(st.DB, classify.Options{}),
		Drawers:    drawer.NewService(st.DB, drawer.Options{}),
		Files:      fs,
	}
	withTools := len(parser.MissingTools()) == 0
	u, err := d.Auth.CreateUser(ctx, auth.NewUser{Username: "wang", Password: "password-123", Email: "wang@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(ctx, d, "nobody"); err == nil {
		t.Error("loading for a missing account should fail")
	}

	first, err := Load(ctx, d, "wang@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || !first[0].Created || first[0].Correspondents != 17 || first[0].Types != 7 || first[0].Tags != 6 ||
		first[1].Name != "某某工作室" || first[1].Correspondents != 7 || first[0].Views != 2 || first[1].Views != 2 {
		t.Errorf("first load = %+v", first)
	}
	if withTools && (first[0].Drawers != 6 || first[1].Drawers != 5) {
		t.Errorf("drawers = %d, %d", first[0].Drawers, first[1].Drawers)
	}
	again, err := Load(ctx, d, "wang")
	if err != nil {
		t.Fatal(err)
	}
	if withTools && (first[0].Documents != 3 || first[1].Documents != 3) {
		t.Errorf("documents loaded = %d, %d; want 3 each", first[0].Documents, first[1].Documents)
	}
	// v1.11.0: the recovery code of encrypted storages, once
	if len(first[0].StorageRecoveryCode) != 29 || again[0].StorageRecoveryCode != "" {
		t.Errorf("storage recovery code = %q, then %q", first[0].StorageRecoveryCode, again[0].StorageRecoveryCode)
	}
	if first[0].Devices != 1 || first[1].Devices != 0 {
		t.Errorf("devices = %d, %d; want a phone in the family workspace only", first[0].Devices, first[1].Devices)
	}
	if first[0].Workflows != 2 || first[1].Workflows != 0 {
		t.Errorf("workflows = %d, %d", first[0].Workflows, first[1].Workflows)
	}
	// v1.9.1: the note, the recording and the photo, once
	if withTools && (first[0].Previewed != 14 || again[0].Previewed != 0) { // v1.9.3 adds an OFD, a workbook and a zip; v1.9.7 an Outlook e-mail, a Word draft and slides; v1.9.8 a handbook for the reading view; v1.11.1 the handbook as .doc and .odt; v1.11.3 two signed e-invoices
		t.Errorf("previewer documents = %d, then %d", first[0].Previewed, again[0].Previewed)
	}
	var notifyOn int
	if err := d.DB.QueryRow(`SELECT count(*) FROM workspace_members WHERE user_id = ? AND notify_email = 1`, u.ID).Scan(&notifyOn); err != nil || notifyOn != 1 {
		t.Errorf("reminder emails on in %d workspaces, %v; want the family workspace", notifyOn, err)
	}
	if ds, err := d.Auth.ListDevices(ctx, u.ID); err != nil || len(ds) != 1 || ds[0].Name != "小王的 iPhone" || ds[0].LastSeenAt == nil {
		t.Errorf("demo devices = %+v, %v", ds, err)
	}
	if withTools && (first[0].Books != len(familyBooks)+1 || first[1].Books != 0) {
		t.Errorf("books = %d, %d", first[0].Books, first[1].Books)
	}
	for _, r := range again {
		if r.Created || r.Correspondents+r.Types+r.Tags+r.Documents+r.Views+r.Drawers+r.Devices+r.Books != 0 {
			t.Errorf("second load added %+v, want nothing", r)
		}
	}

	list, _ := d.Workspaces.List(ctx, u.ID)
	if len(list) != 3 {
		t.Fatalf("workspaces = %+v", list)
	}
	home := tenant.New(list[1].ID, tenant.RoleOwner)
	corr, _ := d.Taxonomy.List(ctx, home, taxonomy.Correspondent)
	for _, c := range corr {
		if c.Name == "国家电网" && (c.Rule.Algorithm != "any" || c.Rule.Match != "国家电网") {
			t.Errorf("国家电网 rule = %+v", c.Rule)
		}
	}
	tags, _ := d.Taxonomy.ListTags(ctx, home)
	if len(tags) != 6 || tags[0].Name != "保险" || tags[0].Color != "#7A4E8C" {
		t.Errorf("tags = %+v", tags)
	}
	// Recorded as the system, not as a person.
	var via string
	if err := st.DB.QueryRow(`SELECT actor_via FROM audit_events WHERE action = 'correspondent.create' LIMIT 1`).Scan(&via); err != nil || via != "system" {
		t.Errorf("audit via = %q, %v", via, err)
	}

	if !withTools {
		return
	}
	// The generated PDFs carry real Chinese text, metadata from the prototype
	// and the built-in amount.
	var title, content, date string
	var corrID, typeID sql.NullInt64
	var inbox int
	if err := st.DB.QueryRow(`SELECT title, content, created_date, correspondent_id, document_type_id, inbox FROM documents WHERE workspace_id = ? ORDER BY id LIMIT 1`, list[1].ID).
		Scan(&title, &content, &date, &corrID, &typeID, &inbox); err != nil {
		t.Fatal(err)
	}
	if title != "2023年1月电费账单" || !strings.Contains(content, "2023年1月电费账单") || !strings.Contains(content, "国家电网") ||
		date != "2023-01-05" || !corrID.Valid || !typeID.Valid || inbox != 0 {
		t.Errorf("first sample = %q %q %q corr=%v type=%v inbox=%d", title, content, date, corrID, typeID, inbox)
	}
	// 原件编号 follow the prototype: archived documents numbered by date.
	var asn int64
	if err := st.DB.QueryRow(`SELECT asn FROM documents WHERE workspace_id = ? AND title = '2023年1月电费账单'`, list[1].ID).Scan(&asn); err != nil || asn != 7 {
		t.Errorf("asn = %d, %v; want 7", asn, err)
	}
	// The classifier was trained once per workspace (with this few samples maybe only the reason is stored).
	var models int
	if err := st.DB.QueryRow(`SELECT count(*) FROM classifier_models`).Scan(&models); err != nil || models != 2 {
		t.Errorf("classifier models = %d, %v; want one per workspace", models, err)
	}
	// The books are in the 书房 with their catalog entries; the cover's text is searchable.
	var inLibrary int
	if err := st.DB.QueryRow(`SELECT count(*) FROM books b JOIN documents d ON d.id = b.document_id JOIN drawers w ON w.id = d.drawer_id
		WHERE w.kind = 'library' AND d.inbox = 0 AND b.isbn != '' AND (d.content LIKE '%著%' OR d.content LIKE '%床前明月光%')`).Scan(&inLibrary); err != nil || inLibrary != len(familyBooks)+1 {
		t.Errorf("books in the library = %d, %v", inLibrary, err)
	}
	var covers int
	if err := st.DB.QueryRow(`SELECT count(*) FROM books WHERE cover = 1`).Scan(&covers); err != nil || covers != 3 {
		t.Errorf("covers = %d, %v", covers, err)
	}
	var positions int
	if err := st.DB.QueryRow(`SELECT count(*) FROM reading_progress WHERE user_id = ? AND position != ''`, u.ID).Scan(&positions); err != nil || positions != 2 {
		t.Errorf("reading positions = %d, %v", positions, err)
	}
	var finished, marks int
	_ = st.DB.QueryRow(`SELECT count(*) FROM reading_progress WHERE user_id = ? AND status = 'finished' AND rating = 5`, u.ID).Scan(&finished)
	_ = st.DB.QueryRow(`SELECT count(*) FROM highlights WHERE user_id = ?`, u.ID).Scan(&marks)
	if finished != 1 || marks != 2 {
		t.Errorf("finished books = %d, highlights = %d", finished, marks)
	}
	var listed int
	_ = st.DB.QueryRow(`SELECT count(*) FROM book_list_items i JOIN book_lists l ON l.id = i.list_id WHERE l.name = '四大名著'`).Scan(&listed)
	if listed != 4 {
		t.Errorf("books in the list = %d", listed)
	}
	var convs int
	_ = st.DB.QueryRow(`SELECT count(*) FROM qa_messages m JOIN qa_conversations c ON c.id = m.conversation_id WHERE c.user_id = ?`, u.ID).Scan(&convs)
	if eeBuild && convs != 2 {
		t.Errorf("sample conversation messages = %d", convs)
	}
	var merges int
	_ = st.DB.QueryRow(`SELECT count(*) FROM agent_proposals WHERE kind = 'merge' AND status = 'pending'
		AND CAST(json_extract(payload, '$.merge[0]') AS INTEGER) = (SELECT id FROM correspondents WHERE name = '国网北京')`).Scan(&merges)
	if eeBuild && merges != 1 {
		t.Errorf("merge suggestions = %d", merges)
	}
	var notes int
	_ = st.DB.QueryRow(`SELECT count(*) FROM yearbook_notes`).Scan(&notes)
	if eeBuild && notes != 1 {
		t.Errorf("yearbook paragraphs = %d", notes)
	}
	var summaries, cards, catalog int
	_ = st.DB.QueryRow(`SELECT (SELECT count(*) FROM book_summaries), (SELECT count(*) FROM review_cards WHERE user_id = ?),
		(SELECT count(*) FROM agent_proposals WHERE kind = 'book_metadata' AND status = 'pending')`, u.ID).Scan(&summaries, &cards, &catalog)
	var collections int
	_ = st.DB.QueryRow(`SELECT count(*) FROM collection_shares`).Scan(&collections)
	if eeBuild && collections < 1 { // the test loads a few documents; a full load also shares the health check reports
		t.Errorf("shared collections = %d", collections)
	}
	if eeBuild && (summaries != 1 || cards != 2 || catalog != 1) {
		t.Errorf("library: summaries %d, cards %d, catalog suggestions %d", summaries, cards, catalog)
	}
	var fen int64
	if err := st.DB.QueryRow(`SELECT v.value_monetary FROM custom_field_values v JOIN custom_fields f ON f.id = v.field_id
		WHERE f.key = 'amount' AND v.document_id = (SELECT id FROM documents WHERE title = '2023年1月电费账单')`).Scan(&fen); err != nil || fen != 21240 {
		t.Errorf("amount = %d, %v; want 21240 fen", fen, err)
	}
}

func TestExtrasAreRepeatable(t *testing.T) {
	if len(parser.MissingTools()) > 0 {
		t.Skip("needs the PDF tools")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	fs := files.NewStore(t.TempDir())
	ing := ingest.NewService(st.DB, jobs.NewQueue(st.DB), fs, events.NewBus(), ingest.Options{Parser: parser.Config{OCRLanguages: "eng", OCRMode: "skip"}, MaxUploadSize: 10 << 20})
	d := Deps{DB: st.DB, Ingest: ing}
	if _, err := st.DB.Exec(`INSERT INTO users (id, username, password_hash, password_changed_at, created_at, updated_at) VALUES (1, 'wang', 'x', 1, 1, 1);
		INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, '王家', 'family', 1)`); err != nil {
		t.Fatal(err)
	}
	scope := tenant.New(1, tenant.RoleOwner)
	if x, err := loadExtras(ctx, d, 1, scope); err != nil || x != (extrasReport{}) {
		t.Fatalf("without the lease: %+v %v", x, err)
	}
	pdf, err := samplePDF(docData{title: leaseTitle, docType: "合同", correspondent: "链家", date: "2023-09-01"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := ing.Import(ctx, scope, leaseTitle+".pdf", strings.NewReader(string(pdf)), ingest.Overrides{}, ingest.Upload{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`UPDATE documents SET title = ? WHERE id = ?`, leaseTitle, id); err != nil {
		t.Fatal(err)
	}
	x, err := loadExtras(ctx, d, 1, scope)
	if err != nil || len(x.recoveryCode) != 29 {
		t.Fatalf("first: %+v %v", x, err)
	}
	if x.recoveryCode = ""; x != (extrasReport{shares: 3, versions: 1, duplicates: 1}) { // one ordinary link, one watermarked and view-only (v1.9.2), a watermarked photo (v1.11.2)
		t.Fatalf("first: %+v", x)
	}
	if x, err := loadExtras(ctx, d, 1, scope); err != nil || x != (extrasReport{duplicates: 1}) {
		t.Fatalf("again: %+v %v", x, err)
	}
	// no work receipts here, so no bundle; with one, a bundle, once
	var bundles int
	_ = st.DB.QueryRow(`SELECT count(*) FROM share_bundles`).Scan(&bundles)
	if bundles != 0 {
		t.Errorf("bundles without receipts = %d", bundles)
	}
	if _, err := ing.Import(ctx, scope, "增值税普通发票-打印机.txt", strings.NewReader("打印机发票"), ingest.Overrides{Title: "增值税普通发票-打印机"}, ingest.Upload{}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := loadExtras(ctx, d, 1, scope); err != nil {
			t.Fatal(err)
		}
	}
	var docs int
	_ = st.DB.QueryRow(`SELECT count(*), (SELECT count(*) FROM share_bundle_documents) FROM share_bundles`).Scan(&bundles, &docs)
	if bundles != 1 || docs != 1 {
		t.Errorf("bundles = %d with %d documents", bundles, docs)
	}
	// v1.8.0: the account is 爸爸 and looks after 乐乐, once
	var access string
	_ = st.DB.QueryRow(`SELECT group_concat(p.name || ':' || a.level, ',') FROM (SELECT * FROM person_access ORDER BY level DESC) a
		JOIN persons p ON p.id = a.person_id`).Scan(&access)
	if access != "爸爸:self,乐乐:guardian" {
		t.Errorf("person access = %q", access)
	}
	// v1.8.1: and has a key, made once
	var keys int
	_ = st.DB.QueryRow(`SELECT count(*) FROM person_keys`).Scan(&keys)
	if keys != 1 {
		t.Errorf("keys = %d", keys)
	}
}
