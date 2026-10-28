package api

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// totpCodeAt reimplements RFC 6238 (HMAC-SHA1, 6 digits, 30s step) so the
// test can produce a valid code for a secret the server just generated,
// without reaching into the unexported auth package internals.
func totpCodeAt(t *testing.T, secret string, now time.Time) string {
	t.Helper()
	b, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatalf("decode secret %q: %v", secret, err)
	}
	step := now.Unix() / 30
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, b)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[offset])&0x7f)<<24 | uint32(sum[offset+1])<<16 | uint32(sum[offset+2])<<8 | uint32(sum[offset+3])
	return fmt.Sprintf("%06d", code%1000000)
}

func TestTwoFactorSetupEnableStatus(t *testing.T) {
	e, c := newEnvWithClock(t, "auto")
	alice := e.setupAdmin()

	status := alice.mustDo(http.MethodGet, "/api/v1/me/2fa", nil, http.StatusOK)
	if status.body["enabled"] != false {
		t.Fatalf("initial status: %v", status.body)
	}

	setup := alice.mustDo(http.MethodPost, "/api/v1/me/2fa/setup", nil, http.StatusOK)
	secret, _ := setup.body["secret"].(string)
	uri, _ := setup.body["otpauthUri"].(string)
	if secret == "" || uri == "" {
		t.Fatalf("setup: %v", setup.body)
	}

	if r := alice.do(http.MethodPost, "/api/v1/me/2fa/enable", map[string]any{"code": "000000"}); r.code != http.StatusBadRequest {
		t.Fatalf("enable with a wrong code: status %d, body %s", r.code, r.raw)
	}

	code := totpCodeAt(t, secret, c.Now())
	enabled := alice.mustDo(http.MethodPost, "/api/v1/me/2fa/enable", map[string]any{"code": code}, http.StatusOK)
	codes, _ := enabled.body["codes"].([]any)
	if len(codes) != 10 {
		t.Fatalf("enable: %v", enabled.body)
	}

	status = alice.mustDo(http.MethodGet, "/api/v1/me/2fa", nil, http.StatusOK)
	if status.body["enabled"] != true || status.body["recoveryCodesLeft"] != float64(10) {
		t.Fatalf("status after enable: %v", status.body)
	}

	me := alice.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK)
	user, _ := me.body["user"].(map[string]any)
	if user["twoFactorEnabled"] != true {
		t.Fatalf("me.user.twoFactorEnabled = %v, want true", user["twoFactorEnabled"])
	}

	// Disabling requires the current password.
	if r := alice.do(http.MethodPost, "/api/v1/me/2fa/disable", map[string]any{"password": "wrong"}); r.code != http.StatusBadRequest {
		t.Fatalf("disable with wrong password: status %d, body %s", r.code, r.raw)
	}
	alice.mustDo(http.MethodPost, "/api/v1/me/2fa/disable", map[string]any{"password": "password-123"}, http.StatusNoContent)

	status = alice.mustDo(http.MethodGet, "/api/v1/me/2fa", nil, http.StatusOK)
	if status.body["enabled"] != false || status.body["recoveryCodesLeft"] != float64(0) {
		t.Fatalf("status after disable: %v", status.body)
	}
	me = alice.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK)
	user, _ = me.body["user"].(map[string]any)
	if user["twoFactorEnabled"] != false {
		t.Fatalf("me.user.twoFactorEnabled after disable = %v, want false", user["twoFactorEnabled"])
	}
}

func TestAdminDisableTwoFactorRequiresAdmin(t *testing.T) {
	e := newEnv(t, "auto", nil)
	e.setupAdmin()
	bob := e.user("bob")

	if r := bob.do(http.MethodDelete, "/api/v1/admin/users/1/2fa", nil); r.code != http.StatusForbidden {
		t.Fatalf("bob DELETE .../2fa: status %d, want 403; body %s", r.code, r.raw)
	}
}

// TestAdminCannotDisableOwnTwoFactorWithoutPassword: the admin endpoint must
// not become a password-free way for an admin to turn off their own
// two-factor authentication; that goes through /me/2fa/disable.
func TestAdminCannotDisableOwnTwoFactorWithoutPassword(t *testing.T) {
	e, c := newEnvWithClock(t, "auto")
	admin := e.setupAdmin()
	enableTwoFactor(t, c, admin)

	r := admin.do(http.MethodDelete, "/api/v1/admin/users/1/2fa", nil)
	if r.code != http.StatusBadRequest || code(r) != "invalid_request" {
		t.Fatalf("admin DELETE own .../2fa: status %d, body %s; want 400 invalid_request", r.code, r.raw)
	}
	if msg, _ := r.body["message"].(string); msg != "请在“设置 → 密码与安全”中关闭自己的两步验证" {
		t.Errorf("message = %q", msg)
	}
	status := admin.mustDo(http.MethodGet, "/api/v1/me/2fa", nil, http.StatusOK)
	if status.body["enabled"] != true {
		t.Fatalf("two-factor after refused self-disable: %v, want still enabled", status.body)
	}

	// Another account's two-factor can still be turned off by the admin.
	bob := e.user("bob")
	enableTwoFactor(t, c, bob)
	admin.mustDo(http.MethodDelete, "/api/v1/admin/users/2/2fa", nil, http.StatusNoContent)
	if st := bob.mustDo(http.MethodGet, "/api/v1/me/2fa", nil, http.StatusOK); st.body["enabled"] != false {
		t.Fatalf("bob two-factor after admin disable: %v, want disabled", st.body)
	}
}

