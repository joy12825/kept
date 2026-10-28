// Package paperless imports a Paperless-ngx export (document_exporter)
// into a workspace: correspondents, document types, tags (with parents),
// custom fields, documents with their files, and notes (v0.2).
//
// Files are copied as they are: the original stays the original and
// Paperless's archive version becomes the archive, so nothing is OCRed
// again. Documents already in the workspace (same SHA-256) are skipped.
package paperless

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"kept/server/internal/audit"
	"kept/server/internal/document"
	"kept/server/internal/files"
	"kept/server/internal/filter"
	"kept/server/internal/i18n"
	"kept/server/internal/parser"
	"kept/server/internal/pinyin"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/taxonomy"
	"kept/server/internal/tenant"
)

// Source marks imported documents (documents.source).
const Source = "import"

// record is one object of the manifest.
type record struct {
	Model     string          `json:"model"`
	PK        int64           `json:"pk"`
	Fields    json.RawMessage `json:"fields"`
	File      string          `json:"__exported_file_name__"`
	Archive   string          `json:"__exported_archive_name__"`
	Thumbnail string          `json:"__exported_thumbnail_name__"`
}

type matchFields struct {
	Name          string `json:"name"`
	Match         string `json:"match"`
	Algorithm     int    `json:"matching_algorithm"`
	IsInsensitive bool   `json:"is_insensitive"`
}

type tagFields struct {
	matchFields
	Color    string `json:"color"`
	IsInbox  bool   `json:"is_inbox_tag"`
	Parent   *int64 `json:"parent"`
	TNParent *int64 `json:"tn_parent"`
}

type fieldFields struct {
	Name      string `json:"name"`
	DataType  string `json:"data_type"`
	ExtraData struct {
		SelectOptions   []json.RawMessage `json:"select_options"`
		DefaultCurrency string            `json:"default_currency"`
	} `json:"extra_data"`
}

type docFields struct {
	Title            string  `json:"title"`
	Content          string  `json:"content"`
	Created          string  `json:"created"`
	Added            string  `json:"added"`
	Modified         string  `json:"modified"`
	Correspondent    *int64  `json:"correspondent"`
	DocumentType     *int64  `json:"document_type"`
	Tags             []int64 `json:"tags"`
	MimeType         string  `json:"mime_type"`
	ASN              *int64  `json:"archive_serial_number"`
	OriginalFilename string  `json:"original_filename"`
}

type noteFields struct {
	Note     string `json:"note"`
	Created  string `json:"created"`
	Document int64  `json:"document"`
}

type instanceFields struct {
	Document int64           `json:"document"`
	Field    int64           `json:"field"`
	Text     *string         `json:"value_text"`
	Bool     *bool           `json:"value_bool"`
	URL      *string         `json:"value_url"`
	Date     *string         `json:"value_date"`
	Int      *int64          `json:"value_int"`
	Float    *float64        `json:"value_float"`
	Monetary *string         `json:"value_monetary"`
	Select   json.RawMessage `json:"value_select"`
	DocIDs   []int64         `json:"value_document_ids"`
}

// Report says what an import did.
type Report struct {
	Correspondents, Types, Tags, Fields int // created (existing ones with the same name are reused)
	Documents                           int // imported
	Skipped                             int // already in the workspace
	Notes                               int
	Missing                             []string // documents whose file is not in the export
	Warnings                            []string
}

// Deps are the services the import uses.
type Deps struct {
	DB       *sql.DB
	Taxonomy *taxonomy.Service
	Files    *files.Store
	Placer   document.Placer // nil: no drawers
	Now      func() time.Time
}

// Load reads the manifest of an export directory: manifest.json and, for
// exports made with --split-manifest, the *-manifest.json files.
func Load(dir string) ([]record, error) {
	var all []record
	main := filepath.Join(dir, "manifest.json")
	if err := readManifest(main, &all); err != nil {
		return nil, fmt.Errorf("读取 %s：%w", main, err)
	}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || p == main || !strings.HasSuffix(d.Name(), "-manifest.json") {
			return err
		}
		return readManifest(p, &all)
	})
	return all, err
}

func readManifest(p string, all *[]record) error {
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	var rs []record
	if err := json.Unmarshal(b, &rs); err != nil {
		return err
	}
	*all = append(*all, rs...)
	return nil
}

