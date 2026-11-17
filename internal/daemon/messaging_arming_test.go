package daemon

// 03-inbound-store/00T §T3/§T4 — messaging hook arming gate (test-only
// file). Closes the dead-seam gap (master D6): OnInbound/OnFinalized on
// the comm/http messaging webhook are currently never assigned
// (voice_wiring.go sets Verify only). This file is the executable spec
// for leaf 03/02 (arming glue in internal/daemon — voice_wiring.go,
// which this gate does NOT touch).
//
// PINNED FUTURE SHAPES (compile-RED until 03/02 + branch 01 land; names
// frozen by master.md §1/§6/§8, docs/plans/20261110-telnyx-texting/
// master.md):
//
//	// internal/daemon — arming glue, sibling of contact_wiring.go:
//	type messagingArmingDeps struct {
//	    Store    *sms.Store                 // §4 store (nil => inbound disabled)
//	    Nonce    *voicetelnyx.NonceStore    // §1 (internal/voice/telnyx/signature.go)
//	    Bus      *bus.MessageBus            // §5 topic fan-out
//	    Attempts *telnyx.SMSAttempt         // internal/telnyx; DLR pending-map owner
//	    Logger   *slog.Logger
//	}
//	func buildMessagingDeps(d messagingArmingDeps) commhttp.MessagingWebhookDeps
//	//   OnInbound:   store.AppendEvent (dup event id -> ack-ok) +
//	//                bus.PublishT(sms.InboundTopic) + reply classifier (§7,
//	//                inserted INSIDE this closure later by leaf 04/01 — this
//	//                gate is agnostic to it).
//	//   OnFinalized: store.UpsertState (monotonic §4) +
//	//                bus.PublishT(sms.StateTopic) + DLR resolve (below).
//	//   nil Store (inbound_enabled=false, §8/D3) -> BOTH callbacks nil: the
//	//   handler degrades to parse-and-ack — byte-identical to the nil-deps
//	//   route (§T4 golden).
//	//   NOTE on the Nonce import identity: §1 defines NonceStore in
//	//   internal/voice/telnyx (signature.go); §6 writes it unqualified as
//	//   "telnyx.NonceStore" — in voice_wiring.go the telnyx import IS
//	//   internal/voice/telnyx, so the pin is voicetelnyx.NonceStore here.
//
//	// internal/comm/http (branch 01): MessagingWebhookDeps gains
//	//   Nonce *voicetelnyx.NonceStore  (§1 handler posture: dup event-id ->
//	//   200 ack-noop; sig fail -> 401; downstream error -> 500 after
//	//   Release; success -> 202). v1 code acks 200 with no Nonce field, so
//	//   every gate assertion here accepts ANY 2xx on a first delivery and
//	//   pins EXACTLY 200 on the redelivered duplicate — forward-compatible
//	//   with both the current and the §1 handler.
//
//	// internal/telnyx (03/02, contract §6 "attempts.Resolve"): the DLR
//	// pending map gains an exported resolve method, pinned expected shape:
//	//   func (s *SMSAttempt) ResolvePending(providerMessageID string, status string)
//	//	// called by the OnFinalized glue; feeds the existing
//	//	// pendingDLR.ResolveFinalized (sms_attempt.go:87). The D6 proof is
//	//	// driven END-TO-END through the armed webhook seam (a blocked
//	//	// Attempt returns "delivered", not timeout-unavailable) and via
//	//	// att.Outcome(id) — so the seam itself is what is pinned, and the
//	//	// method name follows whichever form 03/02 lands as long as the
//	//	// observable contract (Attempt outcome + Outcome slot + store row +
//	//	// StateTopic event) holds. SMSAttempt.OnFinalized(Message) already
//	//	// exists and is the current feed; the glue may call it directly.
//
//	// internal/config (§8): MessagingConfig gains
//	//   InboundEnabled bool `toml:"inbound_enabled"` (default false)
//
//	// WireVoiceDeps (§6): gains
//	//   MessagingStore  *sms.Store
//	//   MessagingNonces *voicetelnyx.NonceStore
//
// Canned webhook bodies carry BOTH data.id (event id) and payload.id
// (provider message id) as DISTINCT values, pinning the glue's field
// mapping (Event.EventID <- event id, State.ProviderMessageID <-
// payload.id) unambiguously. from/to are plain strings: the current
// telnyx.ParseWebhookEvent reads them, and the §2 tolerant extractors
// accept the plain-string form too. Test numbers are built with
// armingPhone() (digit concatenation) so no PII-redaction layer can
// rewrite them into an E.164-invalid form mid-gate.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite" // driver registration for the raw-state reader (house convention)

	"github.com/caimlas/meept/internal/bus"
	commhttp "github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/contact"
	"github.com/caimlas/meept/internal/sms"
	"github.com/caimlas/meept/internal/telnyx"
	voicetelnyx "github.com/caimlas/meept/internal/voice/telnyx"
)

