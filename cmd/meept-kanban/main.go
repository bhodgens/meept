// Command meept-kanban is the kanban sidecar gateway: the only network-exposed
// process of the kanban capability (master.md C7). It holds NO logic and NO
// database handle — every read and mutation is forwarded to the daemon's
// kanban.* RPC over the CLI's transport client (internal/transport). The
// daemon stays bound to loopback and the Unix socket.
//
// Security posture:
//   - No TLS in this binary. TLS terminates at an operator proxy (Caddy /
//     nginx / Tailscale serve).
//   - Default bind 127.0.0.1:8790. A non-loopback bind requires the explicit
//     -allow-lan flag AND logs a loud boot warning: exposing the board is
//     never accidental.
//   - The web bundle is served from WEBDIR (else $MEEPT_HOME/webui) read-only.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/kanbansvc"
	"github.com/caimlas/meept/internal/transport"
)

// defaultBind is the loopback default from C7.
const defaultBind = "127.0.0.1:8790"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "meept-kanban:", err)
		os.Exit(1)
	}
}

func run() error {
	bind := flag.String("bind", defaultBind, "listen address (host:port)")
	allowLAN := flag.Bool("allow-lan", false, "permit a non-loopback bind (you then own the TLS termination)")
	socket := flag.String("daemon-socket", "", "daemon RPC socket (default $MEEPT_HOME/meept.sock)")
	baseURL := flag.String("daemon-http", "", "daemon HTTP base URL (overrides -daemon-socket when set)")
	webdir := flag.String("webdir", "", "web bundle root (default $WEBDIR, else $MEEPT_HOME/webui)")
	pairStorePath := flag.String("pair-store", "", "pairing token store file (default $MEEPT_HOME/kanban/tokens.json)")
	var allowOrigins stringSlice
	flag.Var(&allowOrigins, "allow-origin", "exact browser Origin to accept on the WS relay IN ADDITION to loopback (repeatable or comma-list; set by the daemon from [kanban].allowed_origins for the TLS-proxy deployment)")
	flag.Parse()

	if err := guardBind(*bind, *allowLAN); err != nil {
		return err
	}

	daemon, err := dialDaemon(*socket, *baseURL)
	if err != nil {
		return fmt.Errorf("connect daemon: %w", err)
	}
	defer daemon.Close()

	bundle := resolveWebdir(*webdir)
	if bundle == "" {
		slog.Warn("kanban sidecar: web bundle not found; serving the REST/WS API only",
			"hint", "run `make build-gui` and set WEBDIR")
	}

	// Pairing (04/03) is wired: the TokenStore is the production GrantChecker
	// (its token file lives at $MEEPT_HOME/kanban/tokens.json, 0600) and
	// POST /api/v1/pair/exchange arms the loopback handshake — an operator
	// (or the daemon, leaf 07/01) mints a code with GeneratePairCode and
	// prints it once; the browser exchanges it for a grant-carrying bearer
	// token. Until a code is minted and exchanged, the store knows no tokens
	// and every API route answers 401 — refusing is correct: a board
	// reachable without auth is not a board, it is an outage waiting to
	// happen.
	pairStore, err := kanbansvc.NewTokenStore(*pairStorePath)
	if err != nil {
		return fmt.Errorf("load token store: %w", err)
	}
	authz := pairStore

	// Board-event pump (follow-up gap: the GUI live feed): the daemon's
	// bus.subscribe/bus.poll RPC pair, riding the SAME transport client
	// the gateway's REST→RPC forwards use. Replaces the IdleSource
	// placeholder: without it the /api/v1/events relay carried no deltas.
	events, err := kanbansvc.NewBusPump(daemon, kanbansvc.PumpOptions{})
	if err != nil {
		return fmt.Errorf("build event pump: %w", err)
	}
	defer events.Close()

	gateway, err := kanbansvc.NewGateway(daemon, events, authz, bundle)
	if err != nil {
		return fmt.Errorf("build gateway: %w", err)
	}
	if err := gateway.SetPairing(pairStore); err != nil {
		return fmt.Errorf("arm pairing: %w", err)
	}
	if len(allowOrigins) > 0 {
		gateway.SetAllowedOrigins(allowOrigins)
		slog.Info("kanban sidecar: WS origin allow-list armed", "origins", strings.Join(allowOrigins, ","))
	}

	srv := &http.Server{
		Addr:              *bind,
		Handler:           gateway,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       kanbansvc.ReadIdleTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("kanban sidecar listening", "addr", *bind, "web_bundle", bundle != "")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("serve: %w", err)
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			// Drain-window expiry means a client connection is wedged
			// mid-request (a half-written POST body keeps the server
			// handler blocked forever; net/http Shutdown waits on it
			// indefinitely). The operator's SIGTERM outranks one stuck
			// client: force-close the remainder so the sidecar always
			// exits cleanly instead of dying with "shutdown: context
			// deadline exceeded" and a nonzero status.
			if !errors.Is(err, context.DeadlineExceeded) {
				return fmt.Errorf("shutdown: %w", err)
			}
			slog.Warn("kanban sidecar: shutdown drain timed out with open connections; force-closing",
				"error", err)
			if closeErr := srv.Close(); closeErr != nil {
				slog.Warn("kanban sidecar: forced listener close failed", "error", closeErr)
			}
		}
		slog.Info("kanban sidecar stopped")
		return nil
	}
}