var algorithms = map[int]string{0: "none", 1: "any", 2: "all", 3: "literal", 4: "regex", 5: "fuzzy", 6: "auto"}

func rule(m matchFields) *taxonomy.MatchRule {
	a := algorithms[m.Algorithm]
	if a == "" {
		a = "auto"
	}
	// Paperless keeps a keyword algorithm with no keywords; it never matches.
	if strings.TrimSpace(m.Match) == "" && a != "auto" {
		a = "none"
	}
	return &taxonomy.MatchRule{Algorithm: a, Match: m.Match, CaseInsensitive: m.IsInsensitive}
}

// importer carries the state of one import.
type importer struct {
	d      Deps
	scope  tenant.Scope
	dir    string
	userID int64
	rep    Report
	q      *sqlc.Queries

	corr, types, tags, fields map[int64]int64 // Paperless pk -> our ID
	inboxTags                 map[int64]bool
	fieldInfo                 map[int64]importedField
	docs                      map[int64]int64
}

type importedField struct {
	id       int64
	dataType string
	options  map[string]string // Paperless option id or index -> our option id
}

// Import loads the export in dir into the workspace. userID writes the
// notes (the workspace's administrator running the import).
func Import(ctx context.Context, d Deps, scope tenant.Scope, userID int64, dir string) (Report, error) {
	if d.Now == nil {
		d.Now = time.Now
	}
	recs, err := Load(dir)
	if err != nil {
		return Report{}, err
	}
	im := &importer{d: d, scope: scope, dir: dir, userID: userID, q: sqlc.New(d.DB),
		corr: map[int64]int64{}, types: map[int64]int64{}, tags: map[int64]int64{}, fields: map[int64]int64{},
		inboxTags: map[int64]bool{}, fieldInfo: map[int64]importedField{}, docs: map[int64]int64{}}
	by := map[string][]record{}
	for _, r := range recs {
		by[r.Model] = append(by[r.Model], r)
	}
	for _, m := range []string{"documents.correspondent", "documents.documenttype", "documents.tag", "documents.customfield", "documents.document", "documents.note", "documents.customfieldinstance"} {
		sort.Slice(by[m], func(i, j int) bool { return by[m][i].PK < by[m][j].PK })
	}
	if err := im.simple(ctx, taxonomy.Correspondent, by["documents.correspondent"], im.corr, &im.rep.Correspondents); err != nil {
		return im.rep, err
	}
	if err := im.simple(ctx, taxonomy.DocumentType, by["documents.documenttype"], im.types, &im.rep.Types); err != nil {
		return im.rep, err
	}
	if err := im.importTags(ctx, by["documents.tag"]); err != nil {
		return im.rep, err
	}
	if err := im.importFields(ctx, by["documents.customfield"]); err != nil {
		return im.rep, err
	}
	values := map[int64][]instanceFields{}
	for _, r := range by["documents.customfieldinstance"] {
		var v instanceFields
		if json.Unmarshal(r.Fields, &v) == nil {
			values[v.Document] = append(values[v.Document], v)
		}
	}
	for _, r := range by["documents.document"] {
		if err := ctx.Err(); err != nil {
			return im.rep, err
		}
		if err := im.importDocument(ctx, r, values[r.PK]); err != nil {
			return im.rep, fmt.Errorf("文档 %d：%w", r.PK, err)
		}
	}
	if err := im.links(ctx, values); err != nil {
		return im.rep, err
	}
	if err := im.importNotes(ctx, by["documents.note"]); err != nil {
		return im.rep, err
	}
	err = audit.Record(ctx, im.q, audit.Event{
		WorkspaceID: scope.WorkspaceID(), Action: "import.paperless", TargetType: "workspace", TargetID: strconv.FormatInt(scope.WorkspaceID(), 10),
		Summary: fmt.Sprintf(i18n.N("从 Paperless 导入了 %d 份文档（跳过已有的 %d 份）"), im.rep.Documents, im.rep.Skipped), At: d.Now(),
	})
	return im.rep, err
}

