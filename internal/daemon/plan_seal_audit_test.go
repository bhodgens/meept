package daemon

// Daemon audit 2026-09-08 fixes — H1 (parked-turn job failure), H8 (tree-leaf
// annotation corrupting persisted phase names), M12 (treeLeafPathForPhase
// global-index aliasing), M13 (seal-pipeline staging cross-assignment).
//
// Contract for these tests:
//   - H1: a step job whose loop returns ("", nil) while parked on
//     StateQuotaWait must NOT fail the job — the processor surfaces a
//     provider-wait sentinel the worker's requeueOnProviderWait already
//     honors (requeue, no retry consumed, no failure), and a genuinely
//     empty response must keep the hard-failure defense.
//   - H8/M12: tree-mode seals persist CLEAN phase names and a sidecar
//     phase-index→leaf-path map that resolves each phase's FIRST leaf —
//     exercised through the real wirePlanSealHandler pipeline.
//   - M13: concurrent seal stagings stay isolated — A takes A's phases,
//     B takes B's phases.
//
// The seal-pipeline end-to-end tests ride the sealWiringFixture declared in
// plan_seal_wiring_test.go (leaf 04's integration harness) — same package,
// shared fixture, no duplicate construction.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/queue"
	pkgsecurity "github.com/caimlas/meept/pkg/security"
)

// ---------------------------------------------------------------------------
// H1: parked-turn ("", nil) must requeue, not fail
// ---------------------------------------------------------------------------

// emptyOKChatter returns a successful empty response — the shape a loop
// produces AFTER its internal park path short-circuits (chatWithFailoverRaw
// parks the turn and the reasoning cycle surfaces ("", nil)).
type emptyOKChatter struct{}

func (c *emptyOKChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	return &llm.Response{Content: ""}, nil
}

func (c *emptyOKChatter) ChatWithProgress(ctx context.Context, msgs []llm.ChatMessage, _ llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, msgs, opts...)
}

func (c *emptyOKChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "empty-ok"}
}

// throttleFirstChatter fails once with a ThrottleBackoffError, then answers —
// the real park trigger when a TurnParker is wired to the loop.
type throttleFirstChatter struct {
	mu     sync.Mutex
	calls  int
	retry  time.Time
	answer string
}

func (c *throttleFirstChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.calls == 1 {
		return nil, &llm.ThrottleBackoffError{
			ProviderID: "p1",
			ModelID:    "m1",
			RetryAt:    c.retry,
			Attempt:    0,
		}
	}
	return &llm.Response{Content: c.answer}, nil
}

func (c *throttleFirstChatter) ChatWithProgress(ctx context.Context, msgs []llm.ChatMessage, _ llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, msgs, opts...)
}

func (c *throttleFirstChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "throttle-first"}
}

// newParkedTurnTestLoop builds a loop over the given chatter with a parker
// wired (not started — no draining), tiny failure-policy steps so the
// scheduled resume time is observable.
func newParkedTurnTestLoop(t *testing.T, chatter llm.Chatter, parker *agent.TurnParker) *agent.AgentLoop {
	t.Helper()
	agent.SetFailurePolicyDefaults(llm.FailurePolicyConfig{
		Horizon:      time.Hour,
		BaseThrottle: 50 * time.Millisecond,
		PollFloor:    time.Minute,
	})
	t.Cleanup(func() { agent.SetFailurePolicyDefaults(llm.FailurePolicyConfig{}) })

	loop := agent.NewAgentLoop("sess-parked-job-test", t.TempDir(),
		agent.WithMessageBus(bus.New(nil, slog.New(slog.DiscardHandler))),
		agent.WithLLMChatter(chatter),
		agent.WithSecurityChecker(pkgsecurity.NewPermissionChecker(pkgsecurity.Config{})),
	)
	if parker != nil {
		loop.SetTurnParker(parker)
	}
	return loop
}

