package agent

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// Pins for bughunt L2 (2026-11-24): the convergence detector's history is
// keyed PER CONVERSATION because one task-scoped loop serves its step jobs
// concurrently, each under its own conversation id. resetTurnGuards used to
// clear the WHOLE MAP at every turn start, so a step job starting while a
// sibling step's window was live wiped that sibling's detection window —
// silently undoing the per-conversation keying at every turn boundary.
// The fix scopes the turn-start convergence hygiene to the starting turn's
// own key (resetTurnGuards(conversationID) / ResetConversation).

// (i) Two overlapping windows for different keys: starting a turn on
// conversation A (loop.resetTurnGuards("A")) must NOT clear the
// live window of sibling conversation B. Pre-fix, the whole-map Reset
// emptied B's streak and B would never converge on its repeated replies.
func TestConvergenceTurnReset_DoesNotClearSiblingWindow(t *testing.T) {
	loop := NewAgentLoop("sess", ".") //nolint:govet // fieldalignment not enforced here
	cd := loop.convergenceDetector
	if cd == nil {
		t.Fatal("NewAgentLoop must initialize convergenceDetector")
	}

	// Sibling step B has a live window: two identical replies in flight.
	for i := 0; i < 2; i++ {
		if cd.recordResponseFor("step-taskB-2", "B stuck reply", false) {
			t.Fatalf("premature convergence in sibling B at entry %d", i+1)
		}
	}

	// Another step A starts its turn on the SAME loop.
	loop.resetTurnGuards("step-taskA-1")

	// B's window must have survived: the third identical reply converges.
	if !cd.recordResponseFor("step-taskB-2", "B stuck reply", false) {
		t.Fatal("L2 regression: turn-start reset on a different conversation wiped the sibling's live convergence window")
	}

	// A's own key was still cleared (fresh window: first reply must not trip).
	if cd.recordResponseFor("step-taskA-1", "A reply", false) {
		t.Fatal("starting turn on A must still clear A's own stale window")
	}
}

// (ii) Same-key re-entry semantics: a NEW turn on the SAME conversation
// still gets the documented within-turn reset — the stale window of the
// previous turn on that key must not carry over. This is the e2e-run-3
// (2026-09-10) semantic preserved at per-key granularity.
func TestConvergenceTurnReset_SameKeyReEntryStillClears(t *testing.T) {
	loop := NewAgentLoop("sess", ".") //nolint:govet // fieldalignment not enforced here
	cd := loop.convergenceDetector

	// Previous turn on this key left two identical replies one short of
	// tripping.
	for i := 0; i < 2; i++ {
		if cd.recordResponseFor("conv-x", "stale reply", false) {
			t.Fatalf("unexpected convergence on stale entry %d", i+1)
		}
	}

	// Fresh turn on the SAME conversation resets that key.
	loop.resetTurnGuards("conv-x")

	// A full threshold of NEW identical replies is needed before converging.
	for i := 0; i < cd.config.ConvergenceThreshold-1; i++ {
		if cd.recordResponseFor("conv-x", "fresh reply", false) {
			t.Fatalf("converged early at new entry %d; same-key turn reset must clear the stale window", i+1)
		}
	}
	if !cd.recordResponseFor("conv-x", "fresh reply", false) {
		t.Fatal("within-conversation streak must still converge after a same-key turn reset")
	}
}

// The conversation-agnostic posture stays: resetTurnGuards() with no turn
// conversation keeps the conservative whole-map clear (pinning
// TestResetTurnGuards_ClearsConvergenceHistory's semantics).
func TestConvergenceTurnReset_EmptyKeyClearsAll(t *testing.T) {
	loop := NewAgentLoop("sess", ".") //nolint:govet // fieldalignment not enforced here
	cd := loop.convergenceDetector
	cd.recordResponseFor("conv-a", "same", false)
	cd.recordResponseFor("conv-b", "same", false)

	loop.resetTurnGuards()

	// Every window is empty: three fresh identical replies are needed
	// before any key converges again.
	for i := 0; i < 2; i++ {
		if cd.recordResponseFor("conv-b", "same", false) {
			t.Fatalf("conv-b converged early at entry %d after whole-map reset", i+1)
		}
	}
	if !cd.recordResponseFor("conv-b", "same", false) {
		t.Fatal("resetTurnGuards() must still clear every conversation window")
	}
}

// Storage bound (L2): with per-key resets the map is no longer recycled
// wholesale each turn, so expiry + cap pruning must bound it. Fill well
// past the cap with fresh entries and verify a record prunes back to the
// documented bound.
func TestConvergenceDetector_PrunesToWindowCap(t *testing.T) {
	cd := newConvergenceDetector(DefaultDetectionConfig(), slog.Default())
	// Seed the map past the cap directly (entries carry current timestamps,
	// so only the count cap can evict — expiry sweep alone would not).
	cd.mu.Lock()
	for i := 0; i < maxConvergenceWindows+64; i++ {
		cd.histories[fmt.Sprintf("conv-%d", i)] = []responseSignature{
			{contentHash: "h", hasTools: false, timestamp: time.Now()},
		}
	}
	cd.mu.Unlock()

	cd.recordResponseFor("trigger", "a reply", false)

	cd.mu.Lock()
	got := len(cd.histories)
	_, triggerLive := cd.histories["trigger"]
	cd.mu.Unlock()
	// prune runs BEFORE the write: the map is capped, then "trigger" is
	// (re)created, so the post-record bound is cap+1.
	if got > maxConvergenceWindows+1 {
		t.Fatalf("window count %d exceeds cap %d+1; prune must bound per-key-reset storage", got, maxConvergenceWindows)
	}
	if !triggerLive {
		t.Fatal("trigger key must exist")
	}
}

// Concurrent step jobs sharing one loop: turns starting on many keys while
// siblings record must never race and must never strand another key's
// streak below its own record count (run with -race).
func TestConvergenceDetector_ConcurrentTurnResetsAndRecords(t *testing.T) {
	loop := NewAgentLoop("sess", ".") //nolint:govet // fieldalignment not enforced here
	cd := loop.convergenceDetector
	const keys = 8
	const rounds = 30

	var wg sync.WaitGroup
	for k := 0; k < keys; k++ {
		key := fmt.Sprintf("conv-%d", k)
		wg.Add(2)
		go func() {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				cd.recordResponseFor(key, strings.Repeat("x", r%3+1), false)
			}
		}()
		go func() {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				loop.resetTurnGuards(key)
			}
		}()
	}
	wg.Wait()

	cd.mu.Lock()
	n := len(cd.histories)
	cd.mu.Unlock()
	if n > maxConvergenceWindows+keys {
		t.Fatalf("map unbounded under concurrent churn: %d keys", n)
	}
}
