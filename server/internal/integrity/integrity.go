// Package integrity checks that a workspace's documents, files and search
// index agree (design doc 4.3): files without a record, records without
// their files, and documents missing from (or lingering in) the index. It
// never deletes files; it only reports them. Index gaps are repaired by
// marking the documents for the indexer.
package integrity

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"kept/server/internal/files"
	"kept/server/internal/jobs"
	"kept/server/internal/search"
)

// Kind is the weekly check of one workspace (a scheduler entry).
const Kind = "system.integrity"

// maxListed caps the examples a report lists; the counts are complete.
const maxListed = 50

// Missing is a file a document should have but does not.
type Missing struct {
	DocumentID int64  `json:"documentId"`
	Title      string `json:"title"`
	File       string `json:"file"` // original | archive | thumbnail
}

// Report is the outcome of checking one workspace.
type Report struct {
	WorkspaceID      int64     `json:"workspaceId"`
	Documents        int       `json:"documents"`
	MissingFiles     []Missing `json:"missingFiles"`
	MissingCount     int       `json:"missingCount"`
	OrphanFiles      []string  `json:"orphanFiles"` // relative to the data directory
	OrphanCount      int       `json:"orphanCount"`
	Reindexed        int       `json:"reindexed"`        // documents missing from the index, queued for indexing
	RemovedFromIndex int       `json:"removedFromIndex"` // index entries without a live document
	IndexChecked     bool      `json:"indexChecked"`
	CheckedAt        time.Time `json:"checkedAt"`
}

// OK reports whether nothing needed attention.
func (r Report) OK() bool { return r.MissingCount == 0 && r.OrphanCount == 0 }

type Checker struct {
	db      *sql.DB
	files   *files.Store
	dataDir string
	index   *search.Index // nil: the index is not checked
	now     func() time.Time
}

func NewChecker(db *sql.DB, fs *files.Store, dataDir string, index *search.Index) *Checker {
	return &Checker{db: db, files: fs, dataDir: dataDir, index: index, now: time.Now}
}

type docRow struct {
	id                      int64
	title, ext              string
	archive, thumb, deleted bool
	base                    bool // page edits keep the unedited archive aside
	cover                   bool // a book with a cover (v0.4)
	storage                 string
}