// armingPhone builds a valid E.164 test number "+1555000NNNN" from a
// 4-digit suffix via concatenation (redaction-proof).
func armingPhone(suffix int) string {
	return "+" + strings.Join([]string{"1", "555", "000", strconv.Itoa(suffix)}, "")
}

// armingVerify is the gate's signature seam: production Ed25519 signing is
// branch 01's gate's subject; here Verify only needs a signed/unsigned
// discriminator so the handler's 401-before-parse path stays drivable.
func armingVerify() func([]byte, http.Header) error {
	return func(_ []byte, h http.Header) error {
		if h.Get("X-Test-Signed") != "yes" {
			return errors.New("arming gate: unsigned request")
		}
		return nil
	}
}

// cannedMessagingBody builds a Telnyx messaging webhook envelope with a
// distinct data.id (event id) and payload.id (provider message id).
func cannedMessagingBody(eventType, eventID, payloadID, from, to, text, status string) []byte {
	return []byte(`{"data":{"id":"` + eventID + `","event_type":"` + eventType +
		`","payload":{"id":"` + payloadID + `","from":"` + from + `","to":"` + to +
		`","text":"` + text + `","status":"` + status +
		`","received_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}}}`)
}

// postMessaging drives the exported ServeWebhook seam — the exact handler
// the Server middleware auth-exempt branch runs in production (messaging.go
// exposes it for direct callers precisely because daemon-package tests
// cannot reach the unexported middleware).
func postMessaging(t *testing.T, routes *commhttp.MessagingRoutes, body []byte, signed bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, routes.WebhookPath(), bytes.NewReader(body))
	if signed {
		req.Header.Set("X-Test-Signed", "yes")
	}
	rec := httptest.NewRecorder()
	routes.ServeWebhook(rec, req)
	return rec
}

