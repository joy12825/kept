// Package checklist keeps the lists of papers an errand needs (v1.6.2): a
// visa, school enrolment, an expense claim. Each item is a filter and how
// many of the newest matching documents it wants; checking a list finds
// them, says which are missing, and which have run out (a built-in due
// date in the past), so the whole set can be shared, downloaded or
// printed at once.
package checklist

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/document"
	"kept/server/internal/filter"
	"kept/server/internal/i18n"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

const (
	maxItems    = 50
	maxCount    = 24 // e.g. two years of monthly payslips
	maxNameLen  = 64
	maxNoteLen  = 1000
	maxLabelLen = 64
)

var (
	ErrNotFound = apperr.New(apperr.KindNotFound, "找不到这份清单")
	errName     = apperr.New(apperr.KindInvalid, "清单名称需要 1 到 64 个字")
	errNote     = apperr.New(apperr.KindInvalid, "说明最多 1000 个字")
	errItems    = apperr.New(apperr.KindInvalid, "清单最多 50 项")
	errLabel    = apperr.New(apperr.KindInvalid, "每一项需要 1 到 64 个字的名称")
	errCount    = apperr.New(apperr.KindInvalid, "每一项要的份数在 1 到 24 之间")
	errFilter   = apperr.New(apperr.KindInvalid, "每一项需要找文档的条件")
)

// Item is one paper the errand needs.
type Item struct {
	ID     string      `json:"id"`
	Label  string      `json:"label"`
	Filter filter.Cond `json:"filter"`
	Count  int         `json:"count"` // the newest this many matching documents
	Note   string      `json:"note,omitempty"`
}

// Checklist is a named list of items.
type Checklist struct {
	ID        int64
	Name      string
	Note      string
	Items     []Item
	UpdatedAt time.Time
}

// Input creates or replaces a checklist.
type Input struct {
	Name  string
	Note  string
	Items []Item
}

// Status of an item when checked.
const (
	StatusOK      = "ok"      // enough documents
	StatusPartial = "partial" // some, fewer than wanted
	StatusMissing = "missing" // none
)

// ItemResult is an item with what was found.
type ItemResult struct {
	Item
	Status    string
	Documents []document.Summary // put in by hand first (v1.6.7), then the newest the filter finds, up to Count
	Expired   []int64            // of Documents, those whose due date has passed
	Attached  []int64            // of Documents, those put in by hand (v1.6.7)
}

type Options struct {
	Documents *document.Service
	Now       func() time.Time
}

type Service struct {
	db  *sql.DB
	q   *sqlc.Queries
	opt Options
}

func NewService(db *sql.DB, opt Options) *Service {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	return &Service{db: db, q: sqlc.New(db), opt: opt}
}

// List returns the workspace's checklists by name.
func (s *Service) List(ctx context.Context, scope tenant.Scope) ([]Checklist, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, note, items, updated_at FROM checklists WHERE workspace_id = ? ORDER BY name, id`, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Checklist{}
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

type scanner interface{ Scan(dest ...any) error }

func scan(r scanner) (Checklist, error) {
	var c Checklist
	var items string
	var updated int64
	if err := r.Scan(&c.ID, &c.Name, &c.Note, &items, &updated); err != nil {
		return c, err
	}
	c.UpdatedAt = time.UnixMilli(updated)
	if err := json.Unmarshal([]byte(items), &c.Items); err != nil || c.Items == nil {
		c.Items = []Item{}
	}
	return c, nil
}

// Get returns one checklist.
func (s *Service) Get(ctx context.Context, scope tenant.Scope, id int64) (Checklist, error) {
	c, err := scan(s.db.QueryRowContext(ctx, `SELECT id, name, note, items, updated_at FROM checklists WHERE workspace_id = ? AND id = ?`, scope.WorkspaceID(), id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// normalize checks an input; each item's filter must compile in the
// workspace (no missing tags or types), new items get an ID.
func (s *Service) normalize(ctx context.Context, scope tenant.Scope, in *Input) error {
	in.Name, in.Note = strings.TrimSpace(in.Name), strings.TrimSpace(in.Note)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > maxNameLen {
		return errName
	}
	if utf8.RuneCountInString(in.Note) > maxNoteLen {
		return errNote
	}
	if len(in.Items) > maxItems {
		return errItems
	}
	seen := map[string]bool{}
	for i := range in.Items {
		it := &in.Items[i]
		it.Label, it.Note = strings.TrimSpace(it.Label), strings.TrimSpace(it.Note)
		if n := utf8.RuneCountInString(it.Label); n < 1 || n > maxLabelLen {
			return errLabel
		}
		if utf8.RuneCountInString(it.Note) > maxNoteLen {
			return errNote
		}
		if it.Count == 0 {
			it.Count = 1
		}
		if it.Count < 1 || it.Count > maxCount {
			return errCount
		}
		if it.Filter.IsZero() {
			return errFilter
		}
		if _, err := s.opt.Documents.List(ctx, scope, document.Query{Filter: it.Filter, Limit: 1}); err != nil {
			return err // a filter naming something gone, or malformed
		}
		if it.ID == "" || seen[it.ID] || len(it.ID) > 16 {
			it.ID = newID()
		}
		seen[it.ID] = true
	}
	return nil
}

func newID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Create adds a checklist.
func (s *Service) Create(ctx context.Context, scope tenant.Scope, in Input) (Checklist, error) {
	if err := s.normalize(ctx, scope, &in); err != nil {
		return Checklist{}, err
	}
	items, _ := json.Marshal(in.Items)
	now := s.opt.Now().UnixMilli()
	actor := audit.ActorFrom(ctx)
	var id int64
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `INSERT INTO checklists (workspace_id, name, note, items, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			scope.WorkspaceID(), in.Name, in.Note, string(items), sql.NullInt64{Int64: actor.UserID, Valid: actor.UserID != 0}, now, now).Scan(&id); err != nil {
			return err
		}
		return s.record(ctx, tx, scope, "checklist.create", id, i18n.N("%s 新建了清单“%s”"), in.Name)
	})
	if err != nil {
		return Checklist{}, err
	}
	return s.Get(ctx, scope, id)
}

