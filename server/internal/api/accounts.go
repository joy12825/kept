package api

import (
	"context"
	"kept/server/internal/i18n"
	"net/http"

	"kept/server/internal/api/gen"
	"kept/server/internal/auth"
	"kept/server/internal/tenant"
	"kept/server/internal/watermark"
)

// ---------- setup, login ----------

// defaultSSOName is the button's name without KEPT_OIDC_NAME (config).
const defaultSSOName = "单点登录"

func (s *Server) GetSetupStatus(ctx context.Context, _ gen.GetSetupStatusRequestObject) (gen.GetSetupStatusResponseObject, error) {
	needs, err := s.auth.NeedsSetup(ctx)
	if err != nil {
		return nil, err
	}
	out := gen.GetSetupStatus200JSONResponse{NeedsSetup: needs}
	if name, ok := s.ssoName(); ok {
		// the web names the default in its own language (v1.10.1)
		out.Sso = &struct {
			Default bool   `json:"default"`
			Name    string `json:"name"`
		}{Default: name == defaultSSOName, Name: name}
	}
	return out, nil
}

func (s *Server) CompleteSetup(ctx context.Context, req gen.CompleteSetupRequestObject) (gen.CompleteSetupResponseObject, error) {
	u, err := s.auth.Setup(ctx, newUser(*req.Body))
	if err != nil {
		return nil, err
	}
	resp, err := s.login(ctx, u.Username, req.Body.Password, true)
	if err != nil {
		return nil, err
	}
	return gen.CompleteSetup200JSONResponse{LoggedInJSONResponse: resp}, nil
}

func (s *Server) Login(ctx context.Context, req gen.LoginRequestObject) (gen.LoginResponseObject, error) {
	remember := req.Body.Remember == nil || *req.Body.Remember
	res, err := s.auth.Login(ctx, req.Body.Login, req.Body.Password, remember, clientFrom(ctx).ClientInfo)
	if err != nil {
		return nil, err
	}
	if res.Challenge != "" {
		return gen.Login202JSONResponse{Challenge: res.Challenge, ExpiresAt: res.ChallengeExpiresAt}, nil
	}
	resp, err := s.loggedIn(ctx, res.Token, res.Principal)
	if err != nil {
		return nil, err
	}
	return gen.Login200JSONResponse{LoggedInJSONResponse: resp}, nil
}

func (s *Server) LoginTwoFactor(ctx context.Context, req gen.LoginTwoFactorRequestObject) (gen.LoginTwoFactorResponseObject, error) {
	token, p, err := s.auth.CompleteTwoFactor(ctx, req.Body.Challenge, deref(req.Body.Code), deref(req.Body.RecoveryCode), clientFrom(ctx).ClientInfo)
	if err != nil {
		return nil, err
	}
	resp, err := s.loggedIn(ctx, token, p)
	if err != nil {
		return nil, err
	}
	return gen.LoginTwoFactor200JSONResponse{LoggedInJSONResponse: resp}, nil
}

// login creates a session for the account setup just created. A fresh
// account never hits a two-factor challenge (auth.Login's res.Challenge is
// always empty here).
func (s *Server) login(ctx context.Context, login, password string, remember bool) (gen.LoggedInJSONResponse, error) {
	res, err := s.auth.Login(ctx, login, password, remember, clientFrom(ctx).ClientInfo)
	if err != nil {
		return gen.LoggedInJSONResponse{}, err
	}
	return s.loggedIn(ctx, res.Token, res.Principal)
}

// loggedIn builds the response body and Set-Cookie header for a session
// that was just created, however it got there (a direct login or the second
// step of a two-factor one).
func (s *Server) loggedIn(ctx context.Context, token string, p auth.Principal) (gen.LoggedInJSONResponse, error) {
	me, err := s.me(ctx, p)
	if err != nil {
		return gen.LoggedInJSONResponse{}, err
	}
	cookie := s.newSessionCookie(ctx, token, p.Persistent).String()
	return gen.LoggedInJSONResponse{Body: me, Headers: gen.LoggedInResponseHeaders{SetCookie: &cookie}}, nil
}

func (s *Server) Logout(ctx context.Context, _ gen.LogoutRequestObject) (gen.LogoutResponseObject, error) {
	if err := s.auth.Logout(ctx, mustPrincipal(ctx)); err != nil {
		return nil, err
	}
	cookie := s.clearedSessionCookie(ctx).String()
	return gen.Logout204Response{Headers: gen.Logout204ResponseHeaders{SetCookie: &cookie}}, nil
}

