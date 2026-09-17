package agent

// F29 (2026-09-17 bughunt) pin: hard task failures must reach the burst
// detector through the PRODUCTION path. The detector was only fed by the
// dispatcher's Signal-A resolver (ResolvePendingOutcome → ok/corrected),
// while hard failures mark dispatch_log 'failed_replan' directly via
// MarkTaskFailedReplan and never passed through that resolver — so real
// failure bursts were invisible. With SetBurstDetector wired (the daemon
// composition does this in components.go), OnJobFailed must feed
// ObserveOutcome("failed_replan") for the step's session.
//
// Wiring-level assertion: drive OnJobFailed enough times to fill the
// detector's warm-up window and require the burst to fire — impossible
// unless the failure path actually observes outcomes.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/task"
)

func TestOnJobFailed_FeedsBurstDetector_FailedReplan(t *testing.T) {
	ts, msgBus, cleanup := newTacticalTestSetup(t)
	defer cleanup()

	s, err := metrics.NewStore(&metrics.StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ts.SetMetricsStore(s)
	ts.escalationManager = NewEscalationManager(EscalationManagerConfig{
		Config:    DefaultEscalationConfig(),
		TaskStore: ts.taskStore,
		Bus:       msgBus,
	})

	// Smallest admissible window (burstMinWindow = 5): five hard failures
	// in one session must trip the burst. Threshold 0.5: five consecutive
	// 1.0 signals put the fast-slow EWMA spread at ~0.83, comfortably
	// above.
	det := metrics.NewBurstDetector(metrics.BurstDetectionConfig{
		Enabled:    true,
		WindowSize: 5,
		Threshold:  0.5,
	}, testLogger())
	ts.SetBurstDetector(det)

	const sessionID = "sess-f29-burst"
	const failures = 5
	for i := 0; i < failures; i++ {
		parent := newTestTask("task-f29-"+string(rune('a'+i)), "f29 burst feed")
		parent.TotalJobs = 1
		if err := ts.taskStore.Create(parent); err != nil {
			t.Fatalf("create task %d: %v", i, err)
		}
		step := task.NewTaskStep(parent.ID, "doomed step", 0)
		step.SessionID = sessionID // production steps carry the originating session
		if err := ts.stepStore.Create(step); err != nil {
			t.Fatalf("create step %d: %v", i, err)
		}
		if err := ts.stepStore.SetJobID(step.ID, "job-f29-"+string(rune('a'+i))); err != nil {
			t.Fatalf("set job id %d: %v", i, err)
		}

		// The dispatch row that routed this task is still pending; the
		// failure path flips it 'failed_replan' (Signal B, unchanged).
		s.RecordDispatch(metrics.DispatchEntry{
			SessionID: sessionID, TaskID: parent.ID,
			AgentID: "llm/code", ClassifierMethod: "llm", TurnNo: i + 1, Outcome: "pending",
		})

		if err := ts.OnJobFailed(context.Background(), "job-f29-"+string(rune('a'+i)), "hard failure"); err != nil {
			t.Fatalf("OnJobFailed %d: %v", i, err)
		}

		var outcome string
		if err := s.DB().Get(&outcome, `SELECT outcome FROM dispatch_log WHERE task_id = ?`, parent.ID); err != nil {
			t.Fatalf("get outcome %d: %v", i, err)
		}
		if outcome != "failed_replan" {
			t.Errorf("failure %d: dispatch outcome = %q, want failed_replan", i, outcome)
		}
	}

	// The detector must have SEEN the failures: the threshold rule ("last
	// n outcomes all failed_replan") fires only if the failure path fed
	// ObserveOutcome. Without the F29 wiring the detector's window is
	// empty here and this is false.
	if !det.ThresholdRule(sessionID, 3) {
		t.Fatal("burst detector never observed the hard failures (F29 starvation still present)")
	}
	if burst, score := det.ObserveOutcome(sessionID, "failed_replan"); !burst {
		t.Errorf("expected burst to fire once the window is failure-saturated; got burst=%v score=%.3f", burst, score)
	}
}

// A nil or disabled detector must leave the failure path unchanged
// (log-only invariant): OnJobFailed succeeds and no burst bookkeeping
// panics.
func TestOnJobFailed_NilBurstDetectorNoOp(t *testing.T) {
	ts, msgBus, cleanup := newTacticalTestSetup(t)
	defer cleanup()

	ts.SetMetricsStore(nil) // typed nil ignored by the setter
	ts.escalationManager = NewEscalationManager(EscalationManagerConfig{
		Config:    DefaultEscalationConfig(),
		TaskStore: ts.taskStore,
		Bus:       msgBus,
	})
	ts.SetBurstDetector(nil)

	parent := newTestTask("task-f29-nil", "nil detector no-op")
	parent.TotalJobs = 1
	if err := ts.taskStore.Create(parent); err != nil {
		t.Fatalf("create task: %v", err)
	}
	step := task.NewTaskStep(parent.ID, "doomed step", 0)
	step.SessionID = "sess-f29-nil"
	if err := ts.stepStore.Create(step); err != nil {
		t.Fatalf("create step: %v", err)
	}
	if err := ts.stepStore.SetJobID(step.ID, "job-f29-nil"); err != nil {
		t.Fatalf("set job id: %v", err)
	}

	if err := ts.OnJobFailed(context.Background(), "job-f29-nil", "hard failure"); err != nil {
		t.Fatalf("OnJobFailed: %v", err)
	}
}

// SetBurstDetector must ignore a disabled detector the same way the
// dispatcher wiring does (inert until enabled).
func TestSetBurstDetector_DisabledDetectorWiring(t *testing.T) {
	ts, _, cleanup := newTacticalTestSetup(t)
	defer cleanup()

	det := metrics.NewBurstDetector(metrics.BurstDetectionConfig{Enabled: false}, testLogger())
	ts.SetBurstDetector(det)
	if ts.burstDetector == nil {
		t.Fatal("SetBurstDetector dropped an explicit detector reference")
	}
	if ts.burstDetector.Enabled() {
		t.Error("disabled detector must stay inert")
	}
}
