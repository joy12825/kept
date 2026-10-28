package api

import (
	"context"
	"encoding/json"
	"kept/server/internal/edition"
	"kept/server/internal/files"
	"slices"
	"strconv"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"kept/server/internal/api/gen"
	"kept/server/internal/document"
	"kept/server/internal/filter"
	"kept/server/internal/pages"
	"kept/server/internal/tenant"
)

// ---------- list, facets, details ----------

func (s *Server) ListDocuments(ctx context.Context, req gen.ListDocumentsRequestObject) (gen.ListDocumentsResponseObject, error) {
	p := req.Params
	f, err := filterParam(p.Filter)
	if err != nil {
		return nil, err
	}
	q := document.Query{Filter: f, Desc: p.Dir == nil || *p.Dir == gen.ListDocumentsParamsDirDesc, Cursor: derefCursor(p.Cursor)}
	if p.Sort != nil {
		q.Sort = string(*p.Sort)
	}
	if p.Limit != nil {
		q.Limit = *p.Limit
	}
	if p.Fields != nil {
		for _, f := range strings.Split(*p.Fields, ",") {
			if id, err := strconv.ParseInt(f, 10, 64); err == nil {
				q.Fields = append(q.Fields, id)
			}
		}
	}
	page, err := s.documents.List(ctx, tenant.MustFrom(ctx), q)
	if err != nil {
		return nil, err
	}
	out := gen.DocumentList{Items: make([]gen.DocumentSummary, len(page.Items)), Total: int(page.Total)}
	if page.Truncated {
		out.Truncated = &page.Truncated
	}
	for i, it := range page.Items {
		out.Items[i] = toAPISummary(it)
	}
	if page.NextCursor != "" {
		out.NextCursor = &page.NextCursor
	}
	return gen.ListDocuments200JSONResponse(out), nil
}

func (s *Server) GetDocumentFacets(ctx context.Context, req gen.GetDocumentFacetsRequestObject) (gen.GetDocumentFacetsResponseObject, error) {
	f, err := filterParam(req.Params.Filter)
	if err != nil {
		return nil, err
	}
	var groups []string
	for _, g := range strings.Split(req.Params.Groups, ",") {
		if g = strings.TrimSpace(g); g != "" {
			groups = append(groups, g)
		}
	}
	facets, err := s.documents.Facets(ctx, tenant.MustFrom(ctx), f, groups)
	if err != nil {
		return nil, err
	}
	out := gen.GetDocumentFacets200JSONResponse{}
	for g, buckets := range facets {
		bs := make([]gen.FacetBucket, len(buckets))
		for i, b := range buckets {
			bs[i] = gen.FacetBucket{Key: b.Key, Count: b.Count}
		}
		out[g] = bs
	}
	return out, nil
}

func (s *Server) NextDocumentASN(ctx context.Context, _ gen.NextDocumentASNRequestObject) (gen.NextDocumentASNResponseObject, error) {
	n, err := s.documents.NextASN(ctx, tenant.MustFrom(ctx))
	if err != nil {
		return nil, err
	}
	return gen.NextDocumentASN200JSONResponse{Asn: n}, nil
}

