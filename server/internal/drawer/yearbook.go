package drawer

import (
	"context"
	"database/sql"
	"fmt"
	"kept/server/internal/i18n"
	"sort"
	"strconv"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/filter"
	"kept/server/internal/privacy"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// Yearbook is one year in review (design doc 5.7). Everything counts the
// documents dated in that year (created_date), inbox included, so the
// numbers match the document list filtered by year; the drawers' counts
// plus Unfiled add up to Documents.
type Yearbook struct {
	Year              int          `json:"year"`
	AsOf              string       `json:"asOf,omitempty"` // today, for the current year; "" otherwise
	Documents         int          `json:"documents"`
	NewCorrespondents int          `json:"newCorrespondents"`
	TopType           *Count       `json:"topType,omitempty"` // the type with the most documents
	BusiestMonth      int          `json:"busiestMonth"`      // 1–12; 0 without documents
	Months            [12]int      `json:"months"`
	Drawers           []YearDrawer `json:"drawers"`
	Unfiled           int          `json:"unfiled"`
	Tags              []YearTag    `json:"tags"`
	Events            []YearEvent  `json:"events"`
	// v0.3 stage 21
	Previous *YearPrevious `json:"previous,omitempty"`
	History  []YearCount   `json:"history"`
	Storage  *YearStorage  `json:"storage,omitempty"`
	// v0.4 stage 28: the books the caller finished this year (their own).
	Books         []YearBook `json:"books"`
	BooksFinished int        `json:"booksFinished"`
}

// YearBook is a book the caller finished in the year.
type YearBook struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	FinishedOn   string `json:"finishedOn"`
	Rating       int    `json:"rating"`
	HasCover     bool   `json:"hasCover"`
	HasThumbnail bool   `json:"hasThumbnail"`
}

type Count struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// YearDrawer is a drawer's year: its documents and its sum facts over the year.
type YearDrawer struct {
	ID    int64     `json:"id"`
	Name  string    `json:"name"`
	Icon  string    `json:"icon"`
	Color string    `json:"color"`
	Count int       `json:"count"`
	Sums  []YearSum `json:"sums"`
}

type YearSum struct {
	Label string `json:"label"`
	Total string `json:"total"`
	Money bool   `json:"money"`
}

// YearTag is a tag marked to show in the yearbook, with the documents of
// the year and their built-in amounts added up.
type YearTag struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Color  string `json:"color"`
	Count  int    `json:"count"`
	Amount string `json:"amount,omitempty"` // "" when none has an amount
}

// YearEvent is one line of 大事记.
type YearEvent struct {
	Date       string `json:"date"` // MM-DD
	Kind       string `json:"kind"` // rule | drawer | milestone
	Text       string `json:"text"`
	DocumentID int64  `json:"documentId,omitempty"`
	DrawerID   int64  `json:"drawerId,omitempty"`
}

var errYear = apperr.New(apperr.KindInvalid, "年份不正确")