// TestAgentJobProcessor_ParkedTurnDoesNotFailJob (H1): a step job whose turn
// parks mid-flight (throttle park → ("", nil) at the processor) must surface
// the provider-wait sentinel — a *llm.QuotaResetError the worker's
// requeueOnProviderWait classifies — instead of the hard
// "agent execution produced empty response" failure. The state check rides
// the loop's own parked predicate: StateQuotaWait.
func TestAgentJobProcessor_ParkedTurnDoesNotFailJob(t *testing.T) {
	p, _, _ := newTestAgentJobProcessor(t)

	parker := agent.NewTurnParker(slog.New(slog.NewTextHandler(io.Discard, nil)), func(context.Context, agent.ParkedTurnRecord) {}, time.Hour)
	chatter := &throttleFirstChatter{retry: time.Now().Add(30 * time.Minute), answer: "late"}
	loop := newParkedTurnTestLoop(t, chatter, parker)
	p.agentLoop = loop

	job := &queue.Job{
		ID:      "job-parked-1",
		TaskID:  "task-parked-1",
		AgentID: "coder",
		Type:    queue.JobTypeProjectTask,
		Payload: stepJobPayload(t, "step-parked-1", "task-parked-1", "do the step work"),
	}
	_, perr := p.Process(context.Background(), job)
	if perr == nil {
		t.Fatal("Process = nil error, want the provider-wait sentinel (not success: no response was produced)")
	}
	if strings.Contains(perr.Error(), "agent execution produced empty response") {
		t.Fatalf("parked turn surfaced the hard empty-response failure: %v", perr)
	}
	var qe *llm.QuotaResetError
	if !errors.As(perr, &qe) {
		t.Fatalf("error %v is not a *llm.QuotaResetError — the worker's requeueOnProviderWait would NOT honor it", perr)
	}
	if qe.ResetAt.IsZero() || !qe.ResetAt.After(time.Now()) {
		t.Errorf("sentinel ResetAt = %v, want the parker's future resume time", qe.ResetAt)
	}
	wait := time.Until(qe.ResetAt)
	if wait < 28*time.Minute || wait > 31*time.Minute {
		t.Errorf("sentinel ResetAt in %v, want ~30m (the parker's schedule)", wait)
	}
	if loop.GetState() != agent.StateQuotaWait {
		t.Errorf("loop state = %v, want StateQuotaWait after the park", loop.GetState())
	}
	if parker.Pending() != 1 {
		t.Errorf("parker.Pending() = %d, want 1 (the parked turn)", parker.Pending())
	}
}

// TestAgentJobProcessor_ParkedTurnSentinelIsQuotaClass pins the fallback
// contract: if any consumer stringifies the sentinel before classification
// (the tactical OnJobFailed path receives a string), the text must still
// classify quota-class so the existing deferral machinery requeues instead
// of failing the step. The loop is parked through its REAL park path (a
// RunOnce turn hitting a ThrottleBackoffError on a wired parker).
func TestAgentJobProcessor_ParkedTurnSentinelIsQuotaClass(t *testing.T) {
	p, _, _ := newTestAgentJobProcessor(t)
	parker := agent.NewTurnParker(slog.New(slog.NewTextHandler(io.Discard, nil)), func(context.Context, agent.ParkedTurnRecord) {}, time.Hour)
	chatter := &throttleFirstChatter{retry: time.Now().Add(30 * time.Minute), answer: "late"}
	loop := newParkedTurnTestLoop(t, chatter, parker)

	// Park a turn through the loop's own machinery — the helper must react
	// to the state, not to how the park happened.
	if _, err := loop.RunOnce(context.Background(), "hello", "conv-parked-sentinel"); err != nil {
		t.Fatalf("parked RunOnce surfaced an error: %v", err)
	}
	if loop.GetState() != agent.StateQuotaWait {
		t.Fatalf("loop state = %v, want StateQuotaWait after the park", loop.GetState())
	}
	p.agentLoop = loop

	job := &queue.Job{ID: "job-parked-2", AgentID: "coder", Type: queue.JobTypeProjectTask,
		Payload: stepJobPayload(t, "step-parked-2", "task-parked-2", "work")}
	sentinel := p.parkedTurnQuotaError(loop, job)
	if sentinel == nil {
		t.Fatal("parkedTurnQuotaError = nil in StateQuotaWait, want the sentinel")
	}
	if got := sentinel.Error(); !strings.Contains(got, "quota limit exceeded") {
		t.Errorf("sentinel text %q lacks the quota-class fingerprint isQuotaClassFailure matches", got)
	}
}

// TestParkedTurnQuotaError_NilLoopNilWhenIdle pins the helper's guards. An
// idle loop yields nil, so Process falls through to the legacy hard
// empty-response failure — the defense the loop's own watchdog normally
// makes unreachable (it terminates gracefully rather than returning a
// truly-empty non-parked turn); the branch stays as defense-in-depth.
func TestParkedTurnQuotaError_NilLoopNilWhenIdle(t *testing.T) {
	p, _, _ := newTestAgentJobProcessor(t)
	if err := p.parkedTurnQuotaError(nil, &queue.Job{ID: "j"}); err != nil {
		t.Errorf("nil loop: parkedTurnQuotaError = %v, want nil", err)
	}
	idle := newParkedTurnTestLoop(t, &emptyOKChatter{}, nil)
	if err := p.parkedTurnQuotaError(idle, &queue.Job{ID: "j"}); err != nil {
		t.Errorf("idle loop: parkedTurnQuotaError = %v, want nil (empty response must keep failing hard)", err)
	}
}

