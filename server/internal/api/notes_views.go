package api

import (
	"context"
	"encoding/json"

	"kept/server/internal/api/gen"
	"kept/server/internal/notes"
	"kept/server/internal/tenant"
	"kept/server/internal/views"
)

// ---------- notes ----------

func (s *Server) ListNotes(ctx context.Context, req gen.ListNotesRequestObject) (gen.ListNotesResponseObject, error) {
	list, err := s.notes.List(ctx, tenant.MustFrom(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	out := make(gen.ListNotes200JSONResponse, len(list))
	for i, n := range list {
		out[i] = toAPINote(n)
	}
	return out, nil
}

func (s *Server) CreateNote(ctx context.Context, req gen.CreateNoteRequestObject) (gen.CreateNoteResponseObject, error) {
	n, err := s.notes.Create(ctx, tenant.MustFrom(ctx), req.Id, req.Body.Body)
	if err != nil {
		return nil, err
	}
	return gen.CreateNote201JSONResponse(toAPINote(n)), nil
}

func (s *Server) DeleteNote(ctx context.Context, req gen.DeleteNoteRequestObject) (gen.DeleteNoteResponseObject, error) {
	if err := s.notes.Delete(ctx, tenant.MustFrom(ctx), req.Id, req.NoteId); err != nil {
		return nil, err
	}
	return gen.DeleteNote204Response{}, nil
}

func toAPINote(n notes.Note) gen.Note {
	return gen.Note{Id: n.ID, Body: n.Body, AuthorId: idPtr(n.AuthorID), AuthorName: n.AuthorName, CreatedAt: n.CreatedAt}
}

// ---------- saved views ----------

func (s *Server) ListSavedViews(ctx context.Context, _ gen.ListSavedViewsRequestObject) (gen.ListSavedViewsResponseObject, error) {
	list, err := s.views.List(ctx, tenant.MustFrom(ctx))
	if err != nil {
		return nil, err
	}
	out := make(gen.ListSavedViews200JSONResponse, len(list))
	for i, v := range list {
		out[i] = toAPIView(v)
	}
	return out, nil
}

func (s *Server) CreateSavedView(ctx context.Context, req gen.CreateSavedViewRequestObject) (gen.CreateSavedViewResponseObject, error) {
	b := req.Body
	f, err := rawFilter(&b.Filter)
	if err != nil {
		return nil, err
	}
	v, err := s.views.Create(ctx, tenant.MustFrom(ctx), views.Input{Name: &b.Name, Filter: f, Sort: b.Sort, Display: fromAPIDisplay(b.Display)})
	if err != nil {
		return nil, err
	}
	return gen.CreateSavedView201JSONResponse(toAPIView(v)), nil
}

func (s *Server) UpdateSavedView(ctx context.Context, req gen.UpdateSavedViewRequestObject) (gen.UpdateSavedViewResponseObject, error) {
	b := req.Body
	in := views.Input{Name: b.Name, Sort: b.Sort, Display: fromAPIDisplay(b.Display)}
	if b.Filter != nil {
		f, err := rawFilter(b.Filter)
		if err != nil {
			return nil, err
		}
		in.Filter = f
	}
	v, err := s.views.Update(ctx, tenant.MustFrom(ctx), req.Id, in)
	if err != nil {
		return nil, err
	}
	return gen.UpdateSavedView200JSONResponse(toAPIView(v)), nil
}

func (s *Server) DeleteSavedView(ctx context.Context, req gen.DeleteSavedViewRequestObject) (gen.DeleteSavedViewResponseObject, error) {
	if err := s.views.Delete(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	return gen.DeleteSavedView204Response{}, nil
}

func rawFilter(f *map[string]any) (*json.RawMessage, error) {
	b, err := json.Marshal(*f)
	if err != nil {
		return nil, invalid("筛选条件的格式不正确")
	}
	raw := json.RawMessage(b)
	return &raw, nil
}

func toAPIView(v views.View) gen.SavedView {
	out := gen.SavedView{Id: v.ID, Name: v.Name, Sort: v.Sort, CreatedBy: idPtr(v.CreatedBy), CreatorName: v.CreatorName, CreatedAt: v.CreatedAt}
	_ = json.Unmarshal(v.Filter, &out.Filter)
	if out.Filter == nil {
		out.Filter = map[string]any{}
	}
	convert(v.Display, &out.Display)
	return out
}

func fromAPIDisplay(d *gen.ViewDisplay) *views.Display {
	if d == nil {
		return nil
	}
	var out views.Display
	convert(d, &out)
	return &out
}

func (s *Server) ReorderSavedViews(ctx context.Context, req gen.ReorderSavedViewsRequestObject) (gen.ReorderSavedViewsResponseObject, error) {
	if err := s.views.Reorder(ctx, tenant.MustFrom(ctx), req.Body.Ids); err != nil {
		return nil, err
	}
	return gen.ReorderSavedViews204Response{}, nil
}
