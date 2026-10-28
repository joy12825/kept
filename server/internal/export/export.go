// Package export writes a workspace out as ordinary files (v1.4.1), to be
// read without kept: one folder per drawer and year, each document named
// by its date and title, and a list of everything (清单.csv for a
// spreadsheet, 清单.json for programs). The searchable PDF is taken when
// there is one, else the original; or always the original.
//
// This is not a backup: restoring is `kept backup` / `kept restore`.
package export

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
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
	"kept/server/internal/jobs"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/taxonomy"
	"kept/server/internal/tenant"
)

// Options say which file of each document to write.
type Options struct {
	// Original writes the uploaded file even when a searchable PDF exists.
	Original bool
	// SkipSensitive leaves sensitive documents out (v1.7.9: a readable
	// copy kept in a cloud storage).
	SkipSensitive bool
}

// Deps are the services an export reads from.
type Deps struct {
	DB        *sql.DB
	Documents *document.Service
	Taxonomy  *taxonomy.Service
	Files     *files.Store
	Now       func() time.Time
}

// Sink receives the exported files, in order.
type Sink interface {
	Add(name string, modified time.Time, r io.Reader, compress bool) error
}

// Report says what was written.
type Report struct {
	Documents int
	Bytes     int64
	Missing   []string // documents whose file was not found (kept in the list)
	// Unchanged and Removed: an update (SyncSink, v1.6.5) left this many
	// files as they were, and removed this many no longer there.
	Unchanged, Removed int
}

// unchanged is a sink that can tell a file is there already (SyncSink).
type unchanged interface {
	Unchanged(name string, modified time.Time, size int64) bool
}

// finisher is a sink with work left after the last file (SyncSink).
type finisher interface {
	Finish() (removed int, err error)
}

const unfiled = "未入抽屉"

var sources = map[string]string{"web": "网页上传", "api": "API", "mobile": "手机", "watch": "监听目录", "mail": "邮件", "import": "从 Paperless 导入", "link": "上传链接", "remote": "远程收件夹"}

// entry is one document in the lists.
type entry struct {
	File             string            `json:"file"`
	ID               int64             `json:"id"`
	Title            string            `json:"title"`
	Date             string            `json:"date"`
	Drawer           string            `json:"drawer,omitempty"`
	Type             string            `json:"type,omitempty"`
	Correspondent    string            `json:"correspondent,omitempty"`
	Person           string            `json:"person,omitempty"`   // v1.6.3
	Location         string            `json:"location,omitempty"` // where the original is kept
	Tags             []string          `json:"tags"`
	ASN              string            `json:"asn,omitempty"`
	AddedAt          string            `json:"addedAt"`
	Source           string            `json:"source"`
	OriginalFilename string            `json:"originalFilename"`
	Fields           map[string]string `json:"fields"`
	Notes            []note            `json:"notes"`
}

type note struct {
	By   string `json:"by,omitempty"`
	At   string `json:"at"`
	Text string `json:"text"`
}

