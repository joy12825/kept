package api

import (
	"context"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"kept/server/internal/api/gen"
	"kept/server/internal/i18n"
	"kept/server/internal/tenant"
)

func (s *Server) ListDocumentVersions(ctx context.Context, req gen.ListDocumentVersionsRequestObject) (gen.ListDocumentVersionsResponseObject, error) {
	vs, err := s.ingest.Versions(ctx, tenant.MustFrom(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	out := gen.ListDocumentVersions200JSONResponse{}
	for _, v := range vs {
		out = append(out, gen.DocumentVersion{Id: v.ID, Number: v.Number, Label: i18n.T(ctx, v.Label), Filename: v.Filename, MimeType: v.MimeType, Size: v.Size,
			Encrypted: v.Encrypted, Current: v.Current, CreatorName: v.CreatorName, CreatedAt: v.CreatedAt})
	}
	return out, nil
}

func (s *Server) AddDocumentVersion(ctx context.Context, req gen.AddDocumentVersionRequestObject) (gen.AddDocumentVersionResponseObject, error) {
	label := ""
	for {
		part, err := req.Body.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, errNoFile
		}
		if err != nil {
			return nil, invalid("上传的内容格式不正确")
		}
		switch part.FormName() {
		case "label":
			v, err := readField(part, 800)
			if err != nil || utf8.RuneCountInString(v) > 200 {
				return nil, invalid("版本说明最多 200 个字")
			}
			label = strings.TrimSpace(v)
		case "file":
			id, err := s.ingest.AddVersion(ctx, tenant.MustFrom(ctx), req.Id, part.FileName(), part, label)
			if err != nil {
				return nil, err
			}
			return gen.AddDocumentVersion202JSONResponse{JobId: id}, nil
		}
	}
}

func (s *Server) RemoveDocumentPassword(ctx context.Context, req gen.RemoveDocumentPasswordRequestObject) (gen.RemoveDocumentPasswordResponseObject, error) {
	id, err := s.ingest.RemovePassword(ctx, tenant.MustFrom(ctx), req.Id, req.Body.Password)
	if err != nil {
		return nil, err
	}
	return gen.RemoveDocumentPassword202JSONResponse{JobId: id}, nil
}

func (s *Server) SetCurrentDocumentVersion(ctx context.Context, req gen.SetCurrentDocumentVersionRequestObject) (gen.SetCurrentDocumentVersionResponseObject, error) {
	id, err := s.ingest.SetCurrentVersion(ctx, tenant.MustFrom(ctx), req.Id, req.VersionId)
	if err != nil {
		return nil, err
	}
	return gen.SetCurrentDocumentVersion202JSONResponse{JobId: id}, nil
}

func (s *Server) DeleteDocumentVersion(ctx context.Context, req gen.DeleteDocumentVersionRequestObject) (gen.DeleteDocumentVersionResponseObject, error) {
	if err := s.ingest.DeleteVersion(ctx, tenant.MustFrom(ctx), req.Id, req.VersionId); err != nil {
		return nil, err
	}
	return gen.DeleteDocumentVersion204Response{}, nil
}

func (s *Server) GetDocumentVersionFile(ctx context.Context, req gen.GetDocumentVersionFileRequestObject) (gen.GetDocumentVersionFileResponseObject, error) {
	if err := s.guardSensitive(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	loc, name, mime, err := s.ingest.VersionFile(ctx, tenant.MustFrom(ctx), req.Id, req.VersionId)
	if err != nil {
		return nil, err
	}
	inline := req.Params.Inline != nil && *req.Params.Inline
	return s.serveFile(ctx, loc, mime, name, !inline)
}

// GetDocumentVersionPreview is a version as a PDF, for the side-by-side
// view (v1.9.7): made on demand from an earlier Office file, e-mail or OFD.
func (s *Server) GetDocumentVersionPreview(ctx context.Context, req gen.GetDocumentVersionPreviewRequestObject) (gen.GetDocumentVersionPreviewResponseObject, error) {
	if err := s.guardSensitive(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	loc, name, err := s.ingest.VersionPreview(ctx, tenant.MustFrom(ctx), req.Id, req.VersionId)
	if err != nil {
		return nil, err
	}
	return s.serveFile(ctx, loc, "application/pdf", name, false)
}

func (s *Server) GetDocumentVersionText(ctx context.Context, req gen.GetDocumentVersionTextRequestObject) (gen.GetDocumentVersionTextResponseObject, error) {
	if err := s.guardSensitive(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	text, source, err := s.ingest.VersionText(ctx, tenant.MustFrom(ctx), req.Id, req.VersionId)
	if err != nil {
		return nil, err
	}
	return gen.GetDocumentVersionText200JSONResponse{Text: text, Source: gen.GetDocumentVersionText200JSONResponseBodySource(source)}, nil
}

func (s *Server) AddDocumentVersionFromDocument(ctx context.Context, req gen.AddDocumentVersionFromDocumentRequestObject) (gen.AddDocumentVersionFromDocumentResponseObject, error) {
	scope := tenant.MustFrom(ctx)
	label := ""
	if req.Body.Label != nil {
		label = strings.TrimSpace(*req.Body.Label)
		if utf8.RuneCountInString(label) > 200 {
			return nil, invalid("版本说明最多 200 个字")
		}
	}
	from := req.Body.DocumentId
	id, err := s.ingest.AddVersionFrom(ctx, scope, req.Id, from, label, func(ctx context.Context) error {
		return s.documents.Absorb(ctx, scope, req.Id, from)
	})
	if err != nil {
		return nil, err
	}
	return gen.AddDocumentVersionFromDocument202JSONResponse{JobId: id}, nil
}