// Update replaces a checklist's name, note and items.
func (s *Service) Update(ctx context.Context, scope tenant.Scope, id int64, in Input) (Checklist, error) {
	if _, err := s.Get(ctx, scope, id); err != nil {
		return Checklist{}, err
	}
	if err := s.normalize(ctx, scope, &in); err != nil {
		return Checklist{}, err
	}
	items, _ := json.Marshal(in.Items)
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE checklists SET name = ?, note = ?, items = ?, updated_at = ? WHERE workspace_id = ? AND id = ?`,
			in.Name, in.Note, string(items), s.opt.Now().UnixMilli(), scope.WorkspaceID(), id); err != nil {
			return err
		}
		// documents put into items that are gone go with them
		keep := []any{id}
		marks := make([]string, 0, len(in.Items))
		for _, it := range in.Items {
			keep = append(keep, it.ID)
			marks = append(marks, "?")
		}
		q := `DELETE FROM checklist_documents WHERE checklist_id = ?`
		if len(marks) > 0 {
			q += ` AND item_id NOT IN (` + strings.Join(marks, ",") + `)`
		}
		if _, err := tx.ExecContext(ctx, q, keep...); err != nil {
			return err
		}
		return s.record(ctx, tx, scope, "checklist.update", id, i18n.N("%s 修改了清单“%s”"), in.Name)
	})
	if err != nil {
		return Checklist{}, err
	}
	return s.Get(ctx, scope, id)
}

// Delete removes a checklist; documents are not touched.
func (s *Service) Delete(ctx context.Context, scope tenant.Scope, id int64) error {
	c, err := s.Get(ctx, scope, id)
	if err != nil {
		return err
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM checklists WHERE workspace_id = ? AND id = ?`, scope.WorkspaceID(), id); err != nil {
			return err
		}
		return s.record(ctx, tx, scope, "checklist.delete", id, i18n.N("%s 删除了清单“%s”"), c.Name)
	})
}

// Check finds each item's newest documents.
func (s *Service) Check(ctx context.Context, scope tenant.Scope, id int64) (Checklist, []ItemResult, error) {
	c, err := s.Get(ctx, scope, id)
	if err != nil {
		return c, nil, err
	}
	today := s.opt.Now().Format("2006-01-02")
	attached, err := s.attached(ctx, scope, id)
	if err != nil {
		return c, nil, err
	}
	out := make([]ItemResult, len(c.Items))
	for i, it := range c.Items {
		r := ItemResult{Item: it, Documents: []document.Summary{}, Expired: []int64{}, Attached: []int64{}}
		// documents put in by hand come first and always count
		taken := map[int64]bool{}
		for _, docID := range attached[it.ID] {
			d, err := s.opt.Documents.GetDetail(ctx, scope, docID)
			if err != nil {
				continue // in the trash
			}
			r.Documents = append(r.Documents, d.Summary)
			r.Attached = append(r.Attached, docID)
			taken[docID] = true
		}
		if want := it.Count - len(r.Documents); want > 0 {
			page, err := s.opt.Documents.List(ctx, scope, document.Query{Filter: it.Filter, Sort: document.SortCreated, Desc: true, Limit: want + len(taken)})
			var ae *apperr.Error
			switch {
			case err == nil:
				for _, d := range page.Items {
					if !taken[d.ID] && len(r.Documents) < it.Count {
						r.Documents = append(r.Documents, d)
					}
				}
			case errors.As(err, &ae) && ae.Kind == apperr.KindInvalid:
				// a filter that no longer compiles (a tag deleted since):
				// only the documents put in by hand count, rather than the
				// whole list failing
			default:
				return c, nil, err
			}
		}
		for _, d := range r.Documents {
			if d.DueDate != "" && d.DueDate < today {
				r.Expired = append(r.Expired, d.ID)
			}
		}
		switch {
		case len(r.Documents) == 0:
			r.Status = StatusMissing
		case len(r.Documents) < it.Count:
			r.Status = StatusPartial
		default:
			r.Status = StatusOK
		}
		out[i] = r
	}
	return c, out, nil
}