// Export writes workspace ws to sink.
func Export(ctx context.Context, d Deps, ws int64, opt Options, sink Sink) (Report, error) {
	if d.Now == nil {
		d.Now = time.Now
	}
	scope := tenant.New(ws, tenant.RoleOwner).Outside() // family members' private documents stay out (v1.8.0)
	names, err := loadNames(ctx, d, scope)
	if err != nil {
		return Report{}, err
	}
	ids, err := documentIDs(ctx, d.DB, ws, opt.SkipSensitive)
	if err != nil {
		return Report{}, err
	}
	notes, err := loadNotes(ctx, d.DB, ws)
	if err != nil {
		return Report{}, err
	}

	var rep Report
	var list []entry
	used := map[string]map[string]int{} // folder → names taken
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		doc, err := d.Documents.GetDetail(ctx, scope, id)
		if err != nil {
			continue // deleted meanwhile
		}
		src, ext := files.Original(ws, id, doc.OriginalExt), doc.OriginalExt
		if !opt.Original && doc.HasArchive {
			src, ext = files.Archive(ws, id), "pdf"
		}
		drawer := unfiled
		if n, ok := names.drawers[doc.DrawerID]; ok {
			drawer = n
		}
		folder := path.Join(safe(drawer), year(doc.CreatedDate))
		if used[folder] == nil {
			used[folder] = map[string]int{}
		}
		file := path.Join(folder, files.FileName(doc.CreatedDate+" "+doc.Title, ext, used[folder]))

		e := entry{File: file, ID: id, Title: doc.Title, Date: doc.CreatedDate, Type: names.types[doc.DocumentTypeID], Correspondent: names.corrs[doc.CorrespondentID],
			Person: names.persons[doc.PersonID], Location: names.locations[doc.LocationID],
			Tags: []string{}, AddedAt: doc.AddedAt.Format("2006-01-02 15:04"), Source: sources[doc.Source], OriginalFilename: doc.OriginalFilename,
			Fields: map[string]string{}, Notes: notes[id]}
		if doc.DrawerID != 0 {
			e.Drawer = drawer
		}
		if e.Source == "" {
			e.Source = sources["web"]
		}
		if e.Notes == nil {
			e.Notes = []note{}
		}
		if doc.ASN != nil {
			e.ASN = fmt.Sprintf("#%06d", *doc.ASN)
		}
		for _, t := range doc.TagIDs {
			if n, ok := names.tags[t]; ok {
				e.Tags = append(e.Tags, n)
			}
		}
		for _, f := range doc.Fields {
			if fd, ok := names.fields[f.FieldID]; ok && f.Value != nil {
				e.Fields[fd.Name] = fieldText(fd, f.Value)
			}
		}

		if u, ok := sink.(unchanged); ok {
			if st, err := d.Files.Stat(ctx, doc.Storage, src); err == nil && u.Unchanged(file, doc.ModifiedAt, st.Size) {
				rep.Unchanged++
				list = append(list, e)
				continue
			}
		}
		f, err := d.Files.Open(ctx, doc.Storage, src)
		if err != nil {
			rep.Missing = append(rep.Missing, file)
		} else {
			cr := &countReader{r: f}
			err = sink.Add(file, doc.ModifiedAt, cr, ext == "txt" || ext == "md")
			f.Close()
			if err != nil {
				return rep, err
			}
			rep.Bytes += cr.n
			rep.Documents++
		}
		list = append(list, e)
	}

	now := d.Now()
	csvData, err := listCSV(list, names.fieldOrder)
	if err != nil {
		return rep, err
	}
	jsonData, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return rep, err
	}
	for _, f := range []struct {
		name string
		data []byte
	}{
		{"清单.csv", csvData},
		{"清单.json", jsonData},
		{"说明.txt", []byte(readme(names.workspace, now, len(list), opt))},
	} {
		if err := sink.Add(f.name, now, bytes.NewReader(f.data), true); err != nil {
			return rep, err
		}
	}
	if fin, ok := sink.(finisher); ok {
		n, err := fin.Finish()
		rep.Removed = n
		if err != nil {
			return rep, err
		}
	}
	return rep, nil
}

type nameSet struct {
	workspace                   string
	drawers, types, corrs, tags map[int64]string
	persons, locations          map[int64]string
	fields                      map[int64]taxonomy.Field
	fieldOrder                  []string
}

