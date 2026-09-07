package agent

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/task"
)

func draftDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newDraftTestPlanner(t *testing.T) (*StrategicPlanner, *task.Store) {
	t.Helper()
	taskStore, err := newTestTaskStore(t.TempDir())
	if err != nil {
		t.Fatalf("newTestTaskStore: %v", err)
	}
	t.Cleanup(func() {
		if err := taskStore.Close(); err != nil {
			t.Errorf("close task store: %v", err)
		}
	})
	sp := NewStrategicPlanner(StrategicPlannerConfig{
		TaskStore:      taskStore,
		StepStore:      taskStore.StepStore(),
		Logger:         draftDiscardLogger(),
		TemplateLoader: newPlannerTemplateLoader(t.TempDir()), // empty tiers: no files on disk
	})
	return sp, taskStore
}

func seedDraftTask(t *testing.T, store *task.Store, id string) *task.Task {
	t.Helper()
	tsk := newTestTask(id, "build the billing export feature")
	tsk.ID = id
	tsk.SetState(task.StatePlanning)
	if err := store.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return tsk
}

// --- SetPlanCompilerEnabled -------------------------------------------------

func TestSetPlanCompilerEnabled_NilReceiver(t *testing.T) {
	var sp *StrategicPlanner
	sp.SetPlanCompilerEnabled(true) // must not panic (nil-guarded setter convention)
}

// --- PlanDraft metadata bag -------------------------------------------------

func TestPlanDraft_MetadataRoundTrip(t *testing.T) {
	d := PlanDraft{
		Markdown:   "# Plan: x\n\n## Meta\n\n- task_id: t-1\n- version: 1\n- status: draft\n- updated: 2026-09-06\n",
		Version:    3,
		UpdatedAt:  time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC),
		SealedHash: "abc123",
	}
	raw, err := marshalPlanDraft(&d)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got, err := unmarshalPlanDraft(raw)
	if err != nil {
		t.Fatalf("unmarshalPlanDraft: %v", err)
	}
	if got.Markdown != d.Markdown {
		t.Errorf("markdown round trip lost content")
	}
	if got.Version != d.Version {
		t.Errorf("version = %d, want %d", got.Version, d.Version)
	}
	if got.SealedHash != d.SealedHash {
		t.Errorf("sealed_hash = %q, want %q", got.SealedHash, d.SealedHash)
	}
	if !got.UpdatedAt.Equal(d.UpdatedAt) {
		t.Errorf("updated_at = %v, want %v", got.UpdatedAt, d.UpdatedAt)
	}
}

func TestUnmarshalPlanDraft_Empty(t *testing.T) {
	d, err := unmarshalPlanDraft(nil)
	if err != nil {
		t.Fatalf("unmarshalPlanDraft(nil): %v", err)
	}
	if d == nil || d.Version != 0 {
		t.Errorf("expected zero-valued draft, got %+v", d)
	}
}

// --- SaveDraft / DraftFor ---------------------------------------------------

func TestSaveDraft_ThenDraftFor(t *testing.T) {
	sp, store := newDraftTestPlanner(t)
	sp.SetPlanCompilerEnabled(true)
	tsk := seedDraftTask(t, store, "task-draft-1")

	md := "# Plan: Billing export\n\n## Meta\n\n- task_id: task-draft-1\n- version: 1\n- status: draft\n- updated: 2026-09-06\n"
	if err := sp.SaveDraft(tsk.ID, md); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	d, ok := sp.DraftFor(tsk.ID)
	if !ok {
		t.Fatal("DraftFor: draft not found after save")
	}
	if d.Markdown != md {
		t.Errorf("markdown mismatch: got %q", d.Markdown)
	}
	if d.Version != 1 {
		t.Errorf("first save version = %d, want 1", d.Version)
	}
}

func TestSaveDraft_IncrementsVersion(t *testing.T) {
	sp, store := newDraftTestPlanner(t)
	tsk := seedDraftTask(t, store, "task-draft-2")

	if err := sp.SaveDraft(tsk.ID, "v1 body"); err != nil {
		t.Fatalf("SaveDraft v1: %v", err)
	}
	if err := sp.SaveDraft(tsk.ID, "v2 body"); err != nil {
		t.Fatalf("SaveDraft v2: %v", err)
	}
	d, ok := sp.DraftFor(tsk.ID)
	if !ok {
		t.Fatal("draft missing")
	}
	if d.Version != 2 {
		t.Errorf("version = %d, want 2", d.Version)
	}
	if d.Markdown != "v2 body" {
		t.Errorf("save did not replace content")
	}
}

func TestSaveDraft_UpdatesTaskStore(t *testing.T) {
	sp, store := newDraftTestPlanner(t)
	tsk := seedDraftTask(t, store, "task-draft-3")

	if err := sp.SaveDraft(tsk.ID, "persisted body"); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	// Fresh read from the store: the draft must live on task.Metadata,
	// not only on the in-memory struct.
	fresh, err := store.GetByID(tsk.ID)
	if err != nil || fresh == nil {
		t.Fatalf("GetByID: %v", err)
	}
	d, ok := sp.draftFromMetadata(fresh)
	if !ok {
		t.Fatal("draft not persisted on task metadata")
	}
	if d.Markdown != "persisted body" {
		t.Errorf("store draft markdown = %q", d.Markdown)
	}
}

