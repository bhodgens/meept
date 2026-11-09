// Messaging HTTP surface (contact-channels leaf 12 T3): the Telnyx SMS
// webhook entry point.
//
//   - POST /api/v1/voice/messaging/webhook - messaging webhooks
//     (message.received inbound texts + message.finalized DLRs).
//     Mandatory Ed25519 signature over the raw body (the SAME
//     voice/telnyx verifier the voice webhook uses - never a second
//     one), THEN telnyx.ParseWebhookEvent, then the injected seams.
//
// AUTH POSTURE: identical to the voice webhook (this file mirrors
// voice.go). The route is called by Telnyx, not by GUI clients, so it
// bypasses the server's bearer-key auth via the existing voiceRoutes
// HandlesPath exemption and is gated by MANDATORY signature verification
// inside the handler: a bad or missing signature is a 401 BEFORE the
// body is ever parsed. When the voice routes are not wired at all the
// route fail-closes to 503 (never signature-optional).
//
// WS CLASSIFICATION: this file creates no bus subscriptions and
// publishes no bus topics (internal/comm/AGENTS.md).
package http

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/caimlas/meept/internal/telnyx"
)

// messagingWebhookPath is the registered route path. It sits under the
// /api/v1/voice/ prefix so the existing HandlesPath bearer-auth
// exemption covers it (Telnyx cannot present a meept API key).
const messagingWebhookPath = "/api/v1/voice/messaging/webhook"

// MessagingWebhookDeps carries the receiver's seams (leaf contract C5).
// Verify is the existing Ed25519 webhook verifier seam (the daemon
// wiring builds it from voice/telnyx.VerifyWebhookSignature with the
// SAME telnyx_public_key as the voice verifiers - never a second
// verifier); OnInbound / OnFinalized are the consumer seams (session
// binding + DLR resolution respectively). Verify is required;
// OnInbound/OnFinalized nil seams degrade to parse-and-ack.
type MessagingWebhookDeps struct {
	Verify      func(payload []byte, headers http.Header) error
	OnInbound   func(msg telnyx.Message) error
	OnFinalized func(msg telnyx.Message) error
}

// MessagingRoutes serves the SMS webhook endpoint on the daemon's HTTP
// server. The messaging parser lives in internal/telnyx (stdlib-only,
// cycle-free); the signature verifier is injected, so this package
// never grows a second Ed25519 implementation.
type MessagingRoutes struct {
	deps MessagingWebhookDeps
	log  *slog.Logger
}

// NewMessagingRoutes builds the messaging route group. deps.Verify is
// required (nil verifier = construction error: the route must never run
// signature-optional).
func NewMessagingRoutes(deps MessagingWebhookDeps, logger *slog.Logger) (*MessagingRoutes, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if deps.Verify == nil {
		return nil, errors.New("messaging routes: enabled requires a signature verifier (telnyx public key unset?)")
	}
	return &MessagingRoutes{deps: deps, log: logger}, nil
}

// WebhookPath returns the registered webhook route path.
func (m *MessagingRoutes) WebhookPath() string { return messagingWebhookPath }

// HandlesPath reports whether p is the messaging webhook route. The
// server middleware folds it into the existing bearer-auth exemption
// branch (Telnyx authenticates with the mandatory Ed25519 signature
// inside the handler).
func (m *MessagingRoutes) HandlesPath(p string) bool { return p == messagingWebhookPath }

// serveMessagingRoute dispatches to the messaging handler with method
// enforcement. The Server middleware calls this in the same auth-exempt
// branch as the voice routes.
func (m *MessagingRoutes) serveMessagingRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	m.handleWebhook(w, r)
}

// ServeWebhook exposes the messaging webhook handler for direct callers
// (the e2e suite drives the exact handler the Server middleware branch
// runs; the Server path itself goes through serveMessagingRoute via
// HandlesPath). Same 401-before-parse posture.
func (m *MessagingRoutes) ServeWebhook(w http.ResponseWriter, r *http.Request) {
	m.serveMessagingRoute(w, r)
}

// handleWebhook: signature -> parse -> dispatch (mirrors the voice
// webhook's ordering exactly).
func (m *MessagingRoutes) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBytes))
	if err != nil {
		http.Error(w, "unreadable webhook body", http.StatusBadRequest)
		return
	}
	if err := m.deps.Verify(body, r.Header); err != nil {
		// Missing/mismatched signature: 401 BEFORE any parsing, so an
		// unsigned request can never drive the messaging seams.
		m.log.Warn("messaging webhook: signature verification failed",
			"error", err, "remote", r.RemoteAddr)
		http.Error(w, "signature verification failed", http.StatusUnauthorized)
		return
	}
	msg, err := telnyx.ParseWebhookEvent(body)
	if err != nil {
		// Not a messaging event we recognise (misrouted webhook): 400.
		m.log.Warn("messaging webhook: unrecognised event", "error", err)
		http.Error(w, "unrecognised messaging event", http.StatusBadRequest)
		return
	}
	switch msg.Direction {
	case "inbound":
		if m.deps.OnInbound != nil {
			if err := m.deps.OnInbound(msg); err != nil {
				// 500 = transient: Telnyx redelivers (the consumer seam
				// may be mid-restart). Same mapping as the voice webhook.
				m.log.Error("messaging webhook: inbound error",
					"error", err, "message_id", msg.ID)
				http.Error(w, "inbound handling error", http.StatusInternalServerError)
				return
			}
		}
	default: // outbound DLR (message.finalized)
		if m.deps.OnFinalized != nil {
			if err := m.deps.OnFinalized(msg); err != nil {
				m.log.Error("messaging webhook: DLR error",
					"error", err, "message_id", msg.ID)
				http.Error(w, "dlr handling error", http.StatusInternalServerError)
				return
			}
		}
	}
	m.log.Info("messaging webhook: event handled",
		"direction", msg.Direction, "message_id", msg.ID, "status", msg.Status)
	writeVoiceOK(w)
}
