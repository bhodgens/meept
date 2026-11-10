package signalleg

// Voice gate (plan 20261109-statemachine-remediation, leaf 05-voice/00T,
// tests 5-7): signal-leg parity + ringing-watchdog RED pins.
//
//	T5 TestSignalLegFreshSession    — bindCall must pass the fresh-session
//	                                  variadic + the built brief (C-parity
//	                                  with the phone/local legs).
//	T6 TestSignalLegEffectKeyStable — the reservation key must be derived
//	                                  from (employee, target, requestID),
//	                                  not the per-Place random ccid (C-V2).
//	T7 TestRingingTimeout           — Place must arm a bounded ring
//	                                  watchdog: FailOutcome(no_answer) +
//	                                  registry release when CONNECTED never
//	                                  lands (C-V3).
//
// Tests only. Each test is RED for missing-implementation reasons today.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/signal"
	"github.com/caimlas/meept/internal/voice"
)

// --- T5: fresh-session parity at the bindCall seam ---

// recordedBindCall is one OnCallStart invocation captured through the
// voiceBindingSeam (the trailing variadic carries the fresh flag).
type recordedBindCall struct {
	ccid      string
	number    string
	employee  string
	direction string
	brief     voice.DayBrief
	fresh     []bool // the variadic as RECEIVED (empty today — bindCall omits it)
}

// recordingBinding satisfies voiceBindingSeam and records every call,
// including the freshSession variadic slice exactly as bindCall passed it.
type recordingBinding struct {
	mu    sync.Mutex
	calls []recordedBindCall
}

func (b *recordingBinding) OnCallStart(_ context.Context, ccid, number, employeeID, direction string,
	brief voice.DayBrief, freshSession ...bool) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, recordedBindCall{
		ccid: ccid, number: number, employee: employeeID,
		direction: direction, brief: brief, fresh: freshSession,
	})
	return "sess-signal-fresh", nil
}

// TestSignalLegFreshSession pins the voice.fresh_session_per_call=true
// parity target: a connected SIGNAL call must resolve a fresh session on
// the binding exactly like the phone leg (holdPhoneCall passes the
// resolved bool — voice_wiring.go:847) and the local leg
// (local.go:422-423), and must carry the populated day brief.
//
// Pinned at the seam the leg CAN reach: bindCall -> voiceBindingSeam.
// The fake records the variadic as delivered.
//
// RED today: leg.go bindCall calls
// vb.OnCallStart(ctx, ..., brief) with NO freshSession argument —
// len(fresh) == 0 regardless of the config flag (there is no
// FreshSession seam on Deps at all), so the signal leg can never open a
// fresh session. The brief half of the assertion passes today (the
// DayBriefBuilder seam exists); the fresh half is the gate.
func TestSignalLegFreshSession(t *testing.T) {
	leg, _, _, _ := newTestLeg(t, func(string) bool { return true })

	rec := &recordingBinding{}
	leg.deps.Binding = rec
	leg.deps.DayBriefBuilder = func(_ context.Context, _, _ string) (voice.DayBrief, error) {
		return voice.DayBrief{DurableMemory: "gate-brief-memory"}, nil
	}

	ccid, err := leg.Place(context.Background(), "emp-fresh", "f1111111-1111-1111-1111-111111111111", "")
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	leg.handleCallEvent(signal.CallEvent{
		EnvelopeSourceUUID: "f1111111-1111-1111-1111-111111111111",
		CallID:             "call-stub-1",
		State:              signal.CallEventStateConnected,
	})

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.calls) != 1 {
		t.Fatalf("binding calls = %d, want 1 (CONNECTED must bind the call)", len(rec.calls))
	}
	got := rec.calls[0]
	if got.ccid != ccid {
		t.Errorf("bound ccid = %q, want %q", got.ccid, ccid)
	}
	// Parity assertion #1 (THE gate): with voice.fresh_session_per_call
	// resolved true for this employee, bindCall must pass fresh=true as
	// the trailing variadic. Today it passes NOTHING (len == 0).
	if len(got.fresh) != 1 || !got.fresh[0] {
		t.Errorf("freshSession variadic = %v, want [true] — signal leg must resolve "+
			"the fresh-session flag like the phone/local legs (voice.fresh_session_per_call=true)", got.fresh)
	}
	// Parity assertion #2: the built brief must arrive (fail-soft
	// posture preserved, but a wired builder means a non-empty brief).
	if got.brief.DurableMemory != "gate-brief-memory" {
		t.Errorf("brief = %+v, want the DayBriefBuilder result to reach OnCallStart", got.brief)
	}
}

// --- T6: stable effect key (C-V2) ---

