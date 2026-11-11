// Messaging HTTP surface (contact-channels leaf 12 T3, hardened by
// plan-tree leaf 01-webhook-hardening/02): the Telnyx SMS webhook entry
// point.
//
//   - POST /api/v1/voice/messaging/webhook - messaging webhooks
//     (message.received inbound texts + message.finalized DLRs).
//     Mandatory Ed25519 signature over the RAW body (the SAME
//     voice/telnyx verifier the voice webhook uses - never a second
//     one), THEN telnyx.ParseMessagingEvent on those same buffered bytes
//     (verify-then-parse), then the injected seams. The legacy
//     webhookEnvelope/telnyx.ParseWebhookEvent intake is DELETED from
//     this surface: real Telnyx payloads type `from` as an OBJECT and
//     `to` as an ARRAY, which the old envelope could not parse.
//
// HANDLER POSTURE (strategist-ported table, gate §H):
//
//	bad signature                -> 401 (no parse, nonce untouched)
//	malformed JSON valid sig     -> 400 (proves verify-then-parse)
//	noop / unknown event type    -> 200 ack (stops Telnyx redelivery)
//	duplicate event id           -> 200 ack-noop (seam not called)
//	downstream seam error        -> Release the nonce, THEN 500
//	                               (redelivery re-reaches the handler)
//	success                      -> 202
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
	"reflect"

	vt "github.com/caimlas/meept/internal/voice/telnyx"
)

// messagingWebhookPath is the registered route path. It sits under the
// /api/v1/voice/ prefix so the existing HandlesPath bearer-auth
// exemption covers it (Telnyx cannot present a meept API key).
const messagingWebhookPath = "/api/v1/voice/messaging/webhook"

// messagingNonce is the replay-protection seam (contract §1): the method
// set a *vt.NonceStore structurally satisfies. The interface shape (not
// the concrete store) lets tests inject fakes and the daemon inject the
// shared NonceStore; nil means no dedupe (compat posture).
type messagingNonce interface {
	CheckFresh(eventID string) bool
	Release(eventID string)
}

// MessagingWebhookDeps carries the receiver's seams (leaf contract C5 +
// 01-webhook-hardening/02). Verify is the existing Ed25519 webhook
// verifier seam (the daemon wiring builds it from voice/telnyx
// signature verification with the SAME telnyx_public_key as the voice
// verifiers - never a second verifier); Nonce is the replay dedupe seam
// (nil = no dedupe, every delivery reaches the consumer seams);
// OnInbound / OnFinalized are the consumer seams (session binding + DLR
// resolution respectively) and receive the NORMALIZED voice/telnyx
// message — the old internal/telnyx envelope type is gone from this
// surface. Verify is required; OnInbound/OnFinalized nil seams degrade
// to parse-and-ack.
type MessagingWebhookDeps struct {
	Verify      func(payload []byte, headers http.Header) error
	Nonce       messagingNonce
	OnInbound   func(msg vt.Message) error
	OnFinalized func(msg vt.Message) error
}

// MessagingRoutes serves the SMS webhook endpoint on the daemon's HTTP
// server. The messaging parser lives in internal/voice/telnyx next to
// the verifier (stdlib-only, cycle-free); this package never grows a
// second Ed25519 implementation or payload parser.
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

