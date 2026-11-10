package voice

// Leaf-02 event-unification tests (tree 20261110-voice-callleg). NON-gate
// file: the frozen gate callleg_gate_test.go stays zero-diff. These pins
// cover the NON-terminal kinds leaf 02 mapped onto the shared enum, plus
// the per-leg omissions the contract documents (see callleg.go's Events
// scope block):
//
//	phone: call.answered -> Connected (additive answered lane).
//	Signal CONNECTED -> Connected is NOT bus-emitted (frozen 00T gate
//	TestCallLegSignalEventsParity reads Ended as the FIRST signal-bus
//	event after an injected CONNECTED; the mapping rides the lane, the
//	adapter skips the bus write). The signalleg-side rig for both the
//	Reconnecting emit and the Connected skip lives in
//	internal/signalleg/callleg_events2_gateless_test.go (import cycle).
//	local: client-join edge (splice's first mic byte) -> Connected.
//	Bus guard: a non-terminal emit after the ccid's terminal latched is
//	dropped (terminal always wins the ordering).

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voice/telnyx"
)

// TestCallLegPhoneAnsweredMapsConnected pins the phone arm: a
// call.answered webhook on a ccid the adapter placed yields
// CallEventConnected (Kind enum equality, non-empty Reason) as the
// FIRST bus event, ahead of the later terminal.
func TestCallLegPhoneAnsweredMapsConnected(t *testing.T) {
	ctx := context.Background()
	tel := &countingController{}
	reg := NewRegistry(4)
	mgr := NewManager(ManagerConfig{StreamURL: "wss://tunnel.example/media"}, tel, noopSpawn)
	mgr.SetReleaseHook(func(ccid string, _ Outcome) { reg.Release(ccid) })
	d := NewDialer(dialConfig(), tel, mgr, reg, newTestLedger(t), nil, testLogger())
	pad := NewPhoneAdapter(d, mgr)

	h, err := pad.Place(ctx, PlaceRequest{
		EmployeeID: "emp-ev2-phone", Target: "+15551230001",
		Leg: LegPhone, RequestID: "req-ev2-phone-1",
	})
	if err != nil {
		t.Fatalf("phone Place: %v", err)
	}
	events := pad.Events()

	if err := mgr.HandleCallEvent(ctx, telnyx.CallEvent{
		EventType: "call.answered", CallControlID: h.CCID, Direction: "outgoing",
	}); err != nil {
		t.Fatalf("call.answered: %v", err)
	}
	ev := callLegNextEvent(t, events, "phone-connected")
	if ev.Kind != CallEventConnected {
		t.Errorf("answered Kind = %v, want CallEventConnected", ev.Kind)
	}
	if ev.Reason == "" {
		t.Error("Connected CallEvent.Reason is empty")
	}

	// The terminal still lands after the transition.
	if err := mgr.HandleCallEvent(ctx, telnyx.CallEvent{
		EventType: "call.hangup", CallControlID: h.CCID, Direction: "outgoing",
	}); err != nil {
		t.Fatalf("call.hangup: %v", err)
	}
	ev = callLegNextEvent(t, events, "phone-ended")
	if ev.Kind != CallEventEnded {
		t.Errorf("post-answer terminal Kind = %v, want CallEventEnded", ev.Kind)
	}
}

// TestCallLegLocalJoinMapsConnected pins the local arm: the splice's
// client-join edge (first mic byte on the client pair InPath) yields
// CallEventConnected before the terminal from LocalLeg.End.
func TestCallLegLocalJoinMapsConnected(t *testing.T) {
	ctx := context.Background()
	f := newLegFixture(t)
	lad := NewLocalAdapter(f.leg)
	h, err := lad.Place(ctx, PlaceRequest{
		EmployeeID: "emp-ev2-local", Target: "joinbox",
		Leg: LegLocal, RequestID: "req-ev2-local-1",
	})
	if err != nil {
		t.Fatalf("local Place: %v", err)
	}
	events := lad.Events()

	// Attach the fake child on the CHILD pair first (same shape as
	// local_test.go's Open tests) so the splice's outbound pump has a
	// live conn and cannot end the call before the join assertion.
	f.dialPair(t, h.CCID)

	// Join as the client: dial the CLIENT pair's mic path (InPath) and
	// write one chunk — the splice pump's first byte fires the join
	// edge. The .out side is dialed too so the pair accepts cleanly
	// (same shape as local_test.go's dialPair, client paths).
	mic, err := net.Dial("unix", filepath.Join(SocketDir(), h.CCID+".c.in"))
	if err != nil {
		t.Fatalf("dial client mic: %v", err)
	}
	t.Cleanup(func() { _ = mic.Close() })
	spkr, err := net.Dial("unix", filepath.Join(SocketDir(), h.CCID+".c.out"))
	if err != nil {
		t.Fatalf("dial client speaker: %v", err)
	}
	t.Cleanup(func() { _ = spkr.Close() })
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := spkr.Read(buf); err != nil {
				return
			}
		}
	}()
	if _, err := mic.Write([]byte("mic first bytes")); err != nil {
		t.Fatalf("mic write: %v", err)
	}

	ev := callLegNextEvent(t, events, "local-connected")
	if ev.Kind != CallEventConnected {
		t.Errorf("join Kind = %v, want CallEventConnected", ev.Kind)
	}
	if ev.Reason == "" {
		t.Error("Connected CallEvent.Reason is empty")
	}

	// Terminal still lands after the transition.
	if err := f.leg.End(h.CCID, "ev2-end"); err != nil {
		t.Fatalf("local End: %v", err)
	}
	ev = callLegNextEvent(t, events, "local-ended")
	if ev.Kind != CallEventEnded {
		t.Errorf("post-join terminal Kind = %v, want CallEventEnded", ev.Kind)
	}
}

// TestCallLegBusDropsLateTransition pins the emit ordering guard: once
// a terminal latched for a ccid, a later NON-terminal emit for the same
// ccid is dropped — a racing observer edge never follows the terminal
// on the bus. (Leaf 02 touched the bus's non-terminal path, so this
// guards exactly that change.)
func TestCallLegBusDropsLateTransition(t *testing.T) {
	b := newCallEventBus()
	b.emit("cc-1", CallEvent{Kind: CallEventEnded, Reason: "hangup"})
	b.emit("cc-1", CallEvent{Kind: CallEventConnected, Reason: "late answer"})
	// A different ccid's transition is unaffected.
	b.emit("cc-2", CallEvent{Kind: CallEventReconnecting, Reason: "carrier"})

	select {
	case ev := <-b.events():
		if ev.Kind != CallEventEnded {
			t.Fatalf("first event = %v, want the terminal", ev.Kind)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal event missing from the bus")
	}
	select {
	case ev := <-b.events():
		if ev.Kind != CallEventReconnecting {
			t.Fatalf("second event = %v, want cc-2's Reconnecting (late cc-1 transition must be dropped)", ev.Kind)
		}
	case <-time.After(time.Second):
		t.Fatal("cc-2 transition missing — late-drop guard over-blocked")
	}
	select {
	case ev := <-b.events():
		t.Fatalf("unexpected third event %+v (late Connected must be dropped)", ev)
	case <-time.After(50 * time.Millisecond):
	}
}
