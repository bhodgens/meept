package http

// Tests for the Telnyx SMS messaging webhook receiver (contact-channels
// leaf 12 T3): bad signature -> 401 and zero dep calls (never parsed);
// good-signature message.received -> OnInbound fired; good-signature
// message.finalized -> OnFinalized fired. The signature fixtures reuse
// the voice tests' signingVerify helper (same Ed25519 scheme, same
// verifier - the reuse the leaf mandates).

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	vt "github.com/caimlas/meept/internal/voice/telnyx"
)

// Gate amendment (leaf 01-webhook-hardening/02 posture, owner-ratified
// supersession 2026-11-10): seams now carry voice/telnyx.Message
// (tolerant normalizer), seam-success is 202 Accepted, and good-signature
// non-messaging events are 200 ack-noops (Telnyx redelivery policy), not
// 400. fakeVoiceManager lives in voice_test.go; messageDeps is the
// messaging seam fake counting dep invocations.
type messageDeps struct {
	mu        sync.Mutex
	inbound   []vt.Message
	finalized []vt.Message
}

func (d *messageDeps) onInbound(msg vt.Message) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.inbound = append(d.inbound, msg)
	return nil
}

func (d *messageDeps) onFinalized(msg vt.Message) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.finalized = append(d.finalized, msg)
	return nil
}

func (d *messageDeps) counts() (int, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.inbound), len(d.finalized)
}

// messagingBody marshals a canonical Telnyx messaging webhook.
func messagingBody(t *testing.T, eventType string, payload map[string]any) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"data": map[string]any{
			"event_type": eventType,
			"payload":    payload,
		},
	})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return body
}

// enabledMessagingRoutes wires MessagingRoutes with the voice tests'
// signing verifier and a recording deps struct.
func enabledMessagingRoutes(t *testing.T) (*MessagingRoutes, *messageDeps, func([]byte) http.Header) {
	t.Helper()
	verify, signWebhook, _ := signingVerify(t)
	deps := &messageDeps{}
	routes, err := NewMessagingRoutes(MessagingWebhookDeps{
		Verify:      verify.Webhook,
		OnInbound:   deps.onInbound,
		OnFinalized: deps.onFinalized,
	}, nil)
	if err != nil {
		t.Fatalf("NewMessagingRoutes: %v", err)
	}
	return routes, deps, signWebhook
}

// doMessaging builds a server whose middleware chain wraps only the
// messaging route (matching production wiring: reached through
// Server.middleware, bypassing bearer auth via HandlesPath).
func doMessaging(t *testing.T, routes *MessagingRoutes, body []byte, headers http.Header) *httptest.ResponseRecorder {
	t.Helper()
	s := NewServer(ServerConfig{}, nil, nil, nil, nil, nil, WithMessagingRoutes(routes))
	handler := s.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("request fell through to the general mux: %s %s", r.Method, r.URL.Path)
	}))
	req := httptest.NewRequest(http.MethodPost, routes.WebhookPath(), bytes.NewReader(body))
	for k, vs := range headers {
		for _, val := range vs {
			req.Header.Add(k, val)
		}
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// TestMessagingWebhookSignatureFail pins the security gate: a tampered
// payload is a 401 and NEITHER dep seam is ever invoked (the body is
// never parsed into action).
func TestMessagingWebhookSignatureFail(t *testing.T) {
	routes, deps, _ := enabledMessagingRoutes(t)
	body := messagingBody(t, "message.received", map[string]any{
		"id":   "inb-x",
		"from": "+15550000001",
		"to":   "+15559999999",
		"text": "should never be acted on",
	})
	rec := doMessaging(t, routes, body, http.Header{}) // no signature headers
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	inbound, finalized := deps.counts()
	if inbound != 0 || finalized != 0 {
		t.Fatalf("deps fired on a bad signature (inbound=%d finalized=%d); the body must never be parsed before verification", inbound, finalized)
	}
}

// TestMessagingWebhookInbound pins the good-signature inbound path:
// message.received fires OnInbound with the parsed Message.
func TestMessagingWebhookInbound(t *testing.T) {
	routes, deps, sign := enabledMessagingRoutes(t)
	body := messagingBody(t, "message.received", map[string]any{
		"id":   "inb-2",
		"from": "+15550000002",
		"to":   "+15559999998",
		"text": "reply yes to confirm",
	})
	rec := doMessaging(t, routes, body, sign(body))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	inbound, _ := deps.counts()
	if inbound != 1 {
		t.Fatalf("OnInbound fired %d times, want 1", inbound)
	}
}

// TestMessagingWebhookFinalized pins the DLR path: message.finalized
// fires OnFinalized (the SMSAttempt pending-map feed).
func TestMessagingWebhookFinalized(t *testing.T) {
	routes, deps, sign := enabledMessagingRoutes(t)
	body := messagingBody(t, "message.finalized", map[string]any{
		"id":     "out-5",
		"from":   "+15559999997",
		"to":     "+15550000003",
		"status": "delivered",
	})
	rec := doMessaging(t, routes, body, sign(body))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	_, finalized := deps.counts()
	if finalized != 1 {
		t.Fatalf("OnFinalized fired %d times, want 1", finalized)
	}
}

// TestMessagingWebhookUnknownEvent pins the ratified redelivery posture
// (gate amendment 2026-11-10, leaf 01/02): a good-signature
// non-messaging event is a 200 ack-noop so Telnyx stops redelivering
// what it sends to a shared webhook URL — deps never fire. (Malformed
// JSON, which cannot be classified, stays a 400 AFTER signature
// verification.)
func TestMessagingWebhookUnknownEvent(t *testing.T) {
	routes, deps, sign := enabledMessagingRoutes(t)
	body := messagingBody(t, "call.answered", map[string]any{"call_control_id": "cc-1"})
	rec := doMessaging(t, routes, body, sign(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (ack-noop posture)", rec.Code)
	}
	inbound, finalized := deps.counts()
	if inbound != 0 || finalized != 0 {
		t.Fatalf("deps fired on a non-messaging event (inbound=%d finalized=%d)", inbound, finalized)
	}
}

// TestNewMessagingRoutesNilVerifier pins the fail-closed construction:
// a nil verifier is an error, never a signature-optional route.
func TestNewMessagingRoutesNilVerifier(t *testing.T) {
	if _, err := NewMessagingRoutes(MessagingWebhookDeps{}, nil); err == nil {
		t.Fatal("NewMessagingRoutes with nil verifier = nil error, want refusal")
	}
}
