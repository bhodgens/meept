package signalleg

// Leaf-02 event-unification tests — SIGNAL arm (tree 20261110-voice-callleg).
// NON-gate file; the frozen callleg_signal_gate_test.go stays zero-diff.
//
// Pins (against the exported voice enum, same import-cycle posture as
// the gate: this rig lives here because voice cannot import signalleg):
//   - carrier RECONNECTING -> voice.CallEventReconnecting on the
//     adapter's Events() bus (non-blocking, best-effort lane);
//   - carrier CONNECTED is NOT bus-emitted (documented per-leg
//     omission — the frozen gate TestCallLegSignalEventsParity reads
//     Ended as the FIRST event after an injected CONNECTED; the
//     RECONNECTING arm below proves the first-event position is free
//     for it, pinning the skip as deliberate);
//   - the terminal still lands after a mid-call Reconnecting.

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/signal"
	"github.com/caimlas/meept/internal/voice"
)

func TestCallLegSignalReconnectingMaps(t *testing.T) {
	ctx := context.Background()
	leg, _, _, _ := newTestLeg(t, func(string) bool { return true })
	sad := voice.NewSignalAdapter(leg)

	h, err := sad.Place(ctx, voice.PlaceRequest{
		EmployeeID: "emp-ev2-signal",
		Target:     "e5555555-5555-5555-5555-555555555555",
		Leg:        voice.LegSignal,
		RequestID:  "req-ev2-signal-1",
	})
	if err != nil {
		t.Fatalf("signal Place: %v", err)
	}
	_ = h
	events := sad.Events()

	// CONNECTED then RECONNECTING: the Connected mapping is received on
	// the leg's lane but skipped at the bus write (frozen-gate posture
	// documented in voice/callleg_signal.go), so the FIRST event must
	// be the Reconnecting — its Kind proves both the skip and the map.
	leg.handleCallEvent(signal.CallEvent{CallID: "call-stub-1", State: signal.CallEventStateConnected})
	leg.handleCallEvent(signal.CallEvent{CallID: "call-stub-1", State: signal.CallEventStateReconnecting})

	ev := signalCallLegEvent(t, events, "signal-reconnecting")
	if ev.Kind != voice.CallEventReconnecting {
		t.Fatalf("first event Kind = %v, want voice.CallEventReconnecting "+
			"(a Connected here means the frozen-gate skip regressed)", ev.Kind)
	}
	if ev.Reason == "" {
		t.Error("Reconnecting CallEvent.Reason is empty")
	}

	// The tolerated reconnect leaves the call live: the terminal still
	// fans out on the same bus afterwards.
	leg.handleCallEvent(signal.CallEvent{CallID: "call-stub-1", State: signal.CallEventStateEnded, Reason: "hangup"})
	ev = signalCallLegEvent(t, events, "signal-ended")
	if ev.Kind != voice.CallEventEnded {
		t.Errorf("terminal after reconnect Kind = %v, want voice.CallEventEnded", ev.Kind)
	}
	if ev.Reason == "" {
		t.Error("terminal CallEvent.Reason is empty")
	}
}
