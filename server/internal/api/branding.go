package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"

	"kept/server/internal/api/gen"
	"kept/server/internal/branding"
)

func (s *Server) GetBranding(ctx context.Context, _ gen.GetBrandingRequestObject) (gen.GetBrandingResponseObject, error) {
	b, err := s.branding.Get(ctx)
	if err != nil {
		return nil, err
	}
	return gen.GetBranding200JSONResponse(toAPIBranding(b)), nil
}

func (s *Server) GetBrandingLogo(ctx context.Context, _ gen.GetBrandingLogoRequestObject) (gen.GetBrandingLogoResponseObject, error) {
	f, ctype, err := s.branding.OpenLogo(ctx)
	if errors.Is(err, branding.ErrNoLogo) {
		return nil, &apiError{http.StatusNotFound, "not_found", "没有自定义 Logo"}
	}
	if err != nil {
		return nil, err
	}
	return logoResponse{f: f, contentType: ctype}, nil
}

// logoResponse streams the logo with long-lived caching: clients request it
// with the version in the URL, so a new logo is a new URL.
type logoResponse struct {
	f           *os.File
	contentType string
}

func (r logoResponse) VisitGetBrandingLogoResponse(w http.ResponseWriter) error {
	defer r.f.Close()
	w.Header().Set("Content-Type", r.contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, err := io.Copy(w, r.f)
	return err
}

func toAPIBranding(b branding.Branding) gen.Branding {
	out := gen.Branding{Name: b.Name}
	if b.LogoVersion != "" {
		u := "/api/v1/branding/logo?v=" + b.LogoVersion
		out.LogoUrl = &u
	}
	return out
}
