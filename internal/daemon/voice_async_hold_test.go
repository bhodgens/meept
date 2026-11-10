package daemon

// Leaf 05-voice/04 (plan 20261109-statemachine-remediation) GREEN-side
// pins for the webhook-decoupled goal hold and the release end frame.
// The webhook-boundary half of finding 1 is pinned by the committed gate
// voice_webhook_gate_test.go TestHoldDoesNotBlockWebhook (never touched
// here); these tests pin the halves the gate comment delegates to the
// implementation leaf:
//
//   - TestAsyncHoldGatesFirstTurn: the hold STILL completes before the
//     employee's first turn — the talk-loop turn runner blocks on the
//     per-call hold gate while the prior goal round holds the employee
//     mutex, and only crosses into Binding.RunTurn after the hold (and
//     the OnCallStart attempt) completed.
//   - TestPendingHoldRollsBackOnRelease: a call that terminal-fires while
//     its hold pends cannot leak the employee mutex or strand state —
//     the hold goroutine observes the release and rolls the registration
//     back exactly once (release-exactly-once invariant).
//   - TestReleaseSendsEndFrame (finding 10): releaseCall ships one C1
//     end frame to the child — call_id carried, `last` OMITTED, reason
//     = outcome — best-effort before teardown.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voice"
)