// Years lists the years that have documents, newest first; the current
// year is always included.
func (s *Service) Years(ctx context.Context, scope tenant.Scope) ([]int, error) {
	vis, vargs := privacy.Visible("d", scope)
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT CAST(substr(d.created_date, 1, 4) AS INTEGER) FROM documents d
		WHERE d.workspace_id = ? AND d.deleted_at IS NULL AND `+vis+` ORDER BY 1 DESC`, append([]any{scope.WorkspaceID()}, vargs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cur := s.opt.Now().Year()
	out := []int{cur}
	for rows.Next() {
		var y int
		if err := rows.Scan(&y); err != nil {
			return nil, err
		}
		if y != cur && y > 1900 && y <= cur+1 {
			out = append(out, y)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	return out, rows.Err()
}

func (s *Service) Yearbook(ctx context.Context, scope tenant.Scope, year int) (Yearbook, error) {
	y, err := s.yearbook(ctx, scope, year)
	if err != nil {
		return y, err
	}
	if year > 1900 {
		p, err := s.yearbook(ctx, scope, year-1)
		if err != nil {
			return y, err
		}
		y.Previous = &YearPrevious{Year: p.Year, Documents: p.Documents, Drawers: p.Drawers, Tags: p.Tags}
	}
	if y.History, err = s.history(ctx, scope, year); err != nil {
		return y, err
	}
	if y.Storage, err = s.storage(ctx, scope, year); err != nil {
		return y, err
	}
	y.Books, err = s.finishedBooks(ctx, scope, year)
	y.BooksFinished = len(y.Books)
	return y, err
}

// finishedBooks lists the books the caller finished in the year.
func (s *Service) finishedBooks(ctx context.Context, scope tenant.Scope, year int) ([]YearBook, error) {
	out := []YearBook{}
	user := audit.ActorFrom(ctx).UserID
	if user == 0 {
		return out, nil
	}
	rows, err := s.q.FinishedBooks(ctx, sqlc.FinishedBooksParams{UserID: user, WorkspaceID: scope.WorkspaceID(),
		FromDate: fmt.Sprintf("%04d-01-01", year), ToDate: fmt.Sprintf("%04d-01-01", year+1)})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out = append(out, YearBook{ID: r.ID, Title: r.Title, FinishedOn: r.FinishedOn, Rating: int(r.Rating), HasCover: r.Cover == 1, HasThumbnail: r.HasThumbnail == 1})
	}
	return out, nil
}

func (s *Service) yearbook(ctx context.Context, scope tenant.Scope, year int) (Yearbook, error) {
	now := s.opt.Now()
	if year < 1900 || year > now.Year()+1 {
		return Yearbook{}, errYear
	}
	ws := scope.WorkspaceID()
	y := Yearbook{Year: year, Drawers: []YearDrawer{}, Tags: []YearTag{}, Events: []YearEvent{}}
	if year == now.Year() {
		y.AsOf = now.Format("2006-01-02")
	}
	from, to := fmt.Sprintf("%04d-01-01", year), fmt.Sprintf("%04d-01-01", year+1)
	// only what the viewer may see: family members' private documents
	// count for them alone (v1.8.0)
	vis, vargs := privacy.Visible("d", scope)
	inYear := `d.workspace_id = ? AND d.deleted_at IS NULL AND d.created_date >= ? AND d.created_date < ? AND ` + vis
	args := append([]any{ws, from, to}, vargs...)

	// Months and the total.
	rows, err := s.db.QueryContext(ctx, `SELECT CAST(substr(d.created_date, 6, 2) AS INTEGER), count(*) FROM documents d WHERE `+inYear+` GROUP BY 1`, args...)
	if err != nil {
		return y, err
	}
	for rows.Next() {
		var m, n int
		if err := rows.Scan(&m, &n); err != nil {
			rows.Close()
			return y, err
		}
		if m >= 1 && m <= 12 {
			y.Months[m-1] = n
			y.Documents += n
		}
	}
	rows.Close()
	for i, n := range y.Months {
		if n > 0 && (y.BusiestMonth == 0 || n > y.Months[y.BusiestMonth-1]) {
			y.BusiestMonth = i + 1
		}
	}

	// Correspondents whose first document is in this year.
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT d.correspondent_id, min(d.created_date) AS first FROM documents d
		WHERE d.workspace_id = ? AND d.deleted_at IS NULL AND d.correspondent_id IS NOT NULL AND `+vis+` GROUP BY d.correspondent_id) AS c WHERE first >= ? AND first < ?`,
		append(append([]any{ws}, vargs...), from, to)...).Scan(&y.NewCorrespondents); err != nil {
		return y, err
	}
	var top Count
	err = s.db.QueryRowContext(ctx, `SELECT t.id, t.name, count(*) FROM documents d JOIN document_types t ON t.id = d.document_type_id
		WHERE `+inYear+` GROUP BY t.id ORDER BY count(*) DESC, t.sort_key LIMIT 1`, args...).Scan(&top.ID, &top.Name, &top.Count)
	if err == nil {
		y.TopType = &top
	} else if err != sql.ErrNoRows {
		return y, err
	}

	// Drawers, in order, with their sum facts over the year.
	counts := map[int64]int{}
	rows, err = s.db.QueryContext(ctx, `SELECT coalesce(d.drawer_id, 0), count(*) FROM documents d WHERE `+inYear+` GROUP BY 1`, args...)
	if err != nil {
		return y, err
	}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return y, err
		}
		counts[id] = n
	}
	rows.Close()
	y.Unfiled = counts[0]
	drawers, err := s.List(ctx, scope)
	if err != nil {
		return y, err
	}
	e, err := s.env(ctx, scope)
	if err != nil {
		return y, err
	}
	for _, d := range drawers {
		if counts[d.ID] == 0 {
			continue
		}
		yd := YearDrawer{ID: d.ID, Name: d.Name, Icon: d.Icon, Color: d.Color, Count: counts[d.ID], Sums: []YearSum{}}
		facts, err := s.Facts(ctx, scope, d.ID)
		if err != nil {
			return y, err
		}
		for _, f := range facts {
			if f.Kind != KindSum {
				continue
			}
			total, money, ok, err := e.sumBetween(ctx, f, from, to)
			if err != nil {
				return y, err
			}
			if ok {
				yd.Sums = append(yd.Sums, YearSum{Label: f.Label, Total: total, Money: money})
			}
		}
		y.Drawers = append(y.Drawers, yd)
	}

	// Tags shown in the yearbook.
	rows, err = s.db.QueryContext(ctx, `SELECT t.id, t.name, t.color, count(DISTINCT d.id),
			sum(v.value_monetary)
		FROM tags t
		LEFT JOIN document_tags dt ON dt.tag_id = t.id
		LEFT JOIN documents d ON d.id = dt.document_id AND d.deleted_at IS NULL AND d.created_date >= ? AND d.created_date < ? AND `+vis+`
		LEFT JOIN custom_fields f ON f.workspace_id = t.workspace_id AND f.key = 'amount'
		LEFT JOIN custom_field_values v ON v.document_id = d.id AND v.field_id = f.id
		WHERE t.workspace_id = ? AND t.show_in_yearbook = 1
		GROUP BY t.id ORDER BY t.sort_key, t.id`, append(append([]any{from, to}, vargs...), ws)...)
	if err != nil {
		return y, err
	}
	for rows.Next() {
		var t YearTag
		var amount sql.NullInt64
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.Count, &amount); err != nil {
			rows.Close()
			return y, err
		}
		if amount.Valid {
			t.Amount = filter.FormatMoney(amount.Int64)
		}
		y.Tags = append(y.Tags, t)
	}
	rows.Close()

	// 大事记: marked documents, the milestone rules' documents, new drawers.
	if y.Events, err = s.ruleEvents(ctx, scope, year); err != nil {
		return y, err
	}
	for _, d := range drawers {
		if d.CreatedAt.Year() == year {
			y.Events = append(y.Events, YearEvent{Date: d.CreatedAt.Format("01-02"), Kind: "drawer", Text: fmt.Sprintf(i18n.N("新建抽屉 %s"), d.Name), DrawerID: d.ID})
		}
	}
	sort.SliceStable(y.Events, func(i, j int) bool { return y.Events[i].Date < y.Events[j].Date })
	return y, nil
}