// simple imports correspondents or document types, reusing same names.
func (im *importer) simple(ctx context.Context, kind taxonomy.Kind, rs []record, ids map[int64]int64, created *int) error {
	existing, err := im.d.Taxonomy.List(ctx, im.scope, kind)
	if err != nil {
		return err
	}
	byName := map[string]int64{}
	for _, it := range existing {
		byName[strings.ToLower(it.Name)] = it.ID
	}
	for _, r := range rs {
		var f matchFields
		if err := json.Unmarshal(r.Fields, &f); err != nil || strings.TrimSpace(f.Name) == "" {
			continue
		}
		if id, ok := byName[strings.ToLower(strings.TrimSpace(f.Name))]; ok {
			ids[r.PK] = id
			continue
		}
		it, err := im.d.Taxonomy.Create(ctx, im.scope, kind, taxonomy.Input{Name: f.Name, Rule: rule(f)})
		if err != nil {
			im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("“%s”没有导入：%v", f.Name, err))
			continue
		}
		ids[r.PK], byName[strings.ToLower(it.Name)] = it.ID, it.ID
		*created++
	}
	return nil
}

// importTags creates parents before children; inbox tags become the inbox.
func (im *importer) importTags(ctx context.Context, rs []record) error {
	existing, err := im.d.Taxonomy.ListTags(ctx, im.scope)
	if err != nil {
		return err
	}
	byName := map[string]int64{}
	for _, t := range existing {
		byName[strings.ToLower(t.Name)] = t.ID
	}
	pending := map[int64]tagFields{}
	for _, r := range rs {
		var f tagFields
		if err := json.Unmarshal(r.Fields, &f); err != nil || strings.TrimSpace(f.Name) == "" {
			continue
		}
		if f.IsInbox {
			im.inboxTags[r.PK] = true
			continue
		}
		pending[r.PK] = f
	}
	for len(pending) > 0 {
		progress := false
		pks := make([]int64, 0, len(pending))
		for pk := range pending {
			pks = append(pks, pk)
		}
		sort.Slice(pks, func(i, j int) bool { return pks[i] < pks[j] })
		for _, pk := range pks {
			f := pending[pk]
			parent := f.Parent
			if parent == nil {
				parent = f.TNParent
			}
			var parentID int64
			if parent != nil {
				if _, waiting := pending[*parent]; waiting {
					continue // its parent first
				}
				parentID = im.tags[*parent]
			}
			delete(pending, pk)
			progress = true
			if id, ok := byName[strings.ToLower(strings.TrimSpace(f.Name))]; ok {
				im.tags[pk] = id
				continue
			}
			t, err := im.d.Taxonomy.CreateTag(ctx, im.scope, taxonomy.TagInput{Name: f.Name, Color: nearestColor(f.Color), ParentID: parentID, Rule: rule(f.matchFields)})
			if err != nil {
				im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("标签“%s”没有导入：%v", f.Name, err))
				continue
			}
			im.tags[pk], byName[strings.ToLower(t.Name)] = t.ID, t.ID
			im.rep.Tags++
		}
		if !progress { // a cycle: import the rest at the top level
			for pk, f := range pending {
				f.Parent, f.TNParent = nil, nil
				pending[pk] = f
			}
		}
	}
	return nil
}

var fieldTypes = map[string]string{"string": "text", "longtext": "text", "url": "url", "date": "date", "boolean": "boolean",
	"integer": "integer", "float": "float", "monetary": "monetary", "select": "select", "documentlink": "document_link"}

