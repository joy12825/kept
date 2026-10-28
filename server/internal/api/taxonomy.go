package api

import (
	"context"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"kept/server/internal/api/gen"
	"kept/server/internal/document"
	"kept/server/internal/edition"
	"kept/server/internal/taxonomy"
	"kept/server/internal/tenant"
)

// Taxonomy lives in the current workspace (X-Workspace-ID). Roles follow the
// defaults: viewers read, editors change.

// ---------- correspondents, document types and persons ----------

func (s *Server) listItems(ctx context.Context, kind taxonomy.Kind) ([]gen.TaxonomyItem, error) {
	items, err := s.taxonomy.List(ctx, tenant.MustFrom(ctx), kind)
	if err != nil {
		return nil, err
	}
	stats, err := s.documents.Stats(ctx, tenant.MustFrom(ctx))
	if err != nil {
		return nil, err
	}
	byID := stats.Types
	switch kind {
	case taxonomy.Correspondent:
		byID = stats.Correspondents
	case taxonomy.Person:
		byID = stats.Persons
	}
	out := make([]gen.TaxonomyItem, len(items))
	for i, it := range items {
		out[i] = toAPIItem(it)
		out[i].DocumentCount, out[i].LastDocumentDate = statFields(byID[it.ID])
	}
	return out, s.withPrivacy(ctx, kind, out)
}

// withPrivacy adds what v1.8.0 knows about persons (who may see their
// private documents) and types (whose documents become private).
func (s *Server) withPrivacy(ctx context.Context, kind taxonomy.Kind, items []gen.TaxonomyItem) error {
	scope := tenant.MustFrom(ctx)
	switch kind {
	case taxonomy.Person:
		people, err := s.privacy.People(ctx, scope)
		if err != nil {
			return err
		}
		for i := range items {
			if p, ok := people[items[i].Id]; ok {
				pp := toAPIPersonPrivacy(p)
				items[i].Privacy = &pp
			}
		}
	case taxonomy.DocumentType:
		types, err := s.privacy.PrivateTypes(ctx, scope.WorkspaceID())
		if err != nil {
			return err
		}
		for i := range items {
			on := types[items[i].Id]
			items[i].PrivateByDefault = &on
		}
	}
	return nil
}

// statFields renders a classification's document count and latest date.
func statFields(st document.Stat) (*int, *openapi_types.Date) {
	n := st.Count
	if st.LastDate == "" {
		return &n, nil
	}
	return &n, apiDate(st.LastDate)
}

func (s *Server) createItem(ctx context.Context, kind taxonomy.Kind, b gen.TaxonomyItemInput) (gen.TaxonomyItem, error) {
	it, err := s.taxonomy.Create(ctx, tenant.MustFrom(ctx), kind, taxonomy.Input{Name: b.Name, Rule: fromAPIRule(b.Rule)})
	return toAPIItem(it), err
}

func (s *Server) updateItem(ctx context.Context, kind taxonomy.Kind, id int64, b gen.TaxonomyItemPatch) (gen.TaxonomyItem, error) {
	scope := tenant.MustFrom(ctx)
	it, err := s.taxonomy.Update(ctx, scope, kind, id, taxonomy.Patch{Name: b.Name, Rule: fromAPIRule(b.Rule), RetentionMonths: b.RetentionMonths})
	if err != nil {
		return gen.TaxonomyItem{}, err
	}
	if kind == taxonomy.DocumentType && b.PrivateByDefault != nil {
		if err := s.privacy.SetPrivateType(ctx, nil, scope.WorkspaceID(), id, *b.PrivateByDefault); err != nil {
			return gen.TaxonomyItem{}, err
		}
	}
	out := []gen.TaxonomyItem{toAPIItem(it)}
	err = s.withPrivacy(ctx, kind, out)
	return out[0], err
}

func (s *Server) ListCorrespondents(ctx context.Context, _ gen.ListCorrespondentsRequestObject) (gen.ListCorrespondentsResponseObject, error) {
	out, err := s.listItems(ctx, taxonomy.Correspondent)
	return gen.ListCorrespondents200JSONResponse(out), err
}

func (s *Server) CreateCorrespondent(ctx context.Context, req gen.CreateCorrespondentRequestObject) (gen.CreateCorrespondentResponseObject, error) {
	it, err := s.createItem(ctx, taxonomy.Correspondent, *req.Body)
	if err != nil {
		return nil, err
	}
	return gen.CreateCorrespondent201JSONResponse(it), nil
}

func (s *Server) UpdateCorrespondent(ctx context.Context, req gen.UpdateCorrespondentRequestObject) (gen.UpdateCorrespondentResponseObject, error) {
	it, err := s.updateItem(ctx, taxonomy.Correspondent, req.Id, *req.Body)
	if err != nil {
		return nil, err
	}
	return gen.UpdateCorrespondent200JSONResponse(it), nil
}

