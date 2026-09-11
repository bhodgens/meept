package agent

import (
	"io"
	"log/slog"
	"testing"
)

// Pins for the e2e T2 convergence-abort fix (2026-09-10).
//
// Run 1 failure: the 8B model returned blank/reasoning-only replies on the
// T2 turn; every blank reply hashes to the same empty string, so the
// convergence detector hit its threshold of 3 in three iterations and
// aborted the turn with "agent responses converged without progress" —
// before the blank-content nudge ladder (loop.go empty-response handling)
// or the reasoning watchdog could do their job.

func newTestConvergenceDetector() *convergenceDetector {
	return newConvergenceDetector(DetectionConfig{
		CycleThreshold:       3,
		ConvergenceThreshold: 3,
		HistorySize:          10,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// Blank replies must not count toward convergence: they belong to the
// nudge ladder, not the stagnation guard.
func TestConvergenceDetector_BlankResponsesDoNotConverge(t *testing.T) {
	cd := newTestConvergenceDetector()

	for i := 0; i < 10; i++ {
		if cd.recordResponse("", false) {
			t.Fatalf("iteration %d: blank responses tripped convergence; blank turns must not converge", i+1)
		}
		if cd.recordResponse("   \n\t  ", false) {
			t.Fatalf("iteration %d: whitespace-only responses tripped convergence", i+1)
		}
	}
}

// Distinct substantive replies without tools do NOT converge (baseline
// sanity: the guard still exists).
func TestConvergenceDetector_DistinctRepliesDoNotConverge(t *testing.T) {
	cd := newTestConvergenceDetector()

	replies := []string{
		"Here is the first answer.",
		"Here is the second answer, different.",
		"Here is a third, also different.",
		"And a fourth distinct reply.",
		"Fifth unique response.",
	}
	for i, r := range replies {
		if cd.recordResponse(r, false) {
			t.Fatalf("iteration %d: distinct replies tripped convergence", i+1)
		}
	}
}

// IDENTICAL substantive replies without tools DO converge — the guard's
// original contract is preserved for the case it was built for.
func TestConvergenceDetector_IdenticalRepliesStillConverge(t *testing.T) {
	cd := newTestConvergenceDetector()

	same := "I cannot do that, sorry."
	for i := 0; i < 3; i++ {
		if cd.recordResponse(same, false) {
			// The identical-reply convergence fired at or before the
			// threshold — but it must fire exactly AT the threshold
			// (3rd), not before.
			if i < 2 {
				t.Fatalf("iteration %d: convergence fired before threshold", i+1)
			}
			return
		}
	}
	t.Fatal("identical no-tool replies did not converge at threshold; guard broken")
}

// Mixed sequence: blank turns interleaved with identical substantive turns
// converge only on the SUBSTANTIVE repetition count.
func TestConvergenceDetector_BlankTurnsDoNotFeedIdenticalStreak(t *testing.T) {
	cd := newTestConvergenceDetector()

	// Two blank turns between each substantive turn — under the old
	// behavior the blanks themselves converged; under the fix they are
	// invisible and the substantive streak needs its own 3.
	blank := func(n int) {
		for i := 0; i < n; i++ {
			if cd.recordResponse("", false) {
				t.Fatal("blank-only turns tripped convergence")
			}
		}
	}

	blank(2)
	if cd.recordResponse("Working on it.", false) {
		t.Fatal("convergence before any repetition")
	}
	blank(2)
	if cd.recordResponse("Working on it.", false) {
		t.Fatal("convergence after 2 identical replies (threshold is 3)")
	}
	blank(2)
	// 3rd identical substantive reply → converge (original contract).
	if !cd.recordResponse("Working on it.", false) {
		t.Fatal("3 identical substantive replies did not converge; guard broken")
	}
}

// Tool-using turns never count toward convergence (pre-existing contract,
// pinned so the blank-turn fix doesn't drift it).
func TestConvergenceDetector_ToolTurnsNeverConverge(t *testing.T) {
	cd := newTestConvergenceDetector()

	for i := 0; i < 6; i++ {
		if cd.recordResponse("same text every time", true) {
			t.Fatalf("iteration %d: tool-using turns tripped convergence", i+1)
		}
	}
}
