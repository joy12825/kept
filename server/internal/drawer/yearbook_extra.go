package drawer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"kept/server/internal/i18n"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/files"
	"kept/server/internal/filter"
	"kept/server/internal/privacy"
	"kept/server/internal/tenant"
)

// v0.3 stage 21: the previous year to compare with, five years of totals,
// milestone rules a workspace can edit, and the storage a year takes.

// YearPrevious is the previous year with the same measures, for the
// differences the page shows.
type YearPrevious struct {
	Year      int          `json:"year"`
	Documents int          `json:"documents"`
	Drawers   []YearDrawer `json:"drawers"`
	Tags      []YearTag    `json:"tags"`
}

// YearCount is one year's documents.
type YearCount struct {
	Year      int `json:"year"`
	Documents int `json:"documents"`
}

// YearStorage is the space the year's documents take, in bytes.
type YearStorage struct {
	Originals  int64      `json:"originals"`
	Searchable int64      `json:"searchable"` // searchable PDFs and their unedited copies
	Thumbnails int64      `json:"thumbnails"`
	Versions   int64      `json:"versions"` // earlier versions
	Total      int64      `json:"total"`
	Largest    []LargeDoc `json:"largest"`
	Workspace  int64      `json:"workspace"` // every document outside the trash
}

type LargeDoc struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Bytes int64  `json:"bytes"`
}

// MilestoneRule turns the documents matching Filter into 大事记 lines
// “Verb title（correspondent）”.
type MilestoneRule struct {
	Verb   string      `json:"verb"`
	Filter filter.Cond `json:"filter"`
}

const (
	maxRules   = 10
	maxVerbLen = 8
)

// MilestoneRules returns the workspace's rules and whether they are the
// defaults: 合同 → 签订, 证件 → 存入, for the types that exist.
func (s *Service) MilestoneRules(ctx context.Context, scope tenant.Scope) ([]MilestoneRule, bool, error) {
	var raw sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT yearbook_rules FROM workspaces WHERE id = ?`, scope.WorkspaceID()).Scan(&raw); err != nil {
		return nil, false, err
	}
	if raw.Valid {
		var rules []MilestoneRule
		if err := json.Unmarshal([]byte(raw.String), &rules); err == nil {
			return rules, false, nil
		}
	}
	rules := []MilestoneRule{}
	for _, d := range []struct{ typ, verb string }{{"合同", i18n.N("签订")}, {"证件", i18n.N("存入")}} {
		var id int64
		err := s.db.QueryRowContext(ctx, `SELECT id FROM document_types WHERE workspace_id = ? AND name = ?`, scope.WorkspaceID(), d.typ).Scan(&id)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		rules = append(rules, MilestoneRule{Verb: d.verb, Filter: fieldCond("type", "in", []int64{id})})
	}
	return rules, true, nil
}

// SetMilestoneRules saves the rules; reset goes back to the defaults.
func (s *Service) SetMilestoneRules(ctx context.Context, scope tenant.Scope, rules []MilestoneRule, reset bool) error {
	var value sql.NullString
	if !reset {
		if len(rules) > maxRules {
			return apperr.New(apperr.KindInvalid, fmt.Sprintf("大事记最多 %d 条规则", maxRules))
		}
		cat, err := filter.LoadCatalog(ctx, s.q, scope)
		if err != nil {
			return err
		}
		cat.Substring = true
		for i := range rules {
			rules[i].Verb = strings.TrimSpace(rules[i].Verb)
			if n := utf8.RuneCountInString(rules[i].Verb); n < 1 || n > maxVerbLen {
				return apperr.New(apperr.KindInvalid, fmt.Sprintf("第 %d 条规则：动词应为 1 到 %d 个字", i+1, maxVerbLen))
			}
			if _, err := filter.Compile(cat, scope, rules[i].Filter); err != nil {
				return err
			}
		}
		b, _ := json.Marshal(rules)
		value = sql.NullString{String: string(b), Valid: true}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE workspaces SET yearbook_rules = ? WHERE id = ?`, value, scope.WorkspaceID()); err != nil {
		return err
	}
	summary := i18n.N("%s 修改了年度回顾的大事记规则")
	if reset {
		summary = i18n.N("%s 把年度回顾的大事记规则恢复为默认")
	}
	if err := audit.Record(ctx, s.q.WithTx(tx), audit.Event{WorkspaceID: scope.WorkspaceID(), Action: "yearbook.rules", TargetType: "workspace",
		TargetID: strconv.FormatInt(scope.WorkspaceID(), 10), Summary: fmt.Sprintf(summary, audit.ActorFrom(ctx).Name), At: s.opt.Now()}); err != nil {
		return err
	}
	return tx.Commit()
}

