// Package api serves the HTTP API defined in api/openapi.yaml. Request and
// response types plus routing are generated into ./gen (make generate);
// this package implements the handlers and cross-cutting middleware.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"kept/server/internal/i18n"
	"kept/server/internal/storage"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"kept/server/internal/api/gen"
	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/auth"
	"kept/server/internal/backup"
	"kept/server/internal/booklists"
	"kept/server/internal/bookmeta"
	"kept/server/internal/books"
	"kept/server/internal/branding"
	"kept/server/internal/checklist"
	"kept/server/internal/classify"
	"kept/server/internal/dedupe"
	"kept/server/internal/document"
	"kept/server/internal/drawer"
	"kept/server/internal/edition"
	"kept/server/internal/events"
	"kept/server/internal/export"
	"kept/server/internal/files"
	"kept/server/internal/ingest"
	"kept/server/internal/jobs"
	"kept/server/internal/notes"
	"kept/server/internal/notify"
	"kept/server/internal/pages"
	"kept/server/internal/privacy"
	"kept/server/internal/quota"
	"kept/server/internal/reading"
	"kept/server/internal/search"
	"kept/server/internal/share"
	"kept/server/internal/system"
	"kept/server/internal/taxonomy"
	"kept/server/internal/tenant"
	"kept/server/internal/transfer"
	"kept/server/internal/version"
	"kept/server/internal/views"
	"kept/server/internal/workspace"
)

// Pinger reports whether a dependency is reachable (the database for now).
type Pinger interface {
	Ping(ctx context.Context) error
}

type Deps struct {
	DB           Pinger
	Auth         *auth.Service
	Workspaces   *workspace.Service
	Audit        *audit.Log
	Taxonomy     *taxonomy.Service
	Branding     *branding.Service
	Documents    *document.Service
	Classifier   *classify.Service
	Drawers      *drawer.Service
	Pages        *pages.Service
	Notify       *notify.Service
	Shares       *share.Service
	Dedupe       *dedupe.Service
	System       *system.Service
	Notes        *notes.Service
	Books        *books.Service
	BookLookup   *bookmeta.Client // nil: no online lookup
	Reading      *reading.Service
	BookLists    *booklists.Service
	Views        *views.Service
	Search       *search.Index
	Ingest       *ingest.Service
	Files        *files.Store
	Events       *events.Bus
	Jobs         *jobs.Queue
	Log          *slog.Logger
	SecureCookie string   // auto | always | never (config.SecureCookie)
	PublicURLs   []string // config.PublicURLs: put first in pairing QR codes
	// TrustedProxies: config.TrustedProxies, whose X-Forwarded-For is believed.
	TrustedProxies []netip.Prefix
	BackupDir      *backup.Dir  // where "back up now" writes: KEPT_BACKUP_DIR or the page's choice (v1.13.9); none refuses
	ExportDir      string       // config.ExportDir: the daily export directory, shown on the page (v1.7.9)
	Web            http.Handler // serves the web app for non-/api paths; nil means JSON 404
	// Stopping is done when the server starts shutting down: open event
	// streams end then instead of holding the shutdown up. nil: never.
	Stopping context.Context
	// Quota counts storage and holds the workspaces' limits (v1.0).
	Quota *quota.Service
	// Storage manages where documents' files are kept (v1.7.0).
	Storage *storage.Service
	// Transfer moves documents to another workspace (v1.3.8).
	Transfer *transfer.Service
	// Export writes a workspace out as ordinary files (v1.4.1).
	Export *export.Service
	// Checklists (v1.6.2); nil makes their operations fail
	Checklists *checklist.Service
	// Privacy is family members' private documents (v1.8.0).
	Privacy *privacy.Service
	// Edition is what this instance may do (v1.12); nil allows everything.
	Edition *edition.Service
	// EE are the paid modules (v1.13): in the official build only.
	EE EEDeps
}

