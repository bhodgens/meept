package daemon

// Daemon wiring tests for phase-frontier-parallel leaf 04 (Contract C):
// the plans.parallel_phases CONFIG VALUE must actually reach the
// Orchestrator through the real daemon wiring path (NewOrchestrator +
// SetParallelPhases next to the SetPhaseTransitionHook site) and change
// observable phase-dispatch behavior — not just get set on the struct.
//
// The deep behavior matrix lives in internal/agent (leaf 02's
// TestAdvancePhases_SerialEquivalence / TestAdvancePhases_ParallelFrontier);
// here we prove the config value's plumbing with one seeded scenario run
// through a REAL message bus, exactly as handleJobCompleted triggers it in
// production (queue.job.completed → tactical.OnJobCompleted →
// maybeTransitionPhase).

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// wiringFixture is a minimal but REAL component graph: persistent queue,
// task store + step store, plan manager on a SQLite plan store, tactical
// scheduler, and an orchestrator built through NewOrchestrator (the
// components.go construction path) — flag wired via SetParallelPhases the
// way daemon.go wires it from cfg.Plans.ParallelPhases.
type wiringFixture struct {
	t          *testing.T
	taskStore  *task.Store
	store      *task.StepStore
	stub       *plan.SQLiteStore
	orch       *agent.Orchestrator
	tactical   *agent.TacticalScheduler
	msgBus     *bus.MessageBus
	pq         *queue.PersistentQueue
	taskID     string
	ctx        context.Context
	cancel     context.CancelFunc
	hookCalls  []string
	hookMu     sync.Mutex
	transition func(taskID, from, to string)
}

// newWiringFixture builds the graph; enabled mirrors the config value being
// tested (false = serial default, true = parallel frontier). provisioner (may
// be nil) is wired via SetPhaseWorktreeProvisioner so worktree-consumption
// tests can observe real per-phase provisioning through startPhase.
func newWiringFixture(t *testing.T, enabled bool, provisioner func(ctx context.Context, taskID, phaseID, phaseName string) (string, error)) *wiringFixture {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	msgBus := bus.New(nil, logger)
	t.Cleanup(func() { msgBus.Close() })

	// Task store carries the tasks + task_steps tables (one SQLite file,
	// both stores share the same DB as in production).
	dir := t.TempDir()
	taskStore, err := task.NewStore(filepath.Join(dir, "tasks.db"), logger)
	if err != nil {
		t.Fatalf("task.NewStore: %v", err)
	}
	t.Cleanup(func() {
		if err := taskStore.Close(); err != nil {
			t.Errorf("close task store: %v", err)
		}
	})

	queueDB := filepath.Join(dir, "queue.db")
	pq, err := queue.NewPersistentQueue(queueDB, msgBus, logger)
	if err != nil {
		t.Fatalf("NewPersistentQueue: %v", err)
	}
	t.Cleanup(func() {
		if err := pq.Close(); err != nil {
			t.Errorf("close queue: %v", err)
		}
	})

	planStore, err := plan.NewSQLiteStore(filepath.Join(dir, "plans.db"), logger)
	if err != nil {
		t.Fatalf("plan.NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() {
		if err := planStore.Close(); err != nil {
			t.Errorf("close plan store: %v", err)
		}
	})

	plansCfg := planConfigForWiring(enabled)
	planMgr := plan.NewPlanManager(planStore, msgBus, plansCfg, nil, logger)

	stepStore := taskStore.StepStore()
	tactical := agent.NewTacticalScheduler(agent.TacticalSchedulerConfig{
		StepStore: stepStore,
		TaskStore: taskStore,
		Queue:     pq,
		Bus:       msgBus,
		Logger:    logger,
	})

	// Real orchestrator construction path (components.go) + the flag wiring
	// this leaf adds next to the SetPhaseTransitionHook site in daemon.go.
	orch := agent.NewOrchestrator(agent.OrchestratorDeps{
		Tactical:    tactical,
		PlanManager: planMgr,
		Bus:         msgBus,
		Logger:      logger,
		StepStore:   stepStore,
	})
	orch.SetParallelPhases(enabled)
	if provisioner != nil {
		orch.SetPhaseWorktreeProvisioner(provisioner)
	}

	f := &wiringFixture{
		t:         t,
		taskStore: taskStore,
		store:     stepStore,
		stub:      planStore,
		orch:      orch,
		tactical:  tactical,
		msgBus:    msgBus,
		pq:        pq,
		taskID:    "task-daemon-wiring",
		ctx:       ctx,
		cancel:    cancel,
	}
	orch.SetPhaseTransitionHook(func(taskID, from, to string) {
		f.hookMu.Lock()
		defer f.hookMu.Unlock()
		f.hookCalls = append(f.hookCalls, from+"->"+to)
		if f.transition != nil {
			f.transition(taskID, from, to)
		}
	})

	if err := orch.Start(ctx); err != nil {
		t.Fatalf("orchestrator.Start: %v", err)
	}
	t.Cleanup(func() {
		if err := orch.Stop(context.Background()); err != nil {
			t.Errorf("orchestrator.Stop: %v", err)
		}
	})
	time.Sleep(20 * time.Millisecond) // subscription goroutines spin up

	return f
}

