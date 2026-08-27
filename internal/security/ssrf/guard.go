// Package ssrf provides a centralized SSRF (Server-Side Request Forgery)
// guard for outbound HTTP clients. It enforces:
//
//   - a scheme allowlist (http/https only),
//   - IP/CIDR blocklists covering loopback, private, link-local, and
//     cloud-metadata ranges (checked for URL literals, resolved hostnames,
//     and again at dial time),
//   - per-hop redirect re-validation with a configurable hop limit.
//
// The guard is used by the web_fetch and web_search builtin tools; see
// docs/workflows/security.md for configuration.
//
// # DNS rebinding (TOCTOU) limitation
//
// CheckURL resolves hostnames and validates every returned IP, and the
// DialContext installed by WrapClient re-validates at socket-connect time
// through the same resolver. This closes the common rebinding window where
// DNS answers change between pre-flight check and dial, but a determined
// attacker who can flip DNS answers between the dial-time lookup and the
// TCP connect itself is out of scope. Per-hop redirect re-checking
// mitigates the common redirect-based bypass cases.
package ssrf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Sentinel errors returned by Guard. Callers may match with errors.Is.
var (
	// ErrSchemeNotAllowed is returned for non-http(s) URL schemes.
	ErrSchemeNotAllowed = errors.New("ssrf: scheme not allowed")
	// ErrMissingHost is returned for URLs without a host component.
	ErrMissingHost = errors.New("ssrf: URL missing host")
	// ErrBlockedAddress is returned when a URL literal, a resolved IP, or a
	// dial target falls inside a blocked CIDR range.
	ErrBlockedAddress = errors.New("ssrf: address blocked")
	// ErrTooManyRedirects is returned when a redirect chain exceeds
	// MaxRedirects hops.
	ErrTooManyRedirects = errors.New("ssrf: too many redirects")
)

// DefaultMaxRedirects is the redirect hop limit applied when
// GuardConfig.MaxRedirects is zero.
const DefaultMaxRedirects = 5

// DefaultBlockedCIDRs is the blocklist applied when GuardConfig.BlockedCIDRs
// is empty. An explicitly configured BlockedCIDRs list fully replaces these
// defaults. 169.254.169.254 (cloud metadata) is listed explicitly even
// though 169.254.0.0/16 already covers it, so it survives trimming of the
// surrounding range.
var DefaultBlockedCIDRs = []string{
	"127.0.0.0/8",        // loopback
	"10.0.0.0/8",         // private (RFC 1918)
	"172.16.0.0/12",      // private (RFC 1918)
	"192.168.0.0/16",     // private (RFC 1918)
	"169.254.0.0/16",     // link-local
	"169.254.169.254/32", // cloud metadata endpoint (emphasized)
	"::1/128",            // IPv6 loopback
	"fc00::/7",           // IPv6 unique-local
	"fe80::/10",          // IPv6 link-local
}

// ResolverFunc resolves a hostname to its IP addresses. Tests inject a
// static resolver so no real DNS traffic occurs.
type ResolverFunc func(ctx context.Context, host string) ([]net.IP, error)

// DialContextFunc matches the signature of http.Transport.DialContext.
type DialContextFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// GuardConfig configures a Guard. The zero value yields the defaults:
// http(s) only, DefaultBlockedCIDRs, no allowlists, DefaultMaxRedirects.
type GuardConfig struct {
	// AllowedHosts bypass all IP checks via dot-delimited suffix match
	// (e.g., "api.github.com" also matches "sub.api.github.com").
	AllowedHosts []string
	// AllowedCIDRs exempt IPs from the blocklist (e.g., "10.0.0.0/8" for
	// corporate networks). Checked before BlockedCIDRs.
	AllowedCIDRs []string
	// BlockedCIDRs replaces DefaultBlockedCIDRs when non-empty. Multicast
	// and unspecified addresses are always blocked regardless.
	BlockedCIDRs []string
	// MaxRedirects caps redirect hops; zero means DefaultMaxRedirects.
	MaxRedirects int
}

// Guard validates URLs, redirect hops, and dial targets against the
// configured blocklists. A Guard is safe for concurrent use; WithResolver
// returns a copy rather than mutating.
type Guard struct {
	allowedHosts []string
	allowedCIDRs []*net.IPNet
	blockedCIDRs []*net.IPNet
	maxRedirects int
	resolve      ResolverFunc
}

// NewGuard parses all configured CIDRs upfront and fails fast on invalid
// input, so configuration errors surface at startup rather than at fetch
// time.
func NewGuard(cfg GuardConfig) (*Guard, error) {
	if cfg.MaxRedirects < 0 {
		return nil, fmt.Errorf("ssrf: invalid max_redirects %d: must be >= 0", cfg.MaxRedirects)
	}
	g := &Guard{
		maxRedirects: cfg.MaxRedirects,
		resolve:      defaultResolver,
	}
	if g.maxRedirects == 0 {
		g.maxRedirects = DefaultMaxRedirects
	}
	for _, h := range cfg.AllowedHosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if h != "" {
			g.allowedHosts = append(g.allowedHosts, h)
		}
	}
	var err error
	g.allowedCIDRs, err = parseCIDRs(cfg.AllowedCIDRs, "allowed_cidrs")
	if err != nil {
		return nil, err
	}
	blocked := cfg.BlockedCIDRs
	if len(blocked) == 0 {
		blocked = DefaultBlockedCIDRs
	}
	g.blockedCIDRs, err = parseCIDRs(blocked, "blocked_cidrs")
	if err != nil {
		return nil, err
	}
	return g, nil
}

