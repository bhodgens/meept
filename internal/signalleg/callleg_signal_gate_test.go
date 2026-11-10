package signalleg

// CallLeg gate — SIGNAL arm (tree 20261110-voice-callleg, leaf 00T).
// TESTS ONLY — RED until leaf 01/02 land the contract + adapters.
//
// Why this file exists (the note leaf 00T was told to record): the voice
// package's gate file CANNOT reach this rig. internal/signalleg imports
// internal/voice (leg.go Deps), so internal/voice — including its tests —
// can never import internal/signalleg (import cycle), and newTestLeg /
// handleCallEvent / fakeEffects are package-private. The signal arms of
// TestCallLegPlacePin (RequestID idempotency), HangupIdempotentPin, and
// EventsParityAcrossLegs land HERE, asserting against the SAME exported
// voice.CallLeg / voice.CallEvent / voice.CallEventEnded symbols so enum
// parity across all three legs holds by transitivity on one constant.
//
// Adapter shape pinned for leaf 01: voice.NewSignalAdapter takes ONE
// argument and *Leg must satisfy the voice-side seam STRUCTURALLY
// (voice can never name *signalleg.Leg; the seam is a voice interface
// — today's public surface: Place(ctx, employeeID, target, firstLine,
// requestIDs...) / End(ccid) / Accept(ctx, signal.CallEvent, employeeID)).

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/signal"
	"github.com/caimlas/meept/internal/voice"
)

const signalCallLegTimeout = 3 * time.Second

// signalCallLegEvent reads one adapter CallEvent or fails with a named cause.
func signalCallLegEvent(t *testing.T, ch <-chan voice.CallEvent, who string) voice.CallEvent {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatalf("%s: adapter Events() closed before the terminal event arrived", who)
		}
		return ev
	case <-time.After(signalCallLegTimeout):
		t.Fatalf("%s: no voice.CallEvent within %v of the leg's terminal signal", who, signalCallLegTimeout)
	}
	return voice.CallEvent{}
}

// TestCallLegSignalPlacePin pins the signal adapter's Place over the
// real fake-cli rig: fully-populated voice.CallHandle with the leg's
// canonical identities (ccid "signal-...", target "signal:<uuid>",
// Leg = voice.LegSignal), and — the C-V2 arm (20261109 leaf 02
// semantics) — idempotency RIDES THE REQUEST ID: two Places with the
// same (employee, target, requestID), the second after full teardown
// like leg_gate_test.go T6, reserve the SAME effect identity
// (effectKey(employee, target, requestID)); the adapter must not
// bypass or re-key the ledger.
func TestCallLegSignalPlacePin(t *testing.T) {
	ctx := context.Background()
	leg, fx, _, _ := newTestLeg(t, func(string) bool { return true })
	sad := voice.NewSignalAdapter(leg)

	const uuid = "b3333333-3333-3333-3333-333333333333"
	req := voice.PlaceRequest{
		EmployeeID: "emp-gate-signal",
		Target:     uuid,
		Leg:        voice.LegSignal,
		RequestID:  "req-gate-signal-1",
	}
	h, err := sad.Place(ctx, req)
	if err != nil {
		t.Fatalf("signal Place: %v", err)
	}
	if h.CCID == "" || h.Leg == "" || h.EmployeeID == "" || h.Target == "" {
		t.Fatalf("signal CallHandle not fully populated: %+v", h)
	}
	if h.Leg != voice.LegSignal {
		t.Errorf("signal handle.Leg = %q, want %q", h.Leg, voice.LegSignal)
	}
	if !strings.HasPrefix(h.CCID, "signal-") {
		t.Errorf("signal handle.CCID = %q, want the signal- ccid shape", h.CCID)
	}
	if h.Target != "signal:"+uuid {
		t.Errorf("signal handle.Target = %q, want the canonical %q", h.Target, "signal:"+uuid)
	}
	if h.EmployeeID != req.EmployeeID {
		t.Errorf("signal handle.EmployeeID = %q, want %q", h.EmployeeID, req.EmployeeID)
	}

	// Teardown of call #1 (CONNECTED + ENDED), same shape as the
	// T6 precedent in leg_gate_test.go, so the retry passes capacity.
	leg.handleCallEvent(signal.CallEvent{CallID: "call-stub-1", State: signal.CallEventStateConnected})
	leg.handleCallEvent(signal.CallEvent{CallID: "call-stub-1", State: signal.CallEventStateEnded, Reason: "hangup"})

	// Retry with the SAME (employee, target, requestID): one adapter
	// call per Place, both reservations must carry one key.
	if _, err := sad.Place(ctx, req); err != nil {
		t.Fatalf("signal Place retry (same requestID): %v", err)
	}
	fx.mu.Lock()
	defer fx.mu.Unlock()
	if len(fx.reserve) != 2 {
		t.Fatalf("effect reserves = %d, want 2 (one per Place)", len(fx.reserve))
	}
	wantKey := effectKey(req.EmployeeID, "signal:"+uuid, req.RequestID)
	if fx.reserve[0] != fx.reserve[1] {
		t.Errorf("retry reserved a DIFFERENT effect key (%q vs %q) — idempotency must ride the RequestID, not the per-Place ccid",
			fx.reserve[0], fx.reserve[1])
	}
	if fx.reserve[0] != wantKey {
		t.Errorf("effect key = %q, want effectKey(employee, canonical target, requestID) = %q", fx.reserve[0], wantKey)
	}
}