func (s *Server) GetDocument(ctx context.Context, req gen.GetDocumentRequestObject) (gen.GetDocumentResponseObject, error) {
	d, err := s.documents.GetDetail(ctx, tenant.MustFrom(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	return gen.GetDocument200JSONResponse(s.lockSensitive(ctx, s.withRuleDrawer(ctx, s.toAPIDocument(ctx, d)))), nil
}

// ---------- changes ----------

func (s *Server) UpdateDocument(ctx context.Context, req gen.UpdateDocumentRequestObject) (gen.UpdateDocumentResponseObject, error) {
	b := req.Body
	// making one private is the paid edition's; making it ordinary again is not (v1.12)
	if b.Private != nil && *b.Private {
		if err := s.requireEdition(edition.Pro); err != nil {
			return nil, err
		}
	}
	p := document.Patch{
		Title: b.Title, CreatedDate: b.CreatedDate, TypeID: b.DocumentTypeId, CorrespondentID: b.CorrespondentId, PersonID: b.PersonId, LocationID: b.LocationId,
		TagIDs: b.TagIds, ASN: b.Asn, Inbox: b.Inbox, ExpectedModifiedAt: b.ModifiedAt,
		DrawerID: b.DrawerId, DrawerAuto: b.DrawerAuto != nil && *b.DrawerAuto, Milestone: b.IsMilestone, Sensitive: b.Sensitive, Private: b.Private, Content: b.Content,
	}
	fields, err := fieldInputs(b.CustomFields)
	if err != nil {
		return nil, err
	}
	p.CustomFields = fields
	d, err := s.documents.Update(ctx, tenant.MustFrom(ctx), req.Id, p)
	if err != nil {
		return nil, err
	}
	return gen.UpdateDocument200JSONResponse(s.lockSensitive(ctx, s.withRuleDrawer(ctx, s.toAPIDocument(ctx, d)))), nil
}

func (s *Server) DeleteDocument(ctx context.Context, req gen.DeleteDocumentRequestObject) (gen.DeleteDocumentResponseObject, error) {
	n, err := s.documents.Delete(ctx, tenant.MustFrom(ctx), document.Selection{IDs: []int64{req.Id}})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, document.ErrNotFound
	}
	return gen.DeleteDocument204Response{}, nil
}

func (s *Server) RestoreDocument(ctx context.Context, req gen.RestoreDocumentRequestObject) (gen.RestoreDocumentResponseObject, error) {
	if err := s.documents.Restore(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	return gen.RestoreDocument204Response{}, nil
}

func (s *Server) BulkDocuments(ctx context.Context, req gen.BulkDocumentsRequestObject) (gen.BulkDocumentsResponseObject, error) {
	b := req.Body
	sel, err := selection(b.Ids, b.Filter)
	if err != nil {
		return nil, err
	}
	ops := 0
	for _, on := range []bool{b.Edit != nil, b.Delete != nil && *b.Delete, b.Reprocess != nil && *b.Reprocess, b.Classify != nil} {
		if on {
			ops++
		}
	}
	if ops != 1 {
		return nil, invalid("请指定一种操作：edit、delete、reprocess 或 classify")
	}
	scope := tenant.MustFrom(ctx)
	switch {
	case b.Edit != nil:
		e := b.Edit
		fields, err := fieldInputs(e.CustomFields)
		if err != nil {
			return nil, err
		}
		n, err := s.documents.Bulk(ctx, scope, sel, document.BulkEdit{
			TypeID: e.DocumentTypeId, CorrespondentID: e.CorrespondentId, PersonID: e.PersonId, LocationID: e.LocationId, AddTagIDs: derefIDs(e.AddTagIds), RemoveTagIDs: derefIDs(e.RemoveTagIds),
			CustomFields: fields, Inbox: e.Inbox, DrawerID: e.DrawerId,
		})
		if err != nil {
			return nil, err
		}
		return gen.BulkDocuments200JSONResponse{Count: n}, nil
	case b.Delete != nil && *b.Delete:
		n, err := s.documents.Delete(ctx, scope, sel)
		if err != nil {
			return nil, err
		}
		return gen.BulkDocuments200JSONResponse{Count: n}, nil
	}
	ids, err := s.documents.Resolve(ctx, scope, sel)
	if err != nil {
		return nil, err
	}
	if b.Classify != nil {
		if len(ids) == 0 {
			return gen.BulkDocuments200JSONResponse{Count: 0, JobIds: &[]int64{}}, nil
		}
		jid, err := s.classifier.EnqueueApply(ctx, scope, ids, string(b.Classify.Mode))
		if err != nil {
			return nil, err
		}
		return gen.BulkDocuments200JSONResponse{Count: len(ids), JobIds: &[]int64{jid}}, nil
	}
	jobIDs := make([]int64, 0, len(ids))
	for _, id := range ids {
		jid, err := s.ingest.Reprocess(ctx, scope, id)
		if err != nil {
			return nil, err
		}
		jobIDs = append(jobIDs, jid)
	}
	return gen.BulkDocuments200JSONResponse{Count: len(ids), JobIds: &jobIDs}, nil
}

func (s *Server) GetDocumentHistory(ctx context.Context, req gen.GetDocumentHistoryRequestObject) (gen.GetDocumentHistoryResponseObject, error) {
	scope := tenant.MustFrom(ctx)
	if _, err := s.documents.Get(ctx, scope, req.Id); err != nil {
		return nil, err
	}
	f, err := auditFilter(nil, nil, nil, nil, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	entries, next, err := s.audit.ListTarget(ctx, scope, "document", strconv.FormatInt(req.Id, 10), f)
	if err != nil {
		return nil, err
	}
	return gen.GetDocumentHistory200JSONResponse(auditPageIn(ctx, entries, next)), nil
}

// ---------- conversions ----------

func filterParam(p *gen.Filter) (filter.Cond, error) {
	if p == nil {
		return filter.Cond{}, nil
	}
	return filter.ParseString(string(*p))
}

// selection reads the ids-or-filter of a bulk request.
func selection(ids *[]int64, f *map[string]any) (document.Selection, error) {
	if (ids == nil || len(*ids) == 0) == (f == nil) {
		return document.Selection{}, invalid("请用 ids 或 filter 之一选择文档")
	}
	if f == nil {
		return document.Selection{IDs: *ids}, nil
	}
	raw, err := json.Marshal(*f)
	if err != nil {
		return document.Selection{}, err
	}
	c, err := filter.Parse(raw)
	if err != nil {
		return document.Selection{}, err
	}
	return document.Selection{Filter: &c}, nil
}

func fieldInputs(in *[]gen.FieldValue) ([]document.FieldInput, error) {
	if in == nil {
		return nil, nil
	}
	out := make([]document.FieldInput, len(*in))
	for i, f := range *in {
		raw, err := json.Marshal(f.Value)
		if err != nil {
			return nil, invalid("自定义字段的值不正确")
		}
		out[i] = document.FieldInput{FieldID: f.FieldId, Value: raw}
	}
	return out, nil
}

func toAPISummary(d document.Summary) gen.DocumentSummary {
	out := gen.DocumentSummary{
		Id: d.ID, Title: d.Title, CreatedDate: *apiDate(d.CreatedDate), AddedAt: d.AddedAt, ModifiedAt: d.ModifiedAt,
		MimeType: d.MimeType, OriginalFilename: d.OriginalFilename, PageCount: d.PageCount, HasArchive: d.HasArchive,
		HasThumbnail: d.HasThumbnail, Inbox: d.Inbox, TagIds: d.TagIDs, NoteCount: d.NoteCount, Asn: d.ASN,
		CorrespondentId: idPtr(d.CorrespondentID), DocumentTypeId: idPtr(d.DocumentTypeID),
		PersonId: idPtr(d.PersonID), LocationId: idPtr(d.LocationID),
		Sensitive: optTrue(d.Sensitive), Private: optTrue(d.Private),
		DrawerId: idPtr(d.DrawerID), DrawerManual: d.DrawerManual, IsMilestone: d.IsMilestone, IsBook: d.IsBook,
	}
	if d.HasCover {
		out.HasCover = &d.HasCover
	}
	if len(d.FieldValues) > 0 {
		fv := make([]gen.FieldValue, len(d.FieldValues))
		for i, f := range d.FieldValues {
			fv[i] = gen.FieldValue{FieldId: f.FieldID, Value: f.Value}
		}
		out.FieldValues = &fv
	}
	if d.Reading != nil {
		out.Reading = &gen.ReadingSummary{Status: gen.ReadingStatus(d.Reading.Status), Percent: d.Reading.Percent}
	}
	if out.TagIds == nil {
		out.TagIds = []int64{}
	}
	if d.Amount != nil {
		a := filter.FormatMoney(*d.Amount)
		out.Amount = &a
	}
	if d.DueDate != "" {
		out.DueDate = apiDate(d.DueDate)
	}
	if len(d.Highlight) > 0 {
		segs := make([]gen.HighlightSegment, len(d.Highlight))
		for i, h := range d.Highlight {
			segs[i] = gen.HighlightSegment{Text: h.Text, Hit: h.Hit}
		}
		out.Highlight = &segs
	}
	return out
}

func (s *Server) toAPIDocument(ctx context.Context, d document.Detail) gen.Document {
	sum := toAPISummary(d.Summary)
	out := gen.Document{
		Id: sum.Id, Title: sum.Title, CreatedDate: sum.CreatedDate, AddedAt: sum.AddedAt, ModifiedAt: sum.ModifiedAt,
		CorrespondentId: sum.CorrespondentId, DocumentTypeId: sum.DocumentTypeId, PersonId: sum.PersonId, LocationId: sum.LocationId, Sensitive: sum.Sensitive, SensitiveMarked: optTrue(d.SensitiveOwn), TagIds: sum.TagIds, Asn: sum.Asn,
		Amount: sum.Amount, DueDate: sum.DueDate, NoteCount: sum.NoteCount, MimeType: sum.MimeType,
		OriginalFilename: sum.OriginalFilename, PageCount: sum.PageCount, HasArchive: sum.HasArchive,
		HasThumbnail: sum.HasThumbnail, Inbox: sum.Inbox, DrawerId: sum.DrawerId, DrawerManual: sum.DrawerManual, IsMilestone: sum.IsMilestone, IsBook: sum.IsBook, HasCover: sum.HasCover, Reading: sum.Reading, Content: d.Content, Checksum: d.Checksum,
		Fields: make([]gen.FieldValue, len(d.Fields)), FieldSources: toAPISources(ctx, d.FieldSources),
		Source: gen.DocumentSource(d.Source), Private: sum.Private,
		Privacy: &gen.DocumentPrivacy{CanEdit: d.Privacy.CanEdit, CanSetPrivate: d.Privacy.CanSetPrivate, AsUploader: d.Privacy.AsUploader},
	}
	if !out.Source.Valid() {
		out.Source = gen.DocumentSourceWeb
	}
	if d.LinkedFrom != nil {
		links := make([]gen.LinkedDocument, len(d.LinkedFrom))
		for i, l := range d.LinkedFrom {
			links[i] = gen.LinkedDocument{Id: l.ID, Title: l.Title, CreatedDate: *apiDate(l.CreatedDate), Field: l.Field}
		}
		out.LinkedFrom = &links
	}
	if d.OriginalEncrypted {
		out.OriginalEncrypted = &d.OriginalEncrypted
	}
	if s.ingest != nil && s.ingest.ConversionPending(ctx, d.WorkspaceID, d.ID) {
		yes := true
		out.ConversionPending = &yes
	}
	if d.ContentEditedAt != nil {
		t := d.ContentEditedAt.UTC()
		out.ContentEditedAt = &t
	}
	if e := pages.ParseEdits(d.PageEdits); !e.Empty() {
		pe := gen.PageEdits{}
		if len(e.Rotate) > 0 {
			rot := map[string]int{}
			for p, deg := range e.Rotate {
				rot[strconv.Itoa(p)] = deg
			}
			pe.Rotate = &rot
		}
		if len(e.Delete) > 0 {
			del := slices.Clone(e.Delete)
			pe.Delete = &del
		}
		if len(e.Order) > 0 {
			order := slices.Clone(e.Order)
			pe.Order = &order
		}
		out.PageEdits = &pe
	}
	for i, f := range d.Fields {
		out.Fields[i] = gen.FieldValue{FieldId: f.FieldID, Value: f.Value}
	}
	if st, err := s.files.Stat(ctx, d.Storage, files.Original(d.WorkspaceID, d.ID, d.OriginalExt)); err == nil {
		out.OriginalSize = &st.Size
	}
	if d.HasArchive {
		if st, err := s.files.Stat(ctx, d.Storage, files.Archive(d.WorkspaceID, d.ID)); err == nil {
			out.ArchiveSize = &st.Size
		}
	}
	return out
}

// apiDate turns a stored YYYY-MM-DD into the generated date type.
func apiDate(s string) *openapi_types.Date {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return &openapi_types.Date{}
	}
	return &openapi_types.Date{Time: t}
}

func idPtr(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}

func derefIDs(p *[]int64) []int64 {
	if p == nil {
		return nil
	}
	return *p
}

func derefCursor(c *gen.Cursor) string {
	if c == nil {
		return ""
	}
	return string(*c)
}
