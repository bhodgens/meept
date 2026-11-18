package http

// 01-webhook-hardening/00T §H — messaging webhook handler posture gate
// tests (master.md §1 item 1 posture table + item 2 handler rewrite).
//
// Pins the strategist-ported posture table:
//
//	signature fail               -> 401 (no parse, no nonce, no seam)
//	duplicate event id           -> 200 ack-noop (seam NOT called)
//	known-noop / unknown type    -> 200 (stop redelivery; seam NOT called)
//	malformed JSON valid sig     -> 400 (proves verify-then-parse ordering)
//	downstream handler error     -> 500 AND nonce Released first (redelivery re-reaches handler)
//	success                      -> 202
//
// Pins the deps extensions (contract §1 deps note / 02-normalizer task 3):
//
//	MessagingWebhookDeps.Nonce   interface-shaped dedupe seam; the ONLY
//	                             shape that a *vt.NonceStore structurally
//	                             satisfies: CheckFresh(eventID string) bool
//	                             + Release(eventID string). nil Nonce =
//	                             no dedupe (compat).
//	OnInbound / OnFinalized      receive vt.Message (voice/telnyx
//	                             ParseMessagingEvent's normalized output —
//	                             the old webhookEnvelope is DELETED).
//
// The fakes below are structural: the impl leaf's interface name may be
// anything as long as NonceStore's method set assigns to deps.Nonce and
// these fakes' method set does too.

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	vt "github.com/caimlas/meept/internal/voice/telnyx"
)

// errPayloadFake drives the seam-error posture.
var errPayloadFake = errors.New("payload test: seam failure")

// ---- fakes -------------------------------------------------------------

// payloadNonce is the recording NonceStore fake injected via deps.Nonce.
// Method set == the pinned interface shape (vt.NonceStore satisfies it).
type payloadNonce struct {
	mu       sync.Mutex
	held     map[string]bool
	checks   []string
	releases []string
}

func newPayloadNonce() *payloadNonce {
	return &payloadNonce{held: map[string]bool{}}
}

func (f *payloadNonce) CheckFresh(eventID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks = append(f.checks, eventID)
	if f.held[eventID] {
		return false
	}
	f.held[eventID] = true
	return true
}

func (f *payloadNonce) Release(eventID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releases = append(f.releases, eventID)
	delete(f.held, eventID)
}

func (f *payloadNonce) snapshot() (checks, releases []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.checks...), append([]string{}, f.releases...)
}

func (f *payloadNonce) isHeld(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.held[id]
}

// payloadDeps records seam invocations; seamErr makes both seams fail.
type payloadDeps struct {
	mu        sync.Mutex
	inbound   []vt.Message
	finalized []vt.Message
	seamErr   error
}

func (d *payloadDeps) onInbound(msg vt.Message) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.inbound = append(d.inbound, msg)
	return d.seamErr
}

func (d *payloadDeps) onFinalized(msg vt.Message) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.finalized = append(d.finalized, msg)
	return d.seamErr
}

func (d *payloadDeps) setSeamErr(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seamErr = err
}

func (d *payloadDeps) counts() (in, fin int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.inbound), len(d.finalized)
}

// ---- wiring ------------------------------------------------------------

// payloadRoutes wires MessagingRoutes with the voice signing verifier,
// fake seams, and a fake nonce (nil nonce = the compat "no dedupe"
// posture). The Verify seam is wrapped to capture the EXACT bytes it was
// handed so tests can assert verification runs on the RAW body.
func payloadRoutes(t *testing.T, d *payloadDeps, nonce *payloadNonce) (*MessagingRoutes, func([]byte) http.Header, *[]byte) {
	t.Helper()
	verify, sign, _ := signingVerify(t)
	captured := &[]byte{}
	routes, err := NewMessagingRoutes(MessagingWebhookDeps{
		Verify: func(body []byte, h http.Header) error {
			*captured = append([]byte{}, body...)
			return verify.Webhook(body, h)
		},
		Nonce:       nonce,
		OnInbound:   d.onInbound,
		OnFinalized: d.onFinalized,
	}, nil)
	if err != nil {
		t.Fatalf("NewMessagingRoutes: %v", err)
	}
	return routes, sign, captured
}

