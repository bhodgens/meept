package builtin

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// isBlockedAddress reports whether the host should be refused for outbound
// fetches. It blocks loopback, private ranges, link-local, unspecified,
// and multicast addresses to prevent SSRF and cloud-metadata exfiltration.
// Hostnames are not blocked here; they must be resolved and re-checked by
// the caller (see checkURL).
func isBlockedAddress(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr // no port
	}
	if host == "" {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false // hostname; resolved and re-checked by checkURL
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

// checkURL validates a URL against the SSRF blocklist, resolving hostnames
// through the default resolver so DNS-based bypasses (public hostname →
// private IP) are caught. Allowed schemes are http and https only.
func checkURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("scheme %q not allowed", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("URL missing host")
	}
	if isBlockedAddress(host) {
		return fmt.Errorf("host %q is blocked (private/loopback/link-local)", host)
	}
	// Resolve and re-check each A/AAAA record. Hostnames that resolve to
	// private/loopback IPs are rejected.
	ips, err := net.DefaultResolver.LookupIPAddr(context.Background(), host)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", host, err)
	}
	for _, ip := range ips {
		if isBlockedAddress(ip.IP.String()) {
			return fmt.Errorf("host %s resolves to blocked address %s", host, ip.IP)
		}
	}
	return nil
}
