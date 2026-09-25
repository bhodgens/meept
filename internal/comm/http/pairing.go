// Package http: first-run pairing handshake for freshly installed GUIs.
//
// WHY THIS EXISTS
//
// The per-installation dev key lives at $MEEPT_HOME/dev_key and the daemon
// authenticates HTTP/WS clients with it. Historically `make build-gui` baked
// the BUILD machine's key (and endpoint) into the Flutter bundle via
// --dart-define, so every distributed GUI binary shipped the builder's
// secret. The distribution path is now a key-less bundle plus this pairing
// handshake: the daemon prints a one-time pairing code to its console, the
// GUI exchanges that code for the dev key exactly once, and stores it in
// client-side storage.
//
// Threat model / scope:
//   - Routes answer LOOPBACK-ONLY (RemoteAddr must be a loopback IP).
//   - The exchange requires a single-use pairing code generated from
//     crypto/rand, printed once to the daemon console at server Start, and
//     consumed atomically on success (or expired after the TTL).
//   - The dev key is NEVER logged here (not even at debug); the pairing code
//     is printed to stdout (the designed delivery channel) but never passed
//     to the structured logger at info level.
//   - Legacy auth is untouched: when no PairingService is wired (WithPairing
//     not passed), these routes do not exist and the middleware chain is
//     byte-identical to before.
package http

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// pairingPathPrefix is the route prefix for the pairing handshake. Keep in
// sync with the registrations in setupRoutes and the middleware exemption.
const pairingPathPrefix = "/api/v1/pair/"

// pairingCodeTTL is how long a printed pairing code stays valid. A code
// that is never exchanged simply expires; a restart mints a fresh one.
const pairingCodeTTL = 15 * time.Minute

// PairingService implements the one-time pairing handshake.
//
// The zero value is not usable; construct with NewPairingService and pass
// via WithPairing. Activate (called automatically by Server.Start) mints the
// pairing code and prints it to stdout.
type PairingService struct {
	// keyFn returns the API key to hand to a successfully paired client.
	// It must return the SAME key the auth middleware accepts.
	keyFn func() string

	logger *slog.Logger

	mu         sync.Mutex
	code       string    // normalized (no dashes); "" = no code outstanding
	printed    string    // grouped form as printed to the console
	expiresAt  time.Time // zero when no code is outstanding
	activated  bool
	timeSource func() time.Time // injectable for tests
}

// NewPairingService creates a pairing service that exchanges a one-time
// code for the key returned by keyFn. The logger is used only for
// code-free lifecycle lines (never the key or the code).
func NewPairingService(keyFn func() string, logger *slog.Logger) *PairingService {
	if logger == nil {
		logger = slog.Default()
	}
	return &PairingService{
		keyFn:      keyFn,
		logger:     logger,
		timeSource: time.Now,
	}
}

// Activate mints a fresh single-use pairing code and prints it to the
// daemon console (stdout). Called by Server.Start before the listener comes
// up; safe to call again to rotate (re-pair) — any outstanding code is
// replaced.
func (p *PairingService) Activate() {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.timeSource()
	code, printed, err := generatePairingCode()
	if err != nil {
		// crypto/rand failure: do NOT arm a guessable code. Pairing stays
		// unavailable until the next restart. Loud line, no fallback token.
		p.code = ""
		p.printed = ""
		p.expiresAt = time.Time{}
		p.activated = true
		p.logger.Error("pairing: crypto/rand failed; pairing unavailable until restart",
			"error", err)
		return
	}
	p.code = code
	p.printed = printed
	p.expiresAt = now.Add(pairingCodeTTL)
	p.activated = true

	// The console line is the designed delivery channel for the code (the
	// user reads it from the daemon's terminal / log file). It goes to
	// stdout directly, NOT through slog, so it never appears as an
	// info-level log record.
	fmt.Fprintf(os.Stdout, "pairing: one-time pairing code (valid %s, single use): %s\n",
		pairingCodeTTL, printed)
}