// TestCallLegSignalHangupIdempotentPin pins double Hangup through the
// signal adapter as a no-op today's leg behavior, locked IN: first
// Hangup nil + call gone, second Hangup the idempotent nil (re-End of
// a signal-shaped ccid this leg already reaped), never a foreign-id
// error and never a second teardown.
func TestCallLegSignalHangupIdempotentPin(t *testing.T) {
	ctx := context.Background()
	leg, _, _, _ := newTestLeg(t, func(string) bool { return true })
	sad := voice.NewSignalAdapter(leg)

	h, err := sad.Place(ctx, voice.PlaceRequest{
		EmployeeID: "emp-gate-signal-hup",
		Target:     "c4444444-4444-4444-4444-444444444444",
		Leg:        voice.LegSignal,
		RequestID:  "req-gate-signal-hangup-1",
	})
	if err != nil {
		t.Fatalf("signal Place: %v", err)
	}
	if err := sad.Hangup(ctx, h, "gate-first"); err != nil {
		t.Errorf("first signal Hangup = %v, want nil", err)
	}
	if live := leg.Live(); len(live) != 0 {
		t.Errorf("leg holds %d live calls after Hangup, want 0", len(live))
	}
	if err := sad.Hangup(ctx, h, "gate-second"); err != nil {
		t.Errorf("second signal Hangup = %v, want nil (idempotent re-Hangup of a reaped signal ccid)", err)
	}
	if live := leg.Live(); len(live) != 0 {
		t.Errorf("leg holds %d live calls after double Hangup, want 0", len(live))
	}
}

// TestCallLegSignalEventsParity — the signal arm of
// TestCallLegEventsParityAcrossLegs. The leg's NATURAL terminal signal
// (signal-cli ENDED through the leg's own event injection point, the
// same one leg_gate_test.go drives) must surface on the adapter's
// Events() as voice.CallEvent{Kind: voice.CallEventEnded} with a
// NON-EMPTY Reason. Kind is compared to the SAME exported enum
// constant the phone/local arms in internal/voice/callleg_gate_test.go
// assert — enum equality across all three legs, no carrier-specific
// Kind, no string comparison.
func TestCallLegSignalEventsParity(t *testing.T) {
	ctx := context.Background()
	leg, _, _, _ := newTestLeg(t, func(string) bool { return true })
	sad := voice.NewSignalAdapter(leg)

	h, err := sad.Place(ctx, voice.PlaceRequest{
		EmployeeID: "emp-gate-signal-events",
		Target:     "d5555555-5555-5555-5555-555555555555",
		Leg:        voice.LegSignal,
		RequestID:  "req-gate-signal-events-1",
	})
	if err != nil {
		t.Fatalf("signal Place: %v", err)
	}
	_ = h

	events := sad.Events()
	// CONNECTED then the carrier's ENDED(hangup) — today's happy-path
	// terminal sequence on this rig.
	leg.handleCallEvent(signal.CallEvent{CallID: "call-stub-1", State: signal.CallEventStateConnected})
	leg.handleCallEvent(signal.CallEvent{CallID: "call-stub-1", State: signal.CallEventStateEnded, Reason: "hangup"})

	ev := signalCallLegEvent(t, events, "signal")
	if ev.Kind != voice.CallEventEnded {
		t.Errorf("signal terminal Kind = %v, want voice.CallEventEnded (no carrier-specific Kind)", ev.Kind)
	}
	if ev.Reason == "" {
		t.Error("signal terminal CallEvent.Reason is empty; ended events must carry a reason")
	}
}
