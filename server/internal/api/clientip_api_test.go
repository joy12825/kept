package api

import (
	"fmt"
	"net/http"
	"net/netip"
	"testing"
)

// Behind a trusted proxy each client is limited and recorded by its own
// address, not the proxy's.
func TestTrustedProxyGivesEachClientItsOwnAddress(t *testing.T) {
	e := newEnvDeps(t, "auto", nil, nil, func(d *Deps) {
		d.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("192.0.2.10/32")} // the test requests' address
	})
	e.user("wang")
	attacker := e.anon()
	attacker.headers["X-Forwarded-For"] = "198.51.100.66"
	for i := range 50 {
		attacker.do(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": fmt.Sprintf("nobody%d", i), "password": "nope-nope"})
	}
	attacker.mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "wang", "password": "password-123"}, http.StatusTooManyRequests)

	user := e.anon()
	user.headers["X-Forwarded-For"] = "203.0.113.7"
	user.mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "wang", "password": "password-123"}, http.StatusOK)
	var n int
	if err := e.db.QueryRow(`SELECT count(*) FROM sessions WHERE ip = '203.0.113.7'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("sessions from the client's address = %d %v", n, err)
	}
}

// Without KEPT_TRUSTED_PROXIES the header is anyone's to send and is ignored.
func TestForwardedForIsIgnoredByDefault(t *testing.T) {
	e := newEnv(t, "auto", nil)
	e.user("wang")
	c := e.anon()
	c.headers["X-Forwarded-For"] = "203.0.113.7"
	c.mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "wang", "password": "password-123"}, http.StatusOK)
	var n int
	if err := e.db.QueryRow(`SELECT count(*) FROM sessions WHERE ip = '203.0.113.7'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("sessions recorded with the forged address = %d %v", n, err)
	}
}
