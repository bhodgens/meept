package eval

import (
	"fmt"
	"testing"
)

// TestLaneEndToEnd is the lane the Makefile target drives. Exit policy:
//   - lane verdict PASS (0 failures)        -> test passes (exit 0)
//   - loud skip (weights absent, exit 0)    -> test passes (exit 0),
//     with the WARNING naming the missing weights dir on stdout
//   - any case FAIL or lane error           -> test fails (exit 1)
//
// Never a silent pass: the skip branch prints the loud warning (pinned
// by TestWeightsMissingSkipsLoud's weightsStatus contract and asserted
// here via the report), and the make lane inherits test output verbatim.
func TestLaneEndToEnd(t *testing.T) {
	root, err := repoRootFrom(".")
	if err != nil {
		t.Skipf("fixture dir not reachable: %v", err)
	}
	rep, err := RunLane(root)
	if err != nil {
		t.Fatalf("lane error: %v", err)
	}
	if rep.Skipped {
		fmt.Println("VOICE-EVAL LANE: LOUD SKIP (weights absent) — WARNING printed above; nothing was evaluated")
		return
	}
	if rep.Failures > 0 {
		for _, r := range rep.FailReasons {
			t.Errorf("voice-eval case FAIL: %s", r)
		}
		t.Fatalf("voice-eval lane: %d case(s) failed", rep.Failures)
	}
	fmt.Println("VOICE-EVAL LANE: PASS")
}
