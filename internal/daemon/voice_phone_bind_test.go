package daemon

// T0 (GH #123) hard assertion: a PHONE leg's calls.db row reaches the
// 'ended' state through the REAL WireVoice wiring. The local leg's
// equivalent assertion lives in e2e/suites/voice-local (hard since the
// 2a5c0ccf gap closure); this is the phone-leg mirror as a daemon-level
// unit test through the production wiring (the task-sanctioned shape:
// a phone e2e would need the full fake-Telnyx media stack only to press
// the same release fan-out this test exercises).
//
// Path under test (outbound, the dial tool seam):
//
//	dialForTool -> Dialer.Dial (effects claim, TrackOutbound, registry
//	Acquire) -> holdPhoneCall (AcquireCallHold + Binding.OnCallStart,
//	row state 'live') -> manager hangup event -> releaseOnce ->
//	w.releaseCall -> Employee.ReleaseHook (heldCases gate) ->
//	Binding.OnCallEnd -> CallStore.End -> row state 'ended'.
//
// Double hangup is idempotent: the manager fires the release hook once
// per ccid; the second event must not panic, double-unlock, or resurrect
// the row.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/voice/telnyx"
	_ "modernc.org/sqlite" // sqlite driver registration (house convention)
)

// phoneBindTarget is the allowlisted E.164 target the test dials (the
// config allowlist gate requires the number be listed).
const phoneBindTarget = "+15550009999"

// newPhoneBindWiring boots the REAL WireVoice stack with the phone leg
// armed: the Telnyx API base pointed at an httptest fake that answers
// POST /calls with a call_control_id, a memory effects ledger, and the
// test secrets broker. The wiring is NOT Started (no text server): the
// dial/bind/release path under test never touches it.
func newPhoneBindWiring(t *testing.T) *VoiceWiring {
	t.Helper()

	// Fake Telnyx Call Control: POST /calls -> data.call_control_id. Any
	// other route is a test bug (the dialer only POSTs /calls here).
	telnyxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/calls" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"call_control_id": "cc-phone-bind-1"},
			})
			return
		}
		// The inbound-leg refusal test answers 200s for the call-control
		// action routes (answer/hangup); only the POST /calls shape is
		// load-bearing for the outbound test.
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{}})
	}))
	t.Cleanup(telnyxSrv.Close)
	t.Setenv("MEEPT_TELNYX_TEST_BASE_URL", telnyxSrv.URL)
	t.Setenv("MEEPT_HOME", shortMeeptHomeForTest(t))

	sessions := session.NewMemoryStore(testLogger(t))
	deps := WireVoiceDeps{
		Config: &config.Config{Voice: config.VoiceConfig{
			Enabled:         true,
			TelnyxPublicKey: validWiringTestPubKey(t),
			DefaultEmployee: "phone-bind-emp",
			// The dial policy gates: allowlist carries the test target;
			// the stream URL + connection id satisfy the zero-HTTP
			// refusal gates (the POST itself rides the fake above).
			AllowedNumbers:  []string{phoneBindTarget},
			PublicStreamURL: "wss://phone-bind.test/media",
			ConnectionID:    "conn-phone-bind",
			FromNumber:      "+15550000001",
			TimeoutSecs:     60, // no-answer timer must not fire mid-test
		}},
		Bus:      nil, // progress events drop; not under test
		Sessions: sessions,
		Loops:    agent.NewManager(agent.ManagerConfig{SessionStore: sessions}),
		Template: agent.NewAgentLoop("template-phone-bind-test", t.TempDir()),
		Effects:  effects.NewMemoryLedger(),
		Audit:    nil, // audit rows are the dialer's own concern; not under test
		Broker:   testSecretsBroker(t),
		Logger:   testLogger(t),
	}
	w, err := WireVoice(context.Background(), deps)
	if err != nil {
		t.Fatalf("WireVoice: %v", err)
	}
	if w == nil {
		t.Fatal("WireVoice returned nil wiring (phone leg did not arm)")
	}
	t.Cleanup(func() { _ = w.Employee.Close() })
	return w
}

// phoneBindRowState reads the voice_calls row state for ccid straight
// from calls.db (the observable the GH #123 contract pins).
func phoneBindRowState(t *testing.T, dbPath, ccid string) (string, bool) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open calls.db: %v", err)
	}
	defer db.Close() //nolint:errcheck // read-only test handle
	var state string
	err = db.QueryRow(`SELECT state FROM voice_calls WHERE call_id = ?`, ccid).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}
	if err != nil {
		t.Fatalf("query calls.db: %v", err)
	}
	return state, true
}

