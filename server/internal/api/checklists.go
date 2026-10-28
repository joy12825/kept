package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"kept/server/internal/api/gen"
	"kept/server/internal/apperr"
	"kept/server/internal/auth"
	"kept/server/internal/checklist"
	"kept/server/internal/filter"
	"kept/server/internal/ingest"
	"kept/server/internal/tenant"
)

// Checklists of the papers an errand needs (v1.6.2).

func toAPIChecklistItem(it checklist.Item) gen.ChecklistItem {
	id, count := it.ID, it.Count
	out := gen.ChecklistItem{Id: &id, Label: it.Label, Count: &count, Filter: *condMap(&it.Filter)}
	if it.Note != "" {
		out.Note = &it.Note
	}
	return out
}

func toAPIChecklist(c checklist.Checklist) gen.Checklist {
	out := gen.Checklist{Id: c.ID, Name: c.Name, Note: c.Note, Items: make([]gen.ChecklistItem, len(c.Items)), UpdatedAt: c.UpdatedAt.UTC()}
	for i, it := range c.Items {
		out.Items[i] = toAPIChecklistItem(it)
	}
	return out
}

func checklistInput(b gen.ChecklistInput) (checklist.Input, error) {
	in := checklist.Input{Name: b.Name, Items: make([]checklist.Item, len(b.Items))}
	if b.Note != nil {
		in.Note = *b.Note
	}
	for i, it := range b.Items {
		raw, _ := json.Marshal(it.Filter)
		f, err := filter.Parse(raw)
		if err != nil {
			return in, err
		}
		item := checklist.Item{Label: it.Label, Filter: f}
		if it.Id != nil {
			item.ID = *it.Id
		}
		if it.Count != nil {
			item.Count = *it.Count
		}
		if it.Note != nil {
			item.Note = *it.Note
		}
		in.Items[i] = item
	}
	return in, nil
}

func (s *Server) ListChecklists(ctx context.Context, _ gen.ListChecklistsRequestObject) (gen.ListChecklistsResponseObject, error) {
	list, err := s.checklists.List(ctx, tenant.MustFrom(ctx))
	if err != nil {
		return nil, err
	}
	out := make(gen.ListChecklists200JSONResponse, len(list))
	for i, c := range list {
		out[i] = toAPIChecklist(c)
	}
	return out, nil
}

func (s *Server) CreateChecklist(ctx context.Context, req gen.CreateChecklistRequestObject) (gen.CreateChecklistResponseObject, error) {
	in, err := checklistInput(*req.Body)
	if err != nil {
		return nil, err
	}
	c, err := s.checklists.Create(ctx, tenant.MustFrom(ctx), in)
	if err != nil {
		return nil, err
	}
	return gen.CreateChecklist201JSONResponse(toAPIChecklist(c)), nil
}

func (s *Server) GetChecklist(ctx context.Context, req gen.GetChecklistRequestObject) (gen.GetChecklistResponseObject, error) {
	c, err := s.checklists.Get(ctx, tenant.MustFrom(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	return gen.GetChecklist200JSONResponse(toAPIChecklist(c)), nil
}

func (s *Server) UpdateChecklist(ctx context.Context, req gen.UpdateChecklistRequestObject) (gen.UpdateChecklistResponseObject, error) {
	in, err := checklistInput(*req.Body)
	if err != nil {
		return nil, err
	}
	c, err := s.checklists.Update(ctx, tenant.MustFrom(ctx), req.Id, in)
	if err != nil {
		return nil, err
	}
	return gen.UpdateChecklist200JSONResponse(toAPIChecklist(c)), nil
}

func (s *Server) DeleteChecklist(ctx context.Context, req gen.DeleteChecklistRequestObject) (gen.DeleteChecklistResponseObject, error) {
	if err := s.checklists.Delete(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	return gen.DeleteChecklist204Response{}, nil
}

func (s *Server) CheckChecklist(ctx context.Context, req gen.CheckChecklistRequestObject) (gen.CheckChecklistResponseObject, error) {
	c, items, err := s.checklists.Check(ctx, tenant.MustFrom(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	out := gen.ChecklistCheck{Checklist: toAPIChecklist(c), Items: make([]gen.ChecklistCheckItem, len(items))}
	for i, r := range items {
		docs := make([]gen.DocumentSummary, len(r.Documents))
		for j, d := range r.Documents {
			docs[j] = toAPISummary(d)
		}
		it := gen.ChecklistCheckItem{Id: r.ID, Label: r.Label, Count: r.Count, Status: gen.ChecklistCheckItemStatus(r.Status), Documents: docs, ExpiredIds: r.Expired, AttachedIds: r.Attached}
		if r.Note != "" {
			it.Note = &r.Note
		}
		out.Items[i] = it
	}
	return gen.CheckChecklist200JSONResponse(out), nil
}

// Documents put into an item by hand (v1.6.7).

func (s *Server) AttachChecklistDocument(ctx context.Context, req gen.AttachChecklistDocumentRequestObject) (gen.AttachChecklistDocumentResponseObject, error) {
	if err := s.checklists.Attach(ctx, tenant.MustFrom(ctx), req.Id, req.ItemId, req.Body.DocumentId); err != nil {
		return nil, err
	}
	return gen.AttachChecklistDocument204Response{}, nil
}

func (s *Server) DetachChecklistDocument(ctx context.Context, req gen.DetachChecklistDocumentRequestObject) (gen.DetachChecklistDocumentResponseObject, error) {
	if err := s.checklists.Detach(ctx, tenant.MustFrom(ctx), req.Id, req.ItemId, req.DocumentId); err != nil {
		return nil, err
	}
	return gen.DetachChecklistDocument204Response{}, nil
}

// UploadToChecklistItem ingests a file as an upload from the web would,
// putting the new document into the item; a file the workspace has
// already is put in instead (outside the trash).
func (s *Server) UploadToChecklistItem(ctx context.Context, req gen.UploadToChecklistItemRequestObject) (gen.UploadToChecklistItemResponseObject, error) {
	scope := tenant.MustFrom(ctx)
	if err := s.checklists.CheckItem(ctx, scope, req.Id, req.ItemId); err != nil {
		return nil, err
	}
	up := ingest.Upload{Source: ingest.SourceWeb}
	if mustPrincipal(ctx).Via == auth.ViaToken {
		up.Source = ingest.SourceAPI
	}
	ov := ingest.Overrides{Checklist: &ingest.ChecklistItem{ID: req.Id, ItemID: req.ItemId}}
	for {
		part, err := req.Body.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, errNoFile
		}
		if err != nil {
			return nil, invalid("上传的内容格式不正确")
		}
		if part.FormName() == "title" { // v1.6.8: e.g. the item's name for a photo called IMG_1234
			v, err := readField(part, 1024)
			if err != nil {
				return nil, invalid("预设的标题不正确")
			}
			ov.Title = strings.TrimSpace(v)
			continue
		}
		if part.FormName() != "file" {
			continue
		}
		jobID, err := s.ingest.Submit(ctx, scope, part.FileName(), part, ov, up)
		var ae *apperr.Error
		if errors.As(err, &ae) && ae.Code == "duplicate" && ae.Details["inTrash"] == nil {
			if id, ok := ae.Details["documentId"].(int64); ok {
				if err := s.checklists.Attach(ctx, scope, req.Id, req.ItemId, id); err != nil {
					return nil, err
				}
				return gen.UploadToChecklistItem200JSONResponse{DocumentId: id}, nil
			}
		}
		if err != nil {
			return nil, err
		}
		return gen.UploadToChecklistItem202JSONResponse{JobId: jobID}, nil
	}
}