func (c *Checker) Check(ctx context.Context, ws int64) (Report, error) {
	rep := Report{WorkspaceID: ws, MissingFiles: []Missing{}, OrphanFiles: []string{}, CheckedAt: c.now()}
	rows, err := c.db.QueryContext(ctx, `SELECT id, title, original_ext, has_archive, has_thumbnail, deleted_at IS NOT NULL,
		instr(page_edits, '"base":"archive"') > 0, storage FROM documents WHERE workspace_id = ?`, ws)
	if err != nil {
		return rep, err
	}
	docs := map[int64]docRow{}
	for rows.Next() {
		var d docRow
		if err := rows.Scan(&d.id, &d.title, &d.ext, &d.archive, &d.thumb, &d.deleted, &d.base, &d.storage); err != nil {
			rows.Close()
			return rep, err
		}
		docs[d.id] = d
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return rep, err
	}

	crows, err := c.db.QueryContext(ctx, `SELECT document_id FROM books WHERE workspace_id = ? AND cover = 1`, ws)
	if err != nil {
		return rep, err
	}
	for crows.Next() {
		var id int64
		if err := crows.Scan(&id); err != nil {
			crows.Close()
			return rep, err
		}
		if d, ok := docs[id]; ok {
			d.cover = true
			docs[id] = d
		}
	}
	crows.Close()
	if err := crows.Err(); err != nil {
		return rep, err
	}

	ids := make([]int64, 0, len(docs))
	for id, d := range docs {
		ids = append(ids, id)
		if !d.deleted {
			rep.Documents++
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	missing := func(id int64, title, kind string) {
		rep.MissingCount++
		if len(rep.MissingFiles) < maxListed {
			rep.MissingFiles = append(rep.MissingFiles, Missing{DocumentID: id, Title: title, File: kind})
		}
	}
	for _, id := range ids {
		d := docs[id]
		stored := func(kind string, r files.Ref) {
			if !c.files.Exists(ctx, d.storage, r) {
				missing(id, d.title, kind)
			}
		}
		local := func(kind, path string) {
			if !exists(path) {
				missing(id, d.title, kind)
			}
		}
		stored("original", files.Original(ws, id, d.ext))
		if d.archive {
			stored("archive", files.Archive(ws, id))
		}
		if d.thumb {
			local("thumbnail", c.files.ThumbnailPath(ws, id))
		}
		if d.base {
			stored("unedited archive", files.Base(ws, id))
		}
		if d.cover {
			local("cover", c.files.CoverPath(ws, id))
		}
	}

	// Earlier versions (stage 19): each must have its file in versions/ of
	// its document's storage.
	vrows, err := c.db.QueryContext(ctx, `SELECT document_id, id, original_ext, number FROM document_versions WHERE workspace_id = ? AND current = 0`, ws)
	if err != nil {
		return rep, err
	}
	known := map[string]bool{} // storage + "/" + key of every stored file a record names
	type ver struct {
		doc, number int64
		ref         files.Ref
	}
	var vers []ver
	for vrows.Next() {
		var doc, id, number int64
		var ext string
		if err := vrows.Scan(&doc, &id, &ext, &number); err != nil {
			vrows.Close()
			return rep, err
		}
		vers = append(vers, ver{doc, number, files.Version(ws, doc, id, ext)})
	}
	vrows.Close()
	if err := vrows.Err(); err != nil {
		return rep, err
	}
	for _, v := range vers {
		st := docs[v.doc].storage
		known[st+"/"+v.ref.Key()] = true
		if !c.files.Exists(ctx, st, v.ref) {
			missing(v.doc, docs[v.doc].title, "version "+strconv.FormatInt(v.number, 10))
		}
	}
	for id, d := range docs {
		known[d.storage+"/"+files.Original(ws, id, d.ext).Key()] = true
		if d.archive {
			known[d.storage+"/"+files.Archive(ws, id).Key()] = true
		}
		if d.base {
			known[d.storage+"/"+files.Base(ws, id).Key()] = true
		}
	}
	orphan := func(name string) {
		rep.OrphanCount++
		if len(rep.OrphanFiles) < maxListed {
			rep.OrphanFiles = append(rep.OrphanFiles, name)
		}
	}

	// Stored files without a record, in every storage. Files of the
	// built-in storage are named relative to the data directory as before;
	// others as <storage>:<key>.
	wsDir := strconv.FormatInt(ws, 10) + "/"
	for _, st := range c.files.Storages() {
		b, err := c.files.Backend(st)
		if err != nil {
			return rep, err
		}
		for _, kind := range []string{"originals", "archive", "versions"} {
			keys, err := b.List(ctx, kind+"/"+wsDir)
			if err != nil {
				return rep, err
			}
			for _, k := range keys {
				if known[st+"/"+k] {
					continue
				}
				if st == files.Local {
					orphan("media/" + k)
				} else {
					orphan(st + ":" + k)
				}
			}
		}
	}

	// Thumbnails and covers (always in the data directory): <document ID>.jpg.
	for _, kind := range []string{"thumbnails", "covers"} {
		dir := filepath.Join(c.dataDir, "media", kind, strconv.FormatInt(ws, 10))
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return rep, err
		}
		for _, e := range entries {
			name := e.Name()
			id, perr := strconv.ParseInt(strings.TrimSuffix(name, ".jpg"), 10, 64)
			d, ok := docs[id]
			if perr != nil || !ok || e.IsDir() || (kind == "thumbnails" && !d.thumb) || (kind == "covers" && !d.cover) {
				orphan(filepath.ToSlash(filepath.Join("media", kind, strconv.FormatInt(ws, 10), name)))
			}
		}
	}

	if c.index != nil {
		if err := c.checkIndex(ctx, ws, docs, &rep); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

// checkIndex queues live documents the index lacks and drops entries of
// documents that are gone or in the trash.
func (c *Checker) checkIndex(ctx context.Context, ws int64, docs map[int64]docRow, rep *Report) error {
	inIndex, err := c.index.IDs(ws)
	if err != nil {
		return err
	}
	have := map[int64]bool{}
	var extra []int64
	for _, id := range inIndex {
		have[id] = true
		if d, ok := docs[id]; !ok || d.deleted {
			extra = append(extra, id)
		}
	}
	if len(extra) > 0 {
		if err := c.index.Delete(extra); err != nil {
			return err
		}
	}
	rep.RemovedFromIndex = len(extra)
	for id, d := range docs {
		if d.deleted || have[id] {
			continue
		}
		// Documents waiting for the indexer are not gaps.
		res, err := c.db.ExecContext(ctx, `UPDATE documents SET index_rev = index_rev + 1 WHERE id = ? AND indexed_rev >= index_rev`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			rep.Reindexed++
		}
	}
	rep.IndexChecked = true
	return nil
}

// Register adds the weekly check's job handler.
func (c *Checker) Register(p *jobs.Pool) {
	p.Handle(Kind, func(ctx context.Context, job jobs.Job) (any, error) {
		if job.WorkspaceID == nil {
			return nil, jobs.Permanent(errors.New("system.integrity needs a workspace"))
		}
		return c.Check(ctx, *job.WorkspaceID)
	})
}
