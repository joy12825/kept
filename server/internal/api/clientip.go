package api

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// The client's address (v1.0.1). Behind a reverse proxy every connection
// comes from the proxy; its X-Forwarded-For is believed only when the
// connection comes from KEPT_TRUSTED_PROXIES, since anyone can send the
// header. Login limits, sessions and the activity log use this address.

type clientIPKey struct{}

// ClientIP is the client's address the clientIP middleware worked out.
func ClientIP(ctx context.Context) string {
	ip, _ := ctx.Value(clientIPKey{}).(string)
	return ip
}

// clientAddress returns the client's address: the connection's, or, when
// that is a trusted proxy, the rightmost X-Forwarded-For entry that is not
// itself a trusted proxy.
func clientAddress(remoteAddr string, forwarded []string, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || !isTrusted(addr, trusted) {
		return host
	}
	var hops []string
	for _, h := range forwarded {
		hops = append(hops, strings.Split(h, ",")...)
	}
	for i := len(hops) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(hops[i])
		a, err := netip.ParseAddr(hop)
		if err != nil {
			if ap, err2 := netip.ParseAddrPort(hop); err2 == nil {
				a, err = ap.Addr(), nil
			}
		}
		if err != nil {
			break // not an address: stop at what we can vouch for
		}
		addr = a.Unmap()
		if !isTrusted(addr, trusted) {
			break
		}
	}
	return addr.String()
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	a = a.Unmap()
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// clientIP stores the client's address in the request context. The first
// X-Forwarded-For from an untrusted address is logged once, with the
// setting that would make it count.
func (s *Server) clientIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fwd := r.Header.Values("X-Forwarded-For")
		ip := clientAddress(r.RemoteAddr, fwd, s.trustedProxies)
		if len(fwd) > 0 {
			if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil && host == ip {
				s.warnForwarded.Do(func() {
					s.log.Warn("ignoring X-Forwarded-For from an untrusted address; if kept is behind a reverse proxy, set KEPT_TRUSTED_PROXIES to its address",
						"from", host)
				})
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey{}, ip)))
	})
}