// ---------- two-step login (task 4) ----------

// enableTwoFactor turns two-factor authentication on for an already logged
// in client and returns its TOTP secret and one recovery code.
func enableTwoFactor(t *testing.T, c *clock, u *client) (secret string, recoveryCode string) {
	t.Helper()
	setup := u.mustDo(http.MethodPost, "/api/v1/me/2fa/setup", nil, http.StatusOK)
	secret, _ = setup.body["secret"].(string)
	code := totpCodeAt(t, secret, c.Now())
	enabled := u.mustDo(http.MethodPost, "/api/v1/me/2fa/enable", map[string]any{"code": code}, http.StatusOK)
	codes, _ := enabled.body["codes"].([]any)
	if len(codes) == 0 {
		t.Fatalf("enable: %v", enabled.body)
	}
	recoveryCode, _ = codes[0].(string)
	return secret, recoveryCode
}

func TestLoginWithTwoFactorReturns202WithoutACookie(t *testing.T) {
	e, c := newEnvWithClock(t, "auto")
	admin := e.setupAdmin()
	enableTwoFactor(t, c, admin)

	anon := e.anon()
	r := anon.mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "admin", "password": "password-123"}, http.StatusAccepted)
	if r.body["challenge"] == nil || r.body["expiresAt"] == nil {
		t.Fatalf("202 body = %v, want challenge and expiresAt", r.body)
	}
	if r.header.Get("Set-Cookie") != "" {
		t.Errorf("202 response set a cookie: %q", r.header.Get("Set-Cookie"))
	}
}

func TestLoginTwoFactorWithCorrectCodeLogsIn(t *testing.T) {
	e, c := newEnvWithClock(t, "auto")
	admin := e.setupAdmin()
	secret, _ := enableTwoFactor(t, c, admin)

	anon := e.anon()
	r := anon.mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "admin", "password": "password-123"}, http.StatusAccepted)
	challenge, _ := r.body["challenge"].(string)
	if challenge == "" {
		t.Fatalf("202 body = %v, want a challenge", r.body)
	}

	c.Add(30 * time.Second) // past the step consumed by enableTwoFactor's own code
	code := totpCodeAt(t, secret, c.Now())
	r = anon.mustDo(http.MethodPost, "/api/v1/auth/login/2fa", map[string]any{"challenge": challenge, "code": code}, http.StatusOK)
	if r.header.Get("Set-Cookie") == "" {
		t.Error("loginTwoFactor did not set a session cookie")
	}
	anon.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK)
}

func TestLoginTwoFactorWithWrongCodeIsUnauthorized(t *testing.T) {
	e, c := newEnvWithClock(t, "auto")
	admin := e.setupAdmin()
	enableTwoFactor(t, c, admin)

	anon := e.anon()
	r := anon.mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "admin", "password": "password-123"}, http.StatusAccepted)
	challenge, _ := r.body["challenge"].(string)

	r = anon.mustDo(http.MethodPost, "/api/v1/auth/login/2fa", map[string]any{"challenge": challenge, "code": "000000"}, http.StatusUnauthorized)
	if code(r) != "invalid_code" {
		t.Errorf("code = %q, want invalid_code; body %s", code(r), r.raw)
	}
}

func TestLoginTwoFactorExpiredChallengeIsUnauthorized(t *testing.T) {
	e, c := newEnvWithClock(t, "auto")
	admin := e.setupAdmin()
	secret, _ := enableTwoFactor(t, c, admin)

	anon := e.anon()
	r := anon.mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "admin", "password": "password-123"}, http.StatusAccepted)
	challenge, _ := r.body["challenge"].(string)
	c.Add(5*time.Minute + time.Second)

	totp := totpCodeAt(t, secret, c.Now())
	r = anon.mustDo(http.MethodPost, "/api/v1/auth/login/2fa", map[string]any{"challenge": challenge, "code": totp}, http.StatusUnauthorized)
	if code(r) != "challenge_expired" {
		t.Errorf("code = %q, want challenge_expired; body %s", code(r), r.raw)
	}
}

func TestLoginWithoutTwoFactorStillReturns200(t *testing.T) {
	e := newEnv(t, "auto", nil)
	e.setupAdmin()

	anon := e.anon()
	r := anon.mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "admin", "password": "password-123"}, http.StatusOK)
	if r.header.Get("Set-Cookie") == "" {
		t.Error("plain login did not set a session cookie")
	}
}

func TestTwoFactorIsScopedToTheCaller(t *testing.T) {
	e, c := newEnvWithClock(t, "auto")
	alice := e.setupAdmin()
	bob := e.user("bob")

	setup := alice.mustDo(http.MethodPost, "/api/v1/me/2fa/setup", nil, http.StatusOK)
	secret := setup.body["secret"].(string)
	code := totpCodeAt(t, secret, c.Now())
	alice.mustDo(http.MethodPost, "/api/v1/me/2fa/enable", map[string]any{"code": code}, http.StatusOK)

	status := bob.mustDo(http.MethodGet, "/api/v1/me/2fa", nil, http.StatusOK)
	if status.body["enabled"] != false {
		t.Fatalf("bob's status was affected by alice enabling two-factor: %v", status.body)
	}
	me := bob.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK)
	user, _ := me.body["user"].(map[string]any)
	if user["twoFactorEnabled"] != false {
		t.Fatalf("bob.user.twoFactorEnabled = %v, want false", user["twoFactorEnabled"])
	}
}
