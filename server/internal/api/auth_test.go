package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"kept/server/internal/auth"
)

func TestSetupFlow(t *testing.T) {
	e := newEnv(t, "auto", nil)
	c := e.anon()

	r := c.mustDo(http.MethodGet, "/api/v1/setup", nil, http.StatusOK)
	if r.body["needsSetup"] != true {
		t.Fatalf("fresh instance: %v", r.body)
	}
	r = c.mustDo(http.MethodPost, "/api/v1/setup", map[string]any{"username": "Admin", "email": "admin@example.com", "password": "password-123", "displayName": "管理员"}, http.StatusOK)
	user := r.body["user"].(map[string]any)
	if user["username"] != "admin" || user["isAdmin"] != true || r.body["csrfToken"] == nil {
		t.Errorf("setup response: %v", r.body)
	}
	setCookie := r.header.Get("Set-Cookie")
	for _, want := range []string{"kept_session=", "HttpOnly", "SameSite=Lax", "Path=/"} {
		if !strings.Contains(setCookie, want) {
			t.Errorf("Set-Cookie %q lacks %s", setCookie, want)
		}
	}
	if strings.Contains(setCookie, "Secure") {
		t.Error("plain HTTP request got a Secure cookie in auto mode")
	}

	c.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK)
	if r := e.anon().mustDo(http.MethodGet, "/api/v1/setup", nil, http.StatusOK); r.body["needsSetup"] != false {
		t.Errorf("after setup: %v", r.body)
	}
	r = e.anon().mustDo(http.MethodPost, "/api/v1/setup", map[string]any{"username": "other", "email": "other@example.com", "password": "password-123"}, http.StatusConflict)
	if !strings.Contains(r.body["message"].(string), "已经初始化") {
		t.Errorf("second setup message: %v", r.body)
	}
}

func TestSetupByEmailGeneratesUsernameAndLoginByEmail(t *testing.T) {
	e := newEnv(t, "auto", nil)
	c := e.anon()
	r := c.mustDo(http.MethodPost, "/api/v1/setup", map[string]any{"email": "Wang.Xiao@Example.com", "password": "password-123", "displayName": "小王"}, http.StatusOK)
	user := r.body["user"].(map[string]any)
	if user["username"] != "wang.xiao" {
		t.Errorf("auto-generated username: %v", r.body)
	}
	r = e.anon().mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "wang.xiao@example.com", "password": "password-123"}, http.StatusOK)
	if r.body["user"].(map[string]any)["username"] != "wang.xiao" {
		t.Errorf("login by email: %v", r.body)
	}
}

func TestProtectedEndpointRequiresLogin(t *testing.T) {
	e := newEnv(t, "auto", nil)
	r := e.anon().mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusUnauthorized)
	if code(r) != "unauthorized" {
		t.Errorf("body: %v", r.body)
	}
}

func TestLoginErrors(t *testing.T) {
	e := newEnv(t, "auto", nil)
	e.user("wang")
	c := e.anon()
	r := c.mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "wang", "password": "nope-nope"}, http.StatusUnauthorized)
	if r.body["message"] != "用户名或密码不正确" {
		t.Errorf("wrong password: %v", r.body)
	}
	if got := details(r)["remainingAttempts"]; got != float64(4) {
		t.Errorf("remainingAttempts = %v, want 4", got)
	}
	for range 4 {
		c.do(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "wang", "password": "nope-nope"})
	}
	r = c.mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "wang", "password": "password-123"}, http.StatusTooManyRequests)
	if secs, ok := details(r)["retryAfterSeconds"].(float64); !ok || secs <= 0 {
		t.Errorf("retryAfterSeconds = %v", details(r)["retryAfterSeconds"])
	}
	if ra := r.header.Get("Retry-After"); ra == "" {
		t.Error("429 response is missing the Retry-After header")
	} else if want := itoa(details(r)["retryAfterSeconds"].(float64)); ra != want {
		t.Errorf("Retry-After header = %q, want %q", ra, want)
	}
}

func TestRememberMe(t *testing.T) {
	e := newEnv(t, "auto", nil)
	e.user("wang")

	c := e.anon()
	r := c.mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "wang", "password": "password-123", "remember": false}, http.StatusOK)
	if strings.Contains(r.header.Get("Set-Cookie"), "Max-Age") {
		t.Errorf("remember=false Set-Cookie has Max-Age: %q", r.header.Get("Set-Cookie"))
	}

	d := e.anon()
	r = d.mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "wang", "password": "password-123"}, http.StatusOK)
	if !strings.Contains(r.header.Get("Set-Cookie"), "Max-Age") {
		t.Errorf("default login Set-Cookie lacks Max-Age: %q", r.header.Get("Set-Cookie"))
	}
}