type Server struct {
	db             Pinger
	auth           *auth.Service
	workspaces     *workspace.Service
	audit          *audit.Log
	taxonomy       *taxonomy.Service
	branding       *branding.Service
	documents      *document.Service
	classifier     *classify.Service
	pages          *pages.Service
	notify         *notify.Service
	shares         *share.Service
	dedupe         *dedupe.Service
	drawers        *drawer.Service
	system         *system.Service
	notes          *notes.Service
	books          *books.Service
	bookLookup     *bookmeta.Client
	reading        *reading.Service
	bookLists      *booklists.Service
	views          *views.Service
	search         *search.Index
	ingest         *ingest.Service
	files          *files.Store
	events         *events.Bus
	jobs           *jobs.Queue
	log            *slog.Logger
	secureCookie   string
	trustedProxies []netip.Prefix
	warnForwarded  sync.Once
	publicURLs     []string
	backupDir      *backup.Dir
	exportDir      string
	web            http.Handler
	stopping       context.Context
	quota          *quota.Service
	storage        *storage.Service
	transfer       *transfer.Service
	export         *export.Service
	checklists     *checklist.Service
	privacy        *privacy.Service
	edition        *edition.Service
	ee             EEDeps
	// public holds the (lower-cased) operations that need no login: those
	// declared with `security: []` in the OpenAPI spec. Everything else
	// requires a caller. Lower-cased because the generator re-cases IDs.
	public map[string]bool
	// minRole is the role each operation needs in the current workspace
	// (`x-kept-min-role` in the spec); roleNone means the workspace role is
	// irrelevant to it.
	minRole map[string]tenant.Role
	// minEdition is the edition each operation needs (`x-kept-edition`,
	// v1.12); free when unannotated.
	minEdition map[string]edition.Edition
}

// roleNone marks operations that act on the account itself, or that name
// their workspace in the path and check the role there.
const roleNone tenant.Role = "none"

var _ gen.StrictServerInterface = (*Server)(nil)

func NewServer(d Deps) (*Server, error) {
	spec, err := gen.GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("load embedded OpenAPI spec: %w", err)
	}
	public := map[string]bool{}
	minRole := map[string]tenant.Role{}
	minEdition := map[string]edition.Edition{}
	for _, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			id := strings.ToLower(op.OperationID)
			if op.Security != nil && len(*op.Security) == 0 {
				public[id] = true
			}
			role, err := operationMinRole(method, op.Extensions["x-kept-min-role"])
			if err != nil {
				return nil, fmt.Errorf("operation %s: %w", op.OperationID, err)
			}
			minRole[id] = role
			need, err := operationEdition(op.Extensions["x-kept-edition"])
			if err != nil {
				return nil, fmt.Errorf("operation %s: %w", op.OperationID, err)
			}
			minEdition[id] = need
		}
	}
	return &Server{
		db: d.DB, auth: d.Auth, workspaces: d.Workspaces, audit: d.Audit, taxonomy: d.Taxonomy, branding: d.Branding,
		documents: d.Documents, classifier: d.Classifier, drawers: d.Drawers, pages: d.Pages, notify: d.Notify, shares: d.Shares, dedupe: d.Dedupe, system: d.System, notes: d.Notes, books: d.Books, bookLookup: d.BookLookup, reading: d.Reading, bookLists: d.BookLists, views: d.Views, search: d.Search, ingest: d.Ingest, files: d.Files, events: d.Events, jobs: d.Jobs, log: d.Log, secureCookie: d.SecureCookie, trustedProxies: d.TrustedProxies, publicURLs: d.PublicURLs, backupDir: d.BackupDir, exportDir: d.ExportDir, web: d.Web, stopping: d.Stopping, quota: d.Quota, storage: d.Storage, transfer: d.Transfer, export: d.Export, checklists: d.Checklists, privacy: d.Privacy, edition: d.Edition, ee: d.EE,
		public: public, minRole: minRole, minEdition: minEdition,
	}, nil
}

// streamContext is ctx, also cancelled when the server starts shutting
// down (for responses that stay open: event streams, streamed answers).
func (s *Server) streamContext(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	if s.stopping == nil {
		return ctx, cancel
	}
	stop := context.AfterFunc(s.stopping, cancel)
	return ctx, func() { stop(); cancel() }
}