// ---------- me ----------

func (s *Server) GetMe(ctx context.Context, _ gen.GetMeRequestObject) (gen.GetMeResponseObject, error) {
	me, err := s.me(ctx, mustPrincipal(ctx))
	if err != nil {
		return nil, err
	}
	// the language they use now: emails follow it when their account
	// follows the browser (v1.10.2); a request that names none (a script)
	// says nothing about it
	if r, ok := ctx.Value(httpRequestKey{}).(*http.Request); ok && r.Header.Get("Accept-Language") != "" {
		if err := s.auth.SeenLang(ctx, mustPrincipal(ctx).UserID, string(i18n.From(ctx))); err != nil {
			s.log.WarnContext(ctx, "record the language in use", "err", err)
		}
	}
	return gen.GetMe200JSONResponse(me), nil
}

func (s *Server) UpdateMe(ctx context.Context, req gen.UpdateMeRequestObject) (gen.UpdateMeResponseObject, error) {
	p := mustPrincipal(ctx)
	if _, err := s.auth.UpdateProfile(ctx, p.UserID, req.Body.DisplayName, req.Body.Email); err != nil {
		return nil, err
	}
	me, err := s.me(ctx, p)
	if err != nil {
		return nil, err
	}
	return gen.UpdateMe200JSONResponse(me), nil
}

// SetMyLocale sets the account's web language (v1.10.0).
func (s *Server) SetMyLocale(ctx context.Context, req gen.SetMyLocaleRequestObject) (gen.SetMyLocaleResponseObject, error) {
	if err := s.auth.SetLocale(ctx, mustPrincipal(ctx).UserID, string(req.Body.Locale)); err != nil {
		return nil, err
	}
	return gen.SetMyLocale204Response{}, nil
}

func (s *Server) ChangePassword(ctx context.Context, req gen.ChangePasswordRequestObject) (gen.ChangePasswordResponseObject, error) {
	if err := s.auth.ChangePassword(ctx, mustPrincipal(ctx), req.Body.CurrentPassword, req.Body.NewPassword); err != nil {
		return nil, err
	}
	return gen.ChangePassword204Response{}, nil
}

func (s *Server) ListTokens(ctx context.Context, _ gen.ListTokensRequestObject) (gen.ListTokensResponseObject, error) {
	tokens, err := s.auth.ListTokens(ctx, mustPrincipal(ctx).UserID)
	if err != nil {
		return nil, err
	}
	out := make(gen.ListTokens200JSONResponse, len(tokens))
	for i, t := range tokens {
		out[i] = toAPIToken(t)
	}
	return out, nil
}

func (s *Server) CreateToken(ctx context.Context, req gen.CreateTokenRequestObject) (gen.CreateTokenResponseObject, error) {
	plain, t, err := s.auth.CreateToken(ctx, mustPrincipal(ctx).UserID, req.Body.Name)
	if err != nil {
		return nil, err
	}
	return gen.CreateToken201JSONResponse{Token: plain, ApiToken: toAPIToken(t)}, nil
}

func (s *Server) RevokeToken(ctx context.Context, req gen.RevokeTokenRequestObject) (gen.RevokeTokenResponseObject, error) {
	if err := s.auth.RevokeToken(ctx, mustPrincipal(ctx).UserID, req.Id); err != nil {
		return nil, err
	}
	return gen.RevokeToken204Response{}, nil
}

// ---------- admin ----------

func (s *Server) AdminListUsers(ctx context.Context, _ gen.AdminListUsersRequestObject) (gen.AdminListUsersResponseObject, error) {
	users, err := s.auth.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := make(gen.AdminListUsers200JSONResponse, len(users))
	for i, u := range users {
		out[i] = toAPIUser(u)
	}
	return out, nil
}

func (s *Server) AdminCreateUser(ctx context.Context, req gen.AdminCreateUserRequestObject) (gen.AdminCreateUserResponseObject, error) {
	b := req.Body
	if err := s.canAddMember(ctx); err != nil { // v1.12
		return nil, err
	}
	u, err := s.auth.CreateUser(ctx, auth.NewUser{
		Username: deref(b.Username), Password: b.Password, DisplayName: deref(b.DisplayName),
		Email: b.Email, IsAdmin: b.IsAdmin != nil && *b.IsAdmin,
	})
	if err != nil {
		return nil, err
	}
	return gen.AdminCreateUser201JSONResponse(toAPIUser(u)), nil
}

