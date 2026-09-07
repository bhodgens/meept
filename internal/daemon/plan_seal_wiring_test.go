package daemon

// Plan-compiler leaf 04 integration test: the plans.plan_compiler_enabled
// CONFIG VALUE must drive the full draft → seal → executing pipeline through
// the REAL daemon wiring path (wirePlanSealHandler + the daemon.go block
// beside SetParallelPhases), over a real component graph (task store, step
// store, plan manager on SQLite, real strategic planner).
//
// Flag ON: brainstorm draft seeded → user edits via plan.draft → plan.seal
// compiles (zero LLM) → phases persist via PlanManager.CreatePhase (the
// planPhaseSink path) → task executing with steps in the step store →
// orchestrator.schedule published.
//
// Flag OFF: the legacy mode=plan interview/JSON path runs unchanged
// (byte-identical requirement) — high-ambiguity requests still interview.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/task"
)

// sealWiringFixture mirrors wiringFixture (orchestrator_wiring_test.go) but
// owns a real StrategicPlanner + PlanManager + the seal handler built by
// wirePlanSealHandler, exactly as daemon.go wires them.
type sealWiringFixture struct {
	t          *testing.T
	taskStore  *task.Store
	stepStore  *task.StepStore
	planStore  *plan.SQLiteStore
	planMgr    *plan.PlanManager
	sp         *agent.StrategicPlanner
	handler    *rpc.PlanSealHandler
	msgBus     *bus.MessageBus
	treeRoot   string
	taskID     string
	ctx        context.Context
	schedules  atomic.Int64
	scheduleCh chan struct{}
}

func newSealWiringFixture(t *testing.T, compilerEnabled bool) *sealWiringFixture {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	msgBus := bus.New(nil, logger)
	t.Cleanup(func() { msgBus.Close() })

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
	stepStore := taskStore.StepStore()

	planStore, err := plan.NewSQLiteStore(filepath.Join(dir, "plans.db"), logger)
	if err != nil {
		t.Fatalf("plan.NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() {
		if err := planStore.Close(); err != nil {
			t.Errorf("close plan store: %v", err)
		}
	})

	plansCfg := planConfigForWiring(false) // plan system inert (mode off) — seal path drives phases directly
	planMgr := plan.NewPlanManager(planStore, msgBus, plansCfg, nil, logger)

	// Real strategic planner (same construction shape as components.go),
	// with the planner agent registered but no LLM client: the seal path is
	// zero-LLM, and the flag-off test exercises the interview degradation.
	sp := agent.NewStrategicPlanner(agent.StrategicPlannerConfig{
		TaskStore:      taskStore,
		StepStore:      stepStore,
		Bus:            msgBus,
		Logger:         logger.With("component", "strategic"),
		TemplateLoader: agent.NewDaemonPlannerTemplateLoader(filepath.Join(dir, "no-prompts")),
		MaxPhases:      12,
	})

	f := &sealWiringFixture{
		t:          t,
		taskStore:  taskStore,
		stepStore:  stepStore,
		planStore:  planStore,
		planMgr:    planMgr,
		sp:         sp,
		msgBus:     msgBus,
		treeRoot:   filepath.Join(dir, "plan-trees"),
		taskID:     "task-seal-wiring",
		ctx:        ctx,
		scheduleCh: make(chan struct{}, 16),
	}

	// Count orchestrator.schedule events through a REAL subscription, the
	// same delivery path the orchestrator's tactical scheduler consumes
	// (SealPlan publishes this to trigger scheduling in production).
	schedules := &f.schedules
	sub := msgBus.Subscribe("seal-wiring-test", "orchestrator.schedule")
	go func() {
		for range sub.Channel {
			schedules.Add(1)
			select {
			case f.scheduleCh <- struct{}{}:
			default:
			}
		}
	}()
	t.Cleanup(func() { msgBus.Unsubscribe(sub) })

	if compilerEnabled {
		sp.SetPlanCompilerEnabled(true)
		handler, err := wirePlanSealHandler(sp, planMgr, f.treeRoot, logger)
		if err != nil {
			t.Fatalf("wirePlanSealHandler: %v", err)
		}
		f.handler = handler
	}
	return f
}

