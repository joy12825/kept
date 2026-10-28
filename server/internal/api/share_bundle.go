package api

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"kept/server/internal/api/gen"
	"kept/server/internal/share"
	"kept/server/internal/tenant"
)

// Share bundles and the list of every working link (v1.4.2).

func toAPIActiveShare(a share.Active) gen.ActiveShare {
	out := gen.ActiveShare{Kind: gen.ActiveShareKind(a.Kind), Id: a.ID, Title: a.Title, Documents: a.Documents, Token: a.Token,
		File: gen.ActiveShareFile(a.File), ExpiresAt: a.ExpiresAt, CreatedAt: a.CreatedAt, CreatorName: a.CreatorName,
		AccessCount: a.AccessCount, LastAccessAt: a.LastAccessAt, Watermark: optString(a.Watermark), AllowDownload: a.AllowDownload}
	if a.DocumentID != 0 {
		out.DocumentId = &a.DocumentID
	}
	return out
}

func (s *Server) ListActiveShares(ctx context.Context, _ gen.ListActiveSharesRequestObject) (gen.ListActiveSharesResponseObject, error) {
	list, err := s.shares.ListActive(ctx, tenant.MustFrom(ctx))
	if err != nil {
		return nil, err
	}
	out := gen.ListActiveShares200JSONResponse{}
	for _, a := range list {
		out = append(out, toAPIActiveShare(a))
	}
	return out, nil
}

func (s *Server) RevokeShareBundle(ctx context.Context, req gen.RevokeShareBundleRequestObject) (gen.RevokeShareBundleResponseObject, error) {
	if err := s.shares.RevokeBundle(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	return gen.RevokeShareBundle204Response{}, nil
}

// The public pages: no login, nothing about the workspace or the sharer.

func (s *Server) GetPublicBundle(ctx context.Context, req gen.GetPublicBundleRequestObject) (gen.GetPublicBundleResponseObject, error) {
	b, err := s.shares.OpenBundle(ctx, req.Token)
	if err != nil {
		return nil, err
	}
	s.shares.TouchBundle(ctx, b.ID) // a visit is opening the list
	out := gen.GetPublicBundle200JSONResponse{Title: b.Title, ExpiresAt: b.ExpiresAt, AllowDownload: b.AllowDownload, Watermarked: b.Watermark != ""}
	for _, d := range b.Documents {
		var date openapi_types.Date
		_ = date.UnmarshalText([]byte(d.Date))
		out.Documents = append(out.Documents, struct {
			Date      openapi_types.Date `json:"date"`
			Filename  string             `json:"filename"`
			Id        int64              `json:"id"`
			MimeType  string             `json:"mimeType"`
			PageCount int                `json:"pageCount"`
			Size      int64              `json:"size"`
			Title     string             `json:"title"`
		}{Date: date, Filename: d.Filename, Id: d.ID, MimeType: d.MimeType, PageCount: d.PageCount, Size: d.Size, Title: d.Title})
	}
	// the summary a doctor sees first (v1.8.3)
	if raw, err := s.shares.Summary(ctx, b.ID); err != nil {
		return nil, err
	} else if raw != nil {
		var sum gen.HealthReport
		if json.Unmarshal(raw, &sum) == nil {
			out.Summary = &sum
		}
	}
	return out, nil
}

func (s *Server) GetPublicBundleFile(ctx context.Context, req gen.GetPublicBundleFileRequestObject) (gen.GetPublicBundleFileResponseObject, error) {
	b, err := s.shares.OpenBundle(ctx, req.Token)
	if err != nil {
		return nil, err
	}
	for _, d := range b.Documents {
		if d.ID == req.DocumentId {
			download := req.Params.Download != nil && *req.Params.Download
			if download && !b.AllowDownload {
				return nil, share.ErrViewOnly
			}
			loc, err := s.shares.ServedDoc(ctx, b, d)
			if err != nil {
				return nil, err
			}
			resp, err := s.serveFile(ctx, loc, d.MimeType, d.Filename, download)
			if err != nil {
				return nil, err
			}
			resp.noStore = !b.AllowDownload
			return publicBundleFile{resp}, nil
		}
	}
	return nil, share.ErrExpired
}

type publicBundleFile struct{ fileResponse }

func (r publicBundleFile) VisitGetPublicBundleFileResponse(w http.ResponseWriter) error {
	w.Header().Set("X-Robots-Tag", "noindex")
	return r.visit(w)
}

func (s *Server) GetPublicBundleZip(ctx context.Context, req gen.GetPublicBundleZipRequestObject) (gen.GetPublicBundleZipResponseObject, error) {
	b, err := s.shares.OpenBundle(ctx, req.Token)
	if err != nil {
		return nil, err
	}
	if !b.AllowDownload {
		return nil, share.ErrViewOnly
	}
	return bundleZip{s: s, ctx: ctx, b: b}, nil
}

type bundleZip struct {
	s   *Server
	ctx context.Context
	b   share.SharedBundle
}

func (z bundleZip) VisitGetPublicBundleZipResponse(w http.ResponseWriter) error {
	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(z.b.Title+".zip"))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("Cache-Control", "private, no-store")
	h.Set("X-Robots-Tag", "noindex")
	w.WriteHeader(http.StatusOK)
	zw := zip.NewWriter(w)
	for _, d := range z.b.Documents {
		loc, err := z.s.shares.ServedDoc(z.ctx, z.b, d)
		if err != nil {
			z.s.log.ErrorContext(z.ctx, "stamp a shared document", "document", d.ID, "err", err)
			continue
		}
		if err := addFile(zw, func() (io.ReadCloser, error) { return z.s.files.OpenLoc(z.ctx, loc) }, d.Filename, d.ModifiedAt); err != nil {
			z.s.log.ErrorContext(z.ctx, "add a shared document to a zip", "document", d.ID, "err", err)
		}
	}
	return zw.Close()
}