func (s *Server) AdminUpdateUser(ctx context.Context, req gen.AdminUpdateUserRequestObject) (gen.AdminUpdateUserResponseObject, error) {
	p := mustPrincipal(ctx)
	if d := req.Body.Disabled; d != nil {
		if *d && req.Id == p.UserID {
			return nil, invalid("不能停用自己的账号")
		}
		if !*d { // enabling one again counts as one more (v1.12)
			if u, err := s.auth.GetUser(ctx, req.Id); err == nil && u.Disabled {
				if err := s.canAddMember(ctx); err != nil {
					return nil, err
				}
			}
		}
		if err := s.auth.SetDisabled(ctx, req.Id, *d); err != nil {
			return nil, err
		}
	}
	if a := req.Body.IsAdmin; a != nil {
		if err := s.auth.SetAdmin(ctx, req.Id, *a); err != nil {
			return nil, err
		}
	}
	u, err := s.auth.GetUser(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return gen.AdminUpdateUser200JSONResponse(toAPIUser(u)), nil
}

func (s *Server) AdminSetPassword(ctx context.Context, req gen.AdminSetPasswordRequestObject) (gen.AdminSetPasswordResponseObject, error) {
	if err := s.auth.SetPassword(ctx, req.Id, req.Body.Password); err != nil {
		return nil, err
	}
	return gen.AdminSetPassword204Response{}, nil
}

// ---------- conversions ----------

func (s *Server) me(ctx context.Context, p auth.Principal) (gen.Me, error) {
	u, err := s.auth.GetUser(ctx, p.UserID)
	if err != nil {
		return gen.Me{}, err
	}
	// The current workspace: the one the request selected, or, right after
	// logging in (no scope in the context yet), the personal one.
	scope, ok := tenant.From(ctx)
	if !ok {
		scope = p.Scope
	}
	_, ws, err := s.workspaces.Resolve(ctx, p.UserID, scope.WorkspaceID())
	if err != nil {
		return gen.Me{}, err
	}
	me := gen.Me{User: toAPIUser(u), Workspace: toAPIWorkspace(ws), Locale: gen.Locale(u.Locale)}
	lookup := s.bookLookup.Enabled()
	bodies := s.mailCanSaveBodies()
	wm := watermark.Available()
	me.Features = &struct {
		BookLookup *bool `json:"bookLookup,omitempty"`
		MailBody   *bool `json:"mailBody,omitempty"`
		Smtp       bool  `json:"smtp"`
		Watermark  *bool `json:"watermark,omitempty"`
	}{Smtp: s.smtpEnabled(), BookLookup: &lookup, MailBody: &bodies, Watermark: &wm}
	if p.Via == auth.ViaSession {
		me.CsrfToken = &p.CSRFToken
	}
	if until := s.auth.VerifiedUntil(p); !until.IsZero() {
		me.VerifiedUntil = &until
	}
	return me, nil
}

func newUser(b gen.NewAccountRequest) auth.NewUser {
	return auth.NewUser{Username: deref(b.Username), Password: b.Password, DisplayName: deref(b.DisplayName), Email: b.Email}
}

func toAPIUser(u auth.User) gen.User {
	out := gen.User{
		Id: u.ID, Username: u.Username, DisplayName: u.DisplayName,
		IsAdmin: u.IsAdmin, Disabled: u.Disabled, CreatedAt: u.CreatedAt,
		TwoFactorEnabled: u.TwoFactorEnabled,
	}
	if u.SSO {
		out.Sso = &u.SSO
	}
	if u.Email != "" {
		out.Email = &u.Email
	}
	return out
}

func toAPIToken(t auth.Token) gen.ApiToken {
	return gen.ApiToken{Id: t.ID, Name: t.Name, Prefix: t.Prefix, CreatedAt: t.CreatedAt, LastUsedAt: t.LastUsedAt}
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (s *Server) AdminUnlinkUserSSO(ctx context.Context, req gen.AdminUnlinkUserSSORequestObject) (gen.AdminUnlinkUserSSOResponseObject, error) {
	if err := s.auth.UnlinkSSO(ctx, req.Id); err != nil {
		return nil, err
	}
	return gen.AdminUnlinkUserSSO204Response{}, nil
}
