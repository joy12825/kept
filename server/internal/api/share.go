package api

import (
	"context"
	"net/http"
	"strings"

	"kept/server/internal/api/gen"
	"kept/server/internal/edition"
	"kept/server/internal/share"
	"kept/server/internal/tenant"
)

func toAPIShareLink(l share.Link) gen.ShareLink {
	return gen.ShareLink{Id: l.ID, DocumentId: l.DocumentID, Token: l.Token, File: gen.ShareLinkFile(l.File), ExpiresAt: l.ExpiresAt,
		CreatedAt: l.CreatedAt, CreatorName: l.CreatorName, AccessCount: l.AccessCount, LastAccessAt: l.LastAccessAt,
		Watermark: optString(l.Watermark), AllowDownload: l.AllowDownload}
}

// shareSettings reads how a link or bundle is to be made (v1.9.2: the
// watermark and whether it may be downloaded; downloads by default).
func shareSettings(days int, file string, wm *string, download *bool) share.Settings {
	return share.Settings{Days: days, File: file, Protect: share.Protect{Watermark: deref(wm), AllowDownload: download == nil || *download}}
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *Server) ListShareLinks(ctx context.Context, req gen.ListShareLinksRequestObject) (gen.ListShareLinksResponseObject, error) {
	links, err := s.shares.List(ctx, tenant.MustFrom(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	out := gen.ListShareLinks200JSONResponse{}
	for _, l := range links {
		out = append(out, toAPIShareLink(l))
	}
	return out, nil
}

func (s *Server) CreateShareLink(ctx context.Context, req gen.CreateShareLinkRequestObject) (gen.CreateShareLinkResponseObject, error) {
	if err := s.refuseSensitive(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	b := req.Body
	// a watermark is the paid edition's sharing; a plain link is not (v1.13.3)
	if b.Watermark != nil && strings.TrimSpace(*b.Watermark) != "" {
		if err := s.requireEdition(edition.Pro); err != nil {
			return nil, err
		}
	}
	l, err := s.shares.Create(ctx, tenant.MustFrom(ctx), req.Id, shareSettings(b.Days, string(b.File), b.Watermark, b.AllowDownload))
	if err != nil {
		return nil, err
	}
	return gen.CreateShareLink201JSONResponse(toAPIShareLink(l)), nil
}

func (s *Server) RevokeShareLink(ctx context.Context, req gen.RevokeShareLinkRequestObject) (gen.RevokeShareLinkResponseObject, error) {
	if err := s.shares.Revoke(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	return gen.RevokeShareLink204Response{}, nil
}

// The public pages: no login, nothing about the workspace or the sharer.

func (s *Server) GetPublicShare(ctx context.Context, req gen.GetPublicShareRequestObject) (gen.GetPublicShareResponseObject, error) {
	sh, err := s.shares.Open(ctx, req.Token)
	if err != nil {
		return nil, err
	}
	var size int64
	if st, err := s.files.Stat(ctx, sh.File.Storage, sh.File.Ref); err == nil {
		size = st.Size
	}
	return gen.GetPublicShare200JSONResponse{Title: sh.Title, Filename: sh.Filename, MimeType: sh.MimeType, PageCount: sh.PageCount,
		Size: size, ExpiresAt: sh.ExpiresAt, AllowDownload: sh.AllowDownload, Watermarked: sh.Watermark != ""}, nil
}

func (s *Server) GetPublicShareFile(ctx context.Context, req gen.GetPublicShareFileRequestObject) (gen.GetPublicShareFileResponseObject, error) {
	sh, err := s.shares.Open(ctx, req.Token)
	if err != nil {
		return nil, err
	}
	download := req.Params.Download != nil && *req.Params.Download
	if download && !sh.AllowDownload {
		return nil, share.ErrViewOnly
	}
	loc, err := s.shares.Served(ctx, sh)
	if err != nil {
		return nil, err
	}
	resp, err := s.serveFile(ctx, loc, sh.MimeType, sh.Filename, download)
	if err != nil {
		return nil, err
	}
	resp.noStore = !sh.AllowDownload
	// Count a visit once, not for every Range request pdf.js makes.
	if r := resp.r; r.Header.Get("Range") == "" || r.Header.Get("Range") == "bytes=0-" {
		s.shares.Touch(ctx, sh.ID)
	}
	return publicFileResponse{resp}, nil
}

// publicFileResponse is a file for anyone with the link: never indexed.
type publicFileResponse struct{ fileResponse }

func (r publicFileResponse) VisitGetPublicShareFileResponse(w http.ResponseWriter) error {
	w.Header().Set("X-Robots-Tag", "noindex")
	return r.visit(w)
}