// ---------------------------------------------------------------------------
// H8 + M12: clean persisted phase names; sidecar resolves the right leaf
// ---------------------------------------------------------------------------

// sealableTreeModeDraft is a sealable draft whose phase 1 carries 7 steps —
// above the 3-leaf-per-phase sizing, so the tree gate fires AND phase 1
// splits into 3 leaves while phase 2 stays at 1. That is exactly the shape
// M12's global-index aliasing corrupted: phase 1 must resolve to leaf
// "04-*.md" (its FIRST leaf), not leaf "02-*.md" (global index 1).
const sealableTreeModeDraft = `# Plan: Billing CSV export

## Meta

- task_id: task-seal-wiring-tree
- version: 1
- status: draft
- updated: 2026-09-08

## Goal

Export monthly billing summaries as CSV for finance review.

## Decisions

- Decision: CSV over the wire — Rationale: finance tooling ingests CSV directly.

## Open Questions

## Phases

### Phase 1: Extract billing rows

Pull the billing-period rows from the ledger store.

**Produces:**

- ` + "`billing-rows`" + ` (schema) — normalized ledger rows for one billing period

**Consumes:** none

**Steps:**
1. Open the ledger store read handle [code]
2. Query the ledger store for the billing period [code]
3. Validate row completeness flags [code]
4. Normalize rows into the billing-rows schema [code]
5. Drop duplicate row keys [code]
6. Sort rows by posting date [code]
7. Stamp the export batch id on every row [code]

### Phase 2: CSV renderer

Render billing rows as CSV for download.

**Produces:**

- ` + "`csv-renderer`" + ` (interface) — streaming CSV response writer

**Consumes:**

- ` + "`billing-rows`" + ` (schema) — the normalized rows

**Steps:**
1. Implement the CSV streaming writer [code] (needs: billing-rows)
`

// TestPlanSealWiring_TreeModeCleanNamesAndSidecar (H8+M12): a tree-mode seal
// through the REAL pipeline persists CLEAN phase names (joinable to step
// .Phase values), writes the sidecar phase→leaf map, and the sidecar maps
// each phase to its FIRST leaf — phase 1 of a 3-leaf phase resolves to leaf
// index 3, not the global-index-1 alias the old code produced.
func TestPlanSealWiring_TreeModeCleanNamesAndSidecar(t *testing.T) {
	f := newSealWiringFixture(t, true)
	f.taskID = "task-seal-wiring-tree"
	f.seedSealTask()

	if err := f.sp.SaveDraft(f.taskID, "placeholder"); err != nil {
		t.Fatalf("SaveDraft placeholder: %v", err)
	}
	draft := sealableTreeModeDraft
	if _, err := f.callDraft(f.taskID, &draft); err != nil {
		t.Fatalf("plan.draft save: %v", err)
	}
	sealRes, err := f.callSeal(f.taskID)
	if err != nil {
		t.Fatalf("plan.seal: %v", err)
	}
	if sealRes["mode"] != "tree" {
		t.Fatalf("mode = %v, want tree (phase 1 has 7 steps > 3-leaf cap)", sealRes["mode"])
	}

	// Tree files + sidecar exist on disk.
	dir := filepath.Join(f.treeRoot, f.taskID)
	for _, name := range []string{"master.md", phaseLeafSidecarName} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("expected %s after a tree-mode seal: %v", name, err)
		}
	}

	// H8: persisted phase names are CLEAN and joinable to the steps' phase.
	phases, err := f.planMgr.GetPhasesByTask(context.Background(), f.taskID)
	if err != nil {
		t.Fatalf("GetPhasesByTask: %v", err)
	}
	if len(phases) != 2 {
		t.Fatalf("persisted phases = %d, want 2", len(phases))
	}
	if phases[0].Name != "Extract billing rows" || phases[1].Name != "CSV renderer" {
		t.Errorf("persisted phase names = [%q, %q], want clean names without any [tree leaf: …] annotation",
			phases[0].Name, phases[1].Name)
	}
	steps, err := f.stepStore.ListByTaskID(f.taskID)
	if err != nil {
		t.Fatalf("ListByTaskID: %v", err)
	}
	stepPhases := map[string]bool{}
	for _, s := range steps {
		stepPhases[s.Phase] = true
	}
	if !stepPhases[phases[0].Name] || !stepPhases[phases[1].Name] {
		t.Errorf("orchestrator name-join broken: persisted phases %v vs step phases %v",
			[]string{phases[0].Name, phases[1].Name}, stepPhases)
	}

	// M12: sidecar maps each phase to its FIRST leaf. Phase 1 splits into
	// leaves 01–03, so phase 0 → "01-*" and phase 1 → "04-*". The old
	// global-index bug handed phase 1 leaf "02-*" (phase 0's SECOND leaf).
	sidecar, err := readPhaseLeafSidecar(f.treeRoot, f.taskID)
	if err != nil {
		t.Fatalf("readPhaseLeafSidecar: %v", err)
	}
	if sidecar == nil {
		t.Fatal("phase-leaf sidecar missing after a tree-mode seal")
	}
	if got := sidecar["0"]; !strings.HasPrefix(got, "01-") {
		t.Errorf("sidecar[0] = %q, want the phase-1 FIRST leaf 01-*.md", got)
	}
	if got := sidecar["1"]; !strings.HasPrefix(got, "04-") {
		t.Errorf("sidecar[1] = %q, want 04-*.md (global-index aliasing would give the wrong leaf 02-*.md)", got)
	}

	// The helper agrees with the sidecar when fed the compiled specs —
	// recompile the same draft to rebuild the spec list deterministically.
	cp, err := plan.CompileSealed(draft, 12)
	if err != nil {
		t.Fatalf("recompile for helper check: %v", err)
	}
	tree := treeFromDisk(t, dir)
	for i := range cp.Phases {
		if want, got := sidecar[fmt.Sprintf("%d", i)], treeLeafPathForPhase(tree, cp.Phases, i); want != got {
			t.Errorf("treeLeafPathForPhase(phase %d) = %q, sidecar says %q", i, got, want)
		}
	}
}

