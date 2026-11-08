//go:build e2e

// Suite contact-chain (contact-channels plan leaf 00T, T7; gate matrix
// rows F1, F2, F8). RED until the implementation leaves land: the whole
// body is skip-guarded with the named RED reason so `go build ./...`,
// `go vet ./...`, and `make e2e-fast-area AREA=contact-chain` stay green
// while the suite remains registered (pre-implementation posture, same
// as the tool-surface suite's parked pattern).
//
// Coverage (assertions live in the leaf's per-scenario table):
//
//	contact-chain-01  F1+F2  contact record seeded; FAKE channel attempts
//	                        registered; the employee tool invocation walks
//	                        voice(no-answer) -> signal_voice(busy) ->
//	                        signal_msg(delivered) and the chain STOPS.
//	contact-chain-02  F2    every effect/audit row of the run shares ONE
//	                        chain execution id; no second notification per
//	                        channel (dedupe).
//
// Hermetic: no network beyond loopback, no live Telnyx, no real
// signal-cli - channel attempts are fake seam registrations against the
// daemon wiring (the leaf-02 seam), and the employee turn is a scripted
// FakeLLM invocation of the "contact" tool.
package contactchain

import (
	"testing"

	"github.com/caimlas/meept/e2e/harness"
)

// redReason is the named skip reason asserted by the orchestrator: this
// suite activates when leaf 01/02 (contact foundation + wiring) land.
const redReason = "RED: contact-chain implementation not landed (leaf 01/02)"

// TestContactChainOrderedFallback walks the full chain through a live
// scratch daemon: seeded contact record, fake channel attempts, one
// employee tool action, ordered per-channel outcomes with a shared chain
// execution id. Skip-guarded until the seams exist (T7 RED state).
func TestContactChainOrderedFallback(t *testing.T) {
	t.Skip(redReason)

	// --- assertions pinned for GREEN (do not weaken when activating) ---
	//
	// s := harness.Start(t,
	//     harness.WithConfigOverlay(map[string]any{
	//         "voice.enabled": true,
	//         "signal.enabled": true,      // leaf 10 knob
	//         "contact.enabled": true,     // leaf 01/02 knob
	//     }),
	// )
	//
	// // Seed the contact record through the daemon (RPC/CLI surface the
	// // leaf lands): phone E.164-normalized at write, policy = the ordered
	// // chain under test (C1).
	// s.CreateContact(t, contact.Record{
	//     DisplayName: "ada",
	//     Phone:       "+15550000024",
	//     Policy: []contact.Channel{
	//         contact.ChannelTelnyxVoice, contact.ChannelSignalVoice,
	//         contact.ChannelSignalMsg,
	//     },
	// })
	//
	// // Register FAKE channel attempts (no network): voice -> no_answer,
	// // signal_voice -> busy, signal_msg -> delivered (C2 fake seam).
	// s.RegisterFakeChannel(t, "telnyx_voice", "no_answer")
	// s.RegisterFakeChannel(t, "signal_voice", "busy")
	// s.RegisterFakeChannel(t, "signal_msg", "delivered")
	//
	// // One employee tool invocation walks the chain (F8, C6).
	// sessionID := s.CreateSession(t, "contact-chain", s.ProjectDir)
	// s.Fake.EnqueueToolCalls(harness.ToolCall{
	//     Name: "contact",
	//     Arguments: `{"person": "+15550000024", "message": "lunch at noon?"}`,
	// })
	// s.ChatTurn(t, sessionID, "Reach ada with 'lunch at noon?'", 2*time.Minute)
	//
	// // Assert: exactly 3 attempts in policy order; channel 3 delivered;
	// // ONE chain execution id across every effect/audit row (F2 dedupe);
	// // no second notification per channel (F2 no-double-notification).
	// report := s.LastContactReport(t)
	// if len(report.Attempts) != 3 { t.Fatalf(...) }
	// if report.Attempts[2].Outcome != "delivered" { t.Fatalf(...) }
	// ids := s.ChainEffectAuditIDs(t, report.ChainID)
	// if len(ids) != 3 { t.Fatalf("want per-attempt rows all keyed by one chain id") }
	// if s.NotificationCount(t, report.ChainID, "signal_msg") != 1 { t.Fatalf(...) }

	_ = harness.Start // keep the harness import honest in the guarded body
}
