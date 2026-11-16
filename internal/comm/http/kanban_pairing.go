// Package http: browser-facing pairing exchange for the kanban board
// (kanban-board follow-up gap: literal POST /kanban/pair/exchange mux hook).
//
// # WHAT THIS IS
//
// The daemon's kanban lane mints single-use pairing codes over the owner
// RPC socket (`kanban.pair.mint`, printed once to the console) and the
// exchange is live over `kanban.pair.exchange`. A BROWSER pointed at the
// sidecar's /web/ bundle has no owner socket, so it needs an HTTP route
// on the daemon — which is where the codes live (codes are in-memory per
// process; the daemon mints, so the daemon redeems; the shared tokens.json
// then makes the minted bearer token valid at the sidecar, proven by e2e
// case (e)). This is that route.
//
// # SECURITY POSTURE — mirrored from the dev-key pairing precedent
// (internal/comm/http/pairing.go handleExchange), deliberately, exactly:
//   - LOOPBACK-ONLY: isLoopbackRequest gates the handler; a remote caller
//     gets 403 regardless of any code. The precedent's exchange endpoint
//     is loopback-only even when transport.http is publicly bound, and
//     this endpoint inherits that rule: it hands out board tokens, so the
//     blast radius stays where a pairing code was physically read.
//   - NO bearer auth BY DESIGN: the single-use out-of-band code in the
//     body IS the credential. The middleware chain exempts this path from
//     bearer auth exactly like /api/v1/pair/* — a browser pairing for the
//     FIRST time by definition holds no key yet.
//   - RATE-LIMITED: unlike the dev-key handler (6 crypto/rand hex bytes,
//     15-minute TTL, consumed-once), kanban codes are 8 chars from a
//     32-symbol alphabet (~40 bits). The route therefore still runs the
//     chain's per-IP rate limiter before the handler — loopback plus
//     limiter, never loopback alone.
//   - NO ORACLE: wrong, consumed, expired, and missing codes answer the
//     SAME 401 body ("invalid or already-used"), matching the sidecar
//     gateway's ErrInvalidPairCode posture and the precedent's uniform
//     failure shape.
//   - The code and the minted token are NEVER logged at any level; the
//     success line names the fact, not the secret (precedent invariant).
//   - Body cap 4KB (io.LimitReader), like the precedent.
//   - When no kanban exchange is wired ([kanban].enabled=false or pairing
//     degraded) the route does not exist: the middleware branch is
//     absent and the mux registration is skipped — byte-identical.
package http

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
)

// KanbanPairExchangePath is the literal documented route path. Keep in
// sync with the registration in setupRoutes and the middleware branch.
const KanbanPairExchangePath = "/kanban/pair/exchange"

// KanbanPairExchanger is the daemon's kanban pairing seam
// (internal/daemon kanbanPairing.Exchange): consume a single-use code,
// return the bearer token + grants. An error means invalid/consumed and
// MUST NOT be distinguished in the response body.
type KanbanPairExchanger interface {
	Exchange(code string) (token string, grants []string, err error)
}

type kanbanPairingHandler struct {
	exchanger KanbanPairExchanger
	logger    *slog.Logger
}

// WithKanbanPairing arms POST /kanban/pair/exchange on the server. A nil
// exchanger (kanban disabled) leaves the route unregistered and the
// middleware chain byte-identical.
func WithKanbanPairing(exchanger KanbanPairExchanger, logger *slog.Logger) ServerOption {
	return func(s *Server) {
		if exchanger == nil {
			return
		}
		if logger == nil {
			logger = slog.Default()
		}
		s.kanbanPairing = &kanbanPairingHandler{exchanger: exchanger, logger: logger}
	}
}

// ServeHTTP handles POST /kanban/pair/exchange. Registered on the mux so
// the connectivity graph lists it, and intercepted by the middleware
// BEFORE the bearer-auth step (but AFTER the per-IP rate limiter), as the
// code is the credential.
func (h *kanbanPairingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writePairingError(w, http.StatusForbidden, "kanban pairing is restricted to loopback clients")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
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
	token, grants, err := h.exchanger.Exchange(req.Code)
	if err != nil {
		// ONE uniform answer for wrong, consumed, expired, and missing
		// codes — the exchanger's error text is deliberately already
		// "invalid or already-used" (kanban_wiring.go), and nothing more
		// specific may leak through here.
		h.logger.Debug("kanban pairing exchange refused")
		h.writeError(w, http.StatusUnauthorized, "invalid or already-used pairing code")
		return
	}
	// Lifecycle line WITHOUT the code or token (precedent invariant).
	h.logger.Info("kanban pairing: code exchanged; browser token issued", "grants", len(grants))
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"token": token, "grants": grants}); err != nil {
		h.logger.Warn("kanban pairing: exchange reply write failed", "error", err)
	}
}

// writeError mirrors writePairingError's shape — {"error", "message"} —
// with a kanban-scoped error name so a client can tell the lanes apart
// without exposing anything else. Method (not free func) so a failed
// error-body write surfaces at Warn instead of being discarded
// (pre-commit-errors nil-discard ratchet).
func (h *kanbanPairingHandler) writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{
		"error":   "kanban_pairing",
		"message": msg,
	}); err != nil {
		h.logger.Warn("kanban pairing: error reply write failed", "error", err)
	}
}