func TestNonPersistentSessionExpiresSoonerThanPersistent(t *testing.T) {
	e, c := newEnvWithClock(t, "auto")
	if _, err := e.auth.CreateUser(context.Background(), auth.NewUser{Username: "wang", Password: "password-123", Email: "wang@example.com"}); err != nil {
		t.Fatal(err)
	}

	brief := e.anon()
	brief.mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "wang", "password": "password-123", "remember": false}, http.StatusOK)

	lasting := e.anon()
	lasting.mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "wang", "password": "password-123", "remember": true}, http.StatusOK)

	c.Add(13 * time.Hour)
	brief.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusUnauthorized)
	lasting.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK)
}

func TestMalformedBodyIsA400(t *testing.T) {
	e := newEnv(t, "auto", nil)
	req := e.anon()
	req.headers["Content-Type"] = "application/json"
	r := req.do(http.MethodPost, "/api/v1/auth/login", "not an object")
	if r.code != http.StatusBadRequest || code(r) != "invalid_request" {
		t.Errorf("got %d %v", r.code, r.body)
	}
}

func TestCSRFRequiredForCookieWrites(t *testing.T) {
	e := newEnv(t, "auto", nil)
	c := e.user("wang")
	token := c.csrf
	c.csrf = ""
	r := c.mustDo(http.MethodPost, "/api/v1/me/tokens", map[string]any{"name": "脚本"}, http.StatusForbidden)
	if code(r) != "csrf_failed" {
		t.Errorf("body: %v", r.body)
	}
	c.csrf = "wrong"
	c.mustDo(http.MethodPost, "/api/v1/me/tokens", map[string]any{"name": "脚本"}, http.StatusForbidden)
	c.csrf = token
	c.mustDo(http.MethodPost, "/api/v1/me/tokens", map[string]any{"name": "脚本"}, http.StatusCreated)
}

func TestLoginRejectsCrossOriginPost(t *testing.T) {
	e := newEnv(t, "auto", nil)
	e.user("wang")
	c := e.anon()
	c.headers["Origin"] = "https://evil.example"
	r := c.mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "wang", "password": "password-123"}, http.StatusForbidden)
	if code(r) != "csrf_failed" {
		t.Errorf("body: %v", r.body)
	}
	c.headers["Origin"] = "http://example.com" // httptest requests use Host example.com
	c.mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "wang", "password": "password-123"}, http.StatusOK)
}

func TestBearerTokenAuth(t *testing.T) {
	e := newEnv(t, "auto", nil)
	c := e.user("wang")
	r := c.mustDo(http.MethodPost, "/api/v1/me/tokens", map[string]any{"name": "扫描仪"}, http.StatusCreated)
	plain := r.body["token"].(string)

	b := e.anon()
	b.bearer = plain
	r = b.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK)
	if _, has := r.body["csrfToken"]; has {
		t.Error("bearer callers should not receive a CSRF token")
	}
	b.mustDo(http.MethodPost, "/api/v1/me/tokens", map[string]any{"name": "第二个"}, http.StatusCreated) // no CSRF needed

	list := c.mustDo(http.MethodGet, "/api/v1/me/tokens", nil, http.StatusOK).list
	if len(list) != 2 {
		t.Fatalf("tokens: %v", list)
	}
	if strings.Contains(c.do(http.MethodGet, "/api/v1/me/tokens", nil).raw, plain) {
		t.Error("token list leaks the plaintext token")
	}

	b.bearer = "kept_invalid"
	b.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusUnauthorized)
}

func TestLogout(t *testing.T) {
	e := newEnv(t, "auto", nil)
	c := e.user("wang")
	old := c.cookie
	r := c.mustDo(http.MethodPost, "/api/v1/auth/logout", nil, http.StatusNoContent)
	if !strings.Contains(r.header.Get("Set-Cookie"), "Max-Age=0") {
		t.Errorf("logout did not clear the cookie: %q", r.header.Get("Set-Cookie"))
	}
	c.cookie = old
	c.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusUnauthorized)
}

func TestUpdateMeAndChangePassword(t *testing.T) {
	e := newEnv(t, "auto", nil)
	c := e.user("wang")
	r := c.mustDo(http.MethodPatch, "/api/v1/me", map[string]any{"displayName": "小王", "email": "w@example.com"}, http.StatusOK)
	if r.body["user"].(map[string]any)["displayName"] != "小王" {
		t.Errorf("updateMe: %v", r.body)
	}
	// the web's language (v1.10.0): follows the browser until set
	if r.body["locale"] != "" {
		t.Errorf("default locale: %v", r.body["locale"])
	}
	c.mustDo(http.MethodPut, "/api/v1/me/locale", map[string]any{"locale": "en"}, http.StatusNoContent)
	c.mustDo(http.MethodPut, "/api/v1/me/locale", map[string]any{"locale": "fr"}, http.StatusBadRequest)
	if l := c.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK).body["locale"]; l != "en" {
		t.Errorf("locale after setting: %v", l)
	}
	c.mustDo(http.MethodPost, "/api/v1/me/password", map[string]any{"currentPassword": "bad-password", "newPassword": "new-password-1"}, http.StatusBadRequest)
	c.mustDo(http.MethodPost, "/api/v1/me/password", map[string]any{"currentPassword": "password-123", "newPassword": "new-password-1"}, http.StatusNoContent)
	c.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK) // current session survives
	e.anon().mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "wang", "password": "new-password-1"}, http.StatusOK)
}