// guardBind enforces C7: a non-loopback bind needs the flag and gets a loud
// warning. This never silently widens the surface.
func guardBind(bind string, allowLAN bool) error {
	host, port, err := net.SplitHostPort(bind)
	if err != nil {
		return fmt.Errorf("invalid -bind %q (want host:port): %w", bind, err)
	}
	if port == "0" || port == "" {
		return fmt.Errorf("invalid -bind %q: port must not be 0 (the sidecar's address is pinned by the daemon config)", bind)
	}
	if isLoopbackHost(host) {
		return nil
	}
	if !allowLAN {
		return fmt.Errorf("refusing to bind %q: non-loopback needs the explicit -allow-lan flag, and you must terminate TLS in an operator proxy (C7)", bind)
	}
	slog.Warn("kanban sidecar binding to a NON-LOOPBACK address",
		"addr", bind,
		"warning", "this board is now reachable by every host that can route to this port; the sidecar serves NO TLS — put Caddy/nginx/Tailscale in front of it")
	return nil
}

// isLoopbackHost accepts "localhost" and any loopback IP. An empty host means
// the wildcard, which is not loopback.
func isLoopbackHost(host string) bool {
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// resolveWebdir picks the bundle root: flag, then WEBDIR, then $MEEPT_HOME/webui.
// Returns "" when none exists on disk.
func resolveWebdir(flagged string) string {
	candidates := []string{flagged, os.Getenv("WEBDIR"), config.MeeptPath("webui")}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && info.IsDir() { //nolint:gosec // G703: operator-supplied webdir candidate (-webdir flag / WEBDIR env), the same trust class as every flag path
			return candidate
		}
	}
	return ""
}

// stringSlice is a flag.Value collecting a repeatable AND comma-listable
// flag (stdlib strings has no split-on-reuse helper; the daemon boot emits
// one -allow-origin per entry, a human operator may pass a comma list).
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }

func (s *stringSlice) Set(raw string) error {
	for part := range strings.SplitSeq(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			*s = append(*s, trimmed)
		}
	}
	return nil
}

// dialDaemon connects to the daemon over the Unix socket (default) or HTTP.
// It mirrors cmd/meept's connectDaemon without pulling the CLI's config
// loading into the sidecar: the daemon endpoint is a flag, because the daemon
// spawns this child and knows its own socket path.
func dialDaemon(socket, baseURL string) (transport.Client, error) {
	if socket == "" {
		socket = config.MeeptPath("meept.sock")
	}
	cfg := &transport.Config{SocketPath: socket, Transport: "rpc"}
	if baseURL != "" {
		cfg.Transport = "http"
		cfg.HTTPBaseURL = baseURL
	}
	client, err := transport.New(cfg)
	if err != nil {
		return nil, err
	}
	if err := client.Connect(); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}