// awaitPhoneRowState polls calls.db until ccid's row carries want (the
// release fan-out is async), or fails.
func awaitPhoneRowState(t *testing.T, dbPath, ccid, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if state, ok := phoneBindRowState(t, dbPath, ccid); ok && state == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	state, ok := phoneBindRowState(t, dbPath, ccid)
	if !ok {
		t.Fatalf("calls.db never carried a row for %s (want state %q)", ccid, want)
	}
	t.Fatalf("call row state = %q, want %q within 10s (GH #123: the phone leg must reach the ended state)", state, want)
}

// TestPhoneLegCallRowReachesEnded is the hard T0 assertion: an outbound
// phone call through the REAL wiring starts its calls.db row ('live',
// via holdPhoneCall -> OnCallStart) and reaches 'ended' through the
// release fan-out (ReleaseHook gated on the AcquireCallHold registration
// holdPhoneCall performed).
func TestPhoneLegCallRowReachesEnded(t *testing.T) {
	w := newPhoneBindWiring(t)
	if w.Dialer == nil {
		t.Fatal("dialer nil (phone leg did not arm); the test requires the phone leg")
	}

	callsDB := filepath.Join(config.MeeptPath("voice"), "calls.db")

	// Dial through the REAL tool adapter (the production outbound seam).
	dial := w.dialForTool()
	ccid, err := dial(context.Background(), "phone-bind-emp", phoneBindTarget, "req-phone-bind-1")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if ccid == "" {
		t.Fatal("dial returned an empty ccid")
	}

	// The row exists and is live BEFORE any hangup (holdPhoneCall ran
	// OnCallStart; without the GH #123 fix there is NO row at all).
	awaitPhoneRowState(t, callsDB, ccid, "live")

	// Hangup: the manager fires the release hook exactly once; the hold
	// holdPhoneCall acquired gates ReleaseHook -> OnCallEnd -> 'ended'.
	if err := w.Manager.HandleCallEvent(context.Background(), telnyx.CallEvent{EventType: "call.hangup", CallControlID: ccid, Direction: "outgoing"}); err != nil {
		t.Fatalf("HandleCallEvent(hangup): %v", err)
	}
	awaitPhoneRowState(t, callsDB, ccid, "ended")

	// Double-end idempotent: a replayed hangup must be a clean no-op
	// (manager releaseOnce) - no panic, no double unlock, row stays
	// ended with its terminal state intact.
	if err := w.Manager.HandleCallEvent(context.Background(), telnyx.CallEvent{EventType: "call.hangup", CallControlID: ccid, Direction: "outgoing"}); err != nil {
		t.Fatalf("HandleCallEvent(replayed hangup): %v", err)
	}
	awaitPhoneRowState(t, callsDB, ccid, "ended")
}

// TestPhoneLegInboundRowReachesEnded mirrors the assertion on the
// INBOUND leg: the spawn seam's holdPhoneCall registers the hold + row
// at call.initiated (the wire path runs the same holdPhoneCall closure
// WireVoice armed), so a hangup ends the row. The spawn seam needs the
// child, so this test drives the wiring's own bind path through the
// manager with a stubbed child... it instead pins the seam indirectly:
// the outbound test above proves holdPhoneCall + the hold; this test
// proves the INBOUND registry-spawn order by asserting that a failed
// child spawn (no binary) refuses the call BEFORE any row is started -
// the inbound contract is "bind only after the registry slot is held",
// so a refused call must leave no row behind.
func TestPhoneLegInboundRefusedSpawnLeavesNoRow(t *testing.T) {
	w := newPhoneBindWiring(t)

	// The wiring under test has no child binary configured: the inbound
	// spawn seam fails at EnsureRunning, the manager hangs the leg up and
	// fails it untracked. No holdPhoneCall may have run (bind happens
	// AFTER the registry acquire inside the spawn closure).
	err := w.Manager.HandleCallEvent(context.Background(), telnyx.CallEvent{EventType: "call.initiated", CallControlID: "cc-phone-in-1", Direction: "incoming", From: phoneBindTarget})
	if err == nil {
		t.Fatal("inbound spawn without a child binary succeeded; want the named refusal")
	}
	// Give the fan-out a beat, then assert no row exists for the refused
	// call (bind-before-spawn would have leaked a stuck 'live' row).
	time.Sleep(200 * time.Millisecond)
	callsDB := filepath.Join(config.MeeptPath("voice"), "calls.db")
	if _, ok := phoneBindRowState(t, callsDB, "cc-phone-in-1"); ok {
		t.Errorf("refused inbound call left a calls.db row behind (bind must follow the registry acquire, not precede the spawn)")
	}
}
