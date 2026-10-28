package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"kept/server/internal/audit"
	"kept/server/internal/auth"
	"kept/server/internal/i18n"
	"kept/server/internal/tenant"
	"kept/server/internal/workspace"
)

const sessionCookie = "kept_session"

// workspaceHeader selects the workspace a request acts in; without it the
// caller's personal workspace is used.
const workspaceHeader = "X-Workspace-ID"

type httpRequestKey struct{}

type principalKey struct{}
type clientKey struct{}
type workspaceInvalidKey struct{}

type clientInfo struct {
	auth.ClientInfo
	secure bool // set the Secure flag on cookies
}

func principalFrom(ctx context.Context) (auth.Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(auth.Principal)
	return p, ok
}

func mustPrincipal(ctx context.Context) auth.Principal {
	p, ok := principalFrom(ctx)
	if !ok {
		panic("api: handler reached without a principal")
	}
	return p
}

func clientFrom(ctx context.Context) clientInfo {
	c, _ := ctx.Value(clientKey{}).(clientInfo)
	return c
}

// authenticate resolves the caller from a Bearer token or the session
// cookie and stores the principal and its workspace scope in the context.
// Missing or invalid credentials are not an error here: the authorize
// middleware decides per operation whether a caller is required.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), clientKey{}, s.clientInfo(r))
		// Until a caller is known, audit entries (first-run setup) still
		// carry the request ID.
		anonymous := audit.ActorFrom(ctx)
		anonymous.RequestID = RequestID(ctx)
		ctx = audit.WithActor(ctx, anonymous)
		var (
			p   auth.Principal
			err error
		)
		switch {
		case strings.HasPrefix(r.Header.Get("Authorization"), "Bearer "):
			p, err = s.auth.AuthenticateToken(ctx, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		default:
			ck, cerr := r.Cookie(sessionCookie)
			if cerr != nil || ck.Value == "" {
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			p, err = s.auth.Authenticate(ctx, ck.Value)
			if err == nil && p.Refreshed {
				http.SetCookie(w, s.newSessionCookie(ctx, ck.Value, p.Persistent))
			}
			if isUnauthenticated(err) {
				http.SetCookie(w, s.clearedSessionCookie(ctx))
			}
		}
		if err != nil && !isUnauthenticated(err) {
			s.respondError(w, r, err)
			return
		}
		if err == nil {
			ctx = context.WithValue(ctx, principalKey{}, p)
			scope := p.Scope // the personal workspace, unless the request names another
			raw := r.Header.Get(workspaceHeader)
			if p.Via == auth.ViaDevice {
				raw = "" // a phone always acts in the workspace it was paired for
			}
			if raw == "" && r.Method == http.MethodGet {
				// EventSource, <img> and download links cannot send headers, so
				// reads may name the workspace in the query string instead.
				raw = r.URL.Query().Get("workspace")
			}
			if raw != "" {
				resolved, rerr := s.resolveWorkspace(ctx, p.UserID, raw)
				switch {
				case rerr == nil:
					scope = resolved
				case errors.Is(rerr, workspace.ErrNotFound):
					// Not refused here: logging out must keep working with a
					// stale header. authorize rejects everything else.
					ctx = context.WithValue(ctx, workspaceInvalidKey{}, true)
				default:
					s.respondError(w, r, rerr)
					return
				}
			}
			ctx = tenant.With(ctx, scope)
			via := audit.ViaSession
			switch p.Via {
			case auth.ViaToken:
				via = audit.ViaToken
			case auth.ViaDevice:
				via = audit.ViaDevice
			}
			ctx = audit.WithActor(ctx, audit.Actor{UserID: p.UserID, Name: p.DisplayName, Via: via, RequestID: RequestID(ctx)})
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// resolveWorkspace turns an X-Workspace-ID value into the caller's scope
// there. A malformed ID is reported exactly like an unknown workspace.
func (s *Server) resolveWorkspace(ctx context.Context, userID int64, raw string) (tenant.Scope, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return tenant.Scope{}, workspace.ErrNotFound
	}
	scope, _, err := s.workspaces.Resolve(ctx, userID, id)
	return scope, err
}

// workspaceInvalid reports whether the request named a workspace the caller
// cannot use.
func workspaceInvalid(ctx context.Context) bool {
	invalid, _ := ctx.Value(workspaceInvalidKey{}).(bool)
	return invalid
}

func isUnauthenticated(err error) bool {
	ae, ok := err.(*auth.Error)
	return ok && ae.Kind == auth.KindUnauthorized
}

func unsafeMethod(m string) bool {
	return m != http.MethodGet && m != http.MethodHead && m != http.MethodOptions
}

// csrf protects cookie-authenticated writes with a per-session token sent
// in X-CSRF-Token. Writes without a session (login, setup) are
// rejected when the browser reports a foreign Origin.
func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !unsafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		p, ok := principalFrom(r.Context())
		switch {
		case ok && p.Via == auth.ViaSession:
			got := r.Header.Get("X-CSRF-Token")
			if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(p.CSRFToken)) != 1 {
				writeError(w, r, http.StatusForbidden, "csrf_failed", i18n.N("页面已过期，请刷新后重试"), nil)
				return
			}
		case !ok:
			if origin := r.Header.Get("Origin"); origin != "" && !sameHost(origin, r.Host) {
				writeError(w, r, http.StatusForbidden, "csrf_failed", i18n.N("请求来源不受信任"), nil)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func sameHost(origin, host string) bool {
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, host)
}

func (s *Server) clientInfo(r *http.Request) clientInfo {
	ip := ClientIP(r.Context())
	if ip == "" { // outside the middleware chain
		ip = clientAddress(r.RemoteAddr, nil, nil)
	}
	secure := false
	switch s.secureCookie {
	case "always":
		secure = true
	case "auto":
		secure = r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	}
	return clientInfo{ClientInfo: auth.ClientInfo{IP: ip, UserAgent: r.UserAgent()}, secure: secure}
}

// newSessionCookie builds the Set-Cookie for a session. persistent sessions
// carry Max-Age so they survive browser restarts; non-persistent ones are a
// browser-session cookie (no Max-Age), cleared when the browser closes.
func (s *Server) newSessionCookie(ctx context.Context, token string, persistent bool) *http.Cookie {
	c := &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   clientFrom(ctx).secure,
		SameSite: http.SameSiteLaxMode,
	}
	if persistent {
		c.MaxAge = int(s.auth.SessionTTL() / time.Second)
	}
	return c
}

func (s *Server) clearedSessionCookie(ctx context.Context) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1, // serialized as Max-Age=0
		HttpOnly: true,
		Secure:   clientFrom(ctx).secure,
		SameSite: http.SameSiteLaxMode,
	}
}
