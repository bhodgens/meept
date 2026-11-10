package daemon

// Voice gate (plan 20261109-statemachine-remediation, leaf 05-voice/00T,
// test 8): TestHoldDoesNotBlockWebhook — RED pin for the finding that
// holdPhoneCall runs SYNCHRONOUSLY inside the Manager's inbound spawn
// seam (voice_wiring.go, spawn closure -> holdPhoneCall ->
// AcquireCallHold -> mu.Lock). With the per-employee goal mutex already
// held (a prior live call or a scheduled assess round —
// voice_employee.go AcquireCallHold "blocks while any prior call ... is
// live for this employee"), a Telnyx webhook for a NEW inbound call
// stalls inside HandleCallEvent for the whole duration of the prior
// hold: the HTTP handler thread is pinned to unbounded mutex wait time.
//
// Target contract: HandleCallEvent (the webhook path) returns within a
// bounded time while the hold pends — the goal-hold + call-row bind is
// decoupled from the webhook's synchronous response — AND the hold still
// gates the call's first turn (the serialization semantics are preserved;
// only the delivery is made asynchronous).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/tools/builtin"
	"github.com/caimlas/meept/internal/voice/telnyx"
)

// newWebhookHoldWiring boots the real WireVoice stack with the PHONE leg
// fully armed for INBOUND: the fake Telnyx answers every call-control
// action (AnswerCall inside HandleCallEvent must succeed for the spawn
// seam to run), and the child binary is the connect-stub so
// child.EnsureRunning's handshake passes in-process (the same stub the
// local-leg brief test uses — the spawn seam requires EnsureRunning OK
// before the registry Acquire + holdPhoneCall under test).
func newWebhookHoldWiring(t *testing.T) *VoiceWiring {
	t.Helper()

	telnyxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/calls" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"call_control_id": "cc-webhook-out"},
			})
			return
		}
		// answer / hangup / speak etc.: 200 with an empty envelope.
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{}})
	}))
	t.Cleanup(telnyxSrv.Close)
	t.Setenv("MEEPT_TELNYX_TEST_BASE_URL", telnyxSrv.URL)
	t.Setenv("MEEPT_HOME", shortMeeptHomeForTest(t))

	sessions := session.NewMemoryStore(testLogger(t))
	deps := WireVoiceDeps{
		Config: &config.Config{Voice: config.VoiceConfig{
			Enabled:           true,
			TelnyxPublicKey:   validWiringTestPubKey(t),
			DefaultEmployee:   "phone-webhook-emp",
			AllowedNumbers:    []string{"+155****0002"},
			PublicStreamURL:   "wss://phone-webhook.test/media",
			ConnectionID:      "conn-phone-webhook",
			FromNumber:        "+155****0001",
			TimeoutSecs:       3600, // the no-answer timer must not fire mid-test
			ChildBinaryPath:   buildConnectChildStub(t),
			Filler:            []string{"one moment"},
			SupervisorEnabled: true,
		}},
		Bus:      nil,
		Sessions: sessions,
		Loops:    agent.NewManager(agent.ManagerConfig{SessionStore: sessions}),
		Template: agent.NewAgentLoop("template-phone-webhook-test", t.TempDir()),
		Effects:  effects.NewMemoryLedger(),
		Broker:   testSecretsBroker(t),
		Logger:   testLogger(t),
	}
	w, err := WireVoice(context.Background(), deps)
	if err != nil {
		t.Fatalf("WireVoice: %v", err)
	}
	if w == nil {
		t.Fatal("WireVoice returned nil wiring")
	}
	t.Cleanup(func() { _ = w.Employee.Close() })

	// The text server must LISTEN before the spawn's child dials it
	// (handshake probe): same Start posture as TestWireVoiceLocalLegBrief.
	startCtx, stopStart := context.WithCancel(context.Background())
	t.Cleanup(stopStart)
	if sErr := w.Start(startCtx, func(dial builtin.DialerFunc, agentID builtin.AgentIDFunc) {}); sErr != nil {
		t.Fatalf("Start: %v", sErr)
	}
	return w
}

// TestHoldDoesNotBlockWebhook pins the webhook-decoupling target.
//
// Given: the per-employee goal mutex is HELD by a prior call
// (AcquireCallHold("cc-prior", emp) — no release).
// When: a Telnyx inbound webhook drives Manager.HandleCallEvent
// ("call.initiated", incoming) for the SAME employee.
// Then (target): HandleCallEvent returns within a bounded time even
// though the hold pends — the goal bind rides an async path — and the
// hold still gates the call's first turn (serialization preserved; this
// half is asserted GREEN-side by the implementation leaf, e.g. the
// calls.db row only reaches 'live' after the prior hold releases).
//
// RED today: HandleCallEvent blocks indefinitely — the spawn closure
// calls holdPhoneCall synchronously and AcquireCallHold's mu.Lock() waits
// for the prior hold, so the webhook handler thread is pinned. The test
// proves the stall with a bounded wait, releases the prior hold in
// cleanup, and FAILs when the webhook did not return in the window.
func TestHoldDoesNotBlockWebhook(t *testing.T) {
	w := newWebhookHoldWiring(t)

	// 1. Occupy the per-employee goal mutex exactly the production path
	// does (prior live call for phone-webhook-emp).
	priorRelease := w.Employee.AcquireCallHold("cc-prior-holder", "phone-webhook-emp")
	t.Cleanup(priorRelease) // unblock the stalled handler after the verdict

	// 2. Drive the webhook path: an inbound call.initiated for the same
	// employee must NOT be pinned to the held mutex.
	done := make(chan error, 1)
	go func() {
		done <- w.Manager.HandleCallEvent(context.Background(), telnyx.CallEvent{
			EventType:     "call.initiated",
			Direction:     "incoming",
			CallControlID: "cc-webhook-in",
			From:          "+155****0002",
			To:            "+155****0001",
		})
	}()

	const bound = 2 * time.Second
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("HandleCallEvent errored: %v", err)
		}
		// Bounded return: today unreachable while the mutex is held. If
		// an implementation ever returns here, the webhook half passes —
		// the "hold still gates first turn" half stays the impl leaf's
		// GREEN assertion (see doc comment).
	case <-time.After(bound):
		t.Errorf("HandleCallEvent did not return within %v while the employee goal mutex is held — "+
			"holdPhoneCall runs synchronously inside the spawn seam (voice_wiring.go:294-299); "+
			"the webhook must answer within a bound and gate the first turn asynchronously", bound)
	}
}
