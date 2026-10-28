package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"kept/server/internal/files"
	"kept/server/internal/i18n"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"kept/server/internal/api/gen"
	"kept/server/internal/auth"
	"kept/server/internal/ingest"
	"kept/server/internal/tenant"
)

var errNoFile = &apiError{http.StatusBadRequest, "invalid_request", "请选择要上传的文件"}

func (s *Server) UploadDocument(ctx context.Context, req gen.UploadDocumentRequestObject) (gen.UploadDocumentResponseObject, error) {
	var ov ingest.Overrides
	up := ingest.Upload{Source: ingest.SourceWeb}
	onConflict := ingest.ConflictDefault
	switch mustPrincipal(ctx).Via {
	case auth.ViaToken:
		up.Source = ingest.SourceAPI
	case auth.ViaDevice:
		up.Source = ingest.SourceMobile
	}
	for {
		part, err := req.Body.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, errNoFile
		}
		if err != nil {
			return nil, invalid("上传的内容格式不正确")
		}
		switch part.FormName() {
		case "documentTypeId", "correspondentId", "personId":
			id, err := readID(part)
			if err != nil {
				return nil, invalid("预设的类型、往来方或家人不正确")
			}
			switch part.FormName() {
			case "documentTypeId":
				ov.DocumentTypeID = id
			case "correspondentId":
				ov.CorrespondentID = id
			default:
				ov.PersonID = id
			}
		case "drawerId":
			id, err := readID(part)
			if err != nil {
				return nil, invalid("预设的抽屉不正确")
			}
			ov.DrawerID = id
		case "tagIds":
			v, err := readField(part, 4096)
			if err != nil {
				return nil, invalid("预设的标签不正确")
			}
			for _, f := range strings.Split(v, ",") {
				if f = strings.TrimSpace(f); f == "" {
					continue
				}
				id, err := strconv.ParseInt(f, 10, 64)
				if err != nil {
					return nil, invalid("预设的标签不正确")
				}
				ov.TagIDs = append(ov.TagIDs, id)
			}
		case "title":
			v, err := readField(part, 1024)
			if err != nil {
				return nil, invalid("预设的标题不正确")
			}
			ov.Title = v
		case "createdDate":
			v, err := readField(part, 32)
			if _, perr := time.Parse("2006-01-02", strings.TrimSpace(v)); err != nil || perr != nil {
				return nil, invalid("预设的日期应写成 2024-03-05")
			}
			ov.CreatedDate = strings.TrimSpace(v)
		case "asn":
			n, err := readID(part)
			if err != nil || n < 1 || n > 999999 {
				return nil, invalid("预设的原件编号应在 1 到 999999 之间")
			}
			ov.ASN = &n
		case "customFields":
			v, err := readField(part, 64<<10)
			if err != nil || json.Unmarshal([]byte(v), &ov.CustomFields) != nil || len(ov.CustomFields) > 50 {
				return nil, invalid("预设的自定义字段应是 JSON 数组，如 [{\"fieldId\":3,\"value\":\"326.40\"}]，最多 50 个")
			}
		case "password":
			v, err := readField(part, 1024)
			if err != nil {
				return nil, invalid("密码不正确")
			}
			up.Password = v
		case "clientUploadId":
			v, err := readField(part, 65)
			if err != nil {
				return nil, invalid("clientUploadId 不正确")
			}
			up.ClientUploadID = strings.TrimSpace(v)
		case "onConflict":
			v, err := readField(part, 16)
			if err != nil {
				return nil, invalid("onConflict 只能是 version 或 new")
			}
			onConflict = strings.TrimSpace(v)
		case "file":
			scope := tenant.MustFrom(ctx)
			// The same name in the same drawer: a new version (v1.7.13).
			doc, err := s.ingest.VersionTarget(ctx, scope, ov.DrawerID, part.FileName(), onConflict)
			if err != nil {
				return nil, err
			}
			if doc != 0 {
				jobID, err := s.ingest.AddVersion(ctx, scope, doc, part.FileName(), part, "")
				if err != nil {
					return nil, err
				}
				return gen.UploadDocument202JSONResponse{JobId: jobID, VersionOf: &doc}, nil
			}
			jobID, err := s.ingest.Submit(ctx, scope, part.FileName(), part, ov, up)
			if err != nil {
				return nil, err
			}
			return gen.UploadDocument202JSONResponse{JobId: jobID}, nil
		}
	}
}