func (im *importer) importFields(ctx context.Context, rs []record) error {
	existing, err := im.d.Taxonomy.ListFields(ctx, im.scope)
	if err != nil {
		return err
	}
	byName := map[string]taxonomy.Field{}
	for _, f := range existing {
		byName[strings.ToLower(f.Name)] = f
	}
	for _, r := range rs {
		var f fieldFields
		if err := json.Unmarshal(r.Fields, &f); err != nil || strings.TrimSpace(f.Name) == "" {
			continue
		}
		dt, ok := fieldTypes[f.DataType]
		if !ok {
			im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("字段“%s”的类型 %s 不支持，没有导入", f.Name, f.DataType))
			continue
		}
		info := importedField{dataType: dt, options: map[string]string{}}
		var labels []string
		var keys []string
		for i, raw := range f.ExtraData.SelectOptions {
			var s string
			var o struct{ ID, Label string }
			switch {
			case json.Unmarshal(raw, &s) == nil:
				labels, keys = append(labels, s), append(keys, strconv.Itoa(i))
			case json.Unmarshal(raw, &o) == nil:
				labels, keys = append(labels, o.Label), append(keys, o.ID)
			}
		}
		if have, ok := byName[strings.ToLower(strings.TrimSpace(f.Name))]; ok {
			if have.DataType != dt {
				im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("字段“%s”已存在但类型不同，它的值没有导入", f.Name))
				continue
			}
			info.id = have.ID
			for i, label := range labels {
				for _, o := range have.Extra.Options {
					if strings.EqualFold(o.Label, label) {
						info.options[keys[i]] = o.ID
					}
				}
			}
			im.fieldInfo[r.PK] = info
			continue
		}
		extra := taxonomy.FieldExtra{}
		if dt == "monetary" {
			extra.Currency = strings.ToUpper(f.ExtraData.DefaultCurrency)
		}
		for _, label := range labels {
			extra.Options = append(extra.Options, taxonomy.SelectOption{Label: label})
		}
		nf, err := im.d.Taxonomy.CreateField(ctx, im.scope, taxonomy.FieldInput{Name: f.Name, DataType: dt, Extra: extra})
		if err != nil {
			im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("字段“%s”没有导入：%v", f.Name, err))
			continue
		}
		info.id = nf.ID
		for i := range labels {
			if i < len(nf.Extra.Options) {
				info.options[keys[i]] = nf.Extra.Options[i].ID
			}
		}
		im.fieldInfo[r.PK] = info
		im.rep.Fields++
	}
	return nil
}

// localDate turns a Paperless date or date-time into a local YYYY-MM-DD.
func localDate(s string) string {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.In(time.Local).Format("2006-01-02")
	}
	if len(s) >= 10 {
		if _, err := time.Parse("2006-01-02", s[:10]); err == nil {
			return s[:10]
		}
	}
	return ""
}

func millis(s string, fallback time.Time) int64 {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UnixMilli()
	}
	return fallback.UnixMilli()
}

