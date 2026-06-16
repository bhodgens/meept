package transport

import (
	"context"
	"fmt"
	"net"
)

// ssrfDialContext returns a DialContext function that re-validates the
// resolved IP address at dial time. This closes the DNS rebinding window
// between checkRedirectURL (pre-request) and the actual TCP connection:
// even if the authoritative DNS changes between resolution in
// checkRedirectURL and the dial, the dial-time check will reject
// private/loopback/link-local IPs.
//
// This is a local copy of internal/tools/builtin/ssrfDialContext to avoid
// an import cycle (internal/tools/builtin cannot be imported from
// internal/tools/mcp/transport).
//
// When allowPrivate is true the SSRF re-check is skipped entirely. This is
// intended only for tests that hit httptest.NewServer on 127.0.0.1; production
// callers must pass false.
func ssrfDialContext(allowPrivate bool) func(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{}
	if allowPrivate {
		return dialer.DialContext
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("ssrf dial: bad address %q: %w", addr, err)
		}
		// Resolve the host at dial time and re-check each IP.
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("ssrf dial: resolve %s: %w", host, err)
		}
		for _, ip := range ips {
			if isBlockedAddress(ip.IP.String()) {
				return nil, fmt.Errorf("ssrf dial: %s resolves to blocked address %s", host, ip.IP)
			}
		}
		// Dial using the first validated IP.
		target := net.JoinHostPort(ips[0].IP.String(), port)
		return dialer.DialContext(ctx, network, target)
	}
}