func (s *Server) CheckSameName(ctx context.Context, req gen.CheckSameNameRequestObject) (gen.CheckSameNameResponseObject, error) {
	var drawerID int64
	if req.Body.DrawerId != nil {
		drawerID = *req.Body.DrawerId
	}
	matches, auto, err := s.ingest.SameName(ctx, tenant.MustFrom(ctx), drawerID, req.Body.Names)
	if err != nil {
		return nil, err
	}
	out := gen.CheckSameName200JSONResponse{Auto: auto}
	out.Matches = make([]struct {
		DocumentId int64  `json:"documentId"`
		Name       string `json:"name"`
		Title      string `json:"title"`
	}, len(matches))
	for i, m := range matches {
		out.Matches[i].DocumentId, out.Matches[i].Name, out.Matches[i].Title = m.DocumentID, m.Name, m.Title
	}
	return out, nil
}

// readField reads a small text field, refusing one longer than max bytes.
func readField(p *multipart.Part, max int64) (string, error) {
	b, err := io.ReadAll(io.LimitReader(p, max+1))
	if err != nil {
		return "", err
	}
	if int64(len(b)) > max {
		return "", errors.New("field too long")
	}
	return string(b), nil
}

func readID(p *multipart.Part) (int64, error) {
	b, err := io.ReadAll(io.LimitReader(p, 32))
	if err != nil {
		return 0, err
	}
	if len(b) == 0 {
		return 0, nil
	}
	return strconv.ParseInt(string(b), 10, 64)
}