func loadNames(ctx context.Context, d Deps, scope tenant.Scope) (nameSet, error) {
	n := nameSet{drawers: map[int64]string{}, types: map[int64]string{}, corrs: map[int64]string{}, persons: map[int64]string{}, locations: map[int64]string{}, tags: map[int64]string{}, fields: map[int64]taxonomy.Field{}}
	if err := d.DB.QueryRowContext(ctx, `SELECT name FROM workspaces WHERE id = ?`, scope.WorkspaceID()).Scan(&n.workspace); err != nil {
		return n, err
	}
	rows, err := d.DB.QueryContext(ctx, `SELECT id, name FROM drawers WHERE workspace_id = ?`, scope.WorkspaceID())
	if err != nil {
		return n, err
	}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return n, err
		}
		n.drawers[id] = name
	}
	rows.Close()
	for kind, into := range map[taxonomy.Kind]map[int64]string{taxonomy.DocumentType: n.types, taxonomy.Correspondent: n.corrs, taxonomy.Person: n.persons} {
		items, err := d.Taxonomy.List(ctx, scope, kind)
		if err != nil {
			return n, err
		}
		for _, it := range items {
			into[it.ID] = it.Name
		}
	}
	locs, err := d.Taxonomy.ListLocations(ctx, scope)
	if err != nil {
		return n, err
	}
	for _, l := range locs {
		n.locations[l.ID] = l.Name
	}
	tags, err := d.Taxonomy.ListTags(ctx, scope)
	if err != nil {
		return n, err
	}
	for _, t := range tags {
		n.tags[t.ID] = t.Name
	}
	fields, err := d.Taxonomy.ListFields(ctx, scope)
	if err != nil {
		return n, err
	}
	for _, f := range fields { // built-in first, as listed
		n.fields[f.ID] = f
		n.fieldOrder = append(n.fieldOrder, f.Name)
	}
	return n, nil
}