// exchange consumes the outstanding code (if valid) and returns the dev key.
// The returned bool distinguishes "armed" from "no code outstanding".
func (p *PairingService) exchange(presented string) (string, bool) {
	normalized := normalizePairingCode(presented)
	if normalized == "" {
		return "", false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.activated || p.code == "" {
		return "", false
	}
	if p.timeSource().After(p.expiresAt) {
		p.code = ""
		p.printed = ""
		return "", false
	}
	// Constant-time compare, then consume atomically under the same lock.
	if subtle.ConstantTimeCompare([]byte(normalized), []byte(p.code)) != 1 {
		return "", false
	}
	p.code = ""
	p.printed = ""
	p.expiresAt = time.Time{}
	return p.keyFn(), true
}

// status reports whether pairing is armed and whether a code is currently
// outstanding (never the code itself).
func (p *PairingService) status() (armed bool, outstanding bool, expiresIn time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.activated {
		return false, false, 0
	}
	if p.code == "" {
		return true, false, 0
	}
	remaining := p.expiresAt.Sub(p.timeSource())
	if remaining <= 0 {
		p.code = ""
		p.printed = ""
		return true, false, 0
	}
	return true, true, remaining
}

// generatePairingCode mints 6 crypto/rand bytes rendered as both the
// normalized form (12 hex chars) and the human-friendly grouped form
// (XXXX-XXXX-XXXX).
func generatePairingCode() (normalized, printed string, err error) {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	normalized = hex.EncodeToString(raw)
	var b strings.Builder
	for i := 0; i < len(normalized); i++ {
		if i > 0 && i%4 == 0 {
			b.WriteByte('-')
		}
		b.WriteByte(normalized[i])
	}
	return normalized, b.String(), nil
}

// normalizePairingCode canonicalizes user input: lowercase, whitespace and
// dash separators stripped, so "abcd-ef01-2345" and "ABCDEF012345" match.
func normalizePairingCode(s string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(strings.TrimSpace(s)) {
		switch c {
		case '-', ' ', '\t':
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}

// isLoopbackRequest is defined in main_config_handlers.go (strict: unparseable
// or missing-port RemoteAddr is rejected). The pairing handlers reuse it.

func writePairingError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":   "pairing",
		"message": msg,
	})
}

// handleStatus serves GET /api/v1/pair/status: loopback-only, no auth. It
// tells a fresh GUI whether pairing is armed and whether a code is
// outstanding — never the code itself.
func (p *PairingService) handleStatus(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writePairingError(w, http.StatusForbidden, "pairing is restricted to loopback clients")
		return
	}
	armed, outstanding, remaining := p.status()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"pairing":         armed,
		"code_outstanding": outstanding,
		"expires_in_s":    int(remaining.Seconds()),
	})
}

// handleExchange serves POST /api/v1/pair/exchange: loopback-only, no
// (bearer) auth — the one-time pairing code IS the credential. Body:
// {"code": "XXXX-XXXX-XXXX"}. Success consumes the code and returns
// {"api_key": "..."} exactly once.
func (p *PairingService) handleExchange(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writePairingError(w, http.StatusForbidden, "pairing is restricted to loopback clients")
		return
	}
	if r.Method != http.MethodPost {
		writePairingError(w, http.StatusMethodNotAllowed, "use POST with a JSON body")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		writePairingError(w, http.StatusBadRequest, "unreadable request body")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writePairingError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	key, ok := p.exchange(req.Code)
	if !ok {
		// Same response shape for wrong, expired, consumed, and missing
		// codes: no oracle for which state the service is in.
		writePairingError(w, http.StatusForbidden,
			"invalid, expired, or already-used pairing code; restart the daemon for a fresh code")
		return
	}
	// Lifecycle log WITHOUT the key: name the fact, not the secret.
	p.logger.Info("pairing: code exchanged; client provisioned")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"api_key": key})
}

// ServeHTTPForPath dispatches a request whose path is under
// pairingPathPrefix to the right pairing handler. It is called from
// Server.middleware INSTEAD of the rate-limit+auth chain for pairing paths.
func (p *PairingService) ServeHTTPForPath(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/v1/pair/status":
		p.handleStatus(w, r)
	case "/api/v1/pair/exchange":
		p.handleExchange(w, r)
	default:
		writePairingError(w, http.StatusNotFound, "unknown pairing endpoint")
	}
}

// WithPairing arms the pairing handshake on the server: the routes under
// /api/v1/pair/ register and the auth middleware exempts them (the pairing
// code is their credential). Nil disables pairing entirely.
func WithPairing(p *PairingService) ServerOption {
	return func(s *Server) {
		if p != nil {
			s.pairing = p
		}
	}
}