func TestDraftFor_Miss(t *testing.T) {
	sp, _ := newDraftTestPlanner(t)
	if _, ok := sp.DraftFor("task-nonexistent"); ok {
		t.Error("DraftFor miss returned ok=true")
	}
}

func TestDraftFor_NilReceiver(t *testing.T) {
	var sp *StrategicPlanner
	if _, ok := sp.DraftFor("any"); ok {
		t.Error("nil receiver DraftFor returned ok=true")
	}
}

func TestSaveDraft_RefusesNonPlanningState(t *testing.T) {
	sp, store := newDraftTestPlanner(t)
	tsk := newTestTask("task-draft-4", "some task")
	tsk.SetState(task.StateExecuting)
	if err := store.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	err := sp.SaveDraft(tsk.ID, "should refuse")
	if err == nil {
		t.Fatal("SaveDraft on executing task: want error, got nil")
	}
	if !strings.Contains(err.Error(), "planning") {
		t.Errorf("error should mention planning state; got %q", err.Error())
	}
}

func TestSealDraft_StampsHash(t *testing.T) {
	sp, store := newDraftTestPlanner(t)
	tsk := seedDraftTask(t, store, "task-draft-5")

	if err := sp.SaveDraft(tsk.ID, "body"); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if err := sp.SealDraft(tsk.ID, "cafe1234"); err != nil {
		t.Fatalf("SealDraft: %v", err)
	}
	d, ok := sp.DraftFor(tsk.ID)
	if !ok {
		t.Fatal("draft missing")
	}
	if d.SealedHash != "cafe1234" {
		t.Errorf("SealedHash = %q, want cafe1234", d.SealedHash)
	}
}

func TestSealDraft_NoDraft(t *testing.T) {
	sp, store := newDraftTestPlanner(t)
	seedDraftTask(t, store, "task-draft-6")
	if err := sp.SealDraft("task-draft-6", "hash"); err == nil {
		t.Error("SealDraft with no draft: want error, got nil")
	}
}

// --- Interview gate ---------------------------------------------------------

// TestPlan_InterviewGateEnabled seeds a draft scaffold and returns without
// running the one-shot interview (ConductInterview): the draft IS the
// interview. planMultiPhase would also be skipped — the task stays in
// planning awaiting a seal instead of executing.
func TestPlan_InterviewGateEnabled(t *testing.T) {
	sp, store := newDraftTestPlanner(t)
	sp.SetPlanCompilerEnabled(true)
	msgBus := bus.New(nil, draftDiscardLogger())
	sp.bus = msgBus
	defer msgBus.Close()

	interviewed := false
	sp.SetInterviewProbe(func() { interviewed = true })

	tsk := seedDraftTask(t, store, "task-gate-on")
	req := PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "sess-gate",
		Input:     "Build avatar upload with local storage",
		Intent:    "code",
		Mode:      "plan",
		TrueAnalysis: &TrueIntentAnalysis{
			Goal:      "avatar upload",
			Ambiguity: 0.9, // would trigger the interview when disabled
			Scope:     "broad",
		},
	}
	if err := sp.Plan(context.Background(), req); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if interviewed {
		t.Error("planCompilerEnabled: ConductInterview was called, want skipped")
	}

	// Draft scaffold seeded from the request.
	d, ok := sp.DraftFor(tsk.ID)
	if !ok {
		t.Fatal("planCompilerEnabled: no draft seeded")
	}
	if !strings.Contains(d.Markdown, "# Plan:") {
		t.Errorf("scaffold missing title; got:\n%s", d.Markdown)
	}
	if !strings.Contains(d.Markdown, "## Goal") {
		t.Errorf("scaffold missing Goal section; got:\n%s", d.Markdown)
	}
	if !strings.Contains(d.Markdown, "## Open Questions") {
		t.Errorf("scaffold missing Open Questions section; got:\n%s", d.Markdown)
	}
	if !strings.Contains(d.Markdown, "task-gate-on") {
		t.Errorf("scaffold missing task_id meta; got:\n%s", d.Markdown)
	}
	if !strings.Contains(d.Markdown, "avatar upload") {
		t.Errorf("scaffold missing request-derived goal text; got:\n%s", d.Markdown)
	}

	// Task remains in planning — no steps persisted, no execution start.
	fresh, err := store.GetByID(tsk.ID)
	if err != nil || fresh == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if fresh.State != task.StatePlanning {
		t.Errorf("task state = %q, want planning (awaiting seal)", fresh.State)
	}
	steps, err := store.StepStore().ListByTaskID(tsk.ID)
	if err != nil {
		t.Fatalf("ListByTaskID: %v", err)
	}
	if len(steps) != 0 {
		t.Errorf("planCompilerEnabled: %d steps persisted, want 0 (no LLM decomposition before seal)", len(steps))
	}
}