// operationMinRole reads an operation's `x-kept-min-role`. Unannotated
// operations default to viewer for GET and editor for anything else.
func operationMinRole(method string, ext any) (tenant.Role, error) {
	if raw, ok := ext.(json.RawMessage); ok { // older kin-openapi keeps extensions undecoded
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return "", fmt.Errorf("x-kept-min-role: %w", err)
		}
		ext = v
	}
	switch v := ext.(type) {
	case nil:
		if method == http.MethodGet {
			return tenant.RoleViewer, nil
		}
		return tenant.RoleEditor, nil
	case string:
		switch r := tenant.Role(v); r {
		case roleNone, tenant.RoleViewer, tenant.RoleEditor, tenant.RoleOwner:
			return r, nil
		}
		return "", fmt.Errorf("x-kept-min-role: unknown role %q", v)
	default:
		return "", fmt.Errorf("x-kept-min-role: want a string, got %T", ext)
	}
}

// operationEdition reads an operation's `x-kept-edition` (v1.12): pro or
// team; unannotated operations are in every edition.
func operationEdition(ext any) (edition.Edition, error) {
	if raw, ok := ext.(json.RawMessage); ok {
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return edition.Free, fmt.Errorf("x-kept-edition: %w", err)
		}
		ext = v
	}
	switch v := ext.(type) {
	case nil:
		return edition.Free, nil
	case string:
		e, err := edition.Parse(v)
		if err != nil || e == edition.Free {
			return edition.Free, fmt.Errorf("x-kept-edition: want pro or team, got %q", v)
		}
		return e, nil
	default:
		return edition.Free, fmt.Errorf("x-kept-edition: want a string, got %T", ext)
	}
}

// Handler returns the complete HTTP handler with middleware applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	strict := gen.NewStrictHandlerWithOptions(s, []gen.StrictMiddlewareFunc{s.authorize}, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeError(w, r, http.StatusBadRequest, "invalid_request", i18n.N("请求格式不正确"), map[string]any{"reason": err.Error()})
		},
		ResponseErrorHandlerFunc: s.respondError,
	})
	gen.HandlerWithOptions(strict, gen.StdHTTPServerOptions{
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeError(w, r, http.StatusBadRequest, "invalid_request", i18n.N("请求参数不正确"), map[string]any{"reason": err.Error()})
		},
	})
	mux.HandleFunc("GET /manifest.webmanifest", s.manifest)
	// Anything not matched above goes to the web app, unless it isn't
	// mounted (or the path is an API path anyway), in which case JSON 404.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if s.web == nil || strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, r, http.StatusNotFound, "not_found", i18n.N("找不到请求的资源"), nil)
			return
		}
		s.web.ServeHTTP(w, r)
	})
	// Listed innermost first: requestID wraps everything.
	return chain(mux, s.csrf, s.authenticate, withLang, bodyLimit, securityHeaders, gzipJSON, accessLog(s.log), s.clientIP, recoverer(s.log), requestID)
}

// authorize enforces, in order: login for non-public operations; a valid
// current workspace (anything but logout fails when X-Workspace-ID names a
// workspace the caller cannot use); administrator rights for operations
// whose ID starts with "admin"; the operation's minimum role in the
// current workspace; and the edition it needs (v1.12).
func (s *Server) authorize(next gen.StrictHandlerFunc, operationID string) gen.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
		// File downloads need the request itself (Range, If-None-Match).
		ctx = context.WithValue(ctx, httpRequestKey{}, r)
		op := strings.ToLower(operationID)
		if s.public[op] {
			return next(ctx, w, r, req)
		}
		p, ok := principalFrom(ctx)
		if !ok {
			return nil, errUnauthorized
		}
		if workspaceInvalid(ctx) && op != "logout" {
			return nil, errWorkspaceNotFound
		}
		if p.Via == auth.ViaDevice && !deviceAllowed(p.DeviceAccess, op) {
			return nil, errDeviceForbidden
		}
		if strings.HasPrefix(op, "admin") && !p.IsAdmin {
			return nil, errNotAdmin
		}
		if min := s.minRole[op]; min != roleNone && !tenant.MustFrom(ctx).Role().AtLeast(min) {
			return nil, errRoleTooLow
		}
		if err := s.requireEdition(s.minEdition[op]); err != nil {
			return nil, err
		}
		return next(ctx, w, r, req)
	}
}