func (s *Server) DeleteCorrespondent(ctx context.Context, req gen.DeleteCorrespondentRequestObject) (gen.DeleteCorrespondentResponseObject, error) {
	if err := s.taxonomy.Delete(ctx, tenant.MustFrom(ctx), taxonomy.Correspondent, req.Id); err != nil {
		return nil, err
	}
	return gen.DeleteCorrespondent204Response{}, nil
}

func (s *Server) ListDocumentTypes(ctx context.Context, _ gen.ListDocumentTypesRequestObject) (gen.ListDocumentTypesResponseObject, error) {
	out, err := s.listItems(ctx, taxonomy.DocumentType)
	return gen.ListDocumentTypes200JSONResponse(out), err
}

func (s *Server) CreateDocumentType(ctx context.Context, req gen.CreateDocumentTypeRequestObject) (gen.CreateDocumentTypeResponseObject, error) {
	it, err := s.createItem(ctx, taxonomy.DocumentType, *req.Body)
	if err != nil {
		return nil, err
	}
	return gen.CreateDocumentType201JSONResponse(it), nil
}

func (s *Server) UpdateDocumentType(ctx context.Context, req gen.UpdateDocumentTypeRequestObject) (gen.UpdateDocumentTypeResponseObject, error) {
	if p := req.Body.PrivateByDefault; p != nil && *p { // v1.12
		if err := s.requireEdition(edition.Pro); err != nil {
			return nil, err
		}
	}
	it, err := s.updateItem(ctx, taxonomy.DocumentType, req.Id, *req.Body)
	if err != nil {
		return nil, err
	}
	return gen.UpdateDocumentType200JSONResponse(it), nil
}

func (s *Server) DeleteDocumentType(ctx context.Context, req gen.DeleteDocumentTypeRequestObject) (gen.DeleteDocumentTypeResponseObject, error) {
	if err := s.taxonomy.Delete(ctx, tenant.MustFrom(ctx), taxonomy.DocumentType, req.Id); err != nil {
		return nil, err
	}
	return gen.DeleteDocumentType204Response{}, nil
}

// ---------- tags ----------

func (s *Server) ListTags(ctx context.Context, _ gen.ListTagsRequestObject) (gen.ListTagsResponseObject, error) {
	tags, err := s.taxonomy.ListTags(ctx, tenant.MustFrom(ctx))
	if err != nil {
		return nil, err
	}
	stats, err := s.documents.Stats(ctx, tenant.MustFrom(ctx))
	if err != nil {
		return nil, err
	}
	out := make(gen.ListTags200JSONResponse, len(tags))
	for i, t := range tags {
		out[i] = toAPITag(t)
		out[i].DocumentCount, out[i].LastDocumentDate = statFields(stats.Tags[t.ID])
	}
	return out, nil
}

func (s *Server) CreateTag(ctx context.Context, req gen.CreateTagRequestObject) (gen.CreateTagResponseObject, error) {
	b := req.Body
	t, err := s.taxonomy.CreateTag(ctx, tenant.MustFrom(ctx), taxonomy.TagInput{
		Name: b.Name, Color: deref(b.Color), ParentID: derefID(b.ParentId), Rule: fromAPIRule(b.Rule), ShowInYearbook: b.ShowInYearbook != nil && *b.ShowInYearbook,
	})
	if err != nil {
		return nil, err
	}
	return gen.CreateTag201JSONResponse(toAPITag(t)), nil
}

func (s *Server) UpdateTag(ctx context.Context, req gen.UpdateTagRequestObject) (gen.UpdateTagResponseObject, error) {
	b := req.Body
	t, err := s.taxonomy.UpdateTag(ctx, tenant.MustFrom(ctx), req.Id, taxonomy.TagPatch{
		Name: b.Name, Color: b.Color, ParentID: b.ParentId, Rule: fromAPIRule(b.Rule), ShowInYearbook: b.ShowInYearbook,
	})
	if err != nil {
		return nil, err
	}
	return gen.UpdateTag200JSONResponse(toAPITag(t)), nil
}

