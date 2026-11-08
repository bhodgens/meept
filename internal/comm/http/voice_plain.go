// Plain-HTTP loopback listener for the voice routes (leaf 10, T6).
//
// When voice.enabled and voice.loopback_plain_addr are set, the daemon
// serves ONLY the two voice routes (webhook + media websocket) in
// cleartext on that address, so a TLS-terminating tunnel (ngrok, cloud
// flunnel, caddy) can forward Telnyx's public traffic to it. The main
// listener stays TLS-mandatory and untouched; nothing else is reachable
// through this one.
//
// PRODUCTION WIRING: internal/daemon/voice_plain_boot.go (called from
// VoiceWiring.Start) starts this listener when voice.enabled &&
// voice.loopback_plain_addr != "", bound to the voice subsystem's stop
// context so it shuts down with the rest of the stack (audit HIGH: the
// knob was dead before this wiring existed).
//
// The address MUST be a loopback host (127.0.0.0/8 or ::1, with
// "localhost" accepted by name): exposing call-control routes on a
// non-loopback interface would present an unauthenticated-looking surface
// to the local network. A non-loopback bind is a NAMED startup error
// (ErrNonLoopbackVoiceAddr) - fail at startup, never serve. The check runs
// TWICE: the name may pass ensureLoopbackAddr while /etc/hosts resolves
// "localhost" to a routable IP, so the ACTUAL bound address of the opened
// listener (including a caller pre-opened one) is re-verified against
// IsLoopback() before Serve; a mismatch closes the fd and errors.
//
// The mux is wrapped with the same per-IP rate limiter and access logging
// the main listener's middleware chain applies to the voice routes
// (audit MED: a bare mux bypassed both). Defaults match server.go
// (120 req/min, burst 30); tune via PlainListenerOptions.
package http

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// ErrNonLoopbackVoiceAddr is returned when voice.loopback_plain_addr names
// a non-loopback (or unparseable) host. The error text names the value.
var ErrNonLoopbackVoiceAddr = errors.New("voice.loopback_plain_addr must bind a loopback address (127.0.0.0/8 or ::1)")

// PlainListenerOptions configures the optional middleware on the plain
// listener. Zero values fall back to the main listener's defaults
// (120 req/min, burst 30, no XFF trust, slog.Default()).
type PlainListenerOptions struct {
	RateLimitPerMinute int
	RateLimitBurst     int
	TrustXForwardedFor bool
	Logger             *slog.Logger
}

// ensureLoopbackAddr parses addr as host:port and refuses any host that is
// not a loopback IP literal (or "localhost" by name). An unparseable value
// is ALSO a named non-loopback error: the caller cannot have meant a safe
// bind if we cannot prove it is loopback. "localhost" by name is only a
// PRE-CHECK — after Listen, verifyBoundLoopback proves the real IP,
// because /etc/hosts can map the name to a routable address.
func ensureLoopbackAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%w: %q is not host:port", ErrNonLoopbackVoiceAddr, addr)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("%w: got %q", ErrNonLoopbackVoiceAddr, addr)
	}
	return nil
}

// verifyBoundLoopback proves a listener's ACTUAL bound address is a
// loopback IP. It closes the listener and returns a named error on any
// mismatch (audit LOW a/b: name-based checks alone let "localhost" bind a
// routable IP via /etc/hosts, and a caller pre-opened listener skipped
// validation entirely).
func verifyBoundLoopback(ln net.Listener) error {
	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok || tcpAddr.IP == nil || !tcpAddr.IP.IsLoopback() {
		_ = ln.Close()
		return fmt.Errorf("%w: listener bound %v", ErrNonLoopbackVoiceAddr, ln.Addr())
	}
	return nil
}

// voicePlainMux builds the dedicated mux for the plain listener: ONLY the
// two voice routes exist here, so a misrouted tunnel can never reach the
// general API surface (everything else 404s).
func voicePlainMux(voice *VoiceRoutes) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+voice.WebhookPath(), voice.handleWebhook)
	mux.HandleFunc("GET "+voice.MediaPath(), voice.handleMedia)
	return mux
}

// plainVoiceHandler wraps the voice mux with the same per-IP rate limiter
// and status-capturing logging writer the main listener applies to the
// voice routes (server.go middleware chain). The logging writer is the
// OUTER layer so it observes limiter 429s too.
func plainVoiceHandler(voice *VoiceRoutes, opts *PlainListenerOptions) http.Handler {
	limitPerMinute, burst, trustXFF := 120, 30, false
	logger := slog.Default()
	if opts != nil {
		if opts.RateLimitPerMinute > 0 {
			limitPerMinute = opts.RateLimitPerMinute
		}
		if opts.RateLimitBurst > 0 {
			burst = opts.RateLimitBurst
		}
		trustXFF = opts.TrustXForwardedFor
		if opts.Logger != nil {
			logger = opts.Logger
		}
	}
	rateLimited := RateLimitMiddleware(limitPerMinute, burst, trustXFF)(voicePlainMux(voice))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// Close non-WS responses explicitly: a lingering keep-alive conn
		// turns Shutdown's quiescence wait into the net/http issue-22682
		// coin flip (a StateNew conn counts as busy for 5s), stalling the
		// subsystem stop. Tunnel proxies (ngrok/caddy) redial cheaply on
		// loopback. The media websocket is excluded — its handshake needs
		// Connection: Upgrade, and the hijacked socket is force-closed on
		// shutdown.
		if !isVoiceWSUpgrade(r) {
			w.Header().Set("Connection", "close")
		}
		lrw := &loggingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		rateLimited.ServeHTTP(lrw, r)
		if lrw.statusCode >= 400 {
			logger.Warn("voice plain HTTP error response",
				"method", r.Method,
				"path", r.URL.Path,
				"status", lrw.statusCode,
				"remote", r.RemoteAddr,
				"duration", time.Since(start))
			return
		}
		logger.Debug("voice plain HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", lrw.statusCode,
			"duration", time.Since(start))
	})
}