// TestAsyncHoldGatesFirstTurn pins the "hold still gates first turn"
// half of the finding-1 contract on the async path.
func TestAsyncHoldGatesFirstTurn(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	emp := w.Employee
	const empID = "phone-employee"
	const ccid = "cc-async-t1"

	prior := emp.AcquireCallHold("cc-prior-t1", empID)
	t.Cleanup(prior)

	// Same spawn-seam shape as the async webhook path: gate armed, hold
	// on its own goroutine (blocks on the held employee mutex).
	gate := emp.gates.arm(ccid)
	holdDone := make(chan struct{})
	go func() {
		defer close(holdDone)
		defer gate.markReady()
		holdPhoneCall(emp, ccid, empID, "+155****0009", voice.DirectionIncoming, false, gate)
	}()

	// Deterministic turn seam: close the call store so the async hold's
	// OnCallStart FAILS (binding never registers the call) — the
	// post-gate RunTurn then returns the named "unknown call" error
	// instead of executing a real employee loop. The gate ordering is
	// what is under test, not the turn result.
	if err := emp.store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	runner := turnRunnerFor(emp.Binding, emp.gates, voice.Instance{
		CallControlID: ccid, EmployeeID: empID, Target: "+155****0009",
	})
	turnDone := make(chan string, 1)
	go func() {
		_, err := runner.Run(context.Background(), voice.SessionHandle{}, "hello")
		if err != nil {
			turnDone <- err.Error()
			return
		}
		turnDone <- ""
	}()

	select {
	case got := <-turnDone:
		t.Fatalf("turn crossed into the employee BEFORE the hold was applied (got %q) — "+
			"the async hold must still gate the first turn", got)
	case <-time.After(500 * time.Millisecond):
		// Blocked on the gate: correct so far.
	}

	emp.holdMu.Lock()
	_, held := emp.heldCalls[ccid]
	emp.holdMu.Unlock()
	if held {
		t.Fatal("hold registered while the prior goal round still owns the mutex")
	}

	// Release the prior round: the hold completes, the gate opens, and
	// ONLY then does the turn proceed into RunTurn.
	prior()
	select {
	case <-holdDone:
	case <-time.After(2 * time.Second):
		t.Fatal("async hold never completed after the prior release")
	}
	select {
	case got := <-turnDone:
		if !strings.Contains(got, "unknown call") {
			t.Fatalf("post-gate turn outcome = %q, want the binding's unknown-call error "+
				"(proves RunTurn was reached only after the gate opened)", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("turn never crossed the gate after the hold completed")
	}

	// The hold registration is live post-crossing (no release fired):
	// clean it up through the once-guard, and the cleanup prior() above
	// stays a no-op double-unlock guard.
	if !emp.ReleaseForCall(ccid) {
		t.Error("hold registration missing after the gate crossed — AcquireCallHold must register before OnCallStart")
	}
}

// TestPendingHoldRollsBackOnRelease pins release-exactly-once for the
// async path: terminal release while the hold pends must not leave the
// employee mutex parked forever.
func TestPendingHoldRollsBackOnRelease(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	emp := w.Employee
	const empID = "phone-employee"
	const ccid = "cc-async-t2"

	prior := emp.AcquireCallHold("cc-prior-t2", empID)
	t.Cleanup(prior)

	gate := emp.gates.arm(ccid)
	holdDone := make(chan struct{})
	go func() {
		defer close(holdDone)
		defer gate.markReady()
		holdPhoneCall(emp, ccid, empID, "+155****0008", voice.DirectionIncoming, false, gate)
	}()

	// The call terminal-fires while the hold pends on the mutex.
	w.releaseCall(ccid, voice.OutcomeHangup)

	prior()
	select {
	case <-holdDone:
	case <-time.After(2 * time.Second):
		t.Fatal("pended hold never resumed after the prior release")
	}

	// Rollback must have removed the registration (once-guard).
	deadline := time.Now().Add(2 * time.Second)
	for {
		emp.holdMu.Lock()
		_, held := emp.heldCalls[ccid]
		emp.holdMu.Unlock()
		if !held {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("rolled-back hold still registered in heldCalls")
		}
		time.Sleep(2 * time.Millisecond)
	}

	// And the employee mutex is FREE: a fresh hold round must acquire
	// promptly (the 'never parked' invariant on the release side).
	gotFree := make(chan struct{})
	go func() {
		r := emp.AcquireCallHold("cc-after-t2", empID)
		r()
		close(gotFree)
	}()
	select {
	case <-gotFree:
	case <-time.After(2 * time.Second):
		t.Fatal("pending-then-released hold leaked the employee mutex — release must fire exactly once")
	}

	if emp.gates.peek(ccid) != nil {
		t.Error("hold gate not dropped from the tracker on release")
	}
}

// TestReleaseSendsEndFrame pins finding 10: releaseCall ships the C1 end
// frame to the child over the text socket, byte-exact per the AGENTS.md
// frames contract (call_id carried, `last` omitted for end), before the
// rest of the fan-out. Uses the real TextServer + fake-child fixture the
// keys gate established.
func TestReleaseSendsEndFrame(t *testing.T) {
	f := newKeysEmitFixture(t)
	const ccid = "cc-end-1"
	acquireFor(f.w, ccid)

	// Arm the loop (setup also emits the keys line; drain it first so
	// the end-frame scan below is unambiguous).
	f.w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventSetup})
	readKeysLine(t, f.lines)

	f.w.releaseCall(ccid, voice.OutcomeHangup)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case line, ok := <-f.lines:
			if !ok {
				t.Fatal("fake child stream closed before an end line")
			}
			if !strings.Contains(line, `"end"`) {
				continue // say/clear stragglers; keep scanning
			}
			if strings.Contains(line, `"last"`) {
				t.Fatalf("end frame carries a last key (must be OMITTED for end): %s", line)
			}
			var wire map[string]any
			if err := json.Unmarshal([]byte(line), &wire); err != nil {
				t.Fatalf("end line not JSON: %v: %s", err, line)
			}
			if wire["event"] != "end" {
				t.Fatalf("event = %v, want \"end\"", wire["event"])
			}
			if wire["call_id"] != ccid {
				t.Fatalf("call_id = %v, want %s (every frame carries call_id)", wire["call_id"], ccid)
			}
			if wire["reason"] != "hangup" {
				t.Errorf("reason = %v, want \"hangup\" (terminal outcome carried)", wire["reason"])
			}
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Fatal("no C1 end frame reached the child on release (finding 10)")
}

// TestReleaseEndFrameNoChildDoesNotBlock pins the best-effort contract:
// with no child transport at all, the end frame must fail fast (Send,
// unlike SendRaw, never waits on child readiness) and the release
// fan-out must complete. newVoiceWiringForTest's TextServer is never
// started, so Send hits the "no child connected" error immediately.
func TestReleaseEndFrameNoChildDoesNotBlock(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	const ccid = "cc-end-nochild"
	acquireFor(w, ccid)
	emp := w.Employee
	if _, err := emp.Binding.OnCallStart(context.Background(), ccid, "+155****0007",
		"phone-employee", voice.DirectionIncoming, voice.DayBrief{}); err != nil {
		t.Fatalf("OnCallStart: %v", err)
	}

	done := make(chan struct{})
	go func() {
		w.releaseCall(ccid, voice.OutcomeFailed)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("releaseCall blocked on the end frame with no child connected")
	}
	if emp.ReleaseForCall(ccid) {
		// already released by the hook; a true return here means a double
		// registration, which must not happen
		t.Error("hold re-registered after release")
	}
}