// treeFromDisk reconstructs a minimal EmittedTree handle from the sealed
// tree dir for helper cross-checks (only Leaves ordering matters). ReadDir
// sorts lexically — the emitter's zero-padded numbering keeps that
// identical to emission order.
func treeFromDisk(t *testing.T, dir string) *plan.EmittedTree {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read tree dir: %v", err)
	}
	tree := &plan.EmittedTree{}
	for _, e := range entries {
		name := e.Name()
		if name == "master.md" || name == phaseLeafSidecarName {
			continue
		}
		if strings.HasSuffix(name, ".md") {
			tree.Leaves = append(tree.Leaves, plan.LeafFile{Path: name})
		}
	}
	return tree
}

// TestTreeLeafPathForPhase_FirstLeafOfMultiLeafPhase (M12 unit lock): the
// counts-based resolution picks each phase's first leaf across split
// boundaries, including a 2-phase tree where phase 0 owns 2 leaves.
func TestTreeLeafPathForPhase_FirstLeafOfMultiLeafPhase(t *testing.T) {
	specs := []plan.PhaseSpec{
		{Name: "p0", Steps: []plan.StepSpec{{Description: "a"}, {Description: "b"}, {Description: "c"}, {Description: "d"}}},
		{Name: "p1", Steps: []plan.StepSpec{{Description: "e"}}},
	}
	tree := &plan.EmittedTree{Leaves: []plan.LeafFile{
		{Path: "01-a.md"}, {Path: "02-b.md"}, {Path: "03-e.md"},
	}}
	if got := treeLeafPathForPhase(tree, specs, 0); got != "01-a.md" {
		t.Errorf("phase 0 leaf = %q, want its first leaf 01-a.md", got)
	}
	if got := treeLeafPathForPhase(tree, specs, 1); got != "03-e.md" {
		t.Errorf("phase 1 leaf = %q, want 03-e.md (global index 1 would alias 02-b.md)", got)
	}
	if got := treeLeafPathForPhase(tree, specs, 5); got != "" {
		t.Errorf("out-of-range phase = %q, want empty", got)
	}
	if got := treeLeafPathForPhase(nil, specs, 0); got != "" {
		t.Errorf("nil tree = %q, want empty", got)
	}
}

