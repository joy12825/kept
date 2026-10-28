package api

import (
	"archive/zip"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"kept/server/internal/api/gen"
	"kept/server/internal/export"
	"kept/server/internal/i18n"
	"kept/server/internal/tenant"
)

// ExportWorkspace streams the whole workspace as ordinary files in a zip
// (v1.4.1): folders by drawer and year, and the lists.
func (s *Server) ExportWorkspace(ctx context.Context, req gen.ExportWorkspaceRequestObject) (gen.ExportWorkspaceResponseObject, error) {
	ws := tenant.MustFrom(ctx).WorkspaceID()
	// the export holds everything, sensitive documents included (v1.6.4)
	if !s.unlocked(ctx) {
		if n, err := s.countSensitive(ctx, tenant.MustFrom(ctx)); err != nil {
			return nil, err
		} else if n > 0 {
			return nil, errLocked
		}
	}
	opt := export.Options{Original: req.Params.File != nil && *req.Params.File == gen.ExportWorkspaceParamsFileOriginal}
	name, err := s.export.Start(ctx, ws, opt)
	if err != nil {
		return nil, err
	}
	return exportResponse{s: s, ctx: ctx, ws: ws, opt: opt, name: name}, nil
}

type exportResponse struct {
	s    *Server
	ctx  context.Context
	ws   int64
	opt  export.Options
	name string
}

func (r exportResponse) VisitExportWorkspaceResponse(w http.ResponseWriter) error {
	file := i18n.T(r.ctx, fmt.Sprintf(i18n.N("%s-导出-%s.zip"), r.name, time.Now().Format("2006-01-02")))
	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(file))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	zw := zip.NewWriter(w)
	if _, err := r.s.export.Run(r.ctx, r.ws, r.opt, export.ZipSink{W: zw}); err != nil {
		// Headers are gone already: the zip ends early and the browser reports a failed download.
		r.s.log.ErrorContext(r.ctx, "export a workspace", "workspace", r.ws, "err", err)
		return nil
	}
	return zw.Close()
}