// isVoiceWSUpgrade reports whether r is a websocket upgrade handshake. The
// plain listener's Connection: close policy must never touch it (the
// handshake requires Connection: Upgrade).
func isVoiceWSUpgrade(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") &&
		strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// StartVoicePlainListener serves ONLY the voice routes in cleartext on
// loopbackAddr and blocks until ctx is cancelled (graceful shutdown) or the
// listener fails. voice must not be nil to run. The former caller-provided
// *http.Server parameter was dropped: it silently overwrote the caller's
// Handler, and the listener never honored any of its other fields.
func StartVoicePlainListener(ctx context.Context, loopbackAddr string, voice *VoiceRoutes, opts *PlainListenerOptions) error {
	return startVoicePlain(ctx, nil, voice, loopbackAddr, opts)
}

// StartVoicePlainListenerWith is the test/leaf-14 variant that accepts a
// pre-opened listener (addr=="" with ln!=nil) or validates + opens the
// addr itself (ln==nil). With voice==nil it performs ONLY the address
// validation (used by config checks and the loopback-refusal tests). A
// pre-opened listener is STILL loopback-verified against its actual bound
// address (audit LOW b). opts is variadic so existing call sites keep
// compiling with the main-listener defaults.
func StartVoicePlainListenerWith(ctx context.Context, ln net.Listener, voice *VoiceRoutes, addr string, opts ...PlainListenerOptions) error {
	var o *PlainListenerOptions
	if len(opts) > 0 {
		o = &opts[0]
	}
	return startVoicePlain(ctx, ln, voice, addr, o)
}

func startVoicePlain(ctx context.Context, ln net.Listener, voice *VoiceRoutes, addr string, opts *PlainListenerOptions) error {
	if ln != nil {
		// Pre-opened listener: prove the ACTUAL bound address is loopback
		// (validation-only and addr-based checks never ran for it).
		if err := verifyBoundLoopback(ln); err != nil {
			return err
		}
	} else if voice == nil {
		// Validation-only call: nothing to run.
		return ensureLoopbackAddr(addr)
	} else {
		if err := ensureLoopbackAddr(addr); err != nil {
			return err
		}
		var err error
		ln, err = net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("voice plain listener: %w", err)
		}
		// audit LOW a: "localhost" passed the name check above, but
		// /etc/hosts can resolve it to a routable address — re-check the
		// real bound IP and close the fd if it is not loopback.
		if err := verifyBoundLoopback(ln); err != nil {
			return err
		}
	}
	if voice == nil {
		return errors.New("voice plain listener: nil routes")
	}

	srv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler:           plainVoiceHandler(voice, opts),
	}
	errCh := make(chan error, 1)
	go func() {
		// Exactly one value is ALWAYS sent, so every reader below (and the
		// post-force-close drain) is bounded.
		serr := srv.Serve(ln)
		if serr != nil && !errors.Is(serr, http.ErrServerClosed) {
			errCh <- serr
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		// Graceful window. Keep-alive conns make Shutdown's quiescence
		// wait unpredictable — Go treats a StateNew conn that has not read
		// its first request header as busy for 5s (net/http issue 22682),
		// and an in-flight (hijacked) media socket never goes idle at all.
		// So the window is short and failure ESCALATES to a hard Close:
		// this listener is a stateless tunnel front; the splice itself is
		// torn down by the per-call contexts on the same stop signal.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			// audit LOW d: slog at Warn (not stdlib log) and the failure is
			// surfaced, never swallowed into a silent nil.
			logger := slog.Default()
			if opts != nil && opts.Logger != nil {
				logger = opts.Logger
			}
			logger.Warn("voice plain listener graceful shutdown timed out; forcing close", "error", err)
			_ = srv.Close() // force-closes active conns AND the listener fd
			var serveErr error
			select {
			case serveErr = <-errCh:
			case <-time.After(5 * time.Second):
				_ = ln.Close()
			}
			if serveErr != nil {
				return fmt.Errorf("voice plain listener shutdown: graceful=%v serve=%w", err, serveErr)
			}
			return fmt.Errorf("voice plain listener shutdown: forced close after graceful failure: %w", err)
		}
		return nil
	case err := <-errCh:
		// audit LOW c: Serve never closes its listener on an error
		// return — close the fd before handing the error to the caller.
		_ = ln.Close()
		return err
	}
}
