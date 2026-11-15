package agent

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// PIN (turn-registry defect 2026-11-15, a874971a follow-up): TurnRegistry.
// Complete is the rollback/no-op substrate for the handler's early-return
// safety net (the defer armed after ClaimDispatch in handleRequest), so it
// MUST be idempotent: a second Complete on an already-completed turn id is a
// no-op (no panic, tombstone timestamp preserved), the live row is gone, and
// the id stays suppressed — both a Register retry (F16 client-retry dedupe)
// and a second ClaimDispatch (in-flight duplicate gate) must still report the
// turn as existing/claimed so the handler answers duplicate_suppressed.
// ---------------------------------------------------------------------------

// TestTurnRegistry_CompleteIdempotent simulates the safety-net shape:
// ClaimDispatch then Complete twice. The first Complete tombstones the id;
// the second must be a no-op that neither resurrects the row nor evicts the
// tombstone (a resurrected row would re-enable dispatch of finished work; an
// evicted tombstone would let a stale retry re-execute it).
func TestTurnRegistry_CompleteIdempotent(t *testing.T) {
	reg := NewTurnRegistry()
	start := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	fc := newFakeClock(start)
	reg.now = fc.Now

	// The claim itself (the handler's gate at the ClaimDispatch seam).
	if claimed := reg.ClaimDispatch("turn-pin-1", "conv-pin-1"); !claimed {
		t.Fatal("first ClaimDispatch returned false; want claimed=true")
	}

	// Normal-path completion (the funnel at the bottom of handleRequest).
	reg.Complete("turn-pin-1")
	if _, ok := registryGet(t, reg, "turn-pin-1"); ok {
		t.Fatal("live row still tracked after first Complete")
	}

	// The safety-net completion firing AFTER the normal path: must be a
	// silent no-op — same call the defer would make on an early-return
	// race with the funnel.
	reg.Complete("turn-pin-1")
	if _, ok := registryGet(t, reg, "turn-pin-1"); ok {
		t.Fatal("second Complete resurrected the live row")
	}
	if _, done := reg.tombstones["turn-pin-1"]; !done {
		t.Fatal("second Complete dropped the tombstone; retry suppression lost")
	}
	// The tombstone timestamp must be the FIRST completion's, not the
	// second call's clock (Complete overwrites the entry unconditionally,
	// which is safe for suppression semantics but pinned here so a future
	// timestamp-sensitive change is a conscious one).
	if got := reg.tombstones["turn-pin-1"]; !got.Equal(start) {
		t.Errorf("tombstone timestamp = %v, want the first completion's %v", got, start)
	}
}

// TestTurnRegistry_CompletedTurnStaysSuppressed pins the suppression half:
// after ClaimDispatch + Complete (+ a redundant second Complete), the turn id
// must still be suppressed on every entry path — Register (client-retry
// dedupe) reports existing=true WITHOUT re-registering, and ClaimDispatch
// (in-flight duplicate gate) reports false so the handler emits
// duplicate_suppressed instead of re-executing finished work.
func TestTurnRegistry_CompletedTurnStaysSuppressed(t *testing.T) {
	reg := NewTurnRegistry()
	fc := newFakeClock(time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC))
	reg.now = fc.Now

	reg.ClaimDispatch("turn-pin-2", "conv-pin-2")
	reg.Complete("turn-pin-2")
	reg.Complete("turn-pin-2") // the safety net's late no-op

	// A client retry of the finished turn (chat.submit Register path):
	// existing=true, and crucially NO new row (re-registering would let
	// a later ClaimDispatch claim and re-execute the finished work).
	if existing := reg.Register("turn-pin-2", "conv-pin-2"); !existing {
		t.Fatal("Register on completed turn returned existing=false; want suppressed")
	}
	if _, ok := registryGet(t, reg, "turn-pin-2"); ok {
		t.Fatal("Register resurrected a row for a completed turn")
	}

	// A duplicate chat.request of the finished turn (handler gate):
	// must NOT claim — the handler answers duplicate_suppressed.
	if claimed := reg.ClaimDispatch("turn-pin-2", "conv-pin-2"); claimed {
		t.Fatal("ClaimDispatch on completed turn returned true; want suppressed")
	}
}