func documentIDs(ctx context.Context, db *sql.DB, ws int64, skipSensitive bool) ([]int64, error) {
	q := `SELECT d.id FROM documents AS d WHERE d.workspace_id = ? AND d.deleted_at IS NULL`
	if skipSensitive {
		q += ` AND NOT ` + filter.SensitiveSQL
	}
	rows, err := db.QueryContext(ctx, q+` ORDER BY d.created_date, d.id`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func loadNotes(ctx context.Context, db *sql.DB, ws int64) (map[int64][]note, error) {
	rows, err := db.QueryContext(ctx, `SELECT n.document_id, coalesce(nullif(u.display_name, ''), u.username, ''), n.created_at, n.body
		FROM notes n LEFT JOIN users u ON u.id = n.user_id WHERE n.workspace_id = ? ORDER BY n.document_id, n.created_at, n.id`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]note{}
	for rows.Next() {
		var doc, at int64
		var n note
		if err := rows.Scan(&doc, &n.By, &at, &n.Text); err != nil {
			return nil, err
		}
		n.At = time.UnixMilli(at).Format("2006-01-02 15:04")
		out[doc] = append(out[doc], n)
	}
	return out, rows.Err()
}

// fieldText shows a custom field value as a person reads it.
func fieldText(f taxonomy.Field, v any) string {
	switch f.DataType {
	case "boolean":
		if b, ok := v.(bool); ok {
			if b {
				return "是"
			}
			return "否"
		}
	case "select":
		for _, o := range f.Extra.Options {
			if o.ID == fmt.Sprint(v) {
				return o.Label
			}
		}
	case "document_link":
		return "文档 #" + fmt.Sprint(v)
	}
	return fmt.Sprint(v)
}

// listCSV is the list for a spreadsheet: UTF-8 with a byte-order mark, so
// Excel shows Chinese.
func listCSV(list []entry, fields []string) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("\xef\xbb\xbf")
	w := csv.NewWriter(&b)
	head := []string{"文件", "标题", "日期", "抽屉", "类型", "往来方", "家人", "标签", "原件编号", "存放位置", "添加时间", "来源", "原文件名"}
	head = append(head, fields...)
	head = append(head, "备注")
	if err := w.Write(head); err != nil {
		return nil, err
	}
	for _, e := range list {
		row := []string{e.File, e.Title, e.Date, e.Drawer, e.Type, e.Correspondent, e.Person, strings.Join(e.Tags, "、"), e.ASN, e.Location, e.AddedAt, e.Source, e.OriginalFilename}
		for _, f := range fields {
			row = append(row, e.Fields[f])
		}
		var ns []string
		for _, n := range e.Notes {
			ns = append(ns, strings.TrimSpace(n.By+" "+n.At)+"："+n.Text)
		}
		row = append(row, strings.Join(ns, "\n"))
		if err := w.Write(row); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return b.Bytes(), w.Error()
}

func readme(ws string, now time.Time, n int, opt Options) string {
	which := "有可搜索版（识别过文字的 PDF）的文档导出可搜索版，其余导出原件"
	if opt.Original {
		which = "每份文档导出上传时的原件"
	}
	return fmt.Sprintf(`空间“%s”的导出，%s，共 %d 份文档。

目录：每个抽屉一个文件夹（没有放入抽屉的在“未入抽屉”），里面按文档日期的年份分开，文件名是“日期 标题”。%s。

清单.csv：每份文档一行，含标题、日期、类型、往来方、标签、原件编号、字段和备注，可以用 Excel、WPS 或 Numbers 打开。
清单.json：同样的内容，供程序读取。

这份导出不需要任何软件就能翻看；它不是备份，不能导回去。完整备份请用 kept backup。
`, ws, now.Format("2006-01-02 15:04"), n, which)
}

func year(date string) string {
	if len(date) >= 4 {
		if _, err := strconv.Atoi(date[:4]); err == nil {
			return date[:4]
		}
	}
	return "无日期"
}

// safe makes a name usable as a folder on Windows, macOS and Linux.
func safe(name string) string {
	s := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 0x20 {
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	s = strings.TrimRight(s, ". ")
	if s == "" {
		return "_"
	}
	return s
}

type countReader struct {
	r io.Reader
	n int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// ZipSink writes into a zip archive (streamed: nothing on disk).
type ZipSink struct{ W *zip.Writer }

func (z ZipSink) Add(name string, modified time.Time, r io.Reader, compress bool) error {
	method := zip.Store // PDFs and images are compressed already
	if compress {
		method = zip.Deflate
	}
	w, err := z.W.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: modified})
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	return err
}

// DirSink writes into a directory, creating folders as needed.
type DirSink struct{ Dir string }

func (d DirSink) Add(name string, modified time.Time, r io.Reader, _ bool) error {
	dst := filepath.Join(d.Dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Chtimes(dst, modified, modified)
}

// Record notes in the activity log that the workspace was exported.
func Record(ctx context.Context, db *sql.DB, ws int64, opt Options, at time.Time) error {
	summary := i18n.N("%s 导出了整个空间（可搜索版）")
	if opt.Original {
		summary = i18n.N("%s 导出了整个空间（原件）")
	}
	return audit.Record(ctx, sqlc.New(db), audit.Event{WorkspaceID: ws, Action: "workspace.export", TargetType: "workspace", TargetID: strconv.FormatInt(ws, 10),
		Summary: fmt.Sprintf(summary, audit.ActorFrom(ctx).Name), At: at})
}

// Service is Export for the web: the same deps, kept for each request.
type Service struct{ d Deps }

func NewService(d Deps) *Service { return &Service{d: d} }

// Run exports ws to sink.
func (s *Service) Run(ctx context.Context, ws int64, opt Options, sink Sink) (Report, error) {
	return Export(ctx, s.d, ws, opt, sink)
}

// Start records the export and returns the workspace's name.
func (s *Service) Start(ctx context.Context, ws int64, opt Options) (string, error) {
	var name string
	if err := s.d.DB.QueryRowContext(ctx, `SELECT name FROM workspaces WHERE id = ?`, ws).Scan(&name); err != nil {
		return "", err
	}
	now := time.Now()
	if s.d.Now != nil {
		now = s.d.Now()
	}
	return name, Record(ctx, s.d.DB, ws, opt, now)
}

// Manifest is the file an updated export keeps in its directory: the
// files it wrote, so the next update removes only those (never what
// someone else put there).
const Manifest = ".kept-export"

// SyncSink keeps a directory up to date (v1.6.5, `kept export --update`
// and KEPT_EXPORT_DIR): a file whose size and modification time match is
// left alone, and Finish removes the files of the last export that are
// gone now (moved, renamed, deleted documents), then empty folders.
type SyncSink struct {
	Dir  string
	seen map[string]bool
}

// NewSyncSink prepares dir, which must be empty, missing, or an earlier
// export (it has the manifest).
func NewSyncSink(dir string) (*SyncSink, error) {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		if _, err := os.Stat(filepath.Join(dir, Manifest)); err != nil {
			return nil, fmt.Errorf("目录 %s 不是空的，也不是收好导出的目录", dir)
		}
	}
	return &SyncSink{Dir: dir, seen: map[string]bool{}}, nil
}

func (s *SyncSink) Unchanged(name string, modified time.Time, size int64) bool {
	st, err := os.Stat(filepath.Join(s.Dir, filepath.FromSlash(name)))
	if err != nil || !st.Mode().IsRegular() || st.Size() != size {
		return false
	}
	// written with the document's modification time; file systems keep it
	// to a second or better
	if d := st.ModTime().Sub(modified); d < -time.Second || d > time.Second {
		return false
	}
	s.seen[name] = true
	return true
}

func (s *SyncSink) Add(name string, modified time.Time, r io.Reader, compress bool) error {
	s.seen[name] = true
	return DirSink{Dir: s.Dir}.Add(name, modified, r, compress)
}

// Finish removes what the last export wrote and this one did not, and
// writes the new manifest.
func (s *SyncSink) Finish() (int, error) {
	removed := 0
	if old, err := os.ReadFile(filepath.Join(s.Dir, Manifest)); err == nil {
		var names []string
		_ = json.Unmarshal(old, &names)
		for _, n := range names {
			if s.seen[n] || strings.Contains(n, "..") {
				continue
			}
			p := filepath.Join(s.Dir, filepath.FromSlash(n))
			if err := os.Remove(p); err == nil {
				removed++
				// folders left empty go too, up to the export's root
				for dir := filepath.Dir(p); dir != filepath.Clean(s.Dir) && strings.HasPrefix(dir, filepath.Clean(s.Dir)); dir = filepath.Dir(dir) {
					if os.Remove(dir) != nil {
						break
					}
				}
			}
		}
	}
	names := make([]string, 0, len(s.seen))
	for n := range s.seen {
		names = append(names, n)
	}
	sort.Strings(names)
	b, _ := json.Marshal(names)
	return removed, os.WriteFile(filepath.Join(s.Dir, Manifest), b, 0o644)
}

// KindScheduled exports one workspace into KEPT_EXPORT_DIR (v1.6.5).
const KindScheduled = "export.scheduled"

// Register handles the daily export of each workspace into dir, a folder
// per workspace named after it, kept up to date like `kept export
// --update`. A renamed workspace starts a new folder; the old one stays.
func (s *Service) Register(p *jobs.Pool, dir string, log *slog.Logger) {
	p.Handle(KindScheduled, func(ctx context.Context, job jobs.Job) (any, error) {
		if job.WorkspaceID == nil || dir == "" {
			return nil, jobs.Permanent(errors.New("export.scheduled needs a workspace and KEPT_EXPORT_DIR"))
		}
		ws := *job.WorkspaceID
		var name string
		err := s.d.DB.QueryRowContext(ctx, `SELECT name FROM workspaces WHERE id = ? AND deleted_at IS NULL`, ws).Scan(&name)
		if errors.Is(err, sql.ErrNoRows) {
			return "workspace gone", nil
		}
		if err != nil {
			return nil, err
		}
		target := filepath.Join(dir, safe(name))
		sink, err := NewSyncSink(target)
		if err != nil {
			return nil, jobs.Permanent(err)
		}
		rep, err := Export(ctx, s.d, ws, Options{}, sink)
		if err != nil {
			return nil, err
		}
		if rep.Documents+rep.Removed > 0 {
			log.Info("exported", "workspace", ws, "dir", target, "written", rep.Documents, "unchanged", rep.Unchanged, "removed", rep.Removed)
		}
		return map[string]any{"dir": target, "written": rep.Documents, "unchanged": rep.Unchanged, "removed": rep.Removed, "missing": len(rep.Missing)}, nil
	})
}