// ruleEvents lists the year's documents matched by the rules (first rule
// wins) and the ones marked as milestones.
func (s *Service) ruleEvents(ctx context.Context, scope tenant.Scope, year int) ([]YearEvent, error) {
	rules, _, err := s.MilestoneRules(ctx, scope)
	if err != nil {
		return nil, err
	}
	cat, err := filter.LoadCatalog(ctx, s.q, scope)
	if err != nil {
		return nil, err
	}
	cat.Substring = true
	from, to := fmt.Sprintf("%04d-01-01", year), fmt.Sprintf("%04d-01-01", year+1)
	inYear := filter.Cond{All: []filter.Cond{fieldCond("created", "gte", from), fieldCond("created", "lt", to)}}
	seen := map[int64]bool{}
	out := []YearEvent{} // [] in JSON, not null
	collect := func(where string, args []any, kind, verb string) error {
		rows, err := s.db.QueryContext(ctx, `SELECT d.id, d.created_date, d.title, coalesce(c.name, '') FROM documents d
			LEFT JOIN correspondents c ON c.id = d.correspondent_id WHERE `+where+` ORDER BY d.created_date, d.id`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var ev YearEvent
			var date, title, corr string
			if err := rows.Scan(&ev.DocumentID, &date, &title, &corr); err != nil {
				return err
			}
			if seen[ev.DocumentID] {
				continue
			}
			seen[ev.DocumentID] = true
			with := title
			if corr != "" {
				with += "（" + corr + "）"
			}
			ev.Date, ev.Kind, ev.Text = date[5:], kind, strings.TrimSpace(verb+" "+with)
			out = append(out, ev)
		}
		return rows.Err()
	}
	// Marked documents first, so a marked contract reads as a milestone.
	vis, vargs := privacy.Visible("d", scope)
	if err := collect(`d.workspace_id = ? AND d.deleted_at IS NULL AND d.created_date >= ? AND d.created_date < ? AND d.is_milestone = 1 AND `+vis,
		append([]any{scope.WorkspaceID(), from, to}, vargs...), "milestone", ""); err != nil {
		return nil, err
	}
	for _, r := range rules {
		w, err := filter.Compile(cat, scope, filter.Cond{All: []filter.Cond{inYear, r.Filter}})
		if err != nil {
			continue // a rule broken by a deleted type matches nothing
		}
		if err := collect(w.Where, w.Args, "rule", r.Verb); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// history returns the documents of the five years up to year.
func (s *Service) history(ctx context.Context, scope tenant.Scope, year int) ([]YearCount, error) {
	out := make([]YearCount, 5)
	for i := range out {
		out[i].Year = year - 4 + i
	}
	vis, vargs := privacy.Visible("d", scope)
	rows, err := s.db.QueryContext(ctx, `SELECT CAST(substr(d.created_date, 1, 4) AS INTEGER), count(*) FROM documents d
		WHERE d.workspace_id = ? AND d.deleted_at IS NULL AND d.created_date >= ? AND d.created_date < ? AND `+vis+` GROUP BY 1`,
		append([]any{scope.WorkspaceID(), fmt.Sprintf("%04d-01-01", year-4), fmt.Sprintf("%04d-01-01", year+1)}, vargs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var y, n int
		if err := rows.Scan(&y, &n); err != nil {
			return nil, err
		}
		if i := y - (year - 4); i >= 0 && i < 5 {
			out[i].Documents = n
		}
	}
	return out, rows.Err()
}

func size(p string) int64 {
	if st, err := os.Stat(p); err == nil {
		return st.Size()
	}
	return 0
}

// storage adds up the files of the year's documents, and of the whole
// workspace (the documents the viewer sees). Originals, searchable PDFs and
// earlier versions count by the sizes the database records (v1.13.5: asking
// every file's storage took most of a second at 50,000 documents, and was a
// request each on S3); a document whose sizes are not recorded yet is asked
// once, as the quota's recount does. Thumbnails and covers are files of
// this machine.
func (s *Service) storage(ctx context.Context, scope tenant.Scope, year int) (*YearStorage, error) {
	fs := s.opt.Files
	if fs == nil {
		return nil, nil
	}
	ws := scope.WorkspaceID()
	vis, vargs := privacy.Visible("d", scope)
	// earlier versions and books (they have covers), looked up once
	versions, books := map[int64]int64{}, map[int64]bool{}
	if err := s.scanPairs(ctx, `SELECT document_id, CAST(SUM(size) AS BIGINT) FROM document_versions WHERE workspace_id = ? AND current = 0 GROUP BY document_id`, ws,
		func(id, n int64) { versions[id] = n }); err != nil {
		return nil, err
	}
	if err := s.scanPairs(ctx, `SELECT b.document_id, 1 FROM books b JOIN documents d ON d.id = b.document_id WHERE d.workspace_id = ?`, ws,
		func(id, _ int64) { books[id] = true }); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d.id, d.title, d.original_ext, d.created_date, d.storage, d.has_thumbnail, d.original_size, d.derived_size
		FROM documents d WHERE d.workspace_id = ? AND d.deleted_at IS NULL AND `+vis,
		append([]any{ws}, vargs...)...)
	if err != nil {
		return nil, err
	}
	type doc struct {
		id               int64
		title, ext, date string
		storage          string
		thumb, book      bool
		orig, derived    sql.NullInt64
		versions         int64
	}
	var docs []doc
	for rows.Next() {
		var d doc
		var thumb int64
		if err := rows.Scan(&d.id, &d.title, &d.ext, &d.date, &d.storage, &thumb, &d.orig, &d.derived); err != nil {
			rows.Close()
			return nil, err
		}
		d.thumb, d.book, d.versions = thumb == 1, books[d.id], versions[d.id]
		docs = append(docs, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	stored := func(storage string, r files.Ref) int64 {
		if st, err := fs.Stat(ctx, storage, r); err == nil {
			return st.Size
		}
		return 0
	}
	prefix := strconv.Itoa(year) + "-"
	st := &YearStorage{Largest: []LargeDoc{}}
	for _, d := range docs {
		o, a := d.orig.Int64, d.derived.Int64
		if !d.orig.Valid {
			o = stored(d.storage, files.Original(ws, d.id, d.ext))
		}
		if !d.derived.Valid {
			a = stored(d.storage, files.Archive(ws, d.id)) + stored(d.storage, files.Base(ws, d.id))
		}
		var t int64
		if d.thumb {
			t = size(fs.ThumbnailPath(ws, d.id))
		}
		if d.book { // only books have covers
			t += size(fs.CoverPath(ws, d.id))
		}
		all := o + a + t + d.versions
		st.Workspace += all
		if !strings.HasPrefix(d.date, prefix) {
			continue
		}
		st.Originals += o
		st.Searchable += a
		st.Thumbnails += t
		st.Versions += d.versions
		st.Total += all
		st.Largest = append(st.Largest, LargeDoc{ID: d.id, Title: d.title, Bytes: all})
	}
	sort.SliceStable(st.Largest, func(i, j int) bool { return st.Largest[i].Bytes > st.Largest[j].Bytes })
	if len(st.Largest) > 3 {
		st.Largest = st.Largest[:3]
	}
	return st, nil
}

// scanPairs runs a query of two integer columns for workspace ws.
func (s *Service) scanPairs(ctx context.Context, query string, ws int64, each func(a, b int64)) error {
	rows, err := s.db.QueryContext(ctx, query, ws)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var a, b int64
		if err := rows.Scan(&a, &b); err != nil {
			return err
		}
		each(a, b)
	}
	return rows.Err()
}