func (im *importer) importDocument(ctx context.Context, r record, values []instanceFields) error {
	var f docFields
	if err := json.Unmarshal(r.Fields, &f); err != nil {
		return err
	}
	orig := filepath.Join(im.dir, filepath.FromSlash(r.File))
	if r.File == "" || !exists(orig) {
		im.rep.Missing = append(im.rep.Missing, f.Title)
		return nil
	}
	ws := im.scope.WorkspaceID()
	sum, err := sha(orig)
	if err != nil {
		return err
	}
	if _, err := im.q.FindDocumentByChecksum(ctx, sqlc.FindDocumentByChecksumParams{WorkspaceID: ws, Checksum: sum}); err == nil {
		im.rep.Skipped++
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	name := f.OriginalFilename
	if name == "" {
		name = filepath.Base(orig)
	}
	mime, ext, err := parser.Detect(orig, name)
	if err != nil {
		mime, ext = f.MimeType, strings.TrimPrefix(files.SafeExt(name), ".")
	}
	archive := ""
	if r.Archive != "" && exists(filepath.Join(im.dir, filepath.FromSlash(r.Archive))) {
		archive = filepath.Join(im.dir, filepath.FromSlash(r.Archive))
	}
	view := archive
	if view == "" && mime == "application/pdf" {
		view = orig
	}
	pages := 1
	if view != "" {
		if n, err := parser.PDFPages(ctx, view); err == nil {
			pages = n
		}
	}
	var archiveSum sql.NullString
	if archive != "" {
		if s, err := sha(archive); err == nil {
			archiveSum = sql.NullString{String: s, Valid: true}
		}
	}
	title := strings.TrimSpace(f.Title)
	if title == "" {
		title = strings.TrimSuffix(name, filepath.Ext(name))
	}
	now := im.d.Now()
	date := localDate(f.Created)
	if date == "" {
		date = now.Format("2006-01-02")
	}
	inbox := int64(0)
	var tagIDs []int64
	for _, t := range f.Tags {
		if im.inboxTags[t] {
			inbox = 1
		} else if id, ok := im.tags[t]; ok {
			tagIDs = append(tagIDs, id)
		}
	}
	tmp, err := im.d.Files.NewTmpDir("paperless-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	thumb := ""
	if view != "" {
		if p, err := parser.Thumbnail(ctx, view, tmp); err == nil {
			thumb = p
		}
	}

	// The workspace's storage; files bound for another machine are
	// uploaded before the transaction (v1.7.1).
	storage, err := im.q.GetWorkspaceStorage(ctx, ws)
	if err != nil {
		return err
	}
	var staged []*files.Staged
	for _, p := range []string{orig, archive} {
		if p == "" {
			staged = append(staged, nil)
			continue
		}
		st, err := im.d.Files.Stage(ctx, storage, ws, p, false)
		if err != nil {
			return err
		}
		defer st.Discard(ctx, im.d.Files)
		staged = append(staged, st)
	}
	tx, err := im.d.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := im.q.WithTx(tx)
	id, err := q.InsertDocument(ctx, sqlc.InsertDocumentParams{
		WorkspaceID: ws, Title: title, TitleSort: pinyin.TitleKey(title), Content: f.Content, CreatedDate: date,
		AddedAt: millis(f.Added, now), ModifiedAt: millis(f.Modified, now),
		CorrespondentID: nullID(lookup(im.corr, f.Correspondent)), DocumentTypeID: nullID(lookup(im.types, f.DocumentType)),
		MimeType: mime, OriginalFilename: name, OriginalExt: ext, PageCount: int64(pages), Checksum: sum,
		ArchiveChecksum: archiveSum, HasArchive: boolInt(archive != ""), HasThumbnail: boolInt(thumb != ""), Inbox: inbox,
		FieldSources: "{}", Source: Source, Storage: storage,
	})
	if err != nil {
		return err
	}
	for _, t := range tagIDs {
		if err := q.AddDocumentTag(ctx, sqlc.AddDocumentTagParams{DocumentID: id, TagID: t}); err != nil {
			return err
		}
	}
	if f.ASN != nil && *f.ASN >= 1 && *f.ASN <= 999999 {
		if n, err := q.SetDocumentASNIfFree(ctx, sqlc.SetDocumentASNIfFreeParams{Asn: sql.NullInt64{Int64: *f.ASN, Valid: true}, WorkspaceID: ws, ID: id}); err != nil {
			return err
		} else if n == 0 {
			im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("“%s”的原件编号 #%06d 已被使用，没有填入", title, *f.ASN))
		}
	}
	for _, v := range values {
		if err := im.setValue(ctx, q, id, v); err != nil {
			return err
		}
	}
	if im.d.Placer != nil {
		if err := im.d.Placer.PlaceDocument(ctx, tx, ws, id); err != nil {
			return err
		}
	}
	// Files last, then commit; a failure removes what was copied.
	var stored []files.Ref
	var copied []string
	undo := func() {
		bctx := context.WithoutCancel(ctx)
		for _, r := range stored {
			_ = im.d.Files.Remove(bctx, storage, r)
		}
		for _, p := range copied {
			os.Remove(p)
		}
	}
	for i, to := range []files.Ref{files.Original(ws, id, ext), files.Archive(ws, id)} {
		if staged[i] == nil {
			continue
		}
		if err := staged[i].Place(ctx, im.d.Files, to); err != nil {
			undo()
			return err
		}
		stored = append(stored, to)
	}
	if thumb != "" {
		to := im.d.Files.ThumbnailPath(ws, id)
		if err := files.Copy(thumb, to); err != nil {
			undo()
			return err
		}
		copied = append(copied, to)
	}
	if err := tx.Commit(); err != nil {
		undo()
		return err
	}
	im.docs[r.PK] = id
	im.rep.Documents++
	return nil
}

// setValue stores a custom field value (document links come later).
func (im *importer) setValue(ctx context.Context, q *sqlc.Queries, doc int64, v instanceFields) error {
	info, ok := im.fieldInfo[v.Field]
	if !ok {
		return nil
	}
	p := sqlc.UpsertFieldValueParams{DocumentID: doc, FieldID: info.id}
	str := func(s *string) sql.NullString {
		if s == nil {
			return sql.NullString{}
		}
		return sql.NullString{String: *s, Valid: true}
	}
	switch info.dataType {
	case "text", "url":
		p.ValueText = str(v.Text)
		if info.dataType == "url" {
			p.ValueText = str(v.URL)
		}
	case "date":
		if v.Date != nil {
			p.ValueDate = sql.NullString{String: localDate(*v.Date), Valid: localDate(*v.Date) != ""}
		}
	case "boolean":
		if v.Bool != nil {
			p.ValueBool = sql.NullInt64{Int64: boolInt(*v.Bool), Valid: true}
		}
	case "integer":
		if v.Int != nil {
			p.ValueInt = sql.NullInt64{Int64: *v.Int, Valid: true}
		}
	case "float":
		if v.Float != nil {
			p.ValueFloat = sql.NullFloat64{Float64: *v.Float, Valid: true}
		}
	case "monetary":
		if v.Monetary != nil {
			amount := strings.TrimLeft(*v.Monetary, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
			if cents, err := filter.ParseMoney(amount); err == nil {
				p.ValueMonetary = sql.NullInt64{Int64: cents, Valid: true}
			}
		}
	case "select":
		var key string
		var n int
		switch {
		case json.Unmarshal(v.Select, &key) == nil:
		case json.Unmarshal(v.Select, &n) == nil:
			key = strconv.Itoa(n)
		}
		if opt, ok := info.options[key]; ok {
			p.ValueSelect = sql.NullString{String: opt, Valid: true}
		}
	default:
		return nil // document links: after every document is in
	}
	if !p.ValueText.Valid && !p.ValueDate.Valid && !p.ValueBool.Valid && !p.ValueInt.Valid && !p.ValueFloat.Valid && !p.ValueMonetary.Valid && !p.ValueSelect.Valid {
		return nil
	}
	return q.UpsertFieldValue(ctx, p)
}

// links stores document-link values once the linked documents exist.
func (im *importer) links(ctx context.Context, values map[int64][]instanceFields) error {
	for pk, vs := range values {
		doc, ok := im.docs[pk]
		if !ok {
			continue
		}
		for _, v := range vs {
			info, ok := im.fieldInfo[v.Field]
			if !ok || info.dataType != "document_link" || len(v.DocIDs) == 0 {
				continue
			}
			target, ok := im.docs[v.DocIDs[0]]
			if !ok {
				continue
			}
			if err := im.q.UpsertFieldValue(ctx, sqlc.UpsertFieldValueParams{DocumentID: doc, FieldID: info.id, ValueDoclink: sql.NullInt64{Int64: target, Valid: true}}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (im *importer) importNotes(ctx context.Context, rs []record) error {
	for _, r := range rs {
		var n noteFields
		if err := json.Unmarshal(r.Fields, &n); err != nil || strings.TrimSpace(n.Note) == "" {
			continue
		}
		doc, ok := im.docs[n.Document]
		if !ok {
			continue
		}
		if _, err := im.q.InsertNote(ctx, sqlc.InsertNoteParams{
			WorkspaceID: im.scope.WorkspaceID(), DocumentID: doc, UserID: sql.NullInt64{Int64: im.userID, Valid: im.userID != 0},
			Body: n.Note, CreatedAt: millis(n.Created, im.d.Now()),
		}); err != nil {
			return err
		}
		im.rep.Notes++
	}
	return nil
}

// nearestColor maps a Paperless colour (any #rrggbb) to the closest preset
// tag colour; "" lets the taxonomy pick one.
func nearestColor(hex string) string {
	rgb := func(h string) (int, int, int, bool) {
		if len(h) != 7 || h[0] != '#' {
			return 0, 0, 0, false
		}
		v, err := strconv.ParseUint(h[1:], 16, 32)
		if err != nil {
			return 0, 0, 0, false
		}
		return int(v >> 16 & 0xff), int(v >> 8 & 0xff), int(v & 0xff), true
	}
	r, g, b, ok := rgb(hex)
	if !ok {
		return ""
	}
	best, bestD := "", -1
	for _, c := range taxonomy.TagColorList {
		cr, cg, cb, _ := rgb(c)
		d := (r-cr)*(r-cr) + (g-cg)*(g-cg) + (b-cb)*(b-cb)
		if bestD < 0 || d < bestD {
			best, bestD = c, d
		}
	}
	return best
}

func lookup(m map[int64]int64, pk *int64) int64 {
	if pk == nil {
		return 0
	}
	return m[*pk]
}

func nullID(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id != 0} }

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func exists(p string) bool { st, err := os.Stat(p); return err == nil && !st.IsDir() }

func sha(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