// postPayload drives the handler directly (same code path the Server
// middleware branch reaches via HandlesPath).
func postPayload(t *testing.T, routes *MessagingRoutes, body []byte, headers http.Header) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, routes.WebhookPath(), bytes.NewReader(body))
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	rec := httptest.NewRecorder()
	routes.ServeWebhook(rec, req)
	return rec
}

// realShapeMessagingBody builds a canonical Telnyx messaging webhook in
// the REAL payload shape the old webhookEnvelope could not parse: from is
// an OBJECT, to is an ARRAY of objects, and data.id is the event uuid.
func realShapeMessagingBody(t *testing.T, eventType, eventID string, payload map[string]any) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"data": map[string]any{
			"event_type": eventType,
			"id":         eventID,
			"payload":    payload,
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return body
}

// inboundPayload is the real-shape inbound payload (from OBJECT, to
// ARRAY) shared across the posture cases.
func inboundPayload() map[string]any {
	return map[string]any{
		"id":   "msg-posture-inb",
		"from": map[string]any{"phone_number": "+1 (555) 111-2222", "carrier": "Verizon"},
		"to":   []any{map[string]any{"phone_number": "555 999 8888"}, map[string]any{"phone_number": "+15559999999"}},
		"text": "posture check",
	}
}

// ---- posture tests -----------------------------------------------------

// TestPayloadWebhookSuccessInboundIs202 pins the success posture: fresh
// event id + good signature -> seam fires, 202, nonce stays held (the
// point of the nonce is that the NEXT duplicate gets the 200-ack).
func TestPayloadWebhookSuccessInboundIs202(t *testing.T) {
	d := &payloadDeps{}
	n := newPayloadNonce()
	routes, sign, captured := payloadRoutes(t, d, n)
	body := realShapeMessagingBody(t, "message.received", "evt-h-inb", inboundPayload())
	rec := postPayload(t, routes, body, sign(body))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (success posture)", rec.Code)
	}
	in, fin := d.counts()
	if in != 1 || fin != 0 {
		t.Fatalf("OnInbound=%d OnFinalized=%d, want 1/0", in, fin)
	}
	if !n.isHeld("evt-h-inb") {
		t.Error("successful delivery must leave the nonce held until TTL")
	}
	// Verify ran on the RAW bytes: the captured payload equals the exact
	// request body (verify-then-parse precondition).
	if !bytes.Equal(*captured, body) {
		t.Error("Verify seam did not receive the raw body bytes")
	}
	// The seam received the NORMALIZED message (§P inside §H).
	d.mu.Lock()
	msg := d.inbound[0]
	d.mu.Unlock()
	if msg.From != "+15551112222" || msg.To != "+15559998888" {
		t.Errorf("seam got From/To = %q/%q, want normalized +15551111111/+15552222222", msg.From, msg.To)
	}
	if msg.EventID != "evt-h-inb" || msg.ProviderMessageID != "msg-posture-inb" {
		t.Errorf("seam got EventID/ProviderMessageID = %q/%q, want evt-h-inb/msg-posture-inb", msg.EventID, msg.ProviderMessageID)
	}
}

// TestPayloadWebhookSuccessFinalizedIs202 pins the DLR posture:
// message.finalized delivered -> OnFinalized + 202.
func TestPayloadWebhookSuccessFinalizedIs202(t *testing.T) {
	d := &payloadDeps{}
	routes, sign, _ := payloadRoutes(t, d, newPayloadNonce())
	payload := map[string]any{
		"id":     "msg-posture-out",
		"status": "delivered",
		"from":   map[string]any{"phone_number": "+15559999999"},
		"to":     []any{map[string]any{"phone_number": "+15553333333"}},
	}
	body := realShapeMessagingBody(t, "message.finalized", "evt-h-fin", payload)
	rec := postPayload(t, routes, body, sign(body))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	_, fin := d.counts()
	if fin != 1 {
		t.Fatalf("OnFinalized fired %d times, want 1", fin)
	}
}