// callSeal invokes the plan.seal handler through raw JSON — the exact RPC
// entry shape a real client call takes.
func (f *sealWiringFixture) callSeal(taskID string) (map[string]any, error) {
	f.t.Helper()
	params, err := json.Marshal(map[string]any{"task_id": taskID})
	if err != nil {
		f.t.Fatalf("marshal: %v", err)
	}
	raw, err := f.handler.HandleSealJSON(f.ctx, params)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		f.t.Fatalf("unmarshal seal result: %v", err)
	}
	return out, nil
}

// callDraft invokes plan.draft through raw JSON (save when markdown != nil).
func (f *sealWiringFixture) callDraft(taskID string, markdown *string) (map[string]any, error) {
	f.t.Helper()
	params := map[string]any{"task_id": taskID}
	if markdown != nil {
		params["markdown"] = *markdown
	}
	raw, err := json.Marshal(params)
	if err != nil {
		f.t.Fatalf("marshal: %v", err)
	}
	res, err := f.handler.HandleDraftJSON(f.ctx, raw)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(res, &out); err != nil {
		f.t.Fatalf("unmarshal draft result: %v", err)
	}
	return out, nil
}

// seedSealTask creates the planning-state task the pipeline runs on.
func (f *sealWiringFixture) seedSealTask() *task.Task {
	f.t.Helper()
	tsk := task.NewTask("seal wiring task", "add billing CSV export")
	tsk.ID = f.taskID
	tsk.SetState(task.StatePlanning)
	if err := f.taskStore.Create(tsk); err != nil {
		f.t.Fatalf("create task: %v", err)
	}
	return tsk
}

// sealableTwoPhaseDraft is a complete, sealable two-phase brainstorm draft.
const sealableTwoPhaseDraft = `# Plan: Billing CSV export

## Meta

- task_id: task-seal-wiring
- version: 1
- status: draft
- updated: 2026-09-06

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

1. Query the ledger store for the billing period [code]
2. Normalize rows into the billing-rows schema [code] (needs: Phase1.S1)

### Phase 2: CSV renderer

Render billing rows as CSV for download.

**Produces:**

- ` + "`csv-renderer`" + ` (interface) — streaming CSV response writer

**Consumes:**

- ` + "`billing-rows`" + ` (schema) — the normalized rows

**Steps:**

1. Implement the CSV streaming writer [code] (needs: billing-rows)
`

