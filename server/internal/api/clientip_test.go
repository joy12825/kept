package api

import (
	"net/netip"
	"testing"
)

func TestClientAddress(t *testing.T) {
	proxies := []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("10.0.0.7/32")}
	cases := []struct {
		remote  string
		fwd     []string
		trusted []netip.Prefix
		want    string
	}{
		{"203.0.113.5:4000", nil, proxies, "203.0.113.5"},
		// Not from a trusted proxy: the header is ignored.
		{"203.0.113.5:4000", []string{"1.2.3.4"}, proxies, "203.0.113.5"},
		{"172.18.0.2:4000", []string{"1.2.3.4"}, nil, "172.18.0.2"},
		// From the proxy: the client it saw.
		{"172.18.0.2:4000", []string{"198.51.100.9"}, proxies, "198.51.100.9"},
		// A client-supplied entry before the real one does not count.
		{"172.18.0.2:4000", []string{"6.6.6.6, 198.51.100.9"}, proxies, "198.51.100.9"},
		// Two proxies in a row; headers split over several lines.
		{"172.18.0.2:4000", []string{"198.51.100.9", "10.0.0.7"}, proxies, "198.51.100.9"},
		{"[::ffff:172.18.0.2]:4000", []string{"2001:db8::1"}, proxies, "2001:db8::1"},
		// Garbage stops at what we can vouch for.
		{"172.18.0.2:4000", []string{"unknown"}, proxies, "172.18.0.2"},
		{"172.18.0.2:4000", []string{"198.51.100.9:5555"}, proxies, "198.51.100.9"},
	}
	for _, c := range cases {
		if got := clientAddress(c.remote, c.fwd, c.trusted); got != c.want {
			t.Errorf("clientAddress(%s, %q) = %s, want %s", c.remote, c.fwd, got, c.want)
		}
	}
}
