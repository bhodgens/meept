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
//   - DialWithEphemeralRetry rides out the burst with a bounded retry budget
//     (ephemeralRetryAttempts, escalating backoff), so a test does not fail
//     just because the range was momentarily full. One retry proved too
//     weak: measured 8 failures at 1 attempt, 2 at 4, 0 at 5 combined with
//     the SO_LINGER fix below.
//   - BoundedTransport caps idle connections and IdleConnTimeout, and its
//     dialer sets SO_LINGER 0 on the client socket so a client-initiated close
//     sends RST instead of FIN. That is the load-bearing fix: the side that
//     closes first is the side that holds the TIME_WAIT entry, and a test
//     client that closes its own idle connections therefore parks its own
//     ephemeral ports for 30s. Measured on this host (2026-11-01, 40-port
//     pinned window, 300 round trips): client closes first = 260/300 connect
//     failures; same with SetLinger(0) = 0 failures; server closes first = 0
//     failures.
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

// Retry budget for ephemeral exhaustion. A full sweep's saturation lasts
// seconds (TIME_WAIT entries expire on a 30s clock, so ports free up
// steadily), which ONE retry cannot outlast: measured 2026-11-01, a single
// 250ms retry still left 6 failures in a `-p 2` sweep. Five attempts with
// escalating backoff span ~2.4s, which rides out the burst while staying well
// inside the 5s timeout the hermes fixtures give their client.
const (
	ephemeralRetryAttempts = 5
	ephemeralRetryBase     = 200 * time.Millisecond
	ephemeralRetryMax      = 1 * time.Second
)

// destroyOnClose sets SO_LINGER to 0 so Close() sends RST instead of FIN.
//
// Why this matters: the side that closes first holds the TIME_WAIT entry, and
// on loopback both endpoints live in the same kernel. A client-initiated close
// therefore writes an entry whose LOCAL port is the client's ephemeral port —
// exactly the port the next test needs. An RST tears the connection down with
// no TIME_WAIT at all. Measured on this host with a pinned 40-port window and
// 300 round trips: client closes first = 260/300 connect failures; same with
// SO_LINGER 0 = 0 failures; server closes first = 0 failures.
//
// Safe here because these are test fixtures that read each response to EOF
// before closing; no unread response data is discarded.
func destroyOnClose(conn net.Conn) {
	if tc, ok := conn.(*net.TCPConn); ok {
		// Best-effort: a failure leaves the socket on the normal FIN path,
		// which is the pre-existing behaviour, not a new fault.
		_ = tc.SetLinger(0)
	}
}

// DialWithEphemeralRetry dials with net.Dialer and retries when the dial fails
// only because the ephemeral port range is momentarily full. The retry is
// bounded (ephemeralRetryAttempts, escalating backoff) and gated on
// IsEphemeralExhaustion, so a refused/unreachable peer still fails fast.
// Test-only by policy: production code must not hide port exhaustion.
func DialWithEphemeralRetry(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := retryOnExhaustion(ctx, d.DialContext, network, addr)
	if err == nil {
		destroyOnClose(conn)
	}
	return conn, err
}

// WrapDialer returns a DialContext that first runs inner (for example an
// SSRF-checked dialer) and, if inner fails ONLY with ephemeral exhaustion,
// retries inner. The inner dialer runs on every attempt, so wrapping a
// security dialer never bypasses its check — the retry re-dials through the
// same guard. The returned connection gets SO_LINGER 0 (see destroyOnClose).
func WrapDialer(inner func(ctx context.Context, network, addr string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	if inner == nil {
		return DialWithEphemeralRetry
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := retryOnExhaustion(ctx, inner, network, addr)
		if err == nil {
			destroyOnClose(conn)
		}
		return conn, err
	}
}

// retryOnExhaustion calls dial, retrying only while the failure is ephemeral
// exhaustion and attempts remain. The last error is returned so a genuinely
// unreachable peer surfaces unchanged.
func retryOnExhaustion(ctx context.Context, dial func(context.Context, string, string) (net.Conn, error), network, addr string) (net.Conn, error) {
	var lastErr error
	for attempt := 0; attempt < ephemeralRetryAttempts; attempt++ {
		conn, err := dial(ctx, network, addr)
		if err == nil {
			return conn, nil
		}
		if !IsEphemeralExhaustion(err) {
			return nil, err
		}
		lastErr = err
		backoff := ephemeralRetryBase << attempt
		if backoff > ephemeralRetryMax {
			backoff = ephemeralRetryMax
		}
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
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
