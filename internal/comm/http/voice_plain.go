// Plain-HTTP loopback listener for the voice routes (leaf 10, T6).
//
// When voice.enabled and voice.loopback_plain_addr are set, the daemon
// serves ONLY the two voice routes (webhook + media websocket) in
// cleartext on that address, so a TLS-terminating tunnel (ngrok, cloud
// flunnel, caddy) can forward Telnyx's public traffic to it. The main
// listener stays TLS-mandatory and untouched; nothing else is reachable
// through this one.
//
// The address MUST be a loopback host (127.0.0.0/8 or ::1, with
// "localhost" accepted by name): exposing call-control routes on a
// non-loopback interface would present an unauthenticated-looking surface
// to the local network. A non-loopback bind is a NAMED startup error
// (ErrNonLoopbackVoiceAddr) - fail at startup, never serve.
package http

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

// ErrNonLoopbackVoiceAddr is returned when voice.loopback_plain_addr names
// a non-loopback (or unparseable) host. The error text names the value.
var ErrNonLoopbackVoiceAddr = errors.New("voice.loopback_plain_addr must bind a loopback address (127.0.0.0/8 or ::1)")

// ensureLoopbackAddr parses addr as host:port and refuses any host that is
// not a loopback IP literal (or "localhost" by name). An unparseable value
// is ALSO a named non-loopback error: the caller cannot have meant a safe
// bind if we cannot prove it is loopback.
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

// voicePlainMux builds the dedicated mux for the plain listener: ONLY the
// two voice routes exist here, so a misrouted tunnel can never reach the
// general API surface (everything else 404s).
func voicePlainMux(voice *VoiceRoutes) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+voice.WebhookPath(), voice.handleWebhook)
	mux.HandleFunc("GET "+voice.MediaPath(), voice.handleMedia)
	return mux
}

// StartVoicePlainListener serves ONLY the voice routes in cleartext on
// loopbackAddr and blocks until ctx is cancelled (graceful shutdown) or the
// listener fails. voice may be nil only when the caller just wants address
// validation (srv == nil); a nil voice with a real srv panics by design of
// the wiring (leaf 14 injects the routes before starting the listener).
func StartVoicePlainListener(ctx context.Context, loopbackAddr string, voice *VoiceRoutes, srv *http.Server) error {
	return startVoicePlain(ctx, loopbackAddr, voice, srv, nil)
}

// StartVoicePlainListenerWith is the test/leaf-14 variant that accepts a
// pre-opened listener (addr=="", ln!=nil) or validates + opens the addr
// itself (addr!="", ln==nil). With voice==nil it performs ONLY the address
// validation (used by config checks and the loopback-refusal tests).
func StartVoicePlainListenerWith(ctx context.Context, ln net.Listener, voice *VoiceRoutes, addr string) error {
	return startVoicePlain(ctx, addr, voice, nil, ln)
}

func startVoicePlain(ctx context.Context, addr string, voice *VoiceRoutes, srv *http.Server, ln net.Listener) error {
	if ln == nil {
		if err := ensureLoopbackAddr(addr); err != nil {
			return err
		}
	}
	if srv == nil && ln == nil {
		// Validation-only call (voice may be nil): nothing to run.
		return nil
	}
	if voice == nil {
		return errors.New("voice plain listener: nil routes")
	}
	if srv == nil {
		srv = &http.Server{
			ReadHeaderTimeout: 10 * time.Second,
		}
	}
	srv.Handler = voicePlainMux(voice)

	if ln == nil {
		var err error
		ln, err = net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("voice plain listener: %w", err)
		}
	}
	errCh := make(chan error, 1)
	go func() {
		if serr := srv.Serve(ln); serr != nil && !errors.Is(serr, http.ErrServerClosed) {
			errCh <- serr
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			// Listener is closing with the process; in-flight voice routes
			// get the normal handler-level ctx cancellation.
			log.Printf("voice plain listener shutdown: %v", err)
		}
		return nil
	case err := <-errCh:
		return err
	}
}
