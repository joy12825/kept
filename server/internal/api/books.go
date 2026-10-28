package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"kept/server/internal/files"
	"net"
	"strings"

	"kept/server/internal/api/gen"
	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/bookmeta"
	"kept/server/internal/books"
	"kept/server/internal/i18n"
	"kept/server/internal/reading"
	"kept/server/internal/tenant"
)

var errNotBook = apperr.New(apperr.KindNotFound, "这份文档不是书籍")

func toAPIBook(b books.Book) gen.Book {
	return gen.Book{DocumentId: b.DocumentID, Isbn: b.ISBN, Authors: b.Authors, Publisher: b.Publisher, Series: b.Series,
		Edition: b.Edition, Published: b.Published, Pages: b.Pages, Language: b.Language, Description: b.Description,
		HasCover: b.HasCover, UpdatedAt: b.UpdatedAt}
}

func (s *Server) GetDocumentBook(ctx context.Context, req gen.GetDocumentBookRequestObject) (gen.GetDocumentBookResponseObject, error) {
	scope := tenant.MustFrom(ctx)
	if _, err := s.documents.Get(ctx, scope, req.Id); err != nil {
		return nil, err
	}
	b, err := s.books.Get(ctx, scope, req.Id)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, errNotBook
	}
	return gen.GetDocumentBook200JSONResponse(toAPIBook(*b)), nil
}

func (s *Server) SetDocumentBook(ctx context.Context, req gen.SetDocumentBookRequestObject) (gen.SetDocumentBookResponseObject, error) {
	b := req.Body
	in := books.Input{ISBN: deref(b.Isbn), Publisher: deref(b.Publisher), Series: deref(b.Series), Edition: deref(b.Edition),
		Published: deref(b.Published), Pages: derefInt(b.Pages), Language: deref(b.Language), Description: deref(b.Description)}
	if b.Authors != nil {
		in.Authors = *b.Authors
	}
	out, err := s.books.Set(ctx, tenant.MustFrom(ctx), req.Id, in)
	if err != nil {
		return nil, err
	}
	return gen.SetDocumentBook200JSONResponse(toAPIBook(out)), nil
}

func (s *Server) DeleteDocumentBook(ctx context.Context, req gen.DeleteDocumentBookRequestObject) (gen.DeleteDocumentBookResponseObject, error) {
	if err := s.books.Delete(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	return gen.DeleteDocumentBook204Response{}, nil
}

func (s *Server) EnableLibrary(ctx context.Context, _ gen.EnableLibraryRequestObject) (gen.EnableLibraryResponseObject, error) {
	scope := tenant.MustFrom(ctx)
	d, job, err := s.drawers.EnableLibrary(ctx, scope)
	if err != nil {
		return nil, err
	}
	out, err := s.savedDrawer(ctx, scope, d, job)
	if err != nil {
		return nil, err
	}
	return gen.EnableLibrary200JSONResponse(out), nil
}

func (s *Server) DisableLibrary(ctx context.Context, _ gen.DisableLibraryRequestObject) (gen.DisableLibraryResponseObject, error) {
	job, err := s.drawers.DisableLibrary(ctx, tenant.MustFrom(ctx))
	if err != nil {
		return nil, err
	}
	return gen.DisableLibrary200JSONResponse(jobStarted(job)), nil
}

var (
	errLookupOff = apperr.New(apperr.KindUnavailable, "这台服务器没有开启在线查找书目（KEPT_BOOK_SOURCES）")
	errCoverGone = apperr.New(apperr.KindNotFound, "这个数据源没有这本书的封面")
)

// lookupFailure explains why a source could not be asked, in Chinese
// (translated where it is sent, v1.10.1).
func lookupFailure(source string, err error) string {
	var ne net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()):
		return i18n.N("没有及时回应")
	case strings.Contains(err.Error(), "HTTP 429"):
		if source == "googlebooks" {
			return i18n.N("请求太频繁或超出了免费配额，可以配置 KEPT_GOOGLE_BOOKS_KEY")
		}
		return i18n.N("请求太频繁，稍后再试")
	case strings.HasPrefix(err.Error(), "HTTP "):
		return fmt.Sprintf(i18n.N("返回了错误（%s）"), err.Error())
	case strings.Contains(err.Error(), "not the expected JSON"):
		return i18n.N("返回的内容无法识别")
	}
	return i18n.N("连不上，请检查服务器能否访问外网（或配置 HTTPS_PROXY）")
}

