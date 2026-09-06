package agent

// Tests for phase-frontier parallel dispatch (phase-frontier-parallel
// leaf 02, Contract B).
//
// TestAdvancePhases_SerialEquivalence is the Task 1 safety net: with the
// parallelPhases flag off (the zero value), maybeTransitionPhase must
// reproduce the legacy serial behavior exactly — one ACTIVE phase at a
// time, next-in-list-order selection, and the unchanged
// phase-<phaseID>-<stepID> conversationID format.
//
// TestAdvancePhases_ParallelFrontier is the Task 3 table: flag-on frontier
// dispatch starts all ready phases from one trigger, passes fromPhase ==
// "" on frontier activations, keeps the artifact gate, falls back to the
// legacy list-order pick on a detected cycle, and survives concurrent
// terminal events without double-starting a phase.

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/task"
)

// newEquivalenceStep seeds one step and fails the test on store errors.
func newEquivalenceStep(t *testing.T, store *task.StepStore, taskID, id, phase string, seq int) {
	t.Helper()
	step := task.NewTaskStep(taskID, "step "+id, seq)
	step.ID = id
	step.Phase = phase
	if err := store.Create(step); err != nil {
		t.Fatalf("create step %s: %v", id, err)
	}
}

// completeEquivalenceStep marks a step completed through the store's public
// API, the same persist path a job completion takes.
func completeEquivalenceStep(t *testing.T, store *task.StepStore, id string) {
	t.Helper()
	if err := store.SetState(id, task.StepCompleted); err != nil {
		t.Fatalf("complete step %s: %v", id, err)
	}
}

// stepOf re-reads a step so assertions observe persisted state, not the
// seeded in-memory copy.
func stepOf(t *testing.T, store *task.StepStore, id string) *task.TaskStep {
	t.Helper()
	got, err := store.GetByID(id)
	if err != nil {
		t.Fatalf("get step %s: %v", id, err)
	}
	return got
}

// activePhaseCount counts phases that have been started (at least one step
// stamped with a conversationID) but are not yet fully successfully
// terminal — the "ACTIVE phase" observable for flag-off equivalence.
func activePhaseCount(t *testing.T, store *task.StepStore, taskID string, phaseNames []string) int {
	t.Helper()
	active := 0
	for _, name := range phaseNames {
		steps, err := store.GetPhaseSteps(taskID, name)
		if err != nil {
			t.Fatalf("get phase steps for %s: %v", name, err)
		}
		started, allTerminal := false, len(steps) > 0
		for _, s := range steps {
			if s.ConversationID != "" {
				started = true
			}
			if !s.State.IsSuccessfullyTerminal() {
				allTerminal = false
			}
		}
		if started && !allTerminal {
			active++
		}
	}
	return active
}

