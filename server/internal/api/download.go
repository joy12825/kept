package api

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"kept/server/internal/api/gen"
	"kept/server/internal/document"
	"kept/server/internal/files"
	"kept/server/internal/filter"
	"kept/server/internal/i18n"
	"kept/server/internal/tenant"
)

// MaxDownload is the most documents one zip may hold.
const MaxDownload = 1000

var errTooManyToDownload = invalid(fmt.Sprintf("一次最多下载 %d 份文档，请缩小范围", MaxDownload))

// DownloadDocuments streams a zip of the selected documents' files as it
// builds it: nothing is written to disk and no job is queued (stage 4, Q4).
func (s *Server) DownloadDocuments(ctx context.Context, req gen.DownloadDocumentsRequestObject) (gen.DownloadDocumentsResponseObject, error) {
	p := req.Params
	var sel document.Selection
	switch {
	case p.Ids != nil && p.Filter == nil:
		for _, part := range strings.Split(*p.Ids, ",") {
			id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
			if err != nil {
				return nil, invalid("文档 ID 不正确")
			}
			sel.IDs = append(sel.IDs, id)
		}
		if len(sel.IDs) > MaxDownload {
			return nil, errTooManyToDownload
		}
	case p.Filter != nil && p.Ids == nil:
		c, err := filter.ParseString(string(*p.Filter))
		if err != nil {
			return nil, err
		}
		sel.Filter = &c
	default:
		return nil, invalid("请用 ids 或 filter 之一选择文档")
	}
	scope := tenant.MustFrom(ctx)
	ids, err := s.documents.Resolve(ctx, scope, sel)
	if err != nil {
		return nil, err
	}
	if err := s.guardSensitive(ctx, scope, ids...); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, document.ErrNotFound
	}
	if len(ids) > MaxDownload {
		return nil, errTooManyToDownload
	}
	docs := make([]document.Document, 0, len(ids))
	for _, id := range ids {
		d, err := s.documents.Get(ctx, scope, id)
		if err != nil {
			return nil, err
		}
		docs = append(docs, d)
	}
	archive := p.Kind != nil && *p.Kind == gen.DownloadDocumentsParamsKindArchive
	return zipResponse{docs: docs, archive: archive, files: s.files, log: s.log, ctx: ctx, now: time.Now()}, nil
}

type zipResponse struct {
	docs    []document.Document
	archive bool
	files   *files.Store
	log     *slog.Logger
	ctx     context.Context
	now     time.Time
}

func (z zipResponse) VisitDownloadDocumentsResponse(w http.ResponseWriter) error {
	name := i18n.T(z.ctx, fmt.Sprintf(i18n.N("文档-%s.zip"), z.now.Format("2006-01-02")))
	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)

	zw := zip.NewWriter(w)
	used := map[string]int{}
	for _, d := range z.docs {
		ref, ext := files.Original(d.WorkspaceID, d.ID, d.OriginalExt), d.OriginalExt
		if z.archive && d.HasArchive {
			ref, ext = files.Archive(d.WorkspaceID, d.ID), "pdf"
		}
		if err := addFile(zw, func() (io.ReadCloser, error) { return z.files.Open(z.ctx, d.Storage, ref) }, files.FileName(d.Title, ext, used), d.ModifiedAt); err != nil {
			// Headers are gone already: log, skip the file and go on.
			z.log.ErrorContext(z.ctx, "add a document to a zip", "document", d.ID, "err", err)
		}
	}
	return zw.Close()
}

func addFile(zw *zip.Writer, open func() (io.ReadCloser, error), name string, modified time.Time) error {
	f, err := open()
	if err != nil {
		return err
	}
	defer f.Close()
	method := zip.Store // PDFs and images are compressed already
	if strings.HasSuffix(name, ".txt") || strings.HasSuffix(name, ".md") {
		method = zip.Deflate
	}
	out, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: modified})
	if err != nil {
		return err
	}
	_, err = io.Copy(out, f)
	return err
}