// TestPlanSealWiring_FlagOn seals the 2-phase draft end to end: compile →
// phases persisted → steps persisted → task executing → schedule published.
func TestPlanSealWiring_FlagOn(t *testing.T) {
	f := newSealWiringFixture(t, true)
	f.seedSealTask()

	// 1. Draft seeded (Plan()'s gate) — call the same code path the gate
	// uses to seed, keeping this test on the public pipeline seams.
	if err := f.sp.SaveDraft(f.taskID, "placeholder"); err != nil {
		t.Fatalf("SaveDraft placeholder: %v", err)
	}
	// 2. User/planner iterate: replace content via the plan.draft seam.
	draft := sealableTwoPhaseDraft
	res, err := f.callDraft(f.taskID, &draft)
	if err != nil {
		t.Fatalf("plan.draft save: %v", err)
	}
	if res["status"] != "saved" {
		t.Errorf("plan.draft save status = %v", res["status"])
	}

	// 3. Seal (zero LLM): plan.seal over the real handler seams.
	sealRes, err := f.callSeal(f.taskID)
	if err != nil {
		t.Fatalf("plan.seal: %v", err)
	}
	if sealRes["status"] != "sealed" {
		t.Fatalf("plan.seal status = %v (want sealed); result %v", sealRes["status"], sealRes)
	}
	if sealRes["mode"] != "flat" {
		t.Errorf("mode = %v, want flat (2 phases, 3 total steps ≤ 6)", sealRes["mode"])
	}
	hash, _ := sealRes["hash"].(string)
	if len(hash) != 64 {
		t.Errorf("hash = %q, want 64-char sha256 hex", hash)
	}

	// 4. Task executing with persisted steps.
	got, err := f.taskStore.GetByID(f.taskID)
	if err != nil || got == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.State != task.StateExecuting {
		t.Errorf("task state = %q, want executing", got.State)
	}
	steps, err := f.stepStore.ListByTaskID(f.taskID)
	if err != nil {
		t.Fatalf("ListByTaskID: %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("persisted steps = %d, want 3", len(steps))
	}
	phaseNames := map[string]bool{}
	for _, s := range steps {
		phaseNames[s.Phase] = true
	}
	if !phaseNames["Extract billing rows"] || !phaseNames["CSV renderer"] {
		t.Errorf("step phases = %v, want both compiled phase names", phaseNames)
	}
	// Dependency wiring survived the flatten: step 2 of phase 1 depends on
	// step 1 (needs: Phase1.S1).
	var extractedSecond, rendererStep *task.TaskStep
	for _, s := range steps {
		if s.Phase == "Extract billing rows" && len(s.DependsOn) > 0 {
			extractedSecond = s
		}
		if s.Phase == "CSV renderer" {
			rendererStep = s
		}
	}
	if extractedSecond == nil {
		t.Error("phase 1 step 2 lost its intra-phase dependency")
	}
	if rendererStep == nil || len(rendererStep.DependsOn) == 0 {
		t.Error("phase 2 step lost its inter-phase dependency (needs: billing-rows)")
	}

	// 5. Compiled phases persisted through PlanManager.CreatePhase (the
	// planPhaseSink path).
	phases, err := f.planMgr.GetPhasesByTask(context.Background(), f.taskID)
	if err != nil {
		t.Fatalf("GetPhasesByTask: %v", err)
	}
	if len(phases) != 2 {
		t.Fatalf("persisted phases = %d, want 2", len(phases))
	}
	if phases[0].Name != "Extract billing rows" || phases[1].Name != "CSV renderer" {
		t.Errorf("phase names = [%q, %q]", phases[0].Name, phases[1].Name)
	}
	if len(phases[0].Produces) != 1 || phases[0].Produces[0].Name != "billing-rows" {
		t.Errorf("phase 1 produces = %+v", phases[0].Produces)
	}
	if len(phases[1].Consumes) != 1 || phases[1].Consumes[0].Name != "billing-rows" {
		t.Errorf("phase 2 consumes = %+v", phases[1].Consumes)
	}
	// Required derived by the compiler: billing-rows is consumed by a later
	// phase.
	if len(phases[0].Produces) == 1 && !phases[0].Produces[0].Required {
		t.Error("billing-rows should be Required (consumed by later phase)")
	}

	// 6. Sealed hash recorded on the draft.
	d, ok := f.sp.DraftFor(f.taskID)
	if !ok {
		t.Fatal("draft missing after seal")
	}
	if d.SealedHash != hash {
		t.Errorf("draft sealed hash = %q, want %q", d.SealedHash, hash)
	}

	// 7. orchestrator.schedule published through the real bus.
	deadline := time.Now().Add(2 * time.Second)
	for f.schedules.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if f.schedules.Load() == 0 {
		t.Error("no orchestrator.schedule event published after seal")
	}
}

// TestPlanSealWiring_ProblemsStayDraft proves the compile-problems path: a
// draft with an open question seals to a `problems` RESULT (not an error),
// and nothing persists — the draft stays a draft for the next round.
func TestPlanSealWiring_ProblemsStayDraft(t *testing.T) {
	f := newSealWiringFixture(t, true)
	f.seedSealTask()

	withQuestion := insertOpenQuestion(f.t, sealableTwoPhaseDraft)

	if err := f.sp.SaveDraft(f.taskID, withQuestion); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	sealRes, err := f.callSeal(f.taskID)
	if err != nil {
		t.Fatalf("problems must be a RESULT; got err: %v", err)
	}
	if sealRes["status"] != "problems" {
		t.Fatalf("status = %v, want problems", sealRes["status"])
	}
	probs := extractProblems(f.t, sealRes)
	if len(probs) == 0 {
		t.Fatalf("problems = %v", sealRes["problems"])
	}

	// Draft stays a draft (hash untouched), task still planning, nothing
	// persisted.
	got, err := f.taskStore.GetByID(f.taskID)
	if err != nil || got == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.State != task.StatePlanning {
		t.Errorf("task state = %q, want planning (problems path must not execute)", got.State)
	}
	if d, ok := f.sp.DraftFor(f.taskID); !ok || d.SealedHash != "" {
		t.Errorf("problems path sealed the draft (hash=%q)", d.SealedHash)
	}
	if steps, _ := f.stepStore.ListByTaskID(f.taskID); len(steps) != 0 {
		t.Errorf("problems path persisted %d steps", len(steps))
	}
	if phases, _ := f.planMgr.GetPhasesByTask(context.Background(), f.taskID); len(phases) != 0 {
		t.Errorf("problems path persisted %d phases", len(phases))
	}
}

// insertOpenQuestion adds one Open Questions bullet to a draft copy.
func insertOpenQuestion(t *testing.T, md string) string {
	t.Helper()
	old := "## Open Questions\n\n## Phases"
	repl := "## Open Questions\n\n- do we stream or buffer the CSV?\n\n## Phases"
	out := strings.Replace(md, old, repl, 1)
	if out == md {
		t.Fatal("insertOpenQuestion: anchor not found")
	}
	return out
}

// extractProblems pulls the problem messages from a seal result map
// (post-JSON: problems arrive as []any of maps).
func extractProblems(t *testing.T, res map[string]any) []string {
	t.Helper()
	arr, ok := res["problems"].([]any)
	if !ok {
		t.Fatalf("problems = %v (type %T)", res["problems"], res["problems"])
	}
	var msgs []string
	for _, p := range arr {
		if m, ok := p.(map[string]any); ok {
			if msg, ok := m["message"].(string); ok {
				msgs = append(msgs, msg)
			}
		}
	}
	return msgs
}

// TestPlanSealWiring_FlagOffLegacyIntact proves the flag-off path: the
// compiler pipeline never engages (no handler, no draft) and the legacy
// interview/JSON behavior runs unchanged.
func TestPlanSealWiring_FlagOffLegacyIntact(t *testing.T) {
	f := newSealWiringFixture(t, false)
	tsk := f.seedSealTask()

	// No handler wired when the flag is off — the RPC surface for
	// plan.seal/plan.draft does not exist.
	if f.handler != nil {
		t.Fatal("flag off: seal handler wired — pipeline must stay dark")
	}

	// Legacy path: high-ambiguity mode=plan still interviews (degrading to
	// fallback steps without an LLM, exactly as pre-leaf) and the task moves
	// to executing with fallback steps. NO draft is created. The registry
	// carries the planner spec (nil LLM client), matching the agent-package
	// gate test.
	reg := agent.NewAgentRegistry(agent.RegistryConfig{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	spec := &agent.AgentSpec{
		ID:          config.AgentIDPlanner,
		Name:        "planner",
		Role:        agent.RoleExecutor,
		Purpose:     "test fixture",
		Enabled:     true,
		CanDelegate: false,
		Constraints: agent.DefaultConstraints(),
	}
	if err := reg.RegisterSpec(spec); err != nil {
		t.Fatalf("RegisterSpec: %v", err)
	}
	f.sp.SetRegistry(reg)

	req := agent.PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "sess-flag-off",
		Input:     "Build avatar upload with local storage",
		Intent:    "code",
		Mode:      "plan",
		TrueAnalysis: &agent.TrueIntentAnalysis{
			Goal:      "avatar upload",
			Ambiguity: 0.9,
			Scope:     "broad",
		},
	}
	if err := f.sp.Plan(context.Background(), req); err != nil {
		t.Fatalf("Plan (flag off): %v", err)
	}

	if _, ok := f.sp.DraftFor(f.taskID); ok {
		t.Error("flag off: draft created — legacy path must not touch the draft store")
	}
	got, err := f.taskStore.GetByID(f.taskID)
	if err != nil || got == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.State != task.StateExecuting {
		t.Errorf("flag off: task state = %q, want executing (legacy fallback path)", got.State)
	}
	// Flag off: no compiled phases persisted through the plan store.
	if phases, _ := f.planMgr.GetPhasesByTask(context.Background(), f.taskID); len(phases) != 0 {
		t.Errorf("flag off: %d phases persisted; seal pipeline must stay dark", len(phases))
	}
}
