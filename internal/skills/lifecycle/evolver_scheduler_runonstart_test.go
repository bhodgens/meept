package lifecycle

import (
	"testing"
	"time"
)

// TestEvolverSchedulerRunOnStartPropagation pins the daemon-wiring defect
// fixed 2026-10-22: components_wiki.go used NewEvolverScheduler, which
// hardcodes runOnStart=true, so skills.evolver.run_on_start=false was
// never honored and every daemon restart burned a full evolution cycle
// (~10 min, ~50 LLM calls). The explicit constructor must carry the
// configured flag through to the scheduler.
func TestEvolverSchedulerRunOnStartPropagation(t *testing.T) {
	s := NewEvolverSchedulerWithRunOnStart(nil, time.Hour, false, nil)
	if s.runOnStart {
		t.Fatal("NewEvolverSchedulerWithRunOnStart(false) produced runOnStart=true (config would be ignored)")
	}
	s2 := NewEvolverSchedulerWithRunOnStart(nil, time.Hour, true, nil)
	if !s2.runOnStart {
		t.Fatal("NewEvolverSchedulerWithRunOnStart(true) produced runOnStart=false")
	}
	// Legacy constructor keeps its documented run-on-start default.
	s3 := NewEvolverScheduler(nil, time.Hour, nil)
	if !s3.runOnStart {
		t.Fatal("legacy NewEvolverScheduler lost its runOnStart=true default")
	}
}
