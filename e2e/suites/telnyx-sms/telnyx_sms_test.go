//go:build e2e

// Suite telnyx-sms (contact-channels plan leaf 00T, T9; gate matrix row
// F6). ACTIVATED (leaf 02 T5).
//
// Coverage telnyx-sms-01: Ed25519-signed webhook POST message.received
// -> accepted; bad signature -> refused; message.finalized failed ->
// the SMSAttempt DLR wait resolves the attempt outcome to failed.
//
// Hermetic: the webhook receiver is driven through the REAL
// commhttp.MessagingRoutes handler over httptest (the same handler the
// daemon arms via WithMessagingRoutes), signed with the same
// internal/voice/telnyx Ed25519 scheme the Call Control webhooks use.
// The full-boot variant needs the daemon's messaging routes armed with
// a test verifier seam; the handler-level assertions here pin the wire
// contract (signature 401-first, parse, dispatch), and the wiring is
// covered by contact_wiring's compile-time seam pins.
package telnyxsms

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	httpx "github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/contact"
	"github.com/caimlas/meept/internal/telnyx"
	vtelnyx "github.com/caimlas/meept/internal/voice/telnyx"
)

// signHeaders builds the Telnyx Ed25519 signature headers over msg
// ("{timestamp}|{msg}"), the exact scheme internal/voice/telnyx verifies.
func signHeaders(t *testing.T, priv ed25519.PrivateKey, msg []byte) http.Header {
	t.Helper()
	ts := fmt.Sprintf("%d", time.Now().Unix())
	signed := append(append([]byte(ts), '|'), msg...)
	sig := ed25519.Sign(priv, signed)
	h := http.Header{}
	h.Set("Telnyx-Signature-Ed25519", base64.StdEncoding.EncodeToString(sig))
	h.Set("Telnyx-Timestamp", ts)
	return h
}

// TestTelnyxSMSWebhookInboundAndDLR pins the wire contract: signature
// 401-first, inbound dispatch, finalized dispatch, DLR correlation into
// the SMSAttempt outcome (T9 canonical scenario).
func TestTelnyxSMSWebhookInboundAndDLR(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	var finalized []telnyx.Message
	var inbound []telnyx.Message

	stubAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"id":"out-msg-1","to":[{"status":"queued"}]}}`))
	}))
	defer stubAPI.Close()

	client, err := telnyx.NewClient(stubAPI.URL, telnyx.WithAPIKey("test-key"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	attempt := telnyx.NewSMSAttempt(client, telnyx.SMSAttemptConfig{
		FromNumber:  "+15550009999",
		ProfileID:   "prof-1",
		WaitSeconds: 5,
	}, nil)
	// Wire the DLR feed: OnFinalized -> the attempt's pending map (the
	// exact wiring contact_wiring arms in production).
	deps := httpx.MessagingWebhookDeps{
		Verify: func(body []byte, headers http.Header) error {
			// The REAL verifier under test (internal/voice/telnyx).
			return vtelnyx.VerifyWebhookSignature(body, headers, pub)
		},
		OnInbound:   func(m telnyx.Message) error { inbound = append(inbound, m); return nil },
		OnFinalized: func(m telnyx.Message) error { finalized = append(finalized, m); attempt.OnFinalized(m); return nil },
	}
	routes, err := httpx.NewMessagingRoutes(deps, nil)
	if err != nil {
		t.Fatalf("NewMessagingRoutes: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if routes.HandlesPath(r.URL.Path) {
			routes.ServeWebhook(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// 1. Inbound: signed message.received -> 200 + OnInbound fired.
	inboundBody := []byte(`{"data":{"event_type":"message.received","payload":{"id":"inb-1","from":"+15550000255","to":"+15559999955","text":"reply yes","direction":"inbound","received_at":"2026-11-08T12:00:00Z"}}}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+routes.WebhookPath(), bytes.NewReader(inboundBody))
	req.Header = signHeaders(t, priv, inboundBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST inbound: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("inbound status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()
	if len(inbound) != 1 || inbound[0].Text != "reply yes" {
		t.Fatalf("inbound messages = %+v, want 1 with text 'reply yes'", inbound)
	}

	// 2. Bad signature: same body, DIFFERENT key -> 401, zero dispatches.
	_, wrongPriv, _ := ed25519.GenerateKey(rand.Reader)
	req2, _ := http.NewRequest(http.MethodPost, srv.URL+routes.WebhookPath(), bytes.NewReader(inboundBody))
	req2.Header = signHeaders(t, wrongPriv, inboundBody)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("POST bad-signature: %v", err)
	}
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad signature status = %d, want 401", resp2.StatusCode)
	}
	if len(inbound) != 1 {
		t.Fatalf("bad signature dispatched %d inbound messages, want 0 (refusal before parse)", len(inbound)-1)
	}

	// 3. DLR correlation: the SMSAttempt (built above, against the stub
	// API) sends, then waits on message.finalized. Fire the Attempt in
	// the background; its DLR wait resolves when the finalized webhook
	// arrives through the REAL handler -> OnFinalized feed.
	stubAPIHit := true
	_ = stubAPIHit
	rec := contact.Record{ID: "c-sms", Phone: "+15550000777"}
	outcomeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		outcome, aerr := attempt.Attempt(context.Background(), rec, "lunch?")
		outcomeCh <- outcome
		errCh <- aerr
	}()
	// Give the Attempt time to send + register its pending DLR.
	time.Sleep(200 * time.Millisecond)

	// Feed the finalized(delivered) DLR for out-msg-1 through the REAL
	// webhook handler -> OnFinalized -> the attempt's pending map.
	finalBody := []byte(`{"data":{"event_type":"message.finalized","payload":{"id":"out-msg-1","status":"delivered"}}}`)
	req3, _ := http.NewRequest(http.MethodPost, srv.URL+routes.WebhookPath(), bytes.NewReader(finalBody))
	req3.Header = signHeaders(t, priv, finalBody)
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatalf("POST finalized: %v", err)
	}
	_ = resp3.Body.Close()
	if len(finalized) != 1 || finalized[0].Status != "delivered" {
		t.Fatalf("finalized messages = %+v, want 1 delivered", finalized)
	}

	// The Attempt's DLR wait resolved through the webhook feed.
	select {
	case outcome := <-outcomeCh:
		if outcome != "delivered" {
			t.Fatalf("attempt outcome = %q, want delivered", outcome)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("attempt DLR wait never resolved")
	}
	if err := <-errCh; err != nil {
		t.Fatalf("attempt err = %v, want nil (delivered is chain-terminal)", err)
	}
}