// planConfigForWiring mirrors daemon.go reading plans.parallel_phases out of
// the loaded config (mode "off" keeps the plan system inert in tests).
func planConfigForWiring(parallel bool) config.PlansConfig {
	cfg := config.DefaultConfig().Plans
	cfg.Mode = "off"
	cfg.ParallelPhases = parallel
	return cfg
}

// seedPlan creates the plan + phases A (produces x) → (B, C consume x),
// registers task→plan, and seeds steps: A's two steps are dispatched;
// B/C/D's steps stay pending (they are phantom-gated, as in a real plan
// where the scheduler only queues dependency-free steps).
func (f *wiringFixture) seedPlan(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	const planID = "plan-daemon-wiring"
	now := time.Now().UTC()
	if err := f.stub.CreatePlan(ctx, &plan.Plan{
		ID: planID, Title: "wiring",
		State: plan.StatePlanning, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	phases := []*plan.PlanPhase{
		{ID: "pA", PlanID: planID, Name: "A", Sequence: 0, State: plan.PhasePending,
			Produces: []plan.Artifact{{Name: "x", Kind: "file", Description: "thing x"}}},
		// B/C consume x as OPTIONAL (best-effort, never blocks): this test
		// cannot reach the orchestrator's unexported artifact store to
		// record the producing artifact, and required consumes would
		// correctly gate the phase start. Optional consumes still create
		// the frontier edges (Contract A) without gating.
		{ID: "pB", PlanID: planID, Name: "B", Sequence: 1, State: plan.PhasePending,
			Consumes: []plan.Artifact{{Name: "x", Kind: "file"}}},
		{ID: "pC", PlanID: planID, Name: "C", Sequence: 2, State: plan.PhasePending,
			Consumes: []plan.Artifact{{Name: "x", Kind: "file"}}},
	}
	for _, p := range phases {
		if err := f.stub.CreatePhase(ctx, p); err != nil {
			t.Fatalf("CreatePhase %s: %v", p.Name, err)
		}
	}
	f.orch.PlanManager().RegisterTaskPlan(f.taskID, planID)

	tk := task.NewTask("daemon wiring fixture task", "daemon wiring fixture task")
	tk.ID = f.taskID
	if err := f.taskStore.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	// A's steps get enqueued step jobs (dispatched); B/C steps stay pending
	// with no job — exactly how a real plan looks while phase A runs.
	f.seedStep("wsA1", "A", 1)
	f.seedStep("wsA2", "A", 2)
	f.seedStep("wsB1", "B", 3)
	f.seedStep("wsC1", "C", 4)
	f.seedJob("wsA1", "job-wsA1")
	f.seedJob("wsA2", "job-wsA2")
}

// seedJob enqueues a step job for the given step through the REAL persistent
// queue, so handleJobCompleted's extractTaskIDFromJob → GetJobByID resolves
// task/step exactly as in production.
func (f *wiringFixture) seedJob(stepID, jobID string) {
	f.t.Helper()
	payload := map[string]any{
		"step_id":     stepID,
		"task_id":     f.taskID,
		"description": "step " + stepID,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		f.t.Fatalf("marshal job payload: %v", err)
	}
	job := &queue.Job{
		ID:      jobID,
		TaskID:  f.taskID,
		Type:    queue.JobTypeProjectTask,
		State:   queue.StateCompleted,
		Payload: payloadJSON,
	}
	if err := f.pq.Enqueue(f.ctx, job); err != nil {
		f.t.Fatalf("enqueue job %s: %v", jobID, err)
	}
}

// seedStep inserts one step (no job — jobs are attached separately by
// seedJob).
func (f *wiringFixture) seedStep(id, phase string, seq int) {
	f.t.Helper()
	step := task.NewTaskStep(f.taskID, "step "+id, seq)
	step.ID = id
	step.Phase = phase
	if err := f.store.Create(step); err != nil {
		f.t.Fatalf("create step %s: %v", id, err)
	}
}

// taskForWiring returns the task store used to seed the fixture task.
// The fixture task is created directly through the step store's DB-backed
// task store, accessed via the tactical scheduler's task store.
func (f *wiringFixture) completeJob(jobID string, result map[string]any) {
	f.t.Helper()
	payload, err := json.Marshal(result)
	if err != nil {
		f.t.Fatalf("marshal result: %v", err)
	}
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "wiring-test", map[string]any{
		"job_id": jobID,
		"result": json.RawMessage(payload),
	})
	if err != nil {
		f.t.Fatalf("NewBusMessage: %v", err)
	}
	f.msgBus.Publish("queue.job.completed", msg)
}

// waitUntil polls cond until it holds or the timeout elapses (bus delivery
// is asynchronous; no sleeps-where-a-channel-suffices, and no channel is
// exposed across the bus boundary — a bounded poll is the honest tool).
func waitUntil(t *testing.T, timeout time.Duration, cond func() bool, desc string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", desc)
}

// TestDaemonWiring_ParallelPhasesConfigReachesOrchestrator proves the config
// VALUE changes dispatch behavior through the real wiring: flag on → one
// queue.job.completed event for phase A's last step starts BOTH B and C
// (frontier); flag off → only B starts (serial next-in-list).
func TestDaemonWiring_ParallelPhasesConfigReachesOrchestrator(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		want    []string // hook calls after A completes, in sequence order
	}{
		{
			name:    "flag off: serial default starts only the next phase",
			enabled: false,
			want:    []string{"A->B"}, // serial: fromPhase = completed phase
		},
		{
			name:    "flag on: frontier starts all ready phases",
			enabled: true,
			want:    []string{"->B", "->C"}, // frontier: both activations fromPhase == ""
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newWiringFixture(t, tt.enabled, nil)
			f.seedPlan(t)

			// Mark A's steps completed through the store's public API, the
			// same persisted state a real step-job completion produces, then
			// fire ONE terminal event for A's last step.
			if err := f.store.SetState("wsA1", task.StepCompleted); err != nil {
				t.Fatalf("complete wsA1: %v", err)
			}
			if err := f.store.SetState("wsA2", task.StepCompleted); err != nil {
				t.Fatalf("complete wsA2: %v", err)
			}
			f.completeJob("job-wsA2", map[string]any{"success": true})

			waitUntil(t, 5*time.Second, func() bool {
				f.hookMu.Lock()
				defer f.hookMu.Unlock()
				return len(f.hookCalls) >= len(tt.want)
			}, "phase transition hook calls")

			f.hookMu.Lock()
			got := append([]string(nil), f.hookCalls...)
			f.hookMu.Unlock()
			if len(got) != len(tt.want) {
				t.Fatalf("hook calls = %v; want exactly %v", got, tt.want)
			}
			for i, want := range tt.want {
				if got[i] != want {
					t.Fatalf("hook calls = %v; want %v (call %d = %q, want %q)", got, tt.want, i, got[i], want)
				}
			}

			// Both branches: B's step is stamped with its phase-scoped
			// conversationID. C's is stamped ONLY under the frontier flag.
			b1, err := f.store.GetByID("wsB1")
			if err != nil || b1 == nil {
				t.Fatalf("get wsB1: %v", err)
			}
			if b1.ConversationID != "phase-pB-wsB1" {
				t.Errorf("wsB1 ConversationID = %q; want phase-pB-wsB1", b1.ConversationID)
			}
			c1, err := f.store.GetByID("wsC1")
			if err != nil || c1 == nil {
				t.Fatalf("get wsC1: %v", err)
			}
			if tt.enabled {
				if c1.ConversationID != "phase-pC-wsC1" {
					t.Errorf("flag on: wsC1 ConversationID = %q; want phase-pC-wsC1 (frontier)", c1.ConversationID)
				}
			} else if c1.ConversationID != "" {
				t.Errorf("flag off: wsC1 ConversationID = %q; want untouched (serial)", c1.ConversationID)
			}
		})
	}
}
