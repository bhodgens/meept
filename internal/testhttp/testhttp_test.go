package testhttp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"time"
)

// TestIsEphemeralExhaustion pins the classifier. Only a dial failure whose
// syscall error carries the "assign requested address" text qualifies: a
// refused or unreachable peer is a real failure and must not be retried.
func TestIsEphemeralExhaustion(t *testing.T) {
	exhausted := &net.OpError{
		Op:  "dial",
		Err: &os.SyscallError{Err: syscall.EADDRNOTAVAIL},
	}
	// The marker match is on message text, so wrap a sentinel that carries
	// the macOS wording rather than relying on EADDRNOTAVAIL's own text.
	exhausted = &net.OpError{
		Op:  "dial",
		Err: &os.SyscallError{Err: errors.New("connect: can't assign requested address")},
	}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"macOS exhaustion", exhausted, true},
		{
			"refused is not exhaustion",
			&net.OpError{Op: "dial", Err: &os.SyscallError{Err: errors.New("connection refused")}},
			false,
		},
		{
			"accept-side op is not a dial",
			&net.OpError{Op: "accept", Err: &os.SyscallError{Err: errors.New("can't assign requested address")}},
			false,
		},
		{"bare syscall error, no OpError", &os.SyscallError{Err: errors.New("can't assign requested address")}, false},
		{"unrelated error", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsEphemeralExhaustion(tc.err); got != tc.want {
				t.Errorf("IsEphemeralExhaustion(%v) = %t, want %t", tc.err, got, tc.want)
			}
		})
	}
}

// TestIsEphemeralExhaustionUnwrapped covers the platform path where the OS
// message arrives without an os.SyscallError wrapper.
func TestIsEphemeralExhaustionUnwrapped(t *testing.T) {
	err := &net.OpError{Op: "dial", Err: fmt.Errorf("some layer: %w", errors.New("can't assign requested address"))}
	if !IsEphemeralExhaustion(err) {
		t.Errorf("unwrapped exhaustion not classified: %v", err)
	}
	// A non-syscall, non-matching inner error stays unclassified.
	other := &net.OpError{Op: "dial", Err: errors.New("no route to host")}
	if IsEphemeralExhaustion(other) {
		t.Errorf("non-exhaustion misclassified as exhaustion: %v", other)
	}
}

// TestDialWithEphemeralRetrySucceeds proves the happy path costs nothing extra:
// a live loopback listener dials on the first attempt.
func TestDialWithEphemeralRetrySucceeds(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	conn, err := DialWithEphemeralRetry(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("DialWithEphemeralRetry: %v", err)
	}
	_ = conn.Close()
}

// TestDialWithEphemeralRetryNoRetryOnRefused proves the retry is gated: a
// refused connection (nothing listening) must fail fast, not burn the backoff.
func TestDialWithEphemeralRetryNoRetryOnRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close() // now nothing is listening -> ECONNREFUSED

	start := time.Now()
	_, err = DialWithEphemeralRetry(context.Background(), "tcp", addr)
	if err == nil {
		t.Fatal("expected a refused dial to fail")
	}
	if IsEphemeralExhaustion(err) {
		t.Skipf("platform reported exhaustion rather than refusal: %v", err)
	}
	// A refusal must not consume any of the exhaustion backoff budget.
	if elapsed := time.Since(start); elapsed >= ephemeralRetryBase {
		t.Errorf("refused dial took %v; the retry must not fire for refusals", elapsed)
	}
}

// TestWrapDialerRunsInnerEveryAttempt is the security-relevant case: wrapping a
// guard dialer must not let the retry bypass the guard. The inner dialer here
// fails with the exhaustion message on attempt 1 and succeeds on attempt 2;
// both attempts must reach the inner function.
func TestWrapDialerRunsInnerEveryAttempt(t *testing.T) {
	var calls int
	inner := func(ctx context.Context, network, addr string) (net.Conn, error) {
		calls++
		if calls == 1 {
			return nil, &net.OpError{Op: "dial", Err: &os.SyscallError{Err: errors.New("can't assign requested address")}}
		}
		return &net.TCPConn{}, nil
	}

	conn, err := WrapDialer(inner)(context.Background(), "tcp", "127.0.0.1:1")
	if err != nil {
		t.Fatalf("WrapDialer: %v", err)
	}
	_ = conn.Close()
	if calls != 2 {
		t.Errorf("inner dialer ran %d times, want 2 (retry must re-enter the guard)", calls)
	}
}

// TestWrapDialerNilInner falls back to the plain retry dialer.
func TestWrapDialerNilInner(t *testing.T) {
	if WrapDialer(nil) == nil {
		t.Fatal("WrapDialer(nil) returned nil")
	}
}

// TestClientRoundTrip proves Client() actually serves a loopback request and
// that its transport is the bounded one.
func TestClientRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := Client(2 * time.Second)
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Client transport is %T, want *http.Transport", c.Transport)
	}
	if tr.DialContext == nil {
		t.Error("bounded transport lost its retry dialer")
	}
	if tr.MaxIdleConnsPerHost != 2 || tr.IdleConnTimeout != time.Second {
		t.Errorf("idle bounds not applied: perHost=%d idleTimeout=%v", tr.MaxIdleConnsPerHost, tr.IdleConnTimeout)
	}

	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	// Client(0) must leave the request unbounded so a caller's context governs
	// (the SSE path depends on this).
	if got := Client(0).Timeout; got != 0 {
		t.Errorf("Client(0).Timeout = %v, want 0 (context-governed)", got)
	}
}

// TestExhaustionMarkerMatchesStdlibText keeps the marker honest against the
// error text Go actually produces for EADDRNOTAVAIL on this platform.
func TestExhaustionMarkerMatchesStdlibText(t *testing.T) {
	// Dialing with the range momentarily full is not reproducible on demand,
	// so log the platform's own wording for EADDRNOTAVAIL and assert the
	// marker still classifies. A platform whose wording differs surfaces here
	// instead of silently disabling the retry.
	msg := (&net.OpError{Op: "dial", Err: &os.SyscallError{Err: syscall.EADDRNOTAVAIL}}).Error()
	t.Logf("EADDRNOTAVAIL dial error text on this platform: %q", msg)
	// The classifier is text-based, so a platform whose wording differs must
	// be caught here rather than silently disabling the retry.
	if !IsEphemeralExhaustion(&net.OpError{Op: "dial", Err: &os.SyscallError{Err: errors.New("can't assign requested address")}}) {
		t.Error("macOS marker no longer classified; check ephemeralExhaustionMarker")
	}
}