// TestTreeLeavesPerPhase_MatchesEmitterPartition pins the faithful-mirror
// assumption the counts-based resolution rests on: for a spread of phase
// shapes, the mirrored per-phase leaf counts match the real emitter's
// partition.
func TestTreeLeavesPerPhase_MatchesEmitterPartition(t *testing.T) {
	cases := []struct {
		name       string
		stepCounts []int
	}{
		{"single small phase", []int{2}},
		{"two phases at cap", []int{3, 3}},
		{"split phase + single", []int{7, 1}},
		{"boundary cap", []int{4, 4}},
		{"three splits", []int{10, 9, 5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var specs []plan.PhaseSpec
			for _, n := range tc.stepCounts {
				phase := plan.PhaseSpec{Name: fmt.Sprintf("phase-%d", n)}
				for i := 0; i < n; i++ {
					phase.Steps = append(phase.Steps, plan.StepSpec{Description: fmt.Sprintf("step %d", i)})
				}
				specs = append(specs, phase)
			}
			cp := &plan.CompiledPlan{Phases: specs}
			tree, err := plan.EmitTree(cp, plan.TreeEmitOptions{})
			if err != nil {
				t.Fatalf("EmitTree: %v", err)
			}
			// The emitter's real per-phase leaf counts, derived by scanning
			// each leaf's rendered Scope line ("Scope: Phase N (…), steps …").
			emitted := make([]int, len(specs))
			for _, leaf := range tree.Leaves {
				content := leafContent(t, tree, leaf.Path)
				var phaseNo int
				parsed := false
				for _, line := range strings.Split(content, "\n") {
					if !strings.HasPrefix(line, "- **Scope:** Phase ") {
						continue
					}
					if _, err := fmt.Sscanf(line, "- **Scope:** Phase %d", &phaseNo); err != nil {
						t.Fatalf("parse scope line %q: %v", line, err)
					}
					parsed = true
					break
				}
				if !parsed {
					t.Fatalf("no Scope line in %s", leaf.Path)
				}
				emitted[phaseNo-1]++
			}
			mirrored := treeLeavesPerPhase(specs, 0)
			for i := range specs {
				if emitted[i] != mirrored[i] {
					t.Fatalf("phase %d: emitter=%d mirror=%d — the counts mirror drifted from partitionPhases", i, emitted[i], mirrored[i])
				}
			}
		})
	}
}

// leafContent returns a leaf's rendered content from the tree.
func leafContent(t *testing.T, tree *plan.EmittedTree, path string) string {
	t.Helper()
	for _, l := range tree.Leaves {
		if l.Path == path {
			return l.Content
		}
	}
	t.Fatalf("leaf %s not in tree", path)
	return ""
}

// ---------------------------------------------------------------------------
// M13: concurrent seal staging isolation
// ---------------------------------------------------------------------------

// TestSealPipelineState_TaskKeyedStagingIsolation (M13): seal A stages, seal
// B stages, then A executes and gets A's phases (the shared-slot version
// handed A whatever was staged last).
func TestSealPipelineState_TaskKeyedStagingIsolation(t *testing.T) {
	state := &sealPipelineState{}
	state.store("task-A", []agent.PlanPhaseSpec{{Name: "A-one"}, {Name: "A-two"}})
	state.store("task-B", []agent.PlanPhaseSpec{{Name: "B-one"}})

	got := state.take("task-A")
	if len(got) != 2 || got[0].Name != "A-one" || got[1].Name != "A-two" {
		t.Fatalf("task-A took %+v, want its own two phases", got)
	}
	// B unaffected by A's take.
	if got := state.take("task-B"); len(got) != 1 || got[0].Name != "B-one" {
		t.Fatalf("task-B took %+v, want its own phase", got)
	}
	// Take is destructive: nothing staged remains.
	if got := state.take("task-A"); got != nil {
		t.Errorf("second take(task-A) = %+v, want nil (one-shot staging)", got)
	}
	if state.take("missing") != nil {
		t.Error("take on unknown task = non-nil, want nil")
	}
}

// TestSealPipelineState_ConcurrentStaging hammers the map under concurrency
// (the mutex + per-task keying must hold without lost updates).
func TestSealPipelineState_ConcurrentStaging(t *testing.T) {
	state := &sealPipelineState{}
	const tasks = 16
	var wg sync.WaitGroup
	for i := 0; i < tasks; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := fmt.Sprintf("task-%d", n)
			state.store(id, []agent.PlanPhaseSpec{{Name: id}})
			got := state.take(id)
			if len(got) != 1 || got[0].Name != id {
				t.Errorf("task %d took %v, want its own phase", n, got)
			}
		}(i)
	}
	wg.Wait()
	if got := state.take("task-0"); got != nil {
		t.Errorf("leftover staging after all takes: %+v — map leaked an entry", got)
	}
}