func (s *Server) LookupBook(ctx context.Context, req gen.LookupBookRequestObject) (gen.LookupBookResponseObject, error) {
	if !s.bookLookup.Enabled() {
		return nil, errLookupOff
	}
	isbn, err := books.NormalizeISBN(req.Params.Isbn)
	if err != nil {
		return nil, err
	}
	if isbn == "" {
		return nil, invalid("请输入 ISBN")
	}
	out := gen.LookupBook200JSONResponse{Isbn: isbn, Results: []gen.BookLookupResult{}, Failures: []struct {
		Message    string         `json:"message"`
		Source     gen.BookSource `json:"source"`
		SourceName string         `json:"sourceName"`
	}{}}
	for _, r := range s.bookLookup.Lookup(ctx, isbn) {
		src := gen.BookSource(r.Source)
		switch {
		case r.Err != nil && !errors.Is(r.Err, bookmeta.ErrNotFound):
			s.log.WarnContext(ctx, "book lookup failed", "source", r.Source, "err", r.Err)
			out.Failures = append(out.Failures, struct {
				Message    string         `json:"message"`
				Source     gen.BookSource `json:"source"`
				SourceName string         `json:"sourceName"`
			}{Message: i18n.T(ctx, lookupFailure(r.Source, r.Err)), Source: src, SourceName: i18n.T(ctx, bookmeta.Label(r.Source))})
		case r.Meta != nil:
			m := r.Meta
			title := m.Title
			if m.Subtitle != "" {
				title += "：" + m.Subtitle
			}
			authors := m.Authors
			if authors == nil {
				authors = []string{}
			}
			out.Results = append(out.Results, gen.BookLookupResult{Source: src, SourceName: i18n.T(ctx, bookmeta.Label(r.Source)), Title: title, Authors: authors,
				Publisher: m.Publisher, Published: m.Published, Pages: m.Pages, Language: i18n.T(ctx, m.Language), Description: m.Description, Series: m.Series,
				HasCover: m.CoverURL != ""})
		}
	}
	return out, nil
}

func (s *Server) bookResponse(ctx context.Context, id int64) (gen.Book, error) {
	b, err := s.books.Get(ctx, tenant.MustFrom(ctx), id)
	if err != nil {
		return gen.Book{}, err
	}
	if b == nil {
		return gen.Book{}, errNotBook
	}
	return toAPIBook(*b), nil
}