func (s *Server) ReprocessDocument(ctx context.Context, req gen.ReprocessDocumentRequestObject) (gen.ReprocessDocumentResponseObject, error) {
	id, err := s.ingest.Reprocess(ctx, tenant.MustFrom(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	return gen.ReprocessDocument202JSONResponse{JobId: id}, nil
}

// ---------- files ----------

func (s *Server) GetDocumentOriginal(ctx context.Context, req gen.GetDocumentOriginalRequestObject) (gen.GetDocumentOriginalResponseObject, error) {
	d, err := s.documents.Get(ctx, tenant.MustFrom(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	if err := s.guardSensitive(ctx, tenant.MustFrom(ctx), d.ID); err != nil {
		return nil, err
	}
	s.recordTaken(ctx, tenant.MustFrom(ctx), d.ID, d.Title, purposeOf(req.Params.Purpose), i18n.N("原件"))
	return s.serveFile(ctx, files.At(d.Storage, files.Original(d.WorkspaceID, d.ID, d.OriginalExt)), d.MimeType, d.OriginalFilename, true)
}

func (s *Server) GetDocumentPreview(ctx context.Context, req gen.GetDocumentPreviewRequestObject) (gen.GetDocumentPreviewResponseObject, error) {
	d, err := s.documents.Get(ctx, tenant.MustFrom(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	if err := s.guardSensitive(ctx, tenant.MustFrom(ctx), d.ID); err != nil {
		return nil, err
	}
	if req.Params.Unedited != nil && *req.Params.Unedited {
		p, err := s.pages.Unedited(ctx, tenant.MustFrom(ctx), req.Id)
		if err != nil {
			return nil, err
		}
		return s.serveFile(ctx, p, "application/pdf", d.Title+".pdf", false)
	}
	wantOriginal := req.Params.Original != nil && *req.Params.Original && !d.OriginalEncrypted // an encrypted original cannot be shown
	what := i18n.N("原件")                                                                       // a catalog entry: the stored summary reads in English too (v1.10.1)
	if d.HasArchive && !wantOriginal {
		what = i18n.N("可搜索版")
	}
	s.recordTaken(ctx, tenant.MustFrom(ctx), d.ID, d.Title, purposeOf(req.Params.Purpose), what)
	if d.HasArchive && !wantOriginal {
		return s.serveFile(ctx, files.At(d.Storage, files.Archive(d.WorkspaceID, d.ID)), "application/pdf", d.Title+".pdf", false)
	}
	return s.serveFile(ctx, files.At(d.Storage, files.Original(d.WorkspaceID, d.ID, d.OriginalExt)), d.MimeType, d.OriginalFilename, false)
}

func (s *Server) GetDocumentThumbnail(ctx context.Context, req gen.GetDocumentThumbnailRequestObject) (gen.GetDocumentThumbnailResponseObject, error) {
	d, err := s.documents.Get(ctx, tenant.MustFrom(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	if !d.HasThumbnail {
		return nil, &apiError{http.StatusNotFound, "not_found", "这份文档没有缩略图"}
	}
	if err := s.guardSensitive(ctx, tenant.MustFrom(ctx), d.ID); err != nil {
		return nil, err
	}
	return s.serveFile(ctx, files.LocalFile(s.files.ThumbnailPath(d.WorkspaceID, d.ID)), "image/jpeg", "", false)
}

// fileResponse streams a document file with http.ServeContent, which
// handles Range (pdf.js loads large files in pieces), ETag and
// If-None-Match. It satisfies every file operation's response interface.
type fileResponse struct {
	r           *http.Request
	f           files.File
	modTime     time.Time
	contentType string
	disposition string
	noStore     bool // a view-only share (v1.9.2): not kept by the browser
}

func (s *Server) serveFile(ctx context.Context, loc files.Loc, contentType, filename string, attachment bool) (fileResponse, error) {
	f, err := s.files.OpenLoc(ctx, loc)
	if errors.Is(err, os.ErrNotExist) {
		s.log.ErrorContext(ctx, "document file missing", "storage", loc.Storage, "key", loc.Ref.Key(), "path", loc.Path)
		return fileResponse{}, &apiError{http.StatusNotFound, "not_found", "文件不存在"}
	}
	if err != nil {
		return fileResponse{}, err
	}
	resp := fileResponse{r: ctx.Value(httpRequestKey{}).(*http.Request), f: f, modTime: f.Info().ModTime, contentType: contentType}
	if filename != "" {
		kind := "inline"
		if attachment {
			kind = "attachment"
		}
		resp.disposition = fmt.Sprintf("%s; filename*=UTF-8''%s", kind, url.PathEscape(filename))
	}
	return resp, nil
}

func (r fileResponse) visit(w http.ResponseWriter) error {
	defer r.f.Close()
	h := w.Header()
	h.Set("Content-Type", r.contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	// A user's file never runs as a page of this origin (design doc 11.10).
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("Cache-Control", "private, no-cache")
	if r.noStore {
		h.Set("Cache-Control", "private, no-store")
	}
	h.Set("ETag", fmt.Sprintf(`"%x-%x"`, r.modTime.UnixNano(), r.f.Info().Size))
	if r.disposition != "" {
		h.Set("Content-Disposition", r.disposition)
	}
	http.ServeContent(w, r.r, "", r.modTime, r.f)
	return nil
}

func (r fileResponse) VisitGetDocumentOriginalResponse(w http.ResponseWriter) error {
	return r.visit(w)
}
func (r fileResponse) VisitGetDocumentPreviewResponse(w http.ResponseWriter) error { return r.visit(w) }
func (r fileResponse) VisitGetDocumentCoverResponse(w http.ResponseWriter) error   { return r.visit(w) }
func (r fileResponse) VisitGetDocumentThumbnailResponse(w http.ResponseWriter) error {
	return r.visit(w)
}
func (r fileResponse) VisitGetSharedCollectionDocumentFileResponse(w http.ResponseWriter) error {
	return r.visit(w)
}
func (r fileResponse) VisitGetSharedDocumentFileResponse(w http.ResponseWriter) error {
	return r.visit(w)
}
func (r fileResponse) VisitGetDocumentVersionFileResponse(w http.ResponseWriter) error {
	return r.visit(w)
}
func (r fileResponse) VisitGetDocumentVersionPreviewResponse(w http.ResponseWriter) error {
	return r.visit(w)
}