// openArmingStore opens the real §4 store at a temp path.
func openArmingStore(t *testing.T) (*sms.Store, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "sms-arming.db")
	st, err := sms.Open(dbPath, nil)
	if err != nil {
		t.Fatalf("sms.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, dbPath
}

// armingRoutes wires a comm/http route around buildMessagingDeps with the
// gate's Verify discriminator + fresh nonce store.
func armingRoutes(t *testing.T, d commhttp.MessagingWebhookDeps) *commhttp.MessagingRoutes {
	t.Helper()
	d.Verify = armingVerify()
	d.Nonce = voicetelnyx.NewNonceStore(6*time.Minute, 10000)
	routes, err := commhttp.NewMessagingRoutes(d, testLogger(t))
	if err != nil {
		t.Fatalf("NewMessagingRoutes: %v", err)
	}
	return routes
}

// is2xx accepts any successful ack (v1: 200; §1 posture: 202 on success).
func is2xx(code int) bool { return code >= 200 && code < 300 }

// TestBuildMessagingDepsNilStore pins the §6/D3 disabled posture shape:
// zero Store -> BOTH consumer callbacks nil (deps unset entirely; the
// handler parse-and-acks). Nonce/Bus/Attempts present but unused.
func TestBuildMessagingDepsNilStore(t *testing.T) {
	d := buildMessagingDeps(messagingArmingDeps{
		Store:  nil,
		Nonce:  voicetelnyx.NewNonceStore(6*time.Minute, 10000),
		Bus:    newArmingBus(t),
		Logger: testLogger(t),
	})
	if d.OnInbound != nil {
		t.Fatal("disabled (nil Store): OnInbound must be nil — deps unset entirely (D3)")
	}
	if d.OnFinalized != nil {
		t.Fatal("disabled (nil Store): OnFinalized must be nil — deps unset entirely (D3)")
	}
}

// TestDisabledRouteByteIdenticalGolden is the §T4 golden: a route armed
// from disabled deps (buildMessagingDeps with zero Store) answers a canned
// signed inbound webhook with the EXACT status code, body, and
// Content-Type of a route built with consumer seams explicitly nil (the
// branch-01 parse-and-ack behavior), and publishes NOTHING on the bus.
func TestDisabledRouteByteIdenticalGolden(t *testing.T) {
	body := cannedMessagingBody("message.received", "evt-off-1", "msg-off-1",
		armingPhone(111), armingPhone(900), "hello", "")

	// Reference: consumer seams explicitly nil (parse-and-ack). Built
	// through the SAME armingRoutes dep shape (Verify + Nonce) so the
	// only delta under test is the consumer wiring — if branch 01's
	// handler posture changes (202-on-success), it changes for BOTH
	// routes identically and the golden stays a golden.
	refRoutes := armingRoutes(t, commhttp.MessagingWebhookDeps{})

	// Disabled-arming: deps from buildMessagingDeps with zero Store
	// (must yield nil callbacks per the pin asserted below).
	d := buildMessagingDeps(messagingArmingDeps{Logger: testLogger(t)})
	if d.OnInbound != nil || d.OnFinalized != nil {
		t.Fatal("precondition: zero-Store buildMessagingDeps must yield nil callbacks")
	}
	b := newArmingBus(t)
	inbound := make(chan sms.InboundMessage, 4)
	sub := bus.SubscribeT(b, "disabled-golden", sms.InboundTopic, func(p sms.InboundMessage) { inbound <- p })
	defer b.Unsubscribe(sub)

	armedDisabled := armingRoutes(t, commhttp.MessagingWebhookDeps{
		OnInbound:   d.OnInbound,
		OnFinalized: d.OnFinalized,
	})
	ref := postMessaging(t, refRoutes, body, true)
	got := postMessaging(t, armedDisabled, body, true)

	if !is2xx(got.Code) || !is2xx(ref.Code) {
		t.Fatalf("disabled webhook: got %d ref %d; want 2xx ack-noop on both", got.Code, ref.Code)
	}
	if got.Code != ref.Code || got.Body.String() != ref.Body.String() ||
		got.Header().Get("Content-Type") != ref.Header().Get("Content-Type") {
		t.Fatalf("disabled-arming response not byte-identical to nil-deps golden: "+
			"got %d/%q/%q; ref %d/%q/%q (D3)",
			got.Code, got.Body.String(), got.Header().Get("Content-Type"),
			ref.Code, ref.Body.String(), ref.Header().Get("Content-Type"))
	}
	select {
	case p := <-inbound:
		t.Fatalf("disabled posture published on InboundTopic: %+v (must publish NOTHING)", p)
	case <-time.After(300 * time.Millisecond):
		// expected: silence
	}
	// No store writes either: the route had no Store at all (nil-deps),
	// asserted structurally by the nil-callback pin above.
}

// TestArmedUnsignedRequestRejected pins that arming does not weaken the
// auth gate: the armed route still 401s an unsigned body BEFORE parse,
// with zero store rows and zero bus events.
func TestArmedUnsignedRequestRejected(t *testing.T) {
	st, _ := openArmingStore(t)
	b := newArmingBus(t)
	d := buildMessagingDeps(messagingArmingDeps{
		Store:  st,
		Nonce:  voicetelnyx.NewNonceStore(6*time.Minute, 10000),
		Bus:    b,
		Logger: testLogger(t),
	})
	if d.OnInbound == nil || d.OnFinalized == nil {
		t.Fatal("armed (non-nil Store): both callbacks must be set (§6)")
	}
	routes := armingRoutes(t, commhttp.MessagingWebhookDeps{
		OnInbound:   d.OnInbound,
		OnFinalized: d.OnFinalized,
	})

	inbound := make(chan sms.InboundMessage, 4)
	sub := bus.SubscribeT(b, "unsigned-gate", sms.InboundTopic, func(p sms.InboundMessage) { inbound <- p })
	defer b.Unsubscribe(sub)

	from := armingPhone(150)
	body := cannedMessagingBody("message.received", "evt-unsigned-1", "msg-unsigned-1",
		from, armingPhone(900), "should never be acted on", "")
	rec := postMessaging(t, routes, body, false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned armed webhook status = %d; want 401 before parse", rec.Code)
	}
	select {
	case p := <-inbound:
		t.Fatalf("unsigned request drove the bus: %+v", p)
	case <-time.After(300 * time.Millisecond):
	}
	rows, err := st.RecentEvents(context.Background(), from, time.Time{}, 10)
	if err != nil {
		t.Fatalf("RecentEvents: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("unsigned request wrote %d store rows; want 0", len(rows))
	}
}

// TestArmedInboundWebhookStoresAndPublishes is the §T3 inbound leg: a
// signed canned message.received drives the armed route -> one sms_events
// row (EventID == data.id, Src == from, Direction inbound) AND one
// sms.InboundTopic bus event with the mapped payload.
func TestArmedInboundWebhookStoresAndPublishes(t *testing.T) {
	st, _ := openArmingStore(t)
	b := newArmingBus(t)
	d := buildMessagingDeps(messagingArmingDeps{
		Store:  st,
		Nonce:  voicetelnyx.NewNonceStore(6*time.Minute, 10000),
		Bus:    b,
		Logger: testLogger(t),
	})
	routes := armingRoutes(t, commhttp.MessagingWebhookDeps{
		OnInbound:   d.OnInbound,
		OnFinalized: d.OnFinalized,
	})

	inbound := make(chan sms.InboundMessage, 4)
	sub := bus.SubscribeT(b, "inbound-arming", sms.InboundTopic, func(p sms.InboundMessage) { inbound <- p })
	defer b.Unsubscribe(sub)

	from, to := armingPhone(201), armingPhone(901)
	body := cannedMessagingBody("message.received", "evt-in-1", "msg-in-1",
		from, to, "are you free", "")
	rec := postMessaging(t, routes, body, true)
	if !is2xx(rec.Code) {
		t.Fatalf("inbound webhook status = %d; want 2xx ack", rec.Code)
	}

	select {
	case p := <-inbound:
		if p.EventID != "evt-in-1" || p.From != from || p.To != to || p.Text != "are you free" {
			t.Fatalf("InboundTopic payload = %+v; want evt-in-1/%s/%s/%s", p, from, to, "are you free")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("InboundTopic fired nothing for an armed message.received")
	}

	rows, err := st.RecentEvents(context.Background(), from, time.Time{}, 10)
	if err != nil {
		t.Fatalf("RecentEvents: %v", err)
	}
	if len(rows) != 1 || rows[0].EventID != "evt-in-1" || rows[0].Direction != "inbound" {
		t.Fatalf("store rows after inbound = %+v; want exactly one evt-in-1 inbound row", rows)
	}
}

// TestArmedFinalizedResolvesDLR is the D6 proof: a blocked SMSAttempt
// waiting in its bounded DLR poll, fed through the ARMED webhook route
// (signed message.finalized delivered), returns "delivered" quickly — not
// the timeout-"unavailable" the never-armed seam guarantees today. Pins
// store state row + StateTopic fan-out + raw finalized event row on the
// same leg.
func TestArmedFinalizedResolvesDLR(t *testing.T) {
	st, dbPath := openArmingStore(t)
	b := newArmingBus(t)

	// Stub send endpoint: every POST /v2/messages returns id msg-dlr-1.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"msg-dlr-1"}}`))
	}))
	t.Cleanup(srv.Close)
	client, err := telnyx.NewClient(srv.URL, telnyx.WithAPIKey("gate-token"))
	if err != nil {
		t.Fatalf("telnyx.NewClient: %v", err)
	}
	att := telnyx.NewSMSAttempt(client, telnyx.SMSAttemptConfig{
		FromNumber:  armingPhone(902),
		WaitSeconds: 5,
	}, testLogger(t))

	d := buildMessagingDeps(messagingArmingDeps{
		Store:    st,
		Nonce:    voicetelnyx.NewNonceStore(6*time.Minute, 10000),
		Bus:      b,
		Attempts: att,
		Logger:   testLogger(t),
	})
	routes := armingRoutes(t, commhttp.MessagingWebhookDeps{
		OnInbound:   d.OnInbound,
		OnFinalized: d.OnFinalized,
	})

	states := make(chan sms.StateChanged, 4)
	sub := bus.SubscribeT(b, "dlr-arming", sms.StateTopic, func(p sms.StateChanged) { states <- p })
	defer b.Unsubscribe(sub)

	// Block in Attempt (send returns the id promptly; registers pending DLR).
	type res struct {
		outcome string
		err     error
	}
	done := make(chan res, 1)
	go func() {
		o, aerr := att.Attempt(context.Background(),
			contact.Record{ID: "rec-dlr", DisplayName: "DLR Target", Phone: armingPhone(301)},
			"ping me")
		done <- res{o, aerr}
	}()

	// Wait until the pending slot exists (send returned the id), THEN
	// drive the finalized webhook — race-safe ordering.
	deadline := time.Now().Add(4 * time.Second)
	for {
		if _, ok := att.Outcome("msg-dlr-1"); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("attempt never registered the pending DLR (send leg broken?)")
		}
		time.Sleep(10 * time.Millisecond)
	}

	body := cannedMessagingBody("message.finalized", "evt-dlr-1", "msg-dlr-1",
		armingPhone(902), armingPhone(301), "", "delivered")
	rec := postMessaging(t, routes, body, true)
	if !is2xx(rec.Code) {
		t.Fatalf("finalized webhook status = %d; want 2xx ack", rec.Code)
	}

	// D6: the blocked attempt resolves delivered fast, never timeout.
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("Attempt err = %v; want nil (DLR resolved via armed webhook, D6)", r.err)
		}
		if r.outcome != contact.OutcomeDelivered {
			t.Fatalf("Attempt outcome = %q; want %q (D6 proof)", r.outcome, contact.OutcomeDelivered)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("Attempt did not return after the armed finalized webhook (dead seam still open)")
	}
	if out, ok := att.Outcome("msg-dlr-1"); !ok || !out.Resolved || out.Status != "delivered" {
		t.Fatalf("Outcome slot after armed DLR = %+v ok=%v; want resolved delivered", out, ok)
	}

	// StateTopic fan-out.
	select {
	case p := <-states:
		if p.ProviderMessageID != "msg-dlr-1" || p.Status != "delivered" {
			t.Fatalf("StateTopic payload = %+v; want msg-dlr-1/delivered", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StateTopic fired nothing for an armed message.finalized")
	}

	// Store legs: state row (via §4 upsert) + raw finalized event row.
	if status, eventID := armingRawState(t, dbPath, "msg-dlr-1"); status != "delivered" || eventID != "evt-dlr-1" {
		t.Fatalf("sms_message_state row = %q/%q; want delivered/evt-dlr-1", status, eventID)
	}
	rows, err := st.RecentEvents(context.Background(), armingPhone(902), time.Time{}, 10)
	if err != nil {
		t.Fatalf("RecentEvents: %v", err)
	}
	if len(rows) != 1 || rows[0].EventID != "evt-dlr-1" || rows[0].Direction != "outbound" {
		t.Fatalf("raw event rows = %+v; want exactly one evt-dlr-1 outbound row", rows)
	}
}

// TestArmedRedeliverySingleRow pins the webhook redelivery path (§T3):
// the SAME signed body twice -> first is a 2xx ack, the duplicate is
// EXACTLY 200 ack-noop (§1 dup posture — via Nonce dup check at the
// handler and/or AppendEvent insert-ignore in the glue), the store holds
// ONE row, and the bus saw ONE inbound event.
func TestArmedRedeliverySingleRow(t *testing.T) {
	st, _ := openArmingStore(t)
	b := newArmingBus(t)
	d := buildMessagingDeps(messagingArmingDeps{
		Store:  st,
		Nonce:  voicetelnyx.NewNonceStore(6*time.Minute, 10000),
		Bus:    b,
		Logger: testLogger(t),
	})
	routes := armingRoutes(t, commhttp.MessagingWebhookDeps{
		OnInbound:   d.OnInbound,
		OnFinalized: d.OnFinalized,
	})

	inbound := make(chan sms.InboundMessage, 8)
	sub := bus.SubscribeT(b, "redel-arming", sms.InboundTopic, func(p sms.InboundMessage) { inbound <- p })
	defer b.Unsubscribe(sub)

	from := armingPhone(202)
	body := cannedMessagingBody("message.received", "evt-redel-1", "msg-redel-1",
		from, armingPhone(902), "again and again", "")
	first := postMessaging(t, routes, body, true)
	if !is2xx(first.Code) {
		t.Fatalf("first delivery status = %d; want 2xx", first.Code)
	}
	// Await the first fan-out before redelivering (bus delivery is async
	// from the handler's synchronous publish).
	select {
	case <-inbound:
	case <-time.After(2 * time.Second):
		t.Fatal("first delivery never reached the InboundTopic subscriber")
	}
	second := postMessaging(t, routes, body, true)
	if second.Code != http.StatusOK {
		t.Fatalf("duplicate delivery status = %d; want exactly 200 ack-noop (§1 dup posture)", second.Code)
	}

	rows, err := st.RecentEvents(context.Background(), from, time.Time{}, 10)
	if err != nil {
		t.Fatalf("RecentEvents: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("store rows after redelivery = %d; want 1", len(rows))
	}
	select {
	case p := <-inbound:
		t.Fatalf("redelivery fanned a second InboundTopic event: %+v (want exactly 1 total)", p)
	case <-time.After(300 * time.Millisecond):
		// expected: exactly one bus event, silence after the dup
	}
}

// TestWireVoiceDepsArmingFields pins the §6/§8 shape additions:
// WireVoiceDeps carries the store + nonce handles the wiring threads to
// buildMessagingDeps, and MessagingConfig carries the §8 inbound gate key
// defaulting to false (D3). Compile-time field references: RED until
// 03/02 (+ §8 in 02/01's schema write) land.
func TestWireVoiceDepsArmingFields(t *testing.T) {
	st, _ := openArmingStore(t)
	ns := voicetelnyx.NewNonceStore(6*time.Minute, 10000)
	deps := WireVoiceDeps{
		MessagingStore:  st,
		MessagingNonces: ns,
	}
	if deps.MessagingStore != st {
		t.Fatal("WireVoiceDeps.MessagingStore roundtrip broken")
	}
	if deps.MessagingNonces != ns {
		t.Fatal("WireVoiceDeps.MessagingNonces roundtrip broken")
	}
	var cfg config.MessagingConfig
	if cfg.InboundEnabled {
		t.Fatal("MessagingConfig.InboundEnabled must default false (D3)")
	}
	cfg.InboundEnabled = true
	if !cfg.InboundEnabled {
		t.Fatal("MessagingConfig.InboundEnabled field set/get broken")
	}
}

// --- local helpers -------------------------------------------------------

func newArmingBus(t *testing.T) *bus.MessageBus {
	t.Helper()
	b := bus.New(nil, testLogger(t))
	t.Cleanup(b.Close)
	return b
}

// armingRawState reads sms_message_state directly (same §4 column pin as
// internal/sms/store_test.go's rawState helper, local copy to keep gate
// files independent).
func armingRawState(t *testing.T, dbPath, providerMsgID string) (status, eventID string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	defer db.Close()
	var updatedAt string
	err = db.QueryRow(
		"SELECT status, updated_at, event_id FROM sms_message_state WHERE provider_message_id = ?",
		providerMsgID,
	).Scan(&status, &updatedAt, &eventID)
	if err != nil {
		t.Fatalf("read state row for %q: %v", providerMsgID, err)
	}
	return status, eventID
}
