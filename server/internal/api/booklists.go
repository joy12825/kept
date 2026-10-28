package api

import (
	"context"

	"kept/server/internal/api/gen"
	"kept/server/internal/booklists"
	"kept/server/internal/tenant"
)

func toAPIBookListItem(it booklists.Item) gen.BookListItem {
	out := gen.BookListItem{DocumentId: it.DocumentID, Title: it.Title, Authors: it.Authors, HasCover: it.HasCover, HasThumbnail: it.HasThumbnail,
		Note: it.Note, AddedAt: it.AddedAt}
	if it.Status != "" || it.Percent > 0 {
		out.Reading = &gen.ReadingSummary{Status: gen.ReadingStatus(it.Status), Percent: it.Percent}
	}
	return out
}

func toAPIBookList(l booklists.List) gen.BookList {
	out := gen.BookList{Id: l.ID, Name: l.Name, Description: l.Description, Count: l.Count, Preview: make([]gen.BookListItem, len(l.Preview)),
		CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt}
	for i, it := range l.Preview {
		out.Preview[i] = toAPIBookListItem(it)
	}
	return out
}

func (s *Server) ListBookLists(ctx context.Context, _ gen.ListBookListsRequestObject) (gen.ListBookListsResponseObject, error) {
	lists, err := s.bookLists.Lists(ctx, tenant.MustFrom(ctx))
	if err != nil {
		return nil, err
	}
	out := gen.ListBookLists200JSONResponse{}
	for _, l := range lists {
		out = append(out, toAPIBookList(l))
	}
	return out, nil
}

func (s *Server) CreateBookList(ctx context.Context, req gen.CreateBookListRequestObject) (gen.CreateBookListResponseObject, error) {
	l, err := s.bookLists.Create(ctx, tenant.MustFrom(ctx), req.Body.Name, deref(req.Body.Description))
	if err != nil {
		return nil, err
	}
	return gen.CreateBookList201JSONResponse(toAPIBookList(l)), nil
}

func (s *Server) GetBookList(ctx context.Context, req gen.GetBookListRequestObject) (gen.GetBookListResponseObject, error) {
	l, items, err := s.bookLists.Get(ctx, tenant.MustFrom(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	b := toAPIBookList(l)
	out := gen.GetBookList200JSONResponse{Id: b.Id, Name: b.Name, Description: b.Description, Count: b.Count, Preview: b.Preview,
		CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt, Items: make([]gen.BookListItem, len(items))}
	for i, it := range items {
		out.Items[i] = toAPIBookListItem(it)
	}
	return out, nil
}

func (s *Server) UpdateBookList(ctx context.Context, req gen.UpdateBookListRequestObject) (gen.UpdateBookListResponseObject, error) {
	l, err := s.bookLists.Update(ctx, tenant.MustFrom(ctx), req.Id, req.Body.Name, req.Body.Description)
	if err != nil {
		return nil, err
	}
	return gen.UpdateBookList200JSONResponse(toAPIBookList(l)), nil
}

func (s *Server) DeleteBookList(ctx context.Context, req gen.DeleteBookListRequestObject) (gen.DeleteBookListResponseObject, error) {
	if err := s.bookLists.Delete(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	return gen.DeleteBookList204Response{}, nil
}

func (s *Server) AddBookListItem(ctx context.Context, req gen.AddBookListItemRequestObject) (gen.AddBookListItemResponseObject, error) {
	if err := s.bookLists.Add(ctx, tenant.MustFrom(ctx), req.Id, req.Body.DocumentId, deref(req.Body.Note)); err != nil {
		return nil, err
	}
	return gen.AddBookListItem204Response{}, nil
}

func (s *Server) UpdateBookListItem(ctx context.Context, req gen.UpdateBookListItemRequestObject) (gen.UpdateBookListItemResponseObject, error) {
	if err := s.bookLists.SetNote(ctx, tenant.MustFrom(ctx), req.Id, req.DocumentId, req.Body.Note); err != nil {
		return nil, err
	}
	return gen.UpdateBookListItem204Response{}, nil
}

func (s *Server) RemoveBookListItem(ctx context.Context, req gen.RemoveBookListItemRequestObject) (gen.RemoveBookListItemResponseObject, error) {
	if err := s.bookLists.Remove(ctx, tenant.MustFrom(ctx), req.Id, req.DocumentId); err != nil {
		return nil, err
	}
	return gen.RemoveBookListItem204Response{}, nil
}

func (s *Server) ReorderBookList(ctx context.Context, req gen.ReorderBookListRequestObject) (gen.ReorderBookListResponseObject, error) {
	if err := s.bookLists.Reorder(ctx, tenant.MustFrom(ctx), req.Id, req.Body.Ids); err != nil {
		return nil, err
	}
	return gen.ReorderBookList204Response{}, nil
}

func (s *Server) GetDocumentBookLists(ctx context.Context, req gen.GetDocumentBookListsRequestObject) (gen.GetDocumentBookListsResponseObject, error) {
	scope := tenant.MustFrom(ctx)
	if _, err := s.documents.Get(ctx, scope, req.Id); err != nil {
		return nil, err
	}
	ids, err := s.bookLists.ListsOf(ctx, scope, req.Id)
	if err != nil {
		return nil, err
	}
	return gen.GetDocumentBookLists200JSONResponse(ids), nil
}
