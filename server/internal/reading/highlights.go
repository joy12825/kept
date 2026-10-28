package reading

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"kept/server/internal/apperr"
	"kept/server/internal/privacy"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

var (
	errHighlightGone = apperr.New(apperr.KindNotFound, "找不到这条高亮")
	errLocator       = apperr.New(apperr.KindInvalid, "高亮的位置不正确")
	errQuote         = apperr.New(apperr.KindInvalid, "摘录最多 2000 个字")
	errNote          = apperr.New(apperr.KindInvalid, "批注最多 5000 个字")
	errColor         = apperr.New(apperr.KindInvalid, "颜色只能是黄、绿、蓝、粉")
	errEmpty         = apperr.New(apperr.KindInvalid, "高亮需要选中的文字，或者写一句批注")
)

// Colors a highlight can have.
var Colors = []string{"yellow", "green", "blue", "pink"}

// Highlight is a person's mark in a document: an EPUB CFI range or a PDF
// page, the quoted text and a note.
type Highlight struct {
	ID            int64
	DocumentID    int64
	DocumentTitle string // set by Search
	Locator       string
	Quote         string
	Note          string
	Color         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// HighlightInput creates a highlight; Note and Color may change later.
type HighlightInput struct {
	Locator, Quote, Note, Color string
}

func toHighlight(r sqlc.Highlight) Highlight {
	return Highlight{ID: r.ID, DocumentID: r.DocumentID, Locator: r.Locator, Quote: r.Quote, Note: r.Note, Color: r.Color,
		CreatedAt: time.UnixMilli(r.CreatedAt), UpdatedAt: time.UnixMilli(r.UpdatedAt)}
}

func checkText(note, color string) (string, error) {
	if utf8.RuneCountInString(note) > 5000 {
		return "", errNote
	}
	if color == "" {
		color = "yellow"
	}
	if !slices.Contains(Colors, color) {
		return "", errColor
	}
	return color, nil
}

// Highlights lists the user's highlights in a document, in creation order.
func (s *Service) Highlights(ctx context.Context, scope tenant.Scope, userID, docID int64) ([]Highlight, error) {
	if err := s.readable(ctx, scope, docID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListDocumentHighlights(ctx, sqlc.ListDocumentHighlightsParams{UserID: userID, DocumentID: docID, WorkspaceID: scope.WorkspaceID()})
	if err != nil {
		return nil, err
	}
	out := make([]Highlight, len(rows))
	for i, r := range rows {
		out[i] = toHighlight(r)
	}
	return out, nil
}

// AddHighlight marks a passage (or a page) for the user.
func (s *Service) AddHighlight(ctx context.Context, scope tenant.Scope, userID, docID int64, in HighlightInput) (Highlight, error) {
	in.Locator = strings.TrimSpace(in.Locator)
	in.Quote = strings.TrimSpace(in.Quote)
	in.Note = strings.TrimSpace(in.Note)
	if in.Locator == "" || utf8.RuneCountInString(in.Locator) > 2000 {
		return Highlight{}, errLocator
	}
	if utf8.RuneCountInString(in.Quote) > 2000 {
		return Highlight{}, errQuote
	}
	if in.Quote == "" && in.Note == "" {
		return Highlight{}, errEmpty
	}
	color, err := checkText(in.Note, in.Color)
	if err != nil {
		return Highlight{}, err
	}
	if err := s.readable(ctx, scope, docID); err != nil {
		return Highlight{}, err
	}
	r, err := s.q.InsertHighlight(ctx, sqlc.InsertHighlightParams{UserID: userID, DocumentID: docID, WorkspaceID: scope.WorkspaceID(),
		Locator: in.Locator, Quote: in.Quote, Note: in.Note, Color: color, Now: s.now().UnixMilli()})
	if err != nil {
		return Highlight{}, err
	}
	return toHighlight(r), nil
}

// UpdateHighlight changes the note or the colour (nil keeps them).
func (s *Service) UpdateHighlight(ctx context.Context, scope tenant.Scope, userID, id int64, note, color *string) (Highlight, error) {
	r, err := s.q.GetHighlight(ctx, sqlc.GetHighlightParams{ID: id, UserID: userID, WorkspaceID: scope.WorkspaceID()})
	if errors.Is(err, sql.ErrNoRows) {
		return Highlight{}, errHighlightGone
	}
	if err != nil {
		return Highlight{}, err
	}
	if note != nil {
		r.Note = strings.TrimSpace(*note)
	}
	if color != nil {
		r.Color = *color
	}
	if r.Color, err = checkText(r.Note, r.Color); err != nil {
		return Highlight{}, err
	}
	if r.Quote == "" && r.Note == "" {
		return Highlight{}, errEmpty
	}
	r.UpdatedAt = s.now().UnixMilli()
	if err := s.q.UpdateHighlight(ctx, sqlc.UpdateHighlightParams{Note: r.Note, Color: r.Color, Now: r.UpdatedAt, ID: id, UserID: userID, WorkspaceID: scope.WorkspaceID()}); err != nil {
		return Highlight{}, err
	}
	return toHighlight(r), nil
}

// DeleteHighlight removes one of the user's highlights.
func (s *Service) DeleteHighlight(ctx context.Context, scope tenant.Scope, userID, id int64) error {
	n, err := s.q.DeleteHighlight(ctx, sqlc.DeleteHighlightParams{ID: id, UserID: userID, WorkspaceID: scope.WorkspaceID()})
	if err != nil {
		return err
	}
	if n == 0 {
		return errHighlightGone
	}
	return nil
}

// SearchQuery filters the user's highlights in the workspace.
type SearchQuery struct {
	Text       string // in the quote or the note
	Color      string
	DocumentID int64
	Cursor     string
	Limit      int
}

// SearchPage is one page of Search, newest first.
type SearchPage struct {
	Items      []Highlight
	NextCursor string
}

// Search lists the user's highlights across documents outside the trash.
func (s *Service) Search(ctx context.Context, scope tenant.Scope, userID int64, q SearchQuery) (SearchPage, error) {
	limit := q.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	vis, vargs := privacy.Visible("d", scope)
	where := []string{"h.user_id = ?", "h.workspace_id = ?", "d.deleted_at IS NULL", vis}
	args := append([]any{userID, scope.WorkspaceID()}, vargs...)
	if t := strings.TrimSpace(q.Text); t != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.ToLower(t)) + "%"
		where = append(where, `(lower(h.quote) LIKE ? ESCAPE '\' OR lower(h.note) LIKE ? ESCAPE '\')`)
		args = append(args, like, like)
	}
	if q.Color != "" {
		if !slices.Contains(Colors, q.Color) {
			return SearchPage{}, errColor
		}
		where = append(where, "h.color = ?")
		args = append(args, q.Color)
	}
	if q.DocumentID != 0 {
		where = append(where, "h.document_id = ?")
		args = append(args, q.DocumentID)
	}
	if q.Cursor != "" {
		var before int64
		if err := json.Unmarshal([]byte(q.Cursor), &before); err != nil {
			return SearchPage{}, apperr.New(apperr.KindInvalid, "cursor 不正确")
		}
		where = append(where, "h.id < ?")
		args = append(args, before)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT h.id, h.document_id, d.title, h.locator, h.quote, h.note, h.color, h.created_at, h.updated_at
		FROM highlights h JOIN documents d ON d.id = h.document_id WHERE `+strings.Join(where, " AND ")+` ORDER BY h.id DESC LIMIT `+strconv.Itoa(limit+1), args...)
	if err != nil {
		return SearchPage{}, err
	}
	defer rows.Close()
	page := SearchPage{Items: []Highlight{}}
	for rows.Next() {
		var h Highlight
		var created, updated int64
		if err := rows.Scan(&h.ID, &h.DocumentID, &h.DocumentTitle, &h.Locator, &h.Quote, &h.Note, &h.Color, &created, &updated); err != nil {
			return SearchPage{}, err
		}
		h.CreatedAt, h.UpdatedAt = time.UnixMilli(created), time.UnixMilli(updated)
		page.Items = append(page.Items, h)
	}
	if err := rows.Err(); err != nil {
		return SearchPage{}, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextCursor = strconv.FormatInt(page.Items[limit-1].ID, 10)
	}
	return page, nil
}