// sumBetween adds up a sum fact's field over [from, to); ok is false when
// the fact cannot be computed (its field is gone).
func (e *factEnv) sumBetween(ctx context.Context, f Fact, from, to string) (string, bool, bool, error) {
	c := f.Config
	rng := filter.Cond{All: []filter.Cond{fieldCond("created", "gte", from), fieldCond("created", "lt", to)}}
	all := []filter.Cond{fieldCond("drawer", "in", []int64{f.DrawerID}), rng}
	if sf := scopeFilter(c); sf != nil {
		all = append(all, *sf)
	}
	w, err := filter.Compile(e.cat, e.scope, filter.Cond{All: all})
	if err != nil {
		return "", false, false, nil
	}
	var typ string
	if err := e.s.db.QueryRowContext(ctx, `SELECT data_type FROM custom_fields WHERE id = ? AND workspace_id = ?`, c.FieldID, e.scope.WorkspaceID()).Scan(&typ); err != nil {
		if err == sql.ErrNoRows {
			return "", false, false, nil
		}
		return "", false, false, err
	}
	col := map[string]string{"monetary": "v.value_monetary", "integer": "v.value_int", "float": "v.value_float"}[typ]
	if col == "" {
		return "", false, false, nil
	}
	var total float64
	if err := e.s.db.QueryRowContext(ctx, `SELECT coalesce(sum(`+col+`), 0) FROM documents d JOIN custom_field_values v ON v.document_id = d.id AND v.field_id = ?
		WHERE `+w.Where, append([]any{c.FieldID}, w.Args...)...).Scan(&total); err != nil {
		return "", false, false, err
	}
	if typ == "monetary" {
		return filter.FormatMoney(int64(total)), true, true, nil
	}
	return strconv.FormatFloat(total, 'f', -1, 64), false, true, nil
}
