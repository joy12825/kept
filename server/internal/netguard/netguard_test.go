package netguard

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestCheckAddr(t *testing.T) {
	home := Policy{AllowPrivate: true}
	strict := Policy{}
	for _, tc := range []struct {
		ip           string
		home, strict bool // allowed
	}{
		{"8.8.8.8", true, true},
		{"2606:4700::1111", true, true},
		{"192.168.1.20", true, false}, // Home Assistant at home
		{"192.168.1.10", true, false},
		{"172.17.0.3", true, false},
		{"fd12::1", true, false},
		{"127.0.0.1", false, false},
		{"::1", false, false},
		{"::ffff:127.0.0.1", false, false},
		{"169.254.169.254", false, false}, // cloud metadata
		{"fd00:ec2::254", false, false},
		{"fe80::1", false, false},
		{"0.0.0.0", false, false},
		{"224.0.0.1", false, false},
	} {
		ip := netip.MustParseAddr(tc.ip)
		if got := home.CheckAddr(ip) == nil; got != tc.home {
			t.Errorf("%s at home: allowed %v", tc.ip, got)
		}
		if got := strict.CheckAddr(ip) == nil; got != tc.strict {
			t.Errorf("%s strict: allowed %v", tc.ip, got)
		}
	}
}

func TestCheckURL(t *testing.T) {
	p := Policy{AllowPrivate: true, DenyHosts: []string{"gotenberg:3000"}}
	for raw, ok := range map[string]bool{
		"https://hooks.example.com/x": true, "http://192.168.1.5:8123/api/webhook/kept": true,
		"ftp://example.com": false, "file:///etc/passwd": false, "http://localhost:8000": false, "http://a.localhost": false,
		"http://127.0.0.1/": false, "http://[::1]/": false, "http://169.254.169.254/latest/meta-data/": false,
		"http://gotenberg:3000/forms/chromium/convert/url": false, "http://GOTENBERG/": false, "not a url": false,
	} {
		if got := p.CheckURL(raw) == nil; got != ok {
			t.Errorf("%s: allowed %v", raw, got)
		}
	}
}

// The client checks the address it connects to: a name for the loopback
// address, or a redirect there, is refused.
func TestClientRefusesWhatResolvesToTheServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer srv.Close()
	c := Policy{AllowPrivate: true}.Client(2 * time.Second)
	if _, err := c.Get(srv.URL); err == nil || !strings.Contains(err.Error(), "不允许") {
		t.Errorf("%s: %v", srv.URL, err)
	}
	if _, err := c.Get("http://localhost" + srv.URL[strings.LastIndex(srv.URL, ":"):]); err == nil {
		t.Error("localhost went through")
	}
	// a redirect to the server itself
	out := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL, http.StatusFound)
	}))
	defer out.Close()
	open := Policy{AllowPrivate: true}
	open.Resolve = func(context.Context, string) ([]netip.Addr, error) { return nil, nil }
	if _, err := open.Client(2 * time.Second).Get(out.URL); err == nil {
		t.Error("a redirect to loopback went through")
	}
}

func TestDeniedHostIsRefusedByAddress(t *testing.T) {
	p := Policy{AllowPrivate: true, DenyHosts: []string{"gotenberg:3000"},
		Resolve: func(_ context.Context, host string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("172.18.0.2")}, nil
		}}
	if err := p.checkDenied(netip.MustParseAddr("172.18.0.2")); !errors.Is(err, ErrBlocked) {
		t.Errorf("gotenberg by address: %v", err)
	}
	if err := p.checkDenied(netip.MustParseAddr("172.18.0.9")); err != nil {
		t.Errorf("another address: %v", err)
	}
}
