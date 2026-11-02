// Package testhttp provides HTTP client plumbing for the test suite. It is
// test-support: production code must not paper over local port exhaustion (see
// the retry rule below), so no production path imports it.
//
// Why it exists: macOS hands outbound connections ephemeral ports from a range
// of only 16,384 (net.inet.ip.portrange.first/last = 49152-65535) and holds a
// TIME_WAIT port for 2*MSL = 30s (net.inet.tcp.msl = 15000). A full `make test`
// sweep opens thousands of loopback connections, so the range transiently
// saturates and the most socket-hungry package — the one that runs last —
// fails with `dial tcp 127.0.0.1:NNNNN: connect: can't assign requested
// address` (EADDRNOTAVAIL) through no fault of the code under test. This is
// concurrency-specific: `go test -p 1` shows none of it; `-p 2` reproduces it.
// Measured on 2026-11-01: 55 such failures under `make test`, zero under
// `-p 1`, zero when the same package runs alone at -count=5.
//
// Two mitigations live here:
//   - DialWithEphemeralRetry rides out the burst with ONE bounded retry, so a
//     test does not fail just because the range was momentarily full.
//   - BoundedTransport caps idle connections and IdleConnTimeout so finished
//     requests release their client-side ports promptly instead of piling up
//     in TIME_WAIT for later packages to starve on.
//
// Extracted from the file-local pattern in
// internal/comm/http/unified_http_test.go (forest F3 close-out) so every
// socket-heavy fixture shares one implementation.
package testhttp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// ephemeralExhaustionMarker is the substring the OS puts in the dial error when
// the local ephemeral port range is momentarily full. On macOS it is
// "can't assign requested address"; the check is on the syscall error text so it
// matches regardless of the wrapping net.OpError.
const ephemeralExhaustionMarker = "assign requested address"

// IsEphemeralExhaustion reports whether err is a dial failure caused by local
// ephemeral-port exhaustion (EADDRNOTAVAIL), as opposed to a refused or
// unreachable peer. Only this class is safe to retry: a refused connection is a
// real test failure and must surface.
func IsEphemeralExhaustion(err error) bool {
	var oe *net.OpError
	if !errors.As(err, &oe) || oe.Op != "dial" {
		return false
	}
	var se *os.SyscallError
	if errors.As(err, &se) {
		return strings.Contains(se.Err.Error(), ephemeralExhaustionMarker)
	}
	// Some platforms surface the message without an os.SyscallError wrapper.
	return strings.Contains(err.Error(), ephemeralExhaustionMarker)
}

// retryBackoff is how long to wait before the single retry. Short enough to stay
// invisible in a healthy run, long enough for the OS to free a TIME_WAIT slot.
const retryBackoff = 250 * time.Millisecond

// DialWithEphemeralRetry dials with net.Dialer and retries ONCE when the dial
// fails only because the ephemeral port range is momentarily full. The retry is
// bounded (one attempt, short backoff) and gated on IsEphemeralExhaustion, so a
// refused/unreachable peer still fails fast. Test-only by policy: production
// code must not hide port exhaustion.
func DialWithEphemeralRetry(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 10 * time.Second}
	return retryOnce(ctx, d.DialContext, network, addr)
}

// WrapDialer returns a DialContext that first runs inner (for example an
// SSRF-checked dialer) and, if inner fails ONLY with ephemeral exhaustion,
// retries inner once. The inner dialer runs on every attempt, so wrapping a
// security dialer never bypasses its check — the retry re-dials through the
// same guard.
func WrapDialer(inner func(ctx context.Context, network, addr string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	if inner == nil {
		return DialWithEphemeralRetry
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return retryOnce(ctx, inner, network, addr)
	}
}

// retryOnce calls dial once, then a second time only when the first failure is
// ephemeral exhaustion and ctx is still live.
func retryOnce(ctx context.Context, dial func(context.Context, string, string) (net.Conn, error), network, addr string) (net.Conn, error) {
	conn, err := dial(ctx, network, addr)
	if err == nil {
		return conn, nil
	}
	if !IsEphemeralExhaustion(err) {
		return nil, err
	}
	select {
	case <-time.After(retryBackoff):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return dial(ctx, network, addr)
}

// BoundedTransport returns a transport suited to loopback test servers: the
// ephemeral-retry dialer plus tight idle-connection limits so finished requests
// release their client-side ports quickly. MaxIdleConnsPerHost is small because
// test servers are single-host. HTTP/2 is off: a single multiplexed connection
// would hide the per-request churn this transport is meant to bound.
func BoundedTransport() *http.Transport {
	return &http.Transport{
		DialContext:         DialWithEphemeralRetry,
		MaxIdleConns:        4,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     1 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
		ForceAttemptHTTP2:   false,
	}
}

// Client returns an *http.Client using BoundedTransport. timeout bounds the
// whole request; pass 0 to leave it unbounded (the caller's context then
// governs), which is what the SSE path needs. Pass the result as
// githubsvc.ClientPoolOptions.HTTPClient or through a client's transport seam.
func Client(timeout time.Duration) *http.Client {
	return &http.Client{Transport: BoundedTransport(), Timeout: timeout}
}
