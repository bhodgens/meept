//go:build e2e

// Suite telnyx-sms (contact-channels plan leaf 00T, T9; gate matrix row
// F6). RED until leaf 12 lands: skip-guarded with the named RED reason
// so the tree build/vet/e2e-fast stays green while the suite remains
// registered.
//
// Coverage:
//
//	telnyx-sms-01  F6  an Ed25519-signed webhook POST carrying
//	                message.received reaches the employee session as an
//	                inbound row (signature verified against the injected
//	                public key - the EXISTING telnyx-signature verifier,
//	                never a second one; config seam mirrors how Call
//	                Control webhooks verify in tests today).
//	telnyx-sms-02  F6  a message.finalized DLR with status=failed marks
//	                the matching chain attempt outcome failed (DLR
//	                correlation feeds the chain's failure detection).
//	telnyx-sms-03  F6  SendSMS through a stub messaging API parses data.id
//	                and refuses non-E.164 input client-side.
//
// Hermetic: the webhook receiver is the daemon's own HTTP listener on
// loopback; the Telnyx API is a stub httptest server.
package telnyxsms

import (
	"testing"

	"github.com/caimlas/meept/e2e/harness"
)

// redReason is the named skip reason: this suite activates when leaf 12
// (telnyx sms) lands.
const redReason = "RED: telnyx sms implementation not landed (leaf 12)"

// TestTelnyxSMSWebhookInboundAndDLR is the T9 canonical scenario:
// signature-verified inbound -> employee session row; finalized failed ->
// chain attempt outcome failed. Skip-guarded (T9 RED state).
func TestTelnyxSMSWebhookInboundAndDLR(t *testing.T) {
	t.Skip(redReason)

	// --- assertions pinned for GREEN (do not weaken when activating) ---
	//
	// pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	// s := harness.Start(t,
	//     harness.WithConfigOverlay(map[string]any{
	//         "telnyx.messaging_enabled": true,       // leaf 12 knob
	//         "telnyx.messaging_profile": "test-profile",
	//         "telnyx.webhook_pubkey":    base64 of pub, // config seam (C5)
	//     }),
	//     harness.WithExtraEnv(map[string]string{
	//         // stub Telnyx API base for SendSMS (data.id parse, E.164 gate)
	//         "TELNYX_API_BASE": stubAPI.URL,
	//     }),
	// )
	//
	// // 1. Inbound: POST message.received, Ed25519-signed with the
	// //    telnyx-signature scheme (ts|body signing input, same verifier as
	// //    Call Control webhooks). Assert 200 + the employee session gains
	// //    an inbound SMS row (from E.164, text, direction inbound).
	// body := `{"data":{"event_type":"message.received","payload":{"id":"inb-1",
	//          "from":"+15550000025","to":"+15559999995","text":"reply yes",
	//          "received_at":"2026-11-08T12:00:00Z"}}}`
	// post := signWebhook(t, priv, []byte(body))
	// resp := s.PostWebhook(t, "/webhooks/telnyx-messaging", post.headers, post.body)
	// if resp.Code != 200 { t.Fatalf(...) }
	// if !s.SessionHasInboundSMS(t, "+15550000025", "reply yes") { t.Fatalf(...) }
	//
	// // 2. Bad signature: same body, wrong key -> refused (no session row).
	// //
	// // 3. DLR: POST message.finalized status=failed for the outbound id;
	// //    the matching chain attempt outcome becomes "failed" (F6
	// //    correlation), and a delivered DLR marks it delivered.

	_ = harness.Start // keep the harness import honest in the guarded body
}