// TestPlan_InterviewGateDisabled proves the flag-off path is byte-identical:
// high-ambiguity mode=plan requests still conduct the interview exactly as
// before this leaf. The registry carries the planner spec but no LLM client,
// so the interview degrades exactly as it does in a misconfigured env (the
// legacy fallback-steps path), with no network access.
func TestPlan_InterviewGateDisabled(t *testing.T) {
	sp, store := newDraftTestPlanner(t)
	// flag stays off (default false)
	msgBus := bus.New(nil, draftDiscardLogger())
	sp.bus = msgBus
	defer msgBus.Close()

	// Real registry construction path with the planner registered but no
	// LLM client wired (nil in RegistryConfig): RunOnce returns
	// ErrNoLLMClient, ConductInterview maps that to ErrInterviewGenerationFail,
	// and Plan proceeds down the legacy planSinglePhase → fallback path.
	reg := NewAgentRegistry(RegistryConfig{Logger: draftDiscardLogger()})
	spec := &AgentSpec{
		ID:          config.AgentIDPlanner,
		Name:        "planner",
		Role:        RoleExecutor,
		Purpose:     "test fixture",
		Enabled:     true,
		CanDelegate: false,
		Constraints: DefaultConstraints(),
	}
	if err := reg.RegisterSpec(spec); err != nil {
		t.Fatalf("RegisterSpec: %v", err)
	}
	sp.registry = reg

	interviewed := false
	sp.SetInterviewProbe(func() { interviewed = true })

	tsk := seedDraftTask(t, store, "task-gate-off")
	req := PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "sess-gate",
		Input:     "Build avatar upload with local storage",
		Intent:    "code",
		Mode:      "plan",
		TrueAnalysis: &TrueIntentAnalysis{
			Goal:      "avatar upload",
			Ambiguity: 0.9,
			Scope:     "broad",
		},
	}
	if err := sp.Plan(context.Background(), req); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !interviewed {
		t.Error("flag off: ConductInterview not called — regression")
	}
	if _, ok := sp.DraftFor(tsk.ID); ok {
		t.Error("flag off: draft seeded — flag-off path must not touch the draft store")
	}
	// The legacy path persisted fallback steps and moved the task on to
	// executing (byte-identical with pre-leaf behavior).
	fresh, err := store.GetByID(tsk.ID)
	if err != nil || fresh == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if fresh.State != task.StateExecuting {
		t.Errorf("flag off: task state = %q, want executing (legacy fallback path)", fresh.State)
	}
	steps, err := store.StepStore().ListByTaskID(tsk.ID)
	if err != nil {
		t.Fatalf("ListByTaskID: %v", err)
	}
	if len(steps) == 0 {
		t.Error("flag off: no steps persisted — legacy path regressed")
	}
}

// --- Draft scaffold rendering ----------------------------------------------

func TestRenderDraftScaffold(t *testing.T) {
	sp, _ := newDraftTestPlanner(t)
	md, err := sp.renderDraftScaffold("task-scaf-1", "Make the CLI faster")
	if err != nil {
		t.Fatalf("renderDraftScaffold: %v", err)
	}
	for _, want := range []string{
		"# Plan:",
		"## Meta",
		"- task_id: task-scaf-1",
		"- version: 1",
		"- status: draft",
		"## Goal",
		"Make the CLI faster",
		"## Open Questions",
		"## Phases",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("scaffold missing %q; got:\n%s", want, md)
		}
	}
}

// CompileSealed must accept the scaffold shape minus Open Questions (sanity:
// the scaffold conforms to the dialect the compiler parses).
func TestRenderDraftScaffold_CompilesWhenSealable(t *testing.T) {
	sp, _ := newDraftTestPlanner(t)
	md, err := sp.renderDraftScaffold("task-scaf-2", "Do the thing")
	if err != nil {
		t.Fatalf("renderDraftScaffold: %v", err)
	}
	// The fresh scaffold has empty Open Questions and no phases — sealing a
	// no-phase plan must at least reach the compiler's "no phases" problem,
	// proving shape conformance (not a parse explosion elsewhere).
	_, cerr := plan.CompileSealed(md, 10)
	if cerr == nil {
		t.Fatal("fresh scaffold (no phases) should fail compile with 'no phases'")
	}
	var ce *plan.CompileError
	if !asCompileError(cerr, &ce) {
		t.Fatalf("want *plan.CompileError, got %T", cerr)
	}
	found := false
	for _, p := range ce.Problems {
		if strings.Contains(p.Message, "declares no phases") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'declares no phases' problem; got %+v", ce.Problems)
	}
}

func asCompileError(err error, target **plan.CompileError) bool {
	ce, ok := err.(*plan.CompileError)
	if ok {
		*target = ce
	}
	return ok
}