func (s *Server) GetHealth(ctx context.Context, _ gen.GetHealthRequestObject) (gen.GetHealthResponseObject, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	checks := map[string]string{"database": "ok"}
	degraded := false
	if err := s.db.Ping(ctx); err != nil {
		checks["database"], degraded = err.Error(), true
	}
	if s.system != nil {
		if free, low, ok := s.system.Disk(); ok {
			checks["disk"] = "ok"
			if low {
				checks["disk"], degraded = "剩余空间不足："+quota.Human(free), true
			}
		}
	}
	if s.search != nil {
		checks["search"] = "ok"
		if _, err := s.search.Count(); err != nil {
			checks["search"], degraded = err.Error(), true
		}
	}
	if degraded {
		return gen.GetHealth503JSONResponse{Status: gen.HealthStatusDegraded, Checks: checks}, nil
	}
	return gen.GetHealth200JSONResponse{Status: gen.HealthStatusOk, Checks: checks}, nil
}

func (s *Server) GetVersion(context.Context, gen.GetVersionRequestObject) (gen.GetVersionResponseObject, error) {
	return gen.GetVersion200JSONResponse{
		Version:   version.Version,
		Commit:    version.Commit,
		BuildTime: version.BuildTime,
	}, nil
}

// ---------- errors ----------

// apiError is an error a handler returns to produce a specific response.
type apiError struct {
	status  int
	code    string
	message string
}

func (e *apiError) Error() string { return e.message }

var (
	errUnauthorized    = &apiError{http.StatusUnauthorized, "unauthorized", "请先登录"}
	errDeviceForbidden = &apiError{http.StatusForbidden, "device_forbidden", "这台手机不能做这件事，请在网页端操作"}
	errNotAdmin        = &apiError{http.StatusForbidden, "forbidden", "需要管理员权限"}
	errRoleTooLow      = &apiError{http.StatusForbidden, "forbidden", "你在这个空间没有执行此操作的权限"}
	// One response for a workspace that does not exist, was deleted, or that
	// the caller is not a member of: the three must be indistinguishable.
	errWorkspaceNotFound = &apiError{http.StatusNotFound, "workspace_not_found", "找不到这个空间，或你已不是它的成员"}
)

func invalid(msg string) error { return &apiError{http.StatusBadRequest, "invalid_request", msg} }

var kindStatus = map[apperr.Kind]struct {
	status int
	code   string
}{
	apperr.KindInvalid:      {http.StatusBadRequest, "invalid_request"},
	apperr.KindUnauthorized: {http.StatusUnauthorized, "unauthorized"},
	apperr.KindForbidden:    {http.StatusForbidden, "forbidden"},
	apperr.KindNotFound:     {http.StatusNotFound, "not_found"},
	apperr.KindConflict:     {http.StatusConflict, "conflict"},
	apperr.KindRateLimited:  {http.StatusTooManyRequests, "rate_limited"},
	apperr.KindUnavailable:  {http.StatusServiceUnavailable, "unavailable"},
}

// respondError turns a handler error into the unified error body.
func (s *Server) respondError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apiError
	var au *apperr.Error
	switch {
	case errors.As(err, &ae):
		writeError(w, r, ae.status, ae.code, ae.message, nil)
	case errors.As(err, &au):
		m, ok := kindStatus[au.Kind]
		if !ok {
			m.status, m.code = http.StatusBadRequest, "invalid_request"
		}
		code := m.code
		if au.Code != "" {
			code = au.Code
		}
		if m.status == http.StatusTooManyRequests {
			if secs, ok := au.Details["retryAfterSeconds"].(int); ok {
				w.Header().Set("Retry-After", strconv.Itoa(secs))
			}
		}
		writeError(w, r, m.status, code, au.Message, au.Details)
	default:
		s.log.ErrorContext(r.Context(), "handler error", "err", err, "path", r.URL.Path, "request_id", RequestID(r.Context()))
		writeError(w, r, http.StatusInternalServerError, "internal", i18n.N("服务器内部错误"), nil)
	}
}

// withLang puts the request's language in its context (v1.10.1): handlers
// translate what they say with i18n.T.
func withLang(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(i18n.With(r.Context(), i18n.FromRequest(r))))
	})
}

// writeError emits the unified error body {code, message, details}, the
// message in the request's language (v1.10.1).
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, details map[string]any) {
	body := gen.Error{Code: code, Message: i18n.Message(i18n.FromRequest(r), message)}
	if details != nil {
		body.Details = &details
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
