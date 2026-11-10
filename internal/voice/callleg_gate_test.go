package voice

// CallLeg gate (tree 20261110-voice-callleg, leaf 00T). TESTS ONLY — RED
// until leaf 01/02 land callleg.go + the three adapters. Pins contract
// C-B1 (../20261110-voice-callleg-design.md): one CallLeg interface
// (Place/Accept/Hangup/Events), adapters named EXACTLY phoneAdapter /
// localAdapter / signalAdapter (those names ARE the contract), CallHandle
// {CCID, Leg, EmployeeID, Target}, CallEvent {Kind, Reason} with the
// kinds CallEventRinging / CallEventConnected / CallEventReconnecting /
// CallEventEnded / CallEventErrored, and the sentinel ErrNoInbound for
// legs with no inbound concept (local).
//
// Constructor names pinned for leaf 01 (not in DESIGN.md verbatim; the
// gate file is the source):
//   NewPhoneAdapter(d *Dialer, m *Manager) CallLeg
//   NewLocalAdapter(l *LocalLeg) CallLeg
//   NewSignalAdapter(seam) CallLeg — the seam param must be a voice-side
//   interface that *signalleg.Leg satisfies STRUCTURALLY (signalleg
//   imports voice, so voice can never import signalleg; the signal arm's
//   rig lives in internal/signalleg/callleg_signal_gate_test.go).
//
// PlaceRequest fields pinned: EmployeeID, Target, Leg, RequestID,
// FreshSession, DayBrief (per the design's "req carries" list).
//
// Events() channel ownership (who closes it, buffering) is leaf 01's
// call — 00T pins only the VALUE shape (gate doc note).

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voice/telnyx"
)

// callLegTimeout bounds every Events() read so a missing emit fails with
// a named cause instead of hanging the suite.
const callLegTimeout = 3 * time.Second

// callLegNextEvent reads one CallEvent or fails with the leg's name.
func callLegNextEvent(t *testing.T, ch <-chan CallEvent, who string) CallEvent {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatalf("%s: Events() closed before the terminal event arrived", who)
		}
		return ev
	case <-time.After(callLegTimeout):
		t.Fatalf("%s: no CallEvent within %v of the leg's terminal signal", who, callLegTimeout)
	}
	return CallEvent{}
}

// TestCallLegAdaptersExist pins C-B1 shape: the three named adapters
// implement CallLeg, the event enum has five distinct kinds, and
// ErrNoInbound is a real error sentinel. Compile-RED on undefined
// CallLeg/phoneAdapter/localAdapter/signalAdapter/CallEvent*Kind/ErrNoInbound.
func TestCallLegAdaptersExist(t *testing.T) {
	var _ CallLeg = (*phoneAdapter)(nil)
	var _ CallLeg = (*localAdapter)(nil)
	var _ CallLeg = (*signalAdapter)(nil)

	// Five distinct kinds (enum equality, not strings).
	if CallEventRinging == CallEventConnected ||
		CallEventRinging == CallEventReconnecting ||
		CallEventRinging == CallEventEnded ||
		CallEventRinging == CallEventErrored ||
		CallEventConnected == CallEventReconnecting ||
		CallEventConnected == CallEventEnded ||
		CallEventConnected == CallEventErrored ||
		CallEventReconnecting == CallEventEnded ||
		CallEventReconnecting == CallEventErrored ||
		CallEventEnded == CallEventErrored {
		t.Error("CallEvent kinds are not pairwise distinct")
	}

	// The no-inbound sentinel exists and is an error value.
	if ErrNoInbound == nil || ErrNoInbound.Error() == "" {
		t.Error("ErrNoInbound must be a named non-empty error sentinel")
	}
}