// DefaultGuard returns a Guard built from the zero GuardConfig (default
// blocklist, default redirect limit, system resolver). It never fails.
func DefaultGuard() *Guard {
	g, err := NewGuard(GuardConfig{})
	if err != nil {
		// Unreachable: the zero config is always valid.
		panic(fmt.Sprintf("ssrf: default guard construction failed: %v", err))
	}
	return g
}

func parseCIDRs(cidrs []string, field string) ([]*net.IPNet, error) {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			return nil, fmt.Errorf("ssrf: invalid %s entry %q: %w", field, c, err)
		}
		nets = append(nets, n)
	}
	return nets, nil
}

func defaultResolver(ctx context.Context, host string) ([]net.IP, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		ips = append(ips, a.IP)
	}
	return ips, nil
}

// WithResolver returns a copy of g that resolves hostnames through r for
// both CheckURL and dial-time checks, so tests never touch real DNS. A nil
// r keeps the current resolver.
func (g *Guard) WithResolver(r ResolverFunc) *Guard {
	cp := *g
	if r != nil {
		cp.resolve = r
	}
	return &cp
}

// CheckURL validates raw for outbound fetching: scheme allowlist, host
// presence, and IP blocklist. Hostnames are resolved and every returned IP
// must pass. Hosts matching AllowedHosts bypass IP checks entirely.
func (g *Guard) CheckURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("ssrf: invalid URL: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("%w: %q (only http/https)", ErrSchemeNotAllowed, u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%w: %q", ErrMissingHost, raw)
	}
	if g.hostAllowed(host) {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil {
		if g.IPBlocked(ip) {
			return fmt.Errorf("%w: %s", ErrBlockedAddress, ip)
		}
		return nil
	}
	ips, err := g.resolve(context.Background(), host)
	if err != nil {
		return fmt.Errorf("ssrf: resolve %s: %w", host, err)
	}
	for _, ip := range ips {
		if g.IPBlocked(ip) {
			return fmt.Errorf("%w: %s resolves to %s", ErrBlockedAddress, host, ip)
		}
	}
	return nil
}

// CheckRedirect implements the http.Client.CheckRedirect signature and
// re-validates every redirect hop: hop count against MaxRedirects, then the
// full CheckURL pipeline on the target. Returning a non-nil error makes the
// stdlib client stop following redirects.
func (g *Guard) CheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= g.maxRedirects {
		return fmt.Errorf("%w: %d", ErrTooManyRedirects, g.maxRedirects)
	}
	if err := g.CheckURL(req.URL.String()); err != nil {
		return fmt.Errorf("redirect blocked: %w", err)
	}
	return nil
}

// WrapClient installs g.CheckRedirect and a validating DialContext on c and
// returns c. The client's Timeout is left untouched. If c is nil a fresh
// client is returned. An existing *http.Transport is cloned (not mutated)
// before the dial hook is installed; foreign Transport implementations are
// left as-is.
func (g *Guard) WrapClient(c *http.Client) *http.Client {
	if c == nil {
		c = &http.Client{}
	}
	c.CheckRedirect = g.CheckRedirect
	switch tr := c.Transport.(type) {
	case nil:
		c.Transport = &http.Transport{DialContext: g.DialContext(nil)}
	case *http.Transport:
		clone := tr.Clone()
		clone.DialContext = g.DialContext(tr.DialContext)
		c.Transport = clone
	}
	return c
}

// DialContext returns a DialContext function that re-validates the target at
// dial time, closing the DNS-rebinding window between CheckURL and connect.
// Literal IPs are checked directly; hostnames are resolved through the
// guard's resolver and every IP must pass before dialing the first one.
// base is the underlying dialer (nil means a fresh net.Dialer).
func (g *Guard) DialContext(base DialContextFunc) DialContextFunc {
	if base == nil {
		base = (&net.Dialer{}).DialContext
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("ssrf dial: bad address %q: %w", addr, err)
		}
		if ip := net.ParseIP(host); ip != nil {
			if g.IPBlocked(ip) {
				return nil, fmt.Errorf("%w: %s", ErrBlockedAddress, ip)
			}
			return base(ctx, network, addr)
		}
		if g.hostAllowed(host) {
			return base(ctx, network, addr)
		}
		ips, err := g.resolve(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("ssrf dial: resolve %s: %w", host, err)
		}
		for _, ip := range ips {
			if g.IPBlocked(ip) {
				return nil, fmt.Errorf("%w: %s resolves to %s", ErrBlockedAddress, host, ip)
			}
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("ssrf dial: %s resolved no addresses", host)
		}
		return base(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
}

// IPBlocked reports whether ip is denied: matched by a blocked CIDR (after
// checking allowed CIDRs), or multicast/unspecified, which are always
// denied regardless of configuration.
func (g *Guard) IPBlocked(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		ip = v4 // normalize IPv4-in-IPv6 so v4 CIDRs match
	}
	for _, n := range g.allowedCIDRs {
		if n.Contains(ip) {
			return false
		}
	}
	for _, n := range g.blockedCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return ip.IsMulticast() || ip.IsUnspecified()
}

// hostAllowed reports whether host matches AllowedHosts via exact or
// dot-delimited suffix match, case-insensitively.
func (g *Guard) hostAllowed(host string) bool {
	h := strings.ToLower(host)
	for _, a := range g.allowedHosts {
		if h == a || strings.HasSuffix(h, "."+a) {
			return true
		}
	}
	return false
}