func (s *Server) DeleteTag(ctx context.Context, req gen.DeleteTagRequestObject) (gen.DeleteTagResponseObject, error) {
	if err := s.taxonomy.DeleteTag(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	return gen.DeleteTag204Response{}, nil
}

// ---------- custom fields ----------

func (s *Server) ListCustomFields(ctx context.Context, _ gen.ListCustomFieldsRequestObject) (gen.ListCustomFieldsResponseObject, error) {
	fields, err := s.taxonomy.ListFields(ctx, tenant.MustFrom(ctx))
	if err != nil {
		return nil, err
	}
	out := make(gen.ListCustomFields200JSONResponse, len(fields))
	for i, f := range fields {
		out[i] = toAPIField(f)
	}
	return out, nil
}

func (s *Server) CreateCustomField(ctx context.Context, req gen.CreateCustomFieldRequestObject) (gen.CreateCustomFieldResponseObject, error) {
	b := req.Body
	in := taxonomy.FieldInput{Name: b.Name, DataType: string(b.DataType)}
	if b.Extra != nil {
		in.Extra = fromAPIExtra(*b.Extra)
	}
	f, err := s.taxonomy.CreateField(ctx, tenant.MustFrom(ctx), in)
	if err != nil {
		return nil, err
	}
	return gen.CreateCustomField201JSONResponse(toAPIField(f)), nil
}

func (s *Server) UpdateCustomField(ctx context.Context, req gen.UpdateCustomFieldRequestObject) (gen.UpdateCustomFieldResponseObject, error) {
	p := taxonomy.FieldPatch{Name: req.Body.Name}
	if req.Body.Extra != nil {
		e := fromAPIExtra(*req.Body.Extra)
		p.Extra = &e
	}
	f, err := s.taxonomy.UpdateField(ctx, tenant.MustFrom(ctx), req.Id, p)
	if err != nil {
		return nil, err
	}
	return gen.UpdateCustomField200JSONResponse(toAPIField(f)), nil
}

func (s *Server) DeleteCustomField(ctx context.Context, req gen.DeleteCustomFieldRequestObject) (gen.DeleteCustomFieldResponseObject, error) {
	if err := s.taxonomy.DeleteField(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	return gen.DeleteCustomField204Response{}, nil
}

// ---------- conversions ----------

func fromAPIRule(r *gen.MatchRule) *taxonomy.MatchRule {
	if r == nil {
		return nil
	}
	return &taxonomy.MatchRule{Algorithm: string(r.Algorithm), Match: r.Match, CaseInsensitive: r.CaseInsensitive}
}

func toAPIRule(r taxonomy.MatchRule) gen.MatchRule {
	return gen.MatchRule{Algorithm: gen.MatchRuleAlgorithm(r.Algorithm), Match: r.Match, CaseInsensitive: r.CaseInsensitive}
}

func toAPIItem(it taxonomy.Item) gen.TaxonomyItem {
	out := gen.TaxonomyItem{Id: it.ID, Name: it.Name, Initial: it.Initial, SortKey: it.SortKey, Rule: toAPIRule(it.Rule)}
	if it.RetentionMonths > 0 {
		out.RetentionMonths = &it.RetentionMonths
	}
	return out
}

func toAPITag(t taxonomy.TagItem) gen.Tag {
	out := gen.Tag{Id: t.ID, Name: t.Name, Initial: t.Initial, SortKey: t.SortKey, Rule: toAPIRule(t.Rule), Color: t.Color, IsInbox: t.IsInbox, ShowInYearbook: t.ShowInYearbook}
	if t.ParentID != 0 {
		p := t.ParentID
		out.ParentId = &p
	}
	return out
}

func fromAPIExtra(e gen.CustomFieldExtra) taxonomy.FieldExtra {
	out := taxonomy.FieldExtra{Currency: deref(e.Currency), Multiline: e.Multiline != nil && *e.Multiline}
	if e.Options != nil {
		for _, o := range *e.Options {
			out.Options = append(out.Options, taxonomy.SelectOption{ID: deref(o.Id), Label: o.Label})
		}
	}
	return out
}

func toAPIField(f taxonomy.Field) gen.CustomField {
	out := gen.CustomField{Id: f.ID, Name: f.Name, Initial: f.Initial, SortKey: f.SortKey, DataType: gen.CustomFieldType(f.DataType), BuiltIn: f.BuiltIn}
	if f.Key != "" {
		k := f.Key
		out.Key = &k
	}
	if f.Extra.Currency != "" {
		c := f.Extra.Currency
		out.Extra.Currency = &c
	}
	if f.Extra.Multiline {
		out.Extra.Multiline = &f.Extra.Multiline
	}
	if len(f.Extra.Options) > 0 {
		opts := make([]gen.SelectOption, len(f.Extra.Options))
		for i, o := range f.Extra.Options {
			id := o.ID
			opts[i] = gen.SelectOption{Id: &id, Label: o.Label}
		}
		out.Extra.Options = &opts
	}
	return out
}

func derefID(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// ---------- directory in bulk (v1.5.8) ----------

var directoryKinds = map[string]taxonomy.Kind{"correspondents": taxonomy.Correspondent, "document-types": taxonomy.DocumentType, "tags": taxonomy.Tag, "persons": taxonomy.Person}

func (s *Server) BulkDeleteDirectoryItems(ctx context.Context, req gen.BulkDeleteDirectoryItemsRequestObject) (gen.BulkDeleteDirectoryItemsResponseObject, error) {
	n, err := s.taxonomy.BulkDelete(ctx, tenant.MustFrom(ctx), directoryKinds[string(req.Kind)], req.Body.Ids)
	if err != nil {
		return nil, err
	}
	return gen.BulkDeleteDirectoryItems200JSONResponse{Count: n}, nil
}

func (s *Server) MergeDirectoryItems(ctx context.Context, req gen.MergeDirectoryItemsRequestObject) (gen.MergeDirectoryItemsResponseObject, error) {
	n, err := s.taxonomy.MergeInto(ctx, tenant.MustFrom(ctx), directoryKinds[string(req.Kind)], req.Body.Keep, req.Body.Merge)
	if err != nil {
		return nil, err
	}
	return gen.MergeDirectoryItems200JSONResponse{Documents: n}, nil
}