// handleWebhook: read-raw -> signature -> parse -> classify -> dedupe ->
// dispatch. The body is buffered once and verified against the RAW
// bytes, then the SAME bytes are parsed (verify-then-parse; the
// signature covers the exact bytes Telnyx signed).
func (m *MessagingRoutes) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBytes))
	if err != nil {
		http.Error(w, "unreadable webhook body", http.StatusBadRequest)
		return
	}
	if err := m.deps.Verify(body, r.Header); err != nil {
		// Missing/mismatched signature: 401 BEFORE any parsing or nonce
		// use, so an unsigned request can never drive the messaging
		// seams or poison the replay store with attacker event ids.
		m.log.Warn("messaging webhook: signature verification failed",
			"error", err, "remote", r.RemoteAddr)
		http.Error(w, "signature verification failed", http.StatusUnauthorized)
		return
	}
	msg, err := vt.ParseMessagingEvent(body)
	if err != nil {
		if errors.Is(err, vt.ErrUnknownEvent) {
			// Noop family (message.created/deleted) or a non-SMS event
			// type (misrouted webhook): ack 200 so Telnyx STOPS
			// redelivering — superseding the legacy 400 posture
			// (contract §1: unknown event type -> 200).
			m.log.Info("messaging webhook: acking unhandled event type", "error", err)
			writeVoiceOK(w)
			return
		}
		// Parseable-signature-but-malformed-payload: a real client
		// error. 400 AFTER verification (redelivery of garbage helps
		// nobody; the signature already proved the sender).
		m.log.Warn("messaging webhook: malformed event after signature pass", "error", err)
		http.Error(w, "malformed messaging event", http.StatusBadRequest)
		return
	}

	// Replay dedupe on the EVENT id (data.id). CheckFresh is the SETNX:
	// a false result means this event id was already accepted in-window
	// — ack 200 without touching the consumer seams. nil Nonce = no
	// dedupe (compat — including a typed-nil pointer in the interface,
	// which is how callers pass "no store" through the field). An event
	// with no id cannot be deduped: pass it through (the seams' own
	// insert-ignore stays the last guard).
	deduped := nonceUsable(m.deps.Nonce) && msg.EventID != ""
	if deduped && !m.deps.Nonce.CheckFresh(msg.EventID) {
		m.log.Info("messaging webhook: duplicate event acked", "event_id", msg.EventID)
		writeVoiceOK(w)
		return
	}

	// Direction decided by the EVENT TYPE inside the parser: inbound
	// (message.received) drives OnInbound; every other handled type
	// (interack sent/queued/sending + terminal finalized/failed/
	// delivery_failed DLRs) drives OnFinalized, mirroring the legacy
	// switch's routing the contact-chain glue depends on.
	seamErr := m.dispatch(msg)
	if seamErr != nil {
		// Transient consumer failure (mid-restart etc): Release the
		// nonce FIRST so Telnyx's redelivery re-reaches the handler
		// instead of being 200-acked as a duplicate — otherwise the
		// event is LOST. Then 500 to trigger that redelivery.
		if deduped {
			m.deps.Nonce.Release(msg.EventID)
		}
		m.log.Error("messaging webhook: handling error (nonce released, redelivery expected)",
			"error", seamErr, "event_id", msg.EventID,
			"message_id", msg.ProviderMessageID, "direction", msg.Direction)
		http.Error(w, "messaging webhook handling error", http.StatusInternalServerError)
		return
	}
	m.log.Info("messaging webhook: event handled",
		"direction", msg.Direction, "event_id", msg.EventID,
		"message_id", msg.ProviderMessageID, "status", msg.Status)
	writeVoiceAccepted(w)
}

// nonceUsable reports whether the injected dedupe seam is actually
// callable. A field assignment like `Nonce: (*SomeStore)(nil)` yields a
// NON-nil interface wrapping a nil pointer (the gate's nil-nonce compat
// case passes exactly that), so a plain != nil check would panic inside
// the nil receiver's method. Typed-nil must degrade to "no dedupe".
func nonceUsable(n messagingNonce) bool {
	if n == nil {
		return false
	}
	v := reflect.ValueOf(n)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Func, reflect.Slice, reflect.Chan, reflect.Interface:
		return !v.IsNil()
	}
	return true
}

// dispatch routes a normalized event to its consumer seam (nil seam =
// parse-and-ack success).
func (m *MessagingRoutes) dispatch(msg vt.Message) error {
	if msg.Direction == "inbound" {
		if m.deps.OnInbound != nil {
			return m.deps.OnInbound(msg)
		}
		return nil
	}
	if m.deps.OnFinalized != nil {
		return m.deps.OnFinalized(msg)
	}
	return nil
}

// writeVoiceAccepted answers a successfully-processed webhook. 202 is
// the §H success posture (any 2xx stops Telnyx redelivery; the code
// distinguishes "processed" from the 200 ack-noop replies).
func writeVoiceAccepted(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	if _, err := io.WriteString(w, "accepted"); err != nil {
		// Webhook 202-body write: client (Telnyx) treats any 2xx as
		// delivered; a failed body write after WriteHeader is not
		// actionable.
		return
	}
}