func TestSecureCookieModes(t *testing.T) {
	always := newEnv(t, "always", nil)
	always.user("wang")
	r := always.anon().do(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "wang", "password": "password-123"})
	if !strings.Contains(r.header.Get("Set-Cookie"), "Secure") {
		t.Errorf("always mode: %q", r.header.Get("Set-Cookie"))
	}

	auto := newEnv(t, "auto", nil)
	auto.user("wang")
	c := auto.anon()
	c.headers["X-Forwarded-Proto"] = "https"
	r = c.do(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "wang", "password": "password-123"})
	if !strings.Contains(r.header.Get("Set-Cookie"), "Secure") {
		t.Errorf("auto mode behind an HTTPS proxy: %q", r.header.Get("Set-Cookie"))
	}
}

// Accounts are handed out by administrators; there is no self-registration.
func TestNoSelfRegistration(t *testing.T) {
	e := newEnv(t, "auto", nil)
	admin := e.setupAdmin()
	newbie := map[string]any{"username": "newbie", "email": "newbie@example.com", "password": "password-123"}
	if r := e.anon().do(http.MethodPost, "/api/v1/auth/register", newbie); r.code != http.StatusNotFound {
		t.Errorf("POST /auth/register: %d %v, want 404", r.code, r.body)
	}
	r := e.anon().mustDo(http.MethodGet, "/api/v1/setup", nil, http.StatusOK)
	if _, ok := r.body["allowRegistration"]; ok || r.body["needsSetup"] != false {
		t.Errorf("setup status: %v", r.body)
	}
	for _, method := range []string{http.MethodGet, http.MethodPatch} {
		if r := admin.do(method, "/api/v1/admin/settings", map[string]any{"allowRegistration": true}); r.code != http.StatusNotFound {
			t.Errorf("%s /admin/settings: %d %v, want 404", method, r.code, r.body)
		}
	}
	if n := len(admin.mustDo(http.MethodGet, "/api/v1/admin/users", nil, http.StatusOK).list); n != 1 {
		t.Errorf("%d accounts exist, want only the administrator", n)
	}
}

func TestAdminEndpoints(t *testing.T) {
	e := newEnv(t, "auto", nil)
	admin := e.setupAdmin()

	r := admin.mustDo(http.MethodPost, "/api/v1/admin/users", map[string]any{"username": "bob", "email": "bob@example.com", "password": "password-123"}, http.StatusCreated)
	bobID := r.body["id"].(float64)
	if len(admin.mustDo(http.MethodGet, "/api/v1/admin/users", nil, http.StatusOK).list) != 2 {
		t.Error("admin list should show 2 users")
	}

	bob := e.anon()
	bob.mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "bob", "password": "password-123"}, http.StatusOK)
	if r := bob.do(http.MethodGet, "/api/v1/admin/users", nil); r.code != http.StatusForbidden || code(r) != "forbidden" {
		t.Errorf("non-admin GET /admin/users: %d %v", r.code, r.body)
	}

	path := "/api/v1/admin/users/" + itoa(bobID)
	admin.mustDo(http.MethodPatch, path, map[string]any{"disabled": true}, http.StatusOK)
	bob.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusUnauthorized) // sessions ended
	admin.mustDo(http.MethodPatch, path, map[string]any{"disabled": false}, http.StatusOK)
	admin.mustDo(http.MethodPost, path+"/password", map[string]any{"password": "reset-pass-1"}, http.StatusNoContent)
	e.anon().mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "bob", "password": "reset-pass-1"}, http.StatusOK)

	admin.mustDo(http.MethodPatch, "/api/v1/admin/users/9999", map[string]any{"disabled": true}, http.StatusNotFound)
}

func TestAdminCannotDisableThemselves(t *testing.T) {
	e := newEnv(t, "auto", nil)
	admin := e.setupAdmin()
	me := admin.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK)
	id := me.body["user"].(map[string]any)["id"].(float64)
	admin.mustDo(http.MethodPatch, "/api/v1/admin/users/"+itoa(id), map[string]any{"disabled": true}, http.StatusBadRequest)
}

func itoa(f float64) string { return strconv.FormatInt(int64(f), 10) }

func details(r resp) map[string]any {
	d, _ := r.body["details"].(map[string]any)
	return d
}
