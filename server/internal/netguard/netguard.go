// Package netguard keeps requests that users point somewhere (workflow
// webhooks, v1.4.3) away from the server itself: every account owns its
// personal space and so can make workflows, and a webhook must not reach
// this machine's own services, the Docker network's helpers, or a cloud
// provider's metadata address. The address is checked when connecting,
// after DNS, so names that resolve to a blocked address, DNS rebinding and
// redirects are caught too.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// Policy says where requests may go.
type Policy struct {
	// AllowPrivate allows the local network (10/8, 172.16/12,
	// 192.168/16, fc00::/7 …): Home Assistant or a NAS at home. Loopback,
	// link-local and metadata addresses are never allowed.
	AllowPrivate bool
	// DenyHosts are host names (or host:port) never to be reached, e.g. the
	// Gotenberg the server itself uses.
	DenyHosts []string
	// DenyAddrs are addresses never to be reached: the server's own
	// (LocalAddrs), so a webhook cannot come back in over the Docker
	// network.
	DenyAddrs []netip.Addr
	// Resolve looks up a deny host's addresses; nil uses the system
	// resolver.
	Resolve func(ctx context.Context, host string) ([]netip.Addr, error)
}

// ErrBlocked is returned for an address the policy refuses.
var ErrBlocked = errors.New("不允许访问这个地址")

var (
	always = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("255.255.255.255/32"),
		netip.MustParsePrefix("fd00:ec2::254/128"), // AWS's IPv6 metadata address
	}
	private = []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.168.0.0/16"),
		netip.MustParsePrefix("100.64.0.0/10"), // carrier-grade NAT
		netip.MustParsePrefix("198.18.0.0/15"), // benchmarking, used by some routers
		netip.MustParsePrefix("fc00::/7"),
	}
)

// CheckAddr refuses an address the policy does not allow.
func (p Policy) CheckAddr(ip netip.Addr) error {
	ip = ip.Unmap()
	switch {
	case !ip.IsValid(), ip.IsLoopback(), ip.IsUnspecified(), ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast(),
		ip.IsInterfaceLocalMulticast(), ip.IsMulticast():
		return fmt.Errorf("%w：%s", ErrBlocked, ip)
	}
	for _, n := range always {
		if n.Contains(ip) {
			return fmt.Errorf("%w：%s", ErrBlocked, ip)
		}
	}
	for _, d := range p.DenyAddrs {
		if d.Unmap() == ip {
			return fmt.Errorf("%w：%s 是这台服务器自己的地址", ErrBlocked, ip)
		}
	}
	if !p.AllowPrivate {
		for _, n := range private {
			if n.Contains(ip) {
				return fmt.Errorf("%w：%s 是局域网地址", ErrBlocked, ip)
			}
		}
	}
	return nil
}

// CheckURL is the check made when a webhook is saved: the scheme, and a
// host that is plainly not allowed (an address, localhost, a denied host).
// Names are checked again when connecting.
func (p Policy) CheckURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return fmt.Errorf("%w：只能是 http:// 或 https:// 开头的地址", ErrBlocked)
	}
	return p.CheckHost(u.Hostname())
}

// CheckHost is CheckURL for a bare host name or address (v1.6.0: a mail
// account's IMAP server).
func (p Policy) CheckHost(host string) error {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("%w：%s", ErrBlocked, host)
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return p.CheckAddr(ip)
	}
	for _, d := range p.DenyHosts {
		if h, _, err := net.SplitHostPort(d); err == nil {
			d = h
		}
		if strings.EqualFold(host, d) {
			return fmt.Errorf("%w：%s", ErrBlocked, host)
		}
	}
	return nil
}

// Client is an HTTP client whose every connection is checked against the
// policy. It ignores proxy settings: a proxy would connect for it.
func (p Policy) Client(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: timeout, Control: p.control}
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
	}
	return &http.Client{Timeout: timeout, Transport: tr, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("跳转次数太多")
		}
		return p.CheckURL(req.URL.String())
	}}
}

// Dialer is a dialer whose every connection is checked against the policy
// (v1.6.0: IMAP connections of mail accounts).
func (p Policy) Dialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout, Control: p.control}
}

// control runs on the resolved address just before connecting.
func (p Policy) control(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("%w：%s", ErrBlocked, address)
	}
	if err := p.CheckAddr(ap.Addr()); err != nil {
		return err
	}
	return p.checkDenied(ap.Addr())
}

func (p Policy) checkDenied(ip netip.Addr) error {
	if len(p.DenyHosts) == 0 {
		return nil
	}
	resolve := p.Resolve
	if resolve == nil {
		resolve = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, d := range p.DenyHosts {
		if h, _, err := net.SplitHostPort(d); err == nil {
			d = h
		}
		addrs, err := resolve(ctx, d)
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if a.Unmap() == ip.Unmap() {
				return fmt.Errorf("%w：%s", ErrBlocked, d)
			}
		}
	}
	return nil
}

// LocalAddrs lists this machine's interface addresses.
func LocalAddrs() []netip.Addr {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []netip.Addr
	for _, a := range addrs {
		if p, err := netip.ParsePrefix(a.String()); err == nil {
			out = append(out, p.Addr().Unmap())
		}
	}
	return out
}