// TestAdvancePhases_SerialEquivalence pins flag-off behavior to today's
// serial implementation: exactly one active phase at every observation
// point, list-order next-phase selection, and the exact
// phase-<phaseID>-<stepID> conversationID format.
func TestAdvancePhases_SerialEquivalence(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		taskID   string
		planID   string
		register bool
		phases   []plan.PlanPhase
		run      func(t *testing.T, o *Orchestrator, store *task.StepStore)
	}{
		{
			name:     "linear three phases advance in list order",
			taskID:   "task-eqv-linear",
			planID:   "plan-eqv-linear",
			register: true,
			phases: []plan.PlanPhase{
				{ID: "p1", PlanID: "plan-eqv-linear", Name: "One", Sequence: 0, State: plan.PhasePending},
				{ID: "p2", PlanID: "plan-eqv-linear", Name: "Two", Sequence: 1, State: plan.PhasePending},
				{ID: "p3", PlanID: "plan-eqv-linear", Name: "Three", Sequence: 2, State: plan.PhasePending},
			},
			run: func(t *testing.T, o *Orchestrator, store *task.StepStore) {
				const taskID = "task-eqv-linear"
				newEquivalenceStep(t, store, taskID, "s1", "One", 1)
				newEquivalenceStep(t, store, taskID, "s2", "Two", 2)
				newEquivalenceStep(t, store, taskID, "s3", "Three", 3)
				names := []string{"One", "Two", "Three"}

				// Phase One completes -> Two starts. List order: never Three.
				completeEquivalenceStep(t, store, "s1")
				o.maybeTransitionPhase(ctx, "s1", taskID)
				if got := stepOf(t, store, "s2").ConversationID; got != "phase-p2-s2" {
					t.Errorf("s2 ConversationID = %q; want %q", got, "phase-p2-s2")
				}
				if got := stepOf(t, store, "s2").AccumulatedContext; !strings.Contains(got, "Two") {
					t.Errorf("s2 AccumulatedContext missing phase name; got %q", got)
				}
				if got := stepOf(t, store, "s1").ConversationID; got != "" {
					t.Errorf("s1 ConversationID = %q; want untouched", got)
				}
				if got := stepOf(t, store, "s3").ConversationID; got != "" {
					t.Errorf("s3 ConversationID = %q; want untouched", got)
				}
				if n := activePhaseCount(t, store, taskID, names); n != 1 {
					t.Errorf("active phases = %d; want 1", n)
				}

				// Phase Two completes -> Three starts.
				completeEquivalenceStep(t, store, "s2")
				o.maybeTransitionPhase(ctx, "s2", taskID)
				if got := stepOf(t, store, "s3").ConversationID; got != "phase-p3-s3" {
					t.Errorf("s3 ConversationID = %q; want %q", got, "phase-p3-s3")
				}
				if n := activePhaseCount(t, store, taskID, names); n != 1 {
					t.Errorf("active phases after second transition = %d; want 1", n)
				}

				// Phase Three completes -> no next phase; nothing changes.
				completeEquivalenceStep(t, store, "s3")
				o.maybeTransitionPhase(ctx, "s3", taskID)
				if n := activePhaseCount(t, store, taskID, names); n != 0 {
					t.Errorf("active phases after final transition = %d; want 0", n)
				}
			},
		},
		{
			name:     "phase with multiple steps gates on full completion",
			taskID:   "task-eqv-multistep",
			planID:   "plan-eqv-multistep",
			register: true,
			phases: []plan.PlanPhase{
				{ID: "p1", PlanID: "plan-eqv-multistep", Name: "One", Sequence: 0, State: plan.PhasePending},
				{ID: "p2", PlanID: "plan-eqv-multistep", Name: "Two", Sequence: 1, State: plan.PhasePending},
			},
			run: func(t *testing.T, o *Orchestrator, store *task.StepStore) {
				const taskID = "task-eqv-multistep"
				newEquivalenceStep(t, store, taskID, "s1a", "One", 1)
				newEquivalenceStep(t, store, taskID, "s1b", "One", 2)
				newEquivalenceStep(t, store, taskID, "s2", "Two", 3)
				names := []string{"One", "Two"}

				// One of two steps done: phase incomplete -> no transition.
				completeEquivalenceStep(t, store, "s1a")
				o.maybeTransitionPhase(ctx, "s1a", taskID)
				if got := stepOf(t, store, "s2").ConversationID; got != "" {
					t.Errorf("s2 ConversationID = %q; want untouched (phase incomplete)", got)
				}
				if n := activePhaseCount(t, store, taskID, names); n != 0 {
					t.Errorf("active phases = %d; want 0", n)
				}

				// Last step of One completes -> Two starts.
				completeEquivalenceStep(t, store, "s1b")
				o.maybeTransitionPhase(ctx, "s1b", taskID)
				if got := stepOf(t, store, "s2").ConversationID; got != "phase-p2-s2" {
					t.Errorf("s2 ConversationID = %q; want %q", got, "phase-p2-s2")
				}
				if n := activePhaseCount(t, store, taskID, names); n != 1 {
					t.Errorf("active phases = %d; want 1", n)
				}
			},
		},
		{
			name:     "single phase plan is a no-op",
			taskID:   "task-eqv-single",
			planID:   "plan-eqv-single",
			register: true,
			phases: []plan.PlanPhase{
				{ID: "p1", PlanID: "plan-eqv-single", Name: "Only", Sequence: 0, State: plan.PhasePending},
			},
			run: func(t *testing.T, o *Orchestrator, store *task.StepStore) {
				const taskID = "task-eqv-single"
				newEquivalenceStep(t, store, taskID, "s1", "Only", 1)
				completeEquivalenceStep(t, store, "s1")
				// No next phase: safe no-op, no panics, nothing stamped.
				o.maybeTransitionPhase(ctx, "s1", taskID)
				if got := stepOf(t, store, "s1").ConversationID; got != "" {
					t.Errorf("s1 ConversationID = %q; want untouched", got)
				}
			},
		},
		{
			name:     "task with no plan is a no-op",
			taskID:   "task-eqv-noplan",
			register: false,
			run: func(t *testing.T, o *Orchestrator, store *task.StepStore) {
				const taskID = "task-eqv-noplan"
				newEquivalenceStep(t, store, taskID, "s1", "Ghost", 1)
				completeEquivalenceStep(t, store, "s1")
				// GetPhasesByTask returns (nil, nil): safe no-op.
				o.maybeTransitionPhase(ctx, "s1", taskID)
				if got := stepOf(t, store, "s1").ConversationID; got != "" {
					t.Errorf("s1 ConversationID = %q; want untouched", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, store, stub := newTestOrchestrator(t)
			for i := range tt.phases {
				if err := stub.CreatePhase(ctx, &tt.phases[i]); err != nil {
					t.Fatalf("CreatePhase %s: %v", tt.phases[i].Name, err)
				}
			}
			if tt.register {
				o.planManager.RegisterTaskPlan(tt.taskID, tt.planID)
			}
			tt.run(t, o, store)
		})
	}
}

// ---- parallel-frontier fixtures (Task 3) ----

// newFrontierPhase seeds one plan phase for the frontier fixtures.
func newFrontierPhase(t *testing.T, stub *stubPlanStore, ctx context.Context, planID, id, name string, seq int, produces, consumes []plan.Artifact) {
	t.Helper()
	p := &plan.PlanPhase{ID: id, PlanID: planID, Name: name, Sequence: seq, State: plan.PhasePending}
	p.Produces = produces
	p.Consumes = consumes
	if err := stub.CreatePhase(ctx, p); err != nil {
		t.Fatalf("CreatePhase %s: %v", name, err)
	}
}

// newFrontierStep seeds one task step for the frontier fixtures.
func newFrontierStep(t *testing.T, store *task.StepStore, taskID, id, phase string, seq int) {
	t.Helper()
	step := task.NewTaskStep(taskID, "step "+id, seq)
	step.ID = id
	step.Phase = phase
	if err := store.Create(step); err != nil {
		t.Fatalf("create step %s: %v", id, err)
	}
}

// phaseStepIDs returns the step IDs the frontier fixture seeded for a phase.
func phaseStepIDs(phase string) []string {
	switch phase {
	case "A":
		return []string{"fsA1", "fsA2"}
	case "B":
		return []string{"fsB1"}
	case "C":
		return []string{"fsC1"}
	case "D":
		return []string{"fsD1"}
	default:
		return nil
	}
}

// frontierFixtures is the shared fixture for the parallel-frontier table:
// phases A (produces x), B and C (require x), D (cross-phase step dependency
// into B); steps are pending everywhere except where a case completes them.
type frontierFixtures struct {
	o     *Orchestrator
	store *task.StepStore
}

func newFrontierFixtures(t *testing.T, ctx context.Context, taskID string) *frontierFixtures {
	t.Helper()
	o, store, stub := newTestOrchestrator(t)
	newFrontierPhase(t, stub, ctx, "plan-frontier", "pA", "A", 0,
		[]plan.Artifact{{Name: "x", Kind: "file", Description: "thing x"}}, nil)
	newFrontierPhase(t, stub, ctx, "plan-frontier", "pB", "B", 1, nil,
		[]plan.Artifact{{Name: "x", Kind: "file", Required: true}})
	newFrontierPhase(t, stub, ctx, "plan-frontier", "pC", "C", 2, nil,
		[]plan.Artifact{{Name: "x", Kind: "file", Required: true}})
	newFrontierPhase(t, stub, ctx, "plan-frontier", "pD", "D", 3, nil, nil)
	o.planManager.RegisterTaskPlan(taskID, "plan-frontier")
	newFrontierStep(t, store, taskID, "fsA1", "A", 1)
	newFrontierStep(t, store, taskID, "fsA2", "A", 2)
	newFrontierStep(t, store, taskID, "fsB1", "B", 3)
	newFrontierStep(t, store, taskID, "fsC1", "C", 4)
	newFrontierStep(t, store, taskID, "fsD1", "D", 5)
	// The D->B dependency edge lives on D's step, persisted through the
	// store's public API (same shape the strategic planner writes).
	d := stepOf(t, store, "fsD1")
	d.DependsOn = []string{"fsB1"}
	if err := store.UpdatePhaseSteps([]*task.TaskStep{d}); err != nil {
		t.Fatalf("persist D dependsOn: %v", err)
	}
	return &frontierFixtures{o: o, store: store}
}

// assertPhaseUntouched verifies a phase was never started: every step has an
// empty conversationID and no accumulated context.
func assertPhaseUntouched(t *testing.T, store *task.StepStore, phase string) {
	t.Helper()
	for _, id := range phaseStepIDs(phase) {
		s := stepOf(t, store, id)
		if s.ConversationID != "" {
			t.Errorf("phase %s step %s ConversationID = %q; want untouched", phase, id, s.ConversationID)
		}
		if s.AccumulatedContext != "" {
			t.Errorf("phase %s step %s has AccumulatedContext; want untouched", phase, id)
		}
	}
}

// TestAdvancePhases_ParallelFrontier covers Contract B flag-on dispatch:
// sibling phases start together from one trigger, fromPhase == "" on
// frontier activations, the artifact gate still applies, cycles fall back
// to the legacy list-order pick, and concurrent terminal events cannot
// double-start a phase.
func TestAdvancePhases_ParallelFrontier(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		taskID  string
		fixture func(t *testing.T, ctx context.Context, taskID string) *frontierFixtures
		run     func(t *testing.T, f *frontierFixtures, taskID string)
	}{
		{
			name:   "frontier starts both siblings, gated dependent stays pending",
			taskID: "task-frontier-both",
			run: func(t *testing.T, f *frontierFixtures, taskID string) {
				f.o.SetParallelPhases(true)
				// A produced its artifact through the store's public API,
				// as artifact-producing steps do.
				f.o.artifacts.Add(Artifact{Name: "x", Kind: "file", Description: "thing x"}, "fsA2")

				// One trigger: A's LAST step terminal (both A steps done so
				// A counts as complete).
				completeEquivalenceStep(t, f.store, "fsA1")
				completeEquivalenceStep(t, f.store, "fsA2")
				f.o.maybeTransitionPhase(ctx, "fsA2", taskID)

				// BOTH siblings B and C got fresh conversationIDs + startup
				// context from that ONE call; D did not.
				for _, id := range []string{"fsB1", "fsC1"} {
					s := stepOf(t, f.store, id)
					want := "phase-pB-" + id
					if id == "fsC1" {
						want = "phase-pC-" + id
					}
					if s.ConversationID != want {
						t.Errorf("%s ConversationID = %q; want %q", id, s.ConversationID, want)
					}
					if !strings.Contains(s.AccumulatedContext, "x") {
						t.Errorf("%s AccumulatedContext missing consumed artifact x; got %q", id, s.AccumulatedContext)
					}
				}
				assertPhaseUntouched(t, f.store, "D")

				// D starts only after B completes (its dependency edge).
				completeEquivalenceStep(t, f.store, "fsB1")
				f.o.maybeTransitionPhase(ctx, "fsB1", taskID)
				s := stepOf(t, f.store, "fsD1")
				if s.ConversationID != "phase-pD-fsD1" {
					t.Errorf("fsD1 ConversationID = %q; want phase-pD-fsD1", s.ConversationID)
				}
			},
		},
		{
			name:   "fromPhase empty on frontier starts",
			taskID: "task-frontier-hook",
			run: func(t *testing.T, f *frontierFixtures, taskID string) {
				f.o.SetParallelPhases(true)
				f.o.artifacts.Add(Artifact{Name: "x", Kind: "file"}, "fsA2")
				var mu sync.Mutex
				var calls []string // "fromPhase->toPhase"
				f.o.SetPhaseTransitionHook(func(taskID, fromPhase, toPhase string) {
					mu.Lock()
					defer mu.Unlock()
					calls = append(calls, fromPhase+"->"+toPhase)
				})

				completeEquivalenceStep(t, f.store, "fsA1")
				completeEquivalenceStep(t, f.store, "fsA2")
				f.o.maybeTransitionPhase(ctx, "fsA2", taskID)

				mu.Lock()
				defer mu.Unlock()
				// Exactly one hook call per started phase, fromPhase == ""
				// for both frontier activations.
				if len(calls) != 2 {
					t.Fatalf("hook calls = %v; want exactly 2 frontier activations", calls)
				}
				seen := map[string]bool{}
				for _, c := range calls {
					from, to, ok := strings.Cut(c, "->")
					if !ok {
						t.Fatalf("malformed call record %q", c)
					}
					if from != "" {
						t.Errorf("hook fromPhase = %q; want \"\" on frontier activation", from)
					}
					seen[to] = true
				}
				if !seen["B"] || !seen["C"] {
					t.Errorf("hook calls missing B or C: %v", calls)
				}
			},
		},
		{
			name:   "artifact gate still applies to frontier starts",
			taskID: "task-frontier-gate",
			run: func(t *testing.T, f *frontierFixtures, taskID string) {
				f.o.SetParallelPhases(true)
				// B additionally requires y (never produced): mutate the
				// persisted phase record the stub store hands back.
				phases, err := f.o.planManager.GetPhasesByTask(ctx, taskID)
				if err != nil {
					t.Fatalf("GetPhasesByTask: %v", err)
				}
				for _, p := range phases {
					if p.Name == "B" {
						p.Consumes = append(p.Consumes, plan.Artifact{Name: "y", Kind: "doc", Required: true})
					}
				}
				f.o.artifacts.Add(Artifact{Name: "x", Kind: "file"}, "fsA2")

				completeEquivalenceStep(t, f.store, "fsA1")
				completeEquivalenceStep(t, f.store, "fsA2")
				// Graph allows B (its only producer A is done and x is
				// available), but y is missing: B must NOT start; C must.
				// Warn-able condition, no fatal.
				f.o.maybeTransitionPhase(ctx, "fsA2", taskID)

				assertPhaseUntouched(t, f.store, "B")
				s := stepOf(t, f.store, "fsC1")
				if s.ConversationID != "phase-pC-fsC1" {
					t.Errorf("fsC1 ConversationID = %q; want phase-pC-fsC1 (C should start)", s.ConversationID)
				}
				assertPhaseUntouched(t, f.store, "D")
			},
		},
		{
			name:   "cycle fallback warns and starts first incomplete phase in list order",
			taskID: "task-frontier-cycle",
			fixture: func(t *testing.T, ctx context.Context, taskID string) *frontierFixtures {
				// Cycle graph: Alpha produces x and requires y from Beta;
				// Beta produces y and requires x from Alpha. Both artifacts
				// are present and both phases are busy, so each is blocked
				// ONLY by its dependency edge into the other — the frontier
				// is empty and cycleDetected fires. Gamma is the unrelated
				// completed phase that fires the advance.
				o, store, stub := newTestOrchestrator(t)
				newFrontierPhase(t, stub, ctx, "plan-cycle", "pAl", "Alpha", 0,
					[]plan.Artifact{{Name: "x", Kind: "file"}},
					[]plan.Artifact{{Name: "y", Kind: "doc", Required: true}})
				newFrontierPhase(t, stub, ctx, "plan-cycle", "pBe", "Beta", 1,
					[]plan.Artifact{{Name: "y", Kind: "doc"}},
					[]plan.Artifact{{Name: "x", Kind: "file", Required: true}})
				newFrontierPhase(t, stub, ctx, "plan-cycle", "pGa", "Gamma", 2, nil, nil)
				o.planManager.RegisterTaskPlan(taskID, "plan-cycle")
				newFrontierStep(t, store, taskID, "ca1", "Alpha", 1)
				newFrontierStep(t, store, taskID, "cb1", "Beta", 2)
				// Gamma's step is already terminal — the trigger.
				g := task.NewTaskStep(taskID, "step cg1", 3)
				g.ID = "cg1"
				g.Phase = "Gamma"
				g.State = task.StepCompleted
				if err := store.Create(g); err != nil {
					t.Fatalf("create cg1: %v", err)
				}
				// Both cycle artifacts produced, so readiness is blocked
				// purely by the busy-phase dependency edges.
				o.artifacts.Add(Artifact{Name: "x", Kind: "file"}, "ca1")
				o.artifacts.Add(Artifact{Name: "y", Kind: "doc"}, "cb1")
				return &frontierFixtures{o: o, store: store}
			},
			run: func(t *testing.T, f *frontierFixtures, taskID string) {
				f.o.SetParallelPhases(true)

				// Capture the orchestrator's log output so the Warn path is
				// asserted, not assumed.
				var buf bytes.Buffer
				f.o.logger = slog.New(slog.NewTextHandler(&buf, nil))

				var mu sync.Mutex
				var calls []string
				f.o.SetPhaseTransitionHook(func(taskID, fromPhase, toPhase string) {
					mu.Lock()
					defer mu.Unlock()
					calls = append(calls, fromPhase+"->"+toPhase)
				})

				// No panic expected anywhere in this flow.
				f.o.maybeTransitionPhase(ctx, "cg1", taskID)

				if out := buf.String(); !strings.Contains(out, "phase frontier stalled (cycle or unmet dependency)") {
					t.Errorf("expected cycle-fallback Warn in log; got: %s", out)
				}
				mu.Lock()
				defer mu.Unlock()
				// Anti-cycle escape started exactly ONE phase (list-order
				// first incomplete = Alpha), via a frontier-style start.
				if len(calls) != 1 || calls[0] != "->Alpha" {
					t.Fatalf("hook calls = %v; want exactly [\"->Alpha\"]", calls)
				}
				if got := stepOf(t, f.store, "ca1").ConversationID; got != "phase-pAl-ca1" {
					t.Errorf("ca1 ConversationID = %q; want phase-pAl-ca1 (fallback start)", got)
				}
				if got := stepOf(t, f.store, "cb1").ConversationID; got != "" {
					t.Errorf("cb1 ConversationID = %q; want untouched", got)
				}
			},
		},
		{
			name:   "concurrent terminal events start each phase once",
			taskID: "task-frontier-conc",
			run: func(t *testing.T, f *frontierFixtures, taskID string) {
				f.o.SetParallelPhases(true)
				f.o.artifacts.Add(Artifact{Name: "x", Kind: "file"}, "fsA2")
				completeEquivalenceStep(t, f.store, "fsA1")
				completeEquivalenceStep(t, f.store, "fsA2")

				// Two overlapping advancePhasesFrontier calls for the same
				// task (as duplicate terminal events would). The in-flight
				// guard must make the loser a no-op.
				//
				// Deterministic overlap: the phase-transition hook parks the
				// first advance (holding the guard); while it is parked, a
				// second advance runs on this goroutine and must be bounced
				// by the CompareAndSwap. No polling, no sleep races.
				inHook := make(chan struct{})
				release := make(chan struct{})
				var hookCalls atomic.Int32
				f.o.SetPhaseTransitionHook(func(taskID, fromPhase, toPhase string) {
					hookCalls.Add(1)
					select {
					case <-release:
						// Already released; proceed.
					default:
						close(inHook)
						<-release // park while the guard is held
					}
				})

				guardDone := make(chan struct{})
				go func() {
					defer close(guardDone)
					f.o.advancePhasesFrontier(ctx, taskID)
				}()
				// RED-safe: the stub advancePhasesFrontier never calls the
				// hook, so wait bounded rather than hanging forever.
				select {
				case <-inHook: // first caller holds the guard, parked in the hook
				case <-time.After(5 * time.Second):
					t.Fatal("advancePhasesFrontier never started a phase (in-flight guard or dispatch missing)")
				}

				// Second concurrent trigger: must be bounced by the CAS and
				// return without starting anything.
				f.o.advancePhasesFrontier(ctx, taskID)

				// First caller finishes.
				close(release)
				<-guardDone

				// Exactly one phase-start per phase: B and C each started
				// once (fromPhase == "" frontier activation); D never
				// started; A (all-terminal) was never restarted.
				if n := hookCalls.Load(); n != 2 {
					t.Errorf("hook calls = %d; want exactly 2 (B and C started once)", n)
				}
				for _, id := range []string{"fsB1", "fsC1"} {
					if s := stepOf(t, f.store, id); s.ConversationID == "" {
						t.Errorf("%s never started under concurrency", id)
					}
				}
				assertPhaseUntouched(t, f.store, "D")
				if s := stepOf(t, f.store, "fsA1"); s.ConversationID != "" {
					t.Errorf("fsA1 ConversationID = %q; want untouched (A already done)", s.ConversationID)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			build := tt.fixture
			if build == nil {
				build = newFrontierFixtures
			}
			f := build(t, ctx, tt.taskID)
			tt.run(t, f, tt.taskID)
		})
	}
}