// TestCallLegPlacePin pins Place over the phone and local fake rigs:
// every CallHandle field is populated from the leg's own canonical
// identity, the phone leg's RequestID idempotency rides through the
// adapter untouched (retry of the same request: same ccid, zero extra
// POSTs — dialer semantics preserved), and local Accept returns
// ErrNoInbound (the leg has no inbound concept; the sentinel is the
// named contract leaf 01 defines). The signal Place/idempotency arm is
// internal/signalleg/callleg_signal_gate_test.go (import-cycle rule).
func TestCallLegPlacePin(t *testing.T) {
	ctx := context.Background()

	t.Run("phone", func(t *testing.T) {
		tel := &countingController{}
		reg := NewRegistry(4)
		mgr := NewManager(ManagerConfig{StreamURL: "wss://tunnel.example/media"}, tel, noopSpawn)
		mgr.SetReleaseHook(func(ccid string, _ Outcome) { reg.Release(ccid) })
		d := NewDialer(dialConfig(), tel, mgr, reg, newTestLedger(t), nil, testLogger())

		pad := NewPhoneAdapter(d, mgr)
		req := PlaceRequest{
			EmployeeID: "emp-gate-phone",
			Target:     "+15551230001",
			Leg:        LegPhone,
			RequestID:  "req-gate-place-1",
		}
		h, err := pad.Place(ctx, req)
		if err != nil {
			t.Fatalf("phone Place: %v", err)
		}
		if h.CCID == "" || h.Leg == "" || h.EmployeeID == "" || h.Target == "" {
			t.Fatalf("phone CallHandle not fully populated: %+v", h)
		}
		if h.Leg != LegPhone {
			t.Errorf("phone handle.Leg = %q, want %q", h.Leg, LegPhone)
		}
		if h.EmployeeID != req.EmployeeID {
			t.Errorf("phone handle.EmployeeID = %q, want %q", h.EmployeeID, req.EmployeeID)
		}
		// Target rides the dialer's canonical (normalized) form — the
		// same value the shared registry holds for the live instance.
		var wantTarget string
		for _, inst := range reg.Live() {
			if inst.CallControlID == h.CCID {
				wantTarget = inst.Target
			}
		}
		if wantTarget == "" {
			t.Fatalf("registry holds no live instance for placed ccid %q", h.CCID)
		}
		if h.Target != wantTarget {
			t.Errorf("phone handle.Target = %q, want the registry's canonical %q", h.Target, wantTarget)
		}
		if !strings.HasPrefix(h.CCID, "cc-") {
			t.Errorf("phone handle.CCID = %q, want the dialer's ccid shape", h.CCID)
		}

		// Idempotency passthrough: same RequestID must return the
		// reserved ccid without a second POST (dialer C9 semantics —
		// the adapter must not bypass the ledger).
		h2, err := pad.Place(ctx, req)
		if err != nil {
			t.Fatalf("phone Place retry: %v", err)
		}
		if h2.CCID != h.CCID {
			t.Errorf("retry ccid = %q, want reserved %q (same RequestID never rings twice)", h2.CCID, h.CCID)
		}
		if got := tel.count(); got != 1 {
			t.Errorf("DialOut count after retry = %d, want 1", got)
		}

		// Cleanup: no-answer timer would fire mid-suite otherwise.
		if err := pad.Hangup(ctx, h, "gate-cleanup"); err != nil {
			t.Errorf("phone cleanup Hangup: %v", err)
		}
	})

	t.Run("local", func(t *testing.T) {
		f := newLegFixture(t)
		lad := NewLocalAdapter(f.leg)
		h, err := lad.Place(ctx, PlaceRequest{
			EmployeeID: "emp-gate-local",
			Target:     "legbox", // local: Target is the label
			Leg:        LegLocal,
			RequestID:  "req-gate-local-1",
		})
		if err != nil {
			t.Fatalf("local Place: %v", err)
		}
		if h.CCID == "" || h.Leg == "" || h.EmployeeID == "" || h.Target == "" {
			t.Fatalf("local CallHandle not fully populated: %+v", h)
		}
		if h.Leg != LegLocal {
			t.Errorf("local handle.Leg = %q, want %q", h.Leg, LegLocal)
		}
		if !strings.HasPrefix(h.CCID, "local-") {
			t.Errorf("local handle.CCID = %q, want the local- ccid shape", h.CCID)
		}
		if h.Target != "local:legbox" {
			t.Errorf("local handle.Target = %q, want the canonical %q", h.Target, "local:legbox")
		}

		// Local has NO inbound concept: Accept is the named refusal
		// ErrNoInbound, never a silent success.
		if _, err := lad.Accept(ctx, h.CCID); !errors.Is(err, ErrNoInbound) {
			t.Errorf("local Accept error = %v, want ErrNoInbound", err)
		}

		if err := lad.Hangup(ctx, h, "gate-cleanup"); err != nil {
			t.Errorf("local cleanup Hangup: %v", err)
		}
	})
}

// TestCallLegHangupIdempotentPin locks today's already-idempotent
// per-leg teardown IN behind the adapter: double Hangup on each handle
// returns nil both times and the release fan-out fires exactly once
// (no double registry release, no double hook). Signal arm:
// internal/signalleg/callleg_signal_gate_test.go.
func TestCallLegHangupIdempotentPin(t *testing.T) {
	ctx := context.Background()

	t.Run("phone", func(t *testing.T) {
		tel := &countingController{}
		reg := NewRegistry(4)
		releases := 0
		mgr := NewManager(ManagerConfig{StreamURL: "wss://tunnel.example/media"}, tel, noopSpawn)
		mgr.SetReleaseHook(func(ccid string, _ Outcome) {
			releases++
			reg.Release(ccid)
		})
		d := NewDialer(dialConfig(), tel, mgr, reg, newTestLedger(t), nil, testLogger())

		pad := NewPhoneAdapter(d, mgr)
		h, err := pad.Place(ctx, PlaceRequest{
			EmployeeID: "emp-gate-hup", Target: "+15551230001",
			Leg: LegPhone, RequestID: "req-gate-hangup-1",
		})
		if err != nil {
			t.Fatalf("phone Place: %v", err)
		}
		if err := pad.Hangup(ctx, h, "gate-first"); err != nil {
			t.Errorf("first phone Hangup = %v, want nil", err)
		}
		if err := pad.Hangup(ctx, h, "gate-second"); err != nil {
			t.Errorf("second phone Hangup = %v, want nil (idempotent)", err)
		}
		if releases != 1 {
			t.Errorf("release fan-out fired %d times, want exactly 1", releases)
		}
		if live := len(reg.Live()); live != 0 {
			t.Errorf("registry holds %d live calls after Hangup, want 0", live)
		}
	})

	t.Run("local", func(t *testing.T) {
		f := newLegFixture(t)
		lad := NewLocalAdapter(f.leg)
		h, err := lad.Place(ctx, PlaceRequest{
			EmployeeID: "emp-gate-hup", Target: "hupbox",
			Leg: LegLocal, RequestID: "req-gate-local-hangup-1",
		})
		if err != nil {
			t.Fatalf("local Place: %v", err)
		}
		if err := lad.Hangup(ctx, h, "gate-first"); err != nil {
			t.Errorf("first local Hangup = %v, want nil", err)
		}
		if err := lad.Hangup(ctx, h, "gate-second"); err != nil {
			t.Errorf("second local Hangup = %v, want nil (idempotent)", err)
		}
		if got := len(f.reg.releasedSnap()); got != 1 {
			t.Errorf("local registry releases = %d, want exactly 1 (double Hangup must not double-release)", got)
		}
	})
}

