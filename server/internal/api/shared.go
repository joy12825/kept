package api

import (
	"context"
	"kept/server/internal/files"
	"net/http"
	"strconv"
	"strings"

	"kept/server/internal/api/gen"
	"kept/server/internal/document"
	"kept/server/internal/drawer"
	"kept/server/internal/filter"
	"kept/server/internal/taxonomy"
	"kept/server/internal/tenant"
)

// Read-only sharing of drawers (design doc 3.6). The target workspace sees
// documents of another workspace only through these operations, and each
// one checks the share (drawer.Service.Shared / SharedDocument).

func (s *Server) ListDrawerShares(ctx context.Context, req gen.ListDrawerSharesRequestObject) (gen.ListDrawerSharesResponseObject, error) {
	shares, err := s.drawers.Shares(ctx, tenant.MustFrom(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	out := make(gen.ListDrawerShares200JSONResponse, len(shares))
	for i, sh := range shares {
		out[i] = gen.DrawerShare{WorkspaceId: sh.WorkspaceID, Name: sh.Name, Kind: sh.Kind, Color: sh.Color, CreatedAt: sh.CreatedAt}
	}
	return out, nil
}

func (s *Server) UnshareDrawer(ctx context.Context, req gen.UnshareDrawerRequestObject) (gen.UnshareDrawerResponseObject, error) {
	if err := s.drawers.Unshare(ctx, tenant.MustFrom(ctx), req.Id, req.WorkspaceId); err != nil {
		return nil, err
	}
	return gen.UnshareDrawer204Response{}, nil
}

func toAPIShared(d drawer.SharedDrawer) gen.SharedDrawer {
	return gen.SharedDrawer{Id: d.ID, Name: d.Name, Icon: gen.DrawerIcon(d.Icon), Color: gen.DrawerColor(d.Color), Description: d.Description,
		DocumentCount: d.Count, SourceWorkspaceId: d.WorkspaceID, SourceWorkspaceName: d.SourceName}
}

func (s *Server) ListSharedDrawers(ctx context.Context, _ gen.ListSharedDrawersRequestObject) (gen.ListSharedDrawersResponseObject, error) {
	ds, err := s.drawers.SharedWithMe(ctx, tenant.MustFrom(ctx))
	if err != nil {
		return nil, err
	}
	out := make(gen.ListSharedDrawers200JSONResponse, len(ds))
	for i, d := range ds {
		out[i] = toAPIShared(d)
	}
	return out, nil
}

func (s *Server) GetSharedDrawer(ctx context.Context, req gen.GetSharedDrawerRequestObject) (gen.GetSharedDrawerResponseObject, error) {
	d, _, err := s.drawers.Shared(ctx, tenant.MustFrom(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	return gen.GetSharedDrawer200JSONResponse(toAPIShared(d)), nil
}

// sourceNames are the classification names of the source workspace.
type sourceNames struct {
	types, corrs, tags map[int64]string
}

func (s *Server) loadSourceNames(ctx context.Context, src tenant.Scope) (sourceNames, error) {
	n := sourceNames{types: map[int64]string{}, corrs: map[int64]string{}, tags: map[int64]string{}}
	for kind, m := range map[taxonomy.Kind]map[int64]string{taxonomy.DocumentType: n.types, taxonomy.Correspondent: n.corrs} {
		items, err := s.taxonomy.List(ctx, src, kind)
		if err != nil {
			return n, err
		}
		for _, it := range items {
			m[it.ID] = it.Name
		}
	}
	tags, err := s.taxonomy.ListTags(ctx, src)
	if err != nil {
		return n, err
	}
	for _, t := range tags {
		n.tags[t.ID] = t.Name
	}
	return n, nil
}

func (n sourceNames) summary(d document.Summary) gen.SharedDocumentSummary {
	out := gen.SharedDocumentSummary{Id: d.ID, Title: d.Title, CreatedDate: *apiDate(d.CreatedDate), ModifiedAt: d.ModifiedAt, MimeType: d.MimeType,
		PageCount: d.PageCount, HasArchive: d.HasArchive, HasThumbnail: d.HasThumbnail, TagNames: []string{},
		TypeName: strPtr(n.types[d.DocumentTypeID]), CorrespondentName: strPtr(n.corrs[d.CorrespondentID]), Sensitive: optTrue(d.Sensitive)}
	if d.Sensitive {
		out.HasThumbnail = false
	}
	for _, id := range d.TagIDs {
		if name := n.tags[id]; name != "" {
			out.TagNames = append(out.TagNames, name)
		}
	}
	if d.Amount != nil {
		a := filter.FormatMoney(*d.Amount)
		out.Amount = &a
	}
	return out
}

func (s *Server) ListSharedDocuments(ctx context.Context, req gen.ListSharedDocumentsRequestObject) (gen.ListSharedDocumentsResponseObject, error) {
	_, src, err := s.drawers.Shared(ctx, tenant.MustFrom(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	cond := filter.Cond{Field: "drawer", Op: "in", Value: []byte("[" + strconv.FormatInt(req.Id, 10) + "]")}
	out, err := s.sharedList(ctx, src, cond, deref(req.Params.Q), req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	return gen.ListSharedDocuments200JSONResponse(out), nil
}

func (s *Server) GetSharedDocument(ctx context.Context, req gen.GetSharedDocumentRequestObject) (gen.GetSharedDocumentResponseObject, error) {
	src, err := s.drawers.SharedDocument(ctx, tenant.MustFrom(ctx), req.Id, req.DocumentId)
	if err != nil {
		return nil, err
	}
	out, err := s.sharedDetail(ctx, src, req.DocumentId)
	if err != nil {
		return nil, err
	}
	return gen.GetSharedDocument200JSONResponse(out), nil
}

// sharedDetail is a document of a source workspace, read-only.
func (s *Server) sharedDetail(ctx context.Context, src tenant.Scope, docID int64) (gen.SharedDocument, error) {
	d, err := s.documents.GetDetail(ctx, src, docID)
	if err != nil {
		return gen.SharedDocument{}, err
	}
	names, err := s.loadSourceNames(ctx, src)
	if err != nil {
		return gen.SharedDocument{}, err
	}
	var out gen.SharedDocument
	convert(names.summary(d.Summary), &out)
	out.Content, out.OriginalFilename = d.Content, d.OriginalFilename
	if d.Sensitive && !s.unlocked(ctx) {
		t := true
		out.Content, out.Locked = "", &t
	}
	return out, nil
}

func (s *Server) GetSharedDocumentFile(ctx context.Context, req gen.GetSharedDocumentFileRequestObject) (gen.GetSharedDocumentFileResponseObject, error) {
	src, err := s.drawers.SharedDocument(ctx, tenant.MustFrom(ctx), req.Id, req.DocumentId)
	if err != nil {
		return nil, err
	}
	return s.sharedFile(ctx, src, req.DocumentId, string(req.Kind))
}

// sharedFile serves a shared document's original, preview or thumbnail.
func (s *Server) sharedFile(ctx context.Context, src tenant.Scope, docID int64, kind string) (fileResponse, error) {
	d, err := s.documents.Get(ctx, src, docID)
	if err != nil {
		return fileResponse{}, err
	}
	// the reader confirms their own password for a sensitive document (v1.6.4)
	if err := s.guardSensitive(ctx, src, docID); err != nil {
		return fileResponse{}, err
	}
	switch kind {
	case "original":
		return s.serveFile(ctx, files.At(d.Storage, files.Original(d.WorkspaceID, d.ID, d.OriginalExt)), d.MimeType, d.OriginalFilename, true)
	case "thumbnail":
		if !d.HasThumbnail {
			return fileResponse{}, &apiError{http.StatusNotFound, "not_found", "这份文档没有缩略图"}
		}
		return s.serveFile(ctx, files.LocalFile(s.files.ThumbnailPath(d.WorkspaceID, d.ID)), "image/jpeg", "", false)
	}
	if d.HasArchive {
		return s.serveFile(ctx, files.At(d.Storage, files.Archive(d.WorkspaceID, d.ID)), "application/pdf", d.Title+".pdf", false)
	}
	return s.serveFile(ctx, files.At(d.Storage, files.Original(d.WorkspaceID, d.ID, d.OriginalExt)), d.MimeType, d.OriginalFilename, false)
}

// sharedList lists, read-only, the documents of a source workspace that meet
// cond (a shared drawer's or collection's), most recent first.
func (s *Server) sharedList(ctx context.Context, src tenant.Scope, cond filter.Cond, q string, limit *int, cursor *string) (gen.SharedDocumentList, error) {
	conds := []filter.Cond{cond}
	if q = strings.TrimSpace(q); q != "" {
		conds = append(conds, filter.Cond{Text: &q})
	}
	query := document.Query{Filter: filter.Cond{All: conds}, Desc: true, Cursor: derefCursor(cursor)}
	if len(conds) > 1 {
		query.Sort = document.SortRelevance
	}
	if limit != nil {
		query.Limit = *limit
	}
	page, err := s.documents.List(ctx, src, query)
	if err != nil {
		return gen.SharedDocumentList{}, err
	}
	names, err := s.loadSourceNames(ctx, src)
	if err != nil {
		return gen.SharedDocumentList{}, err
	}
	out := gen.SharedDocumentList{Items: make([]gen.SharedDocumentSummary, len(page.Items)), Total: int(page.Total)}
	for i, it := range page.Items {
		out.Items[i] = names.summary(it)
	}
	if page.NextCursor != "" {
		out.NextCursor = &page.NextCursor
	}
	return out, nil
}