// TestSignalLegEffectKeyStable pins C-V2: the outbound reservation key is
// derived from (employee, target, requestID) so a RETRIED REQUEST reuses
// the ledger receipt instead of reserving a fresh key on every Place.
//
// Today leg.go:432 builds effectKey(employeeID, target, ccid) where ccid
// is id.Generate("signal-") — random per Place (leg.go:~412). Place's
// signature carries no requestID, so the construction used here is the
// ledger-observation shape the gate allows: two sequential Places of the
// SAME (employee, target) (a retry after the first call tears down via
// CONNECTED+ENDED) must reserve the SAME effect key — that is the target
// assertion, RED today because the two keys differ by the random ccid.
//
// Implementation-leaf note: when Place gains a requestID (or the caller
// supplies one through Deps), the retry loop below becomes the
// same-requestID retry; the equality assertion stays valid.
func TestSignalLegEffectKeyStable(t *testing.T) {
	leg, fx, _, _ := newTestLeg(t, func(string) bool { return true })
	const uuid = "a2222222-2222-2222-2222-222222222222"

	ccid1, err := leg.Place(context.Background(), "emp-key", uuid, "")
	if err != nil {
		t.Fatalf("Place #1: %v", err)
	}
	// Full teardown of call #1 so call #2 (the retry) passes the
	// capacity gate: CONNECTED then ENDED(hangup) runs the FailOutcome
	// fan-out, and the test rig's release hook frees the registry slot.
	leg.handleCallEvent(signal.CallEvent{CallID: "call-stub-1", State: signal.CallEventStateConnected})
	leg.handleCallEvent(signal.CallEvent{CallID: "call-stub-1", State: signal.CallEventStateEnded, Reason: "hangup"})

	if _, err := leg.Place(context.Background(), "emp-key", uuid, ""); err != nil {
		t.Fatalf("Place #2 (retry, same employee+target): %v", err)
	}
	_ = ccid1

	fx.mu.Lock()
	defer fx.mu.Unlock()
	if len(fx.reserve) != 2 {
		t.Fatalf("effect reserves = %d, want 2 (one per Place)", len(fx.reserve))
	}
	// Target assertion (C-V2): the retry of the same request reuses the
	// receipt, so both reservations carry the SAME key. RED today: the
	// key embeds the per-Place random ccid, so a retry rings a SECOND
	// ledger receipt (the phone dialer's requestID-based key would not).
	if fx.reserve[0] != fx.reserve[1] {
		t.Errorf("effect keys differ across a same-request retry: %q vs %q — "+
			"target C-V2: key over (employee, target, requestID) so a retried request "+
			"reuses the ledger receipt instead of claiming a fresh random-ccid key",
			fx.reserve[0], fx.reserve[1])
	}
}

// --- T7: bounded ringing timeout (C-V3) ---

// TestRingingTimeout pins C-V3: Place acquires the registry slot and the
// effects reservation, then waits for CONNECTED with NO no-answer timer
// (leg.go Place/state-machine path arms nothing — the phone dialer's
// bounded no-answer timer, dial_test.go TestOutboundTimeoutRelease, has
// no signal-leg mirror). Target: a call whose CONNECTED never arrives
// reaches a BOUNDED terminal — Manager.FailOutcome(no_answer) + registry
// Release — without a carrier event.
//
// The fake-cli rig CAN simulate "no CONNECTED ever": simply emit no
// events after Place. The assertion polls for the bounded terminal
// within the window; RED today because nothing fires and the slot stays
// held forever (the poll is the structural absence observation the gate
// allows).
//
// Implementation-leaf note: the window must be >= the eventual
// configurable ring timeout or the leaf must expose a short test knob
// (e.g. Deps.RingingTimeout); today NO knob and NO timer exist, so any
// window is already exceeded by the hang.
func TestRingingTimeout(t *testing.T) {
	leg, _, _, _ := newTestLeg(t, func(string) bool { return true })
	reg := leg.deps.Registry

	type release struct {
		ccid    string
		outcome voice.Outcome
	}
	var (
		rmup sync.Mutex
		ends []release
	)
	leg.deps.Manager.SetReleaseHook(func(ccid string, outcome voice.Outcome) {
		rmup.Lock()
		ends = append(ends, release{ccid: ccid, outcome: outcome})
		rmup.Unlock()
		reg.Release(ccid)
	})

	ccid, err := leg.Place(context.Background(), "emp-ring", "b3333333-3333-3333-3333-333333333333", "")
	if err != nil {
		t.Fatalf("Place: %v", err)
	}

	// No CONNECTED event is ever delivered. The bounded terminal must
	// arrive on the leg's own timer.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(reg.Live()) == 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	rmup.Lock()
	gotEnds := ends
	rmup.Unlock()
	if len(reg.Live()) != 0 {
		t.Fatalf("ringing timeout never fired: slot %q still held after %v with no CONNECTED event — "+
			"Place must arm a bounded no-answer watchdog that FailOutcomes(no_answer) and releases (C-V3)",
			ccid, 2*time.Second)
	}
	if len(gotEnds) != 1 || gotEnds[0].outcome != voice.OutcomeNoAnswer {
		t.Errorf("terminal fan-out = %+v, want exactly one FailOutcome(no_answer) for %q", gotEnds, ccid)
	}
	// Late ENDED is a no-op after the watchdog already tore the call
	// down (once-guard on the terminal; a second release is a bug).
	leg.handleCallEvent(signal.CallEvent{CallID: "call-stub-late", State: signal.CallEventStateEnded, Reason: "hangup"})
	rmup.Lock()
	late := len(ends)
	rmup.Unlock()
	if late != 1 {
		t.Errorf("terminal releases = %d after a late ENDED, want 1 (late ENDED must no-op)", late)
	}
}
