package voice

import (
	"context"
	"log/slog"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// TestParseAllowedStandalone pins A7-12: parseAllowed must NOT be a
// fail-open prefix match. A reply allows only when it starts with the
// standalone word ALLOW (followed by a boundary or end) AND the first
// clause carries no deny marker.
//
// The "must fail at HEAD" cases are the ones the old prefix-match
// implementation (strings.HasPrefix(upper, "ALLOW")) accepted but that are
// not an ALLOW verdict. The "pass unchanged" cases are the legitimate
// ALLOW verdicts that must keep working.
func TestParseAllowedStandalone(t *testing.T) {
	cases := []struct {
		reply string
		want  bool
	}{
		// Must FAIL at HEAD (the old prefix match accepted these):
		{"ALLOWED? NO — not this", false},
		{"ALLOW all access", false},
		{"ALLOW.", false},
		// Must PASS unchanged (legitimate ALLOW verdicts):
		{"ALLOW", true},
		{"allow", true},
		{"ALLOW: the user asked", true},
		{"deny", false},
		{"", false},
	}
	for _, c := range cases {
		if got := parseAllowed(c.reply); got != c.want {
			t.Errorf("parseAllowed(%q) = %v, want %v", c.reply, got, c.want)
		}
	}
}

// TestShortStopCtxHonorsGrace pins A7-7: shortStopCtx must return a context
// that is NOT done immediately after the call, stays not-done after a GC +
// short sleep (the old finalizer could cancel at any time), and only
// becomes done after the grace window elapses.
//
// At HEAD the test may pass by luck (the finalizer only fires when the box
// is GC'd and the GC may not run during the test), so this is a defensive
// pin. If it fails at HEAD, it proves the premature-cancel bug; if it
// passes, it confirms the fix is stable.
func TestShortStopCtxHonorsGrace(t *testing.T) {
	grace := 500 * time.Millisecond
	ctx := shortStopCtx(grace)

	// Immediately after the call, the context must NOT be done.
	if err := ctx.Err(); err != nil {
		t.Fatalf("shortStopCtx ctx.Err() = %v immediately after call, want nil", err)
	}

	// Run GC + a short sleep: the old finalizer could cancel the context at
	// any point here if the local box became unreachable.
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	if err := ctx.Err(); err != nil {
		t.Fatalf("shortStopCtx ctx.Err() = %v after GC+sleep, want nil (no premature cancel)", err)
	}

	// Wait a bit past the grace window: the context's own timer must now
	// have fired.
	deadline := time.Now().Add(grace + 50*time.Millisecond)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if ctx.Err() == nil {
		t.Fatalf("shortStopCtx ctx.Err() = nil after grace elapsed, want non-nil (timeout must fire)")
	}
}

// TestChildProcRespawnStopsStaleIdleTimer pins A4-7: when EnsureRunning
// finds a dead child in the stateDraining/stateRunning state, it must stop
// the stale idle timer before respawning. Without the fix, the orphaned
// idle timer (armed by a prior NoteCallEnd) would fire and kill the NEW
// child early.
//
// The child is killed by a direct SIGKILL (a non-timer means) while the
// idle timer is still armed. After the respawn, the new child must survive
// past the idle window — proving the stale timer was stopped on respawn.
func TestChildProcRespawnStopsStaleIdleTimer(t *testing.T) {
	env := newChildProcTestEnv(t)
	p := NewChildProc(env.cfg(func(c *ChildProcConfig) {
		c.IdleStop = 400 * time.Millisecond
	}), slog.Default(), nil)

	if err := p.EnsureRunning(context.Background()); err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	pid := p.currentPIDForTest()
	// Start and end a call: this arms the idle timer (draining).
	p.NoteCallStart("cc-1")
	p.NoteCallEnd("cc-1")

	// Kill the child by a non-timer means (SIGKILL) while the idle timer is
	// still armed. This leaves state=draining with an armed timer and a dead
	// child — exactly the state the bug's fallthrough falls into.
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill child: %v", err)
	}
	// Wait for the process to actually die (but NOT for the idle timer to
	// fire — the timer is still armed at this point).
	waitGone(t, pid, 5*time.Second)

	// Now EnsureRunning must respawn. The stale idle timer must have been
	// stopped, so the new child must survive past the idle window.
	if err := p.EnsureRunning(context.Background()); err != nil {
		t.Fatalf("EnsureRunning (respawn): %v", err)
	}
	newPID := p.currentPIDForTest()
	if newPID == pid {
		t.Fatal("respawn reused the same pid (should be a new child)")
	}
	// The new child should survive the idle window (no calls active, but
	// the stale timer was stopped so no early kill).
	time.Sleep(800 * time.Millisecond)
	if !p.Alive() || !processAlive(newPID) {
		t.Fatalf("new child died within idle window after respawn (stale idle timer not stopped): pid %d", newPID)
	}
	if err := p.Stop(context.Background()); err != nil {
		t.Fatalf("final Stop: %v", err)
	}
}