func (s *Server) SetBookCover(ctx context.Context, req gen.SetBookCoverRequestObject) (gen.SetBookCoverResponseObject, error) {
	data, err := io.ReadAll(io.LimitReader(req.Body, books.MaxCoverSize+1))
	if err != nil {
		return nil, err
	}
	if err := s.books.SaveCover(ctx, tenant.MustFrom(ctx), req.Id, data); err != nil {
		return nil, err
	}
	b, err := s.bookResponse(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return gen.SetBookCover200JSONResponse(b), nil
}

func (s *Server) DeleteBookCover(ctx context.Context, req gen.DeleteBookCoverRequestObject) (gen.DeleteBookCoverResponseObject, error) {
	if err := s.books.DeleteCover(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	b, err := s.bookResponse(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return gen.DeleteBookCover200JSONResponse(b), nil
}

func (s *Server) FetchBookCover(ctx context.Context, req gen.FetchBookCoverRequestObject) (gen.FetchBookCoverResponseObject, error) {
	if !s.bookLookup.Enabled() {
		return nil, errLookupOff
	}
	scope := tenant.MustFrom(ctx)
	isbn, err := books.NormalizeISBN(req.Body.Isbn)
	if err != nil {
		return nil, err
	}
	// The document must be a book before anything is downloaded.
	if _, err := s.bookResponse(ctx, req.Id); err != nil {
		return nil, err
	}
	data, err := s.bookLookup.Cover(ctx, isbn, string(req.Body.Source), books.MaxCoverSize)
	if errors.Is(err, bookmeta.ErrNotFound) {
		return nil, errCoverGone
	}
	if err != nil {
		s.log.WarnContext(ctx, "cover download failed", "source", req.Body.Source, "err", err)
		return nil, apperr.New(apperr.KindUnavailable, fmt.Sprintf("%s的封面下载失败：%s", bookmeta.Label(string(req.Body.Source)), lookupFailure(string(req.Body.Source), err)))
	}
	if err := s.books.SaveCover(ctx, scope, req.Id, data); err != nil {
		return nil, err
	}
	b, err := s.bookResponse(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return gen.FetchBookCover200JSONResponse(b), nil
}

func (s *Server) GetDocumentCover(ctx context.Context, req gen.GetDocumentCoverRequestObject) (gen.GetDocumentCoverResponseObject, error) {
	path, err := s.books.CoverPath(ctx, tenant.MustFrom(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	return s.serveFile(ctx, files.LocalFile(path), "image/jpeg", "", false)
}

func toAPIReading(p reading.Progress) gen.ReadingProgress {
	return gen.ReadingProgress{Position: p.Position, Percent: p.Percent, Status: gen.ReadingStatus(p.Status), Rating: p.Rating,
		StartedOn: p.StartedOn, FinishedOn: p.FinishedOn, HighlightCount: p.Highlights, UpdatedAt: p.UpdatedAt}
}

func (s *Server) GetReadingProgress(ctx context.Context, req gen.GetReadingProgressRequestObject) (gen.GetReadingProgressResponseObject, error) {
	p, err := s.reading.Get(ctx, tenant.MustFrom(ctx), audit.ActorFrom(ctx).UserID, req.Id)
	if err != nil {
		return nil, err
	}
	return gen.GetReadingProgress200JSONResponse(toAPIReading(p)), nil
}

func (s *Server) SaveReadingProgress(ctx context.Context, req gen.SaveReadingProgressRequestObject) (gen.SaveReadingProgressResponseObject, error) {
	p, err := s.reading.Save(ctx, tenant.MustFrom(ctx), audit.ActorFrom(ctx).UserID, req.Id, req.Body.Position, req.Body.Percent)
	if err != nil {
		return nil, err
	}
	return gen.SaveReadingProgress200JSONResponse(toAPIReading(p)), nil
}

func toAPIHighlight(h reading.Highlight) gen.Highlight {
	return gen.Highlight{Id: h.ID, DocumentId: h.DocumentID, DocumentTitle: strPtr(h.DocumentTitle), Locator: h.Locator, Quote: h.Quote,
		Note: h.Note, Color: gen.HighlightColor(h.Color), CreatedAt: h.CreatedAt, UpdatedAt: h.UpdatedAt}
}

func (s *Server) SetReadingState(ctx context.Context, req gen.SetReadingStateRequestObject) (gen.SetReadingStateResponseObject, error) {
	b := req.Body
	p := reading.StatePatch{Rating: b.Rating, StartedOn: b.StartedOn, FinishedOn: b.FinishedOn}
	if b.Status != nil {
		st := string(*b.Status)
		p.Status = &st
	}
	out, err := s.reading.SetState(ctx, tenant.MustFrom(ctx), audit.ActorFrom(ctx).UserID, req.Id, p)
	if err != nil {
		return nil, err
	}
	return gen.SetReadingState200JSONResponse(toAPIReading(out)), nil
}

func (s *Server) ListDocumentHighlights(ctx context.Context, req gen.ListDocumentHighlightsRequestObject) (gen.ListDocumentHighlightsResponseObject, error) {
	list, err := s.reading.Highlights(ctx, tenant.MustFrom(ctx), audit.ActorFrom(ctx).UserID, req.Id)
	if err != nil {
		return nil, err
	}
	out := gen.ListDocumentHighlights200JSONResponse{}
	for _, h := range list {
		out = append(out, toAPIHighlight(h))
	}
	return out, nil
}

func (s *Server) CreateHighlight(ctx context.Context, req gen.CreateHighlightRequestObject) (gen.CreateHighlightResponseObject, error) {
	b := req.Body
	in := reading.HighlightInput{Locator: b.Locator, Quote: deref(b.Quote), Note: deref(b.Note)}
	if b.Color != nil {
		in.Color = string(*b.Color)
	}
	h, err := s.reading.AddHighlight(ctx, tenant.MustFrom(ctx), audit.ActorFrom(ctx).UserID, req.Id, in)
	if err != nil {
		return nil, err
	}
	return gen.CreateHighlight201JSONResponse(toAPIHighlight(h)), nil
}

func (s *Server) UpdateHighlight(ctx context.Context, req gen.UpdateHighlightRequestObject) (gen.UpdateHighlightResponseObject, error) {
	var color *string
	if req.Body.Color != nil {
		c := string(*req.Body.Color)
		color = &c
	}
	h, err := s.reading.UpdateHighlight(ctx, tenant.MustFrom(ctx), audit.ActorFrom(ctx).UserID, req.Id, req.Body.Note, color)
	if err != nil {
		return nil, err
	}
	return gen.UpdateHighlight200JSONResponse(toAPIHighlight(h)), nil
}

func (s *Server) DeleteHighlight(ctx context.Context, req gen.DeleteHighlightRequestObject) (gen.DeleteHighlightResponseObject, error) {
	if err := s.reading.DeleteHighlight(ctx, tenant.MustFrom(ctx), audit.ActorFrom(ctx).UserID, req.Id); err != nil {
		return nil, err
	}
	return gen.DeleteHighlight204Response{}, nil
}

func (s *Server) SearchHighlights(ctx context.Context, req gen.SearchHighlightsRequestObject) (gen.SearchHighlightsResponseObject, error) {
	p := req.Params
	q := reading.SearchQuery{Text: deref(p.Q), DocumentID: derefID(p.DocumentId), Cursor: deref(p.Cursor), Limit: derefInt(p.Limit)}
	if p.Color != nil {
		q.Color = string(*p.Color)
	}
	page, err := s.reading.Search(ctx, tenant.MustFrom(ctx), audit.ActorFrom(ctx).UserID, q)
	if err != nil {
		return nil, err
	}
	out := gen.SearchHighlights200JSONResponse{Items: make([]gen.Highlight, len(page.Items)), NextCursor: strPtr(page.NextCursor)}
	for i, h := range page.Items {
		out.Items[i] = toAPIHighlight(h)
	}
	return out, nil
}