// record records a change to a checklist. format's first value is the
// actor's name; the summary is translated when shown (v1.10.1).
func (s *Service) record(ctx context.Context, tx *sql.Tx, scope tenant.Scope, action string, id int64, format string, args ...any) error {
	return audit.Record(ctx, s.q.WithTx(tx), audit.Event{WorkspaceID: scope.WorkspaceID(), Action: action, TargetType: "checklist",
		TargetID: strconv.FormatInt(id, 10), Summary: summary(ctx, format, args)})
}

func summary(ctx context.Context, format string, args []any) string {
	return fmt.Sprintf(format, append([]any{audit.ActorFrom(ctx).Name}, args...)...)
}

// recordDoc records a change to a checklist about one document. A private
// document's goes on the document instead (v1.9.10): the activity of a
// private document shows only to those who see it, and its title is not
// in the checklist's.
func (s *Service) recordDoc(ctx context.Context, tx *sql.Tx, scope tenant.Scope, action string, id, docID int64, private bool, format string, args ...any) error {
	if !private {
		return s.record(ctx, tx, scope, action, id, format, args...)
	}
	return audit.Record(ctx, s.q.WithTx(tx), audit.Event{WorkspaceID: scope.WorkspaceID(), Action: action, TargetType: "document",
		TargetID: strconv.FormatInt(docID, 10), Summary: summary(ctx, format, args)})
}

func (s *Service) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// ---------- documents put in by hand (v1.6.7) ----------

var (
	errItem = apperr.New(apperr.KindNotFound, "清单里没有这一项")
	errDoc  = apperr.New(apperr.KindNotFound, "找不到这份文档")
)

// attached lists, per item, the documents put in by hand, newest first.
func (s *Service) attached(ctx context.Context, scope tenant.Scope, id int64) (map[string][]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT cd.item_id, cd.document_id FROM checklist_documents cd JOIN documents d ON d.id = cd.document_id
		WHERE cd.checklist_id = ? AND d.workspace_id = ? AND d.deleted_at IS NULL ORDER BY d.created_date DESC, d.id DESC`, id, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]int64{}
	for rows.Next() {
		var item string
		var doc int64
		if err := rows.Scan(&item, &doc); err != nil {
			return nil, err
		}
		out[item] = append(out[item], doc)
	}
	return out, rows.Err()
}

// item finds an item of a checklist of the workspace.
func (s *Service) item(ctx context.Context, scope tenant.Scope, id int64, itemID string) (Checklist, Item, error) {
	c, err := s.Get(ctx, scope, id)
	if err != nil {
		return c, Item{}, err
	}
	for _, it := range c.Items {
		if it.ID == itemID {
			return c, it, nil
		}
	}
	return c, Item{}, errItem
}

// CheckItem confirms an item exists (before a file is uploaded into it).
func (s *Service) CheckItem(ctx context.Context, scope tenant.Scope, id int64, itemID string) error {
	_, _, err := s.item(ctx, scope, id, itemID)
	return err
}

// Attach puts a document of the workspace into an item by hand.
func (s *Service) Attach(ctx context.Context, scope tenant.Scope, id int64, itemID string, docID int64) error {
	c, it, err := s.item(ctx, scope, id, itemID)
	if err != nil {
		return err
	}
	d, err := s.opt.Documents.Get(ctx, scope, docID)
	if err != nil {
		return errDoc
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, `INSERT INTO checklist_documents (checklist_id, item_id, document_id, added_at) VALUES (?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			id, itemID, docID, s.opt.Now().UnixMilli())
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return nil
		}
		return s.recordDoc(ctx, tx, scope, "checklist.attach", id, docID, d.Private, i18n.N("%s 把《%s》放进了清单“%s”的“%s”"), d.Title, c.Name, it.Label)
	})
}

// Detach takes a document put in by hand out of an item; the document
// stays (and the filter may still find it).
func (s *Service) Detach(ctx context.Context, scope tenant.Scope, id int64, itemID string, docID int64) error {
	c, it, err := s.item(ctx, scope, id, itemID)
	if err != nil {
		return err
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, `DELETE FROM checklist_documents WHERE checklist_id = ? AND item_id = ? AND document_id = ?`, id, itemID, docID)
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return errDoc
		}
		title, private := "", 0
		_ = tx.QueryRowContext(ctx, `SELECT title, private FROM documents WHERE id = ?`, docID).Scan(&title, &private)
		return s.recordDoc(ctx, tx, scope, "checklist.detach", id, docID, private == 1, i18n.N("%s 把《%s》移出了清单“%s”的“%s”"), title, c.Name, it.Label)
	})
}