// TestPayloadWebhookDuplicateEventIs200AckNoop pins the replay posture:
// the identical redelivery (same event id) is CheckFresh-rejected -> 200
// ack-noop and the seam must NOT fire a second time.
func TestPayloadWebhookDuplicateEventIs200AckNoop(t *testing.T) {
	d := &payloadDeps{}
	n := newPayloadNonce()
	routes, sign, _ := payloadRoutes(t, d, n)
	body := realShapeMessagingBody(t, "message.received", "evt-h-dup", inboundPayload())
	if rec := postPayload(t, routes, body, sign(body)); rec.Code != http.StatusAccepted {
		t.Fatalf("first delivery status = %d, want 202", rec.Code)
	}
	rec := postPayload(t, routes, body, sign(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("duplicate status = %d, want 200 ack-noop", rec.Code)
	}
	in, _ := d.counts()
	if in != 1 {
		t.Fatalf("seam fired %d times across a duplicate, want 1 (duplicate must be ack-noop)", in)
	}
}

// TestPayloadWebhookNoopEventTypeIs200 pins known-noop events
// (message.created/deleted): good signature, 200, seam never called.
func TestPayloadWebhookNoopEventTypeIs200(t *testing.T) {
	for _, et := range []string{"message.created", "message.deleted"} {
		d := &payloadDeps{}
		routes, sign, _ := payloadRoutes(t, d, newPayloadNonce())
		body := realShapeMessagingBody(t, et, "evt-h-noop-"+et, inboundPayload())
		rec := postPayload(t, routes, body, sign(body))
		if rec.Code != http.StatusOK {
			t.Errorf("%s status = %d, want 200 (known-noop ack)", et, rec.Code)
		}
		in, fin := d.counts()
		if in != 0 || fin != 0 {
			t.Errorf("%s fired seams (inbound=%d finalized=%d), want 0/0", et, in, fin)
		}
	}
}

// TestPayloadWebhookUnknownTypeIs200 pins the redelivery-storm stop: an
// unknown/misrouted event TYPE (parseable envelope, unhandled type) is a
// 200, NOT a 400 — supersedes the old TestMessagingWebhookUnknownEvent
// 400 posture (contract §1: unknown event type -> 200).
func TestPayloadWebhookUnknownTypeIs200(t *testing.T) {
	d := &payloadDeps{}
	routes, sign, _ := payloadRoutes(t, d, newPayloadNonce())
	body := realShapeMessagingBody(t, "call.answered", "evt-h-unknown", map[string]any{"call_control_id": "cc-1"})
	rec := postPayload(t, routes, body, sign(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("unknown event type status = %d, want 200 (stop redelivery)", rec.Code)
	}
	in, fin := d.counts()
	if in != 0 || fin != 0 {
		t.Fatalf("seams fired on unknown type (%d/%d), want 0/0", in, fin)
	}
}

// TestPayloadWebhookHandlerErrorIs500AndReleasesNonce pins the transient
// posture: seam error -> 500 AND the event nonce is Released so Telnyx's
// redelivery re-reaches the handler (asserted by a second delivery of the
// same event id succeeding with the seam firing again).
func TestPayloadWebhookHandlerErrorIs500AndReleasesNonce(t *testing.T) {
	d := &payloadDeps{seamErr: errPayloadFake}
	n := newPayloadNonce()
	routes, sign, _ := payloadRoutes(t, d, n)
	body := realShapeMessagingBody(t, "message.received", "evt-h-500", inboundPayload())
	rec := postPayload(t, routes, body, sign(body))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("seam error status = %d, want 500", rec.Code)
	}
	if n.isHeld("evt-h-500") {
		t.Fatal("handler error must Release the nonce (else redelivery is 200-acked as a duplicate and the event is LOST)")
	}
	// Redelivery: the same event id must reach the seam again.
	d.setSeamErr(nil)
	rec2 := postPayload(t, routes, body, sign(body))
	if rec2.Code != http.StatusAccepted {
		t.Fatalf("redelivery status = %d, want 202 (released nonce must allow reprocessing)", rec2.Code)
	}
	in, _ := d.counts()
	if in != 2 {
		t.Fatalf("seam fired %d times (fail + redelivery), want 2", in)
	}
}

// TestPayloadWebhookMalformedJSONValidSigIs400 proves the verify-then-
// parse ordering at the handler level: garbage bytes carrying a VALID
// signature pass verification (401 would mean the signature check was
// skipped) and fail at parse with 400 — never a 200/202, never a seam.
func TestPayloadWebhookMalformedJSONValidSigIs400(t *testing.T) {
	d := &payloadDeps{}
	routes, sign, captured := payloadRoutes(t, d, newPayloadNonce())
	body := []byte(`{"data": {"event_type": "message.received"  oops not json`)
	rec := postPayload(t, routes, body, sign(body))
	if rec.Code == http.StatusUnauthorized {
		t.Fatal("valid signature over malformed JSON got 401 — signature verification must pass, then parsing fails")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed JSON with valid signature = %d, want 400 parse error AFTER signature pass", rec.Code)
	}
	if !bytes.Equal(*captured, body) {
		t.Error("Verify did not run on the raw malformed bytes")
	}
	in, fin := d.counts()
	if in != 0 || fin != 0 {
		t.Fatalf("seams fired on malformed body (%d/%d), want 0/0", in, fin)
	}
}

// TestPayloadWebhookBadSignatureSkipsNonceAndSeams pins the gate order:
// a failed signature is 401 and NEVER touches the nonce store or the
// consumer seams (no attacker-controlled event ids into the replay map).
func TestPayloadWebhookBadSignatureSkipsNonceAndSeams(t *testing.T) {
	d := &payloadDeps{}
	n := newPayloadNonce()
	routes, _, _ := payloadRoutes(t, d, n)
	body := realShapeMessagingBody(t, "message.received", "evt-h-badsig", inboundPayload())
	rec := postPayload(t, routes, body, http.Header{})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no-signature status = %d, want 401", rec.Code)
	}
	checks, releases := n.snapshot()
	if len(checks) != 0 || len(releases) != 0 {
		t.Errorf("nonce touched on 401 (checks=%v releases=%v), want untouched", checks, releases)
	}
	in, fin := d.counts()
	if in != 0 || fin != 0 {
		t.Fatalf("seams fired on bad signature (%d/%d), want 0/0", in, fin)
	}
}

// TestPayloadWebhookNilNonceMeansNoDedupe pins the compat note
// (02-normalizer task 2: "NonceStore injected via deps, nil = no dedupe
// for compat"): with deps.Nonce unset, every delivery reaches the seams.
func TestPayloadWebhookNilNonceMeansNoDedupe(t *testing.T) {
	d := &payloadDeps{}
	routes, sign, _ := payloadRoutes(t, d, nil)
	body := realShapeMessagingBody(t, "message.received", "evt-h-nilnonce", inboundPayload())
	for i := range 2 {
		rec := postPayload(t, routes, body, sign(body))
		if rec.Code != http.StatusAccepted {
			t.Fatalf("delivery %d status = %d, want 202 (nil nonce = no dedupe)", i, rec.Code)
		}
	}
	in, _ := d.counts()
	if in != 2 {
		t.Fatalf("seam fired %d times with nil nonce, want 2 (both deliveries processed)", in)
	}
}