// TestCallLegEventsParityAcrossLegs pins C-B1 event parity: each leg's
// NATURAL terminal signal (phone: telnyx call.hangup webhook through the
// manager; local: LocalLeg.End; signal-cli ENDED arm lives in
// internal/signalleg/callleg_signal_gate_test.go and asserts against the
// SAME voice.CallEventEnded constant) yields a CallEvent whose Kind is
// the shared ended enum value with a non-empty Reason. Asserted as ENUM
// EQUALITY across the phone and local arms — no carrier-specific Kind
// may leak (Kind is an enum; free text belongs in Reason).
func TestCallLegEventsParityAcrossLegs(t *testing.T) {
	ctx := context.Background()

	// --- phone arm: call.hangup webhook ---
	tel := &countingController{}
	reg := NewRegistry(4)
	mgr := NewManager(ManagerConfig{StreamURL: "wss://tunnel.example/media"}, tel, noopSpawn)
	mgr.SetReleaseHook(func(ccid string, _ Outcome) { reg.Release(ccid) })
	d := NewDialer(dialConfig(), tel, mgr, reg, newTestLedger(t), nil, testLogger())
	pad := NewPhoneAdapter(d, mgr)
	ph, err := pad.Place(ctx, PlaceRequest{
		EmployeeID: "emp-gate-parity", Target: "+15551230001",
		Leg: LegPhone, RequestID: "req-gate-events-1",
	})
	if err != nil {
		t.Fatalf("phone Place: %v", err)
	}
	phoneEvents := pad.Events()
	// Fire the terminal signal on its own goroutine: if a leaf-01
	// adapter emits synchronously on an unbuffered Events() channel,
	// the read below still proceeds (channel ownership stays leaf 01's).
	hookErr := make(chan error, 1)
	go func() {
		hookErr <- mgr.HandleCallEvent(ctx, telnyx.CallEvent{
			EventType: "call.hangup", CallControlID: ph.CCID, Direction: "outgoing",
		})
	}()
	if err := <-hookErr; err != nil {
		t.Fatalf("phone call.hangup: %v", err)
	}
	phoneEv := callLegNextEvent(t, phoneEvents, "phone")
	if phoneEv.Kind != CallEventEnded {
		t.Errorf("phone terminal Kind = %v, want CallEventEnded (no carrier-specific Kind)", phoneEv.Kind)
	}
	if phoneEv.Reason == "" {
		t.Error("phone terminal CallEvent.Reason is empty; ended events must carry a reason")
	}

	// --- local arm: LocalLeg.End ---
	f := newLegFixture(t)
	lad := NewLocalAdapter(f.leg)
	lh, err := lad.Place(ctx, PlaceRequest{
		EmployeeID: "emp-gate-parity", Target: "paritybox",
		Leg: LegLocal, RequestID: "req-gate-local-events-1",
	})
	if err != nil {
		t.Fatalf("local Place: %v", err)
	}
	localEvents := lad.Events()
	endErr := make(chan error, 1)
	go func() {
		endErr <- f.leg.End(lh.CCID, "gate-parity")
	}()
	if err := <-endErr; err != nil {
		t.Fatalf("local End: %v", err)
	}
	localEv := callLegNextEvent(t, localEvents, "local")
	if localEv.Kind != CallEventEnded {
		t.Errorf("local terminal Kind = %v, want CallEventEnded", localEv.Kind)
	}
	if localEv.Reason == "" {
		t.Error("local terminal CallEvent.Reason is empty; ended events must carry a reason")
	}

	// --- parity assertion (THE gate): the same enum value on both arms.
	// The signal arm in internal/signalleg asserts against CallEventEnded
	// too; equality across all three legs follows by transitivity on the
	// single exported constant — no leg may define its own kind.
	if phoneEv.Kind != localEv.Kind {
		t.Errorf("event parity broken: phone Kind %v != local Kind %v", phoneEv.Kind, localEv.Kind)
	}
}
