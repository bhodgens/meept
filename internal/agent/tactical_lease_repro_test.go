package agent

// 02-routing/04 T1 (I1): lease accounting instrumentation + controlled
// concurrency repro for the F-ORCH-28 semaphore-hang hypothesis.
//
// The counters (leaseAcquired / leaseReleased / held leases) were added
// to tactical.go WITH this test. The hypothesis was: some finalize path
// drops a step's slotLeases entry (or never holds it) without returning
// the two semaphore tokens, so under concurrent probe load a wave slowly
// leaks every slot and ScheduleReadySteps blocks forever with no
// in-flight steps left to fire a completion event.
//
// This repro drives concurrent scheduling waves at 2/4/8 sessions with
// tiny semaphores, completes steps through the lease-release path the
// scheduler itself uses (releaseStepSlot, exactly what OnJobCompleted's
// terminal defer calls), and asserts the ledger balances: every acquire
// has a matching release once all steps are terminal, and the
// semaphore channels are empty. If a code path leaked a lease, the
// balance assertion fails and names the agent whose ledger diverges.

import (
	"context"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/task"
)

func TestLeakRepro_ConcurrentWavesBalanceTheLedger(t *testing.T) {
	for _, wave := range []int{2, 4, 8} {
		t.Run(strconv.Itoa(wave), func(t *testing.T) {
			store, err := task.NewStore(filepath.Join(t.TempDir(), "tasks.db"), testLogger())
			if err != nil {
				t.Fatalf("task store: %v", err)
			}
			t.Cleanup(func() {
				if closeErr := store.Close(); closeErr != nil {
					t.Errorf("close task store: %v", closeErr)
				}
			})
			ts, _ := newLeakReproScheduler(t, store)

			parent := task.NewTask("lease-repro", "concurrent wave")
			parent.TotalJobs = wave
			parent.SetState(task.StateExecuting)
			if err := ts.taskStore.Create(parent); err != nil {
				t.Fatalf("create task: %v", err)
			}
			steps := make([]*task.TaskStep, 0, wave)
			for i := 0; i < wave; i++ {
				step := task.NewTaskStep(parent.ID, "repro step", i)
				step.ToolHint = "code"
				step.AgentID = config.AgentIDCoder
				step.State = task.StepReady
				if err := ts.stepStore.Create(step); err != nil {
					t.Fatalf("create step %d: %v", i, err)
				}
				steps = append(steps, step)
			}

			// Concurrent sweeps hammer the semaphore: many goroutines
			// schedule the same task while other goroutines finalize
			// (release) steps that already hold leases, the shape F-ORCH-28
			// observed under CONCURRENT probe load (the serial bench never
			// hung).
			var wg sync.WaitGroup
			ctx := context.Background()
			for g := 0; g < wave; g++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if err := ts.ScheduleReadySteps(ctx, parent.ID); err != nil {
						t.Errorf("ScheduleReadySteps: %v", err)
					}
				}()
			}
			wg.Wait()

			// Drain: release every held step lease exactly once (the
			// terminal-completion shape).
			for _, step := range steps {
				ts.releaseStepSlot(step.ID)
			}

			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) && len(ts.globalSemaphore) != 0 {
				time.Sleep(20 * time.Millisecond)
			}
			if got := len(ts.globalSemaphore); got != 0 {
				ledger := ts.LeaseLedgerSnapshot()
				t.Errorf("global slots still held = %d after full drain; ledger acquired=%v released=%v held=%v: a finalize path leaked the lease WITHOUT releasing the tokens -- name it before fixing",
					got, ledger.Acquired, ledger.Released, ledger.Held)
			}
			ledger := ts.LeaseLedgerSnapshot()
			for agentID, acquired := range ledger.Acquired {
				released := ledger.Released[agentID]
				held := ledger.Held[agentID]
				if acquired != released+held {
					t.Errorf("lease ledger imbalanced for agent %s: acquired=%d released=%d held=%d -- the finalize path that drops slotLeases entries without lease.release() is %q",
						agentID, acquired, released, held, "OnJobCompleted/OnJobFailed terminal release")
				}
			}
			if len(ledger.Held) != 0 {
				t.Errorf("leases still registered after full drain: held=%v", ledger.Held)
			}
		})
	}
}

// newLeakReproScheduler builds a scheduler over the shared store with a
// 1-slot global semaphore so concurrent waves contend hard.
func newLeakReproScheduler(t *testing.T, store *task.Store) (*TacticalScheduler, *recordingQueue) {
	t.Helper()
	q := &recordingQueue{}
	ts := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore:             store.StepStore(),
		TaskStore:             store,
		Queue:                 q,
		Logger:                testLogger(),
		MaxConcurrentJobs:     1,
		MaxConcurrentPerAgent: 1,
	})
	return ts, q
}
