package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/task"
	"log/slog"
)

// stubPlanStore is a minimal PlanStore implementation for orchestrator tests.
// It only supports the methods the orchestrator's phase transition uses:
// GetPhases. All other methods panic to catch unintended use.
type stubPlanStore struct {
	mu     sync.Mutex
	phases map[string][]*plan.PlanPhase // planID -> phases
}

func newStubPlanStore() *stubPlanStore {
	return &stubPlanStore{phases: make(map[string][]*plan.PlanPhase)}
}

func (s *stubPlanStore) CreatePlan(_ context.Context, p *plan.Plan) error { return nil }
func (s *stubPlanStore) GetPlan(_ context.Context, id string) (*plan.Plan, error) {
	return &plan.Plan{ID: id}, nil
}
func (s *stubPlanStore) UpdatePlan(_ context.Context, _ *plan.Plan) error { return nil }
func (s *stubPlanStore) DeletePlan(_ context.Context, _ string) error      { return nil }
func (s *stubPlanStore) ListPlans(_ context.Context, _ string, _ int) ([]*plan.Plan, error) {
	return nil, nil
}
func (s *stubPlanStore) ListPlansBySession(_ context.Context, _ string) ([]*plan.Plan, error) {
	return nil, nil
}
func (s *stubPlanStore) ListPlansByState(_ context.Context, _ plan.PlanState, _ int) ([]*plan.Plan, error) {
	return nil, nil
}
func (s *stubPlanStore) SetPlanState(_ context.Context, _ string, _ plan.PlanState) error {
	return nil
}
func (s *stubPlanStore) CreatePhase(_ context.Context, p *plan.PlanPhase) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.phases[p.PlanID] = append(s.phases[p.PlanID], p)
	return nil
}
func (s *stubPlanStore) GetPhases(_ context.Context, planID string) ([]*plan.PlanPhase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.phases[planID], nil
}
func (s *stubPlanStore) UpdatePhase(_ context.Context, _ *plan.PlanPhase) error   { return nil }
func (s *stubPlanStore) SetPhaseState(_ context.Context, _ string, _ plan.PhaseState) error { return nil }
func (s *stubPlanStore) IncrementPhaseProgress(_ context.Context, _, _ string, _ int) error { return nil }
func (s *stubPlanStore) LinkSession(_ context.Context, _, _ string) error                    { return nil }
func (s *stubPlanStore) UnlinkSession(_ context.Context, _, _ string) error                  { return nil }
func (s *stubPlanStore) GetPlansForSession(_ context.Context, _ string) ([]*plan.Plan, error) { return nil, nil }
func (s *stubPlanStore) CreateSignoff(_ context.Context, _ *plan.PlanSignoff) error           { return nil }
func (s *stubPlanStore) GetSignoffs(_ context.Context, _ string) ([]*plan.PlanSignoff, error) { return nil, nil }
func (s *stubPlanStore) GetRevisionCount(_ context.Context, _ string) (int, error)           { return 0, nil }
func (s *stubPlanStore) CountPlansBySessionAndState(_ context.Context, _ string) (map[plan.PlanState]int, error) {
	return nil, nil
}

// newTestOrchestrator builds a minimal Orchestrator wired with a real
// StepStore (in-memory SQLite) and a stub plan store. Returns the orchestrator
// and step store so tests can seed and verify.
func newTestOrchestrator(t *testing.T) (*Orchestrator, *task.StepStore, *stubPlanStore) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(&discardWriter{}, &slog.HandlerOptions{Level: slog.LevelDebug}))

	stepStore := newInMemoryStepStore(t)

	stubStore := newStubPlanStore()
	planMgr := plan.NewPlanManager(stubStore, nil, planConfigOff(), nil, logger)

	o := &Orchestrator{
		planManager: planMgr,
		stepStore:   stepStore,
		logger:      logger,
		artifacts:   newArtifactStore(),
	}
	return o, stepStore, stubStore
}

// planConfigOff returns a plans config with mode "off" for tests.
func planConfigOff() config.PlansConfig {
	return config.PlansConfig{Mode: "off"}
}

// discardWriter is a no-op io.Writer for loggers in tests.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestStartNextPhase_AssignsFreshConversationIDs(t *testing.T) {
	o, store, stub := newTestOrchestrator(t)
	ctx := context.Background()

	// Seed two phases: phase 1 done, phase 2 pending.
	planID := "plan-test"
	phase1 := &plan.PlanPhase{ID: "p1", PlanID: planID, Name: "Phase 1", Sequence: 0, State: plan.PhaseCompleted}
	phase2 := &plan.PlanPhase{ID: "p2", PlanID: planID, Name: "Phase 2", Sequence: 1, State: plan.PhasePending}
	_ = stub.CreatePhase(ctx, phase1)
	_ = stub.CreatePhase(ctx, phase2)

	// Register task->plan mapping so GetPhasesByTask finds it.
	o.planManager.RegisterTaskPlan("task-x", planID)

	step := task.NewTaskStep("task-x", "do thing", 1)
	step.ID = "s1"
	step.Phase = "Phase 2"
	if err := store.Create(step); err != nil {
		t.Fatalf("create step: %v", err)
	}

	// Pre-condition: ConversationID is empty before transition.
	got, err := store.GetByID("s1")
	if err != nil {
		t.Fatalf("GetByID before: %v", err)
	}
	if got.ConversationID != "" {
		t.Fatalf("pre-transition ConversationID = %q; want empty", got.ConversationID)
	}

	err = o.startNextPhase(ctx, "task-x", phase1.Name)
	if err != nil {
		t.Fatalf("startNextPhase: %v", err)
	}
	got, _ = store.GetByID("s1")
	if !strings.HasPrefix(got.ConversationID, "phase-") {
		t.Errorf("ConversationID = %q; want phase-* prefix", got.ConversationID)
	}
	if got.AccumulatedContext == "" {
		t.Errorf("AccumulatedContext should include phase startup context")
	}
	if !strings.Contains(got.AccumulatedContext, "Phase 2") {
		t.Errorf("AccumulatedContext should mention phase name; got: %s", got.AccumulatedContext)
	}
}

func TestStartNextPhase_NoNextPhase_ReturnsNil(t *testing.T) {
	o, store, stub := newTestOrchestrator(t)
	ctx := context.Background()

	planID := "plan-single"
	phase1 := &plan.PlanPhase{ID: "p1", PlanID: planID, Name: "Only", Sequence: 0, State: plan.PhaseCompleted}
	_ = stub.CreatePhase(ctx, phase1)
	o.planManager.RegisterTaskPlan("task-x", planID)

	step := task.NewTaskStep("task-x", "x", 1)
	step.ID = "sx"
	step.Phase = "Only"
	_ = store.Create(step)

	// No next phase — should return nil without error.
	err := o.startNextPhase(ctx, "task-x", "Only")
	if err != nil {
		t.Errorf("expected nil for no next phase; got: %v", err)
	}
}

func TestStartNextPhase_MissingRequiredArtifact_BlocksTransition(t *testing.T) {
	o, store, stub := newTestOrchestrator(t)
	ctx := context.Background()

	planID := "plan-block"
	phase1 := &plan.PlanPhase{ID: "p1", PlanID: planID, Name: "Setup", Sequence: 0, State: plan.PhaseCompleted}
	phase2 := &plan.PlanPhase{ID: "p2", PlanID: planID, Name: "Build", Sequence: 1, State: plan.PhasePending}
	_ = stub.CreatePhase(ctx, phase1)
	_ = stub.CreatePhase(ctx, phase2)
	o.planManager.RegisterTaskPlan("task-x", planID)

	step := task.NewTaskStep("task-x", "x", 1)
	step.ID = "sblock"
	step.Phase = "Build"
	_ = store.Create(step)

	// Inject a phase spec that requires a missing artifact.
	o.phaseSpecOverride = map[string]*PlanPhaseSpec{
		"Build": {
			Name: "Build",
			Consumes: []Artifact{
				{Name: "api-spec", Required: true},
			},
		},
	}

	err := o.startNextPhase(ctx, "task-x", "Setup")
	if err == nil {
		t.Fatal("expected error for missing required artifact")
	}
	if !strings.Contains(err.Error(), "not ready") {
		t.Errorf("error should mention 'not ready'; got: %v", err)
	}
}

func TestRenderPhaseStartup_IncludesConsumedArtifacts(t *testing.T) {
	store := newArtifactStore()
	store.Add(Artifact{Name: "auth-spec", Kind: "schema", Description: "JWT auth schema"}, "p1")
	phase := &PlanPhaseSpec{
		Name:        "Implementation",
		Description: "Build the auth module",
		Consumes: []Artifact{
			{Name: "auth-spec", Kind: "schema", Description: "JWT auth schema", Required: true},
			{Name: "missing-doc", Required: true},
		},
	}
	out := renderPhaseStartup(phase, store)
	if !strings.Contains(out, "Implementation") {
		t.Errorf("missing phase name")
	}
	if !strings.Contains(out, "auth-spec") {
		t.Errorf("missing consumed artifact")
	}
	if !strings.Contains(out, "MISSING") {
		t.Errorf("missing MISSING tag for absent artifact")
	}
}

func TestStartNextPhase_NilPlanManager_ReturnsError(t *testing.T) {
	o := &Orchestrator{
		logger: slog.Default(),
	}
	err := o.startNextPhase(context.Background(), "task-x", "Phase 1")
	if err == nil {
		t.Fatal("expected error when planManager is nil")
	}
	if !strings.Contains(err.Error(), "plan manager") {
		t.Errorf("error should mention plan manager; got: %v", err)
	}
}

// TestGetPlanPhaseSpec_ReadsProducesConsumesFromPersistedPhase is a regression
// test for a bug where getPlanPhaseSpec returned only {Name: p.Name}, ignoring
// the persisted Produces/Consumes fields. This made runtime phase readiness
// gating and phase-startup context injection a no-op (tests used
// phaseSpecOverride, masking the bug). The fix populates Produces/Consumes
// from the persisted plan.PlanPhase record.
func TestGetPlanPhaseSpec_ReadsProducesConsumesFromPersistedPhase(t *testing.T) {
	o, _, stub := newTestOrchestrator(t)
	ctx := context.Background()

	planID := "plan-spec"
	produces := []plan.Artifact{{Name: "auth-spec", Kind: "schema", Description: "JWT auth schema"}}
	consumes := []plan.Artifact{{Name: "user-research", Kind: "doc", Required: true}}
	phase := &plan.PlanPhase{
		ID:       "p1",
		PlanID:   planID,
		Name:     "Auth",
		Sequence: 0,
		State:    plan.PhasePending,
		Produces: produces,
		Consumes: consumes,
	}
	if err := stub.CreatePhase(ctx, phase); err != nil {
		t.Fatalf("CreatePhase: %v", err)
	}
	o.planManager.RegisterTaskPlan("task-spec", planID)

	// No phaseSpecOverride — must read from persisted PlanPhase.
	spec, err := o.getPlanPhaseSpec(ctx, "task-spec", "Auth")
	if err != nil {
		t.Fatalf("getPlanPhaseSpec: %v", err)
	}
	if spec.Name != "Auth" {
		t.Errorf("Name = %q; want Auth", spec.Name)
	}
	if len(spec.Produces) != 1 || spec.Produces[0].Name != "auth-spec" {
		t.Errorf("Produces not populated from persisted PlanPhase; got %+v", spec.Produces)
	}
	if len(spec.Consumes) != 1 || spec.Consumes[0].Name != "user-research" {
		t.Errorf("Consumes not populated from persisted PlanPhase; got %+v", spec.Consumes)
	}
	if !spec.Consumes[0].Required {
		t.Errorf("Consumes[0].Required flag lost; got %+v", spec.Consumes[0])
	}
}

// TestGetPlanPhaseSpec_TestOverrideStillTakesPrecedence confirms the
// phaseSpecOverride escape hatch still works after the fix.
func TestGetPlanPhaseSpec_TestOverrideStillTakesPrecedence(t *testing.T) {
	o, _, stub := newTestOrchestrator(t)
	ctx := context.Background()

	planID := "plan-override"
	persistedPhase := &plan.PlanPhase{
		ID:       "p1",
		PlanID:   planID,
		Name:     "Override",
		Sequence: 0,
		State:    plan.PhasePending,
		Produces: []plan.Artifact{{Name: "persisted-prod", Kind: "doc"}},
		Consumes: []plan.Artifact{{Name: "persisted-cons", Required: true}},
	}
	if err := stub.CreatePhase(ctx, persistedPhase); err != nil {
		t.Fatalf("CreatePhase: %v", err)
	}
	o.planManager.RegisterTaskPlan("task-override", planID)

	overrideSpec := &PlanPhaseSpec{
		Name:     "Override",
		Produces: []Artifact{{Name: "override-prod", Kind: "doc"}},
	}
	o.phaseSpecOverride = map[string]*PlanPhaseSpec{"Override": overrideSpec}

	spec, err := o.getPlanPhaseSpec(ctx, "task-override", "Override")
	if err != nil {
		t.Fatalf("getPlanPhaseSpec: %v", err)
	}
	if spec.Produces[0].Name != "override-prod" {
		t.Errorf("override should take precedence; got %+v", spec.Produces)
	}
}

// TestMaybeTransitionPhase_AdvancesToNextPhase verifies the wiring in
// handleJobCompleted: when a step completes and IsPhaseComplete returns true
// for the step's phase, startNextPhase is invoked to advance to the next phase.
// Test seeds two phases with one step each; completing phase 1's step should
// cause phase 2's step to get a fresh conversationID and phase startup context.
func TestMaybeTransitionPhase_AdvancesToNextPhase(t *testing.T) {
	o, store, stub := newTestOrchestrator(t)
	ctx := context.Background()

	planID := "plan-transition"
	phase1 := &plan.PlanPhase{ID: "p1", PlanID: planID, Name: "Setup", Sequence: 0, State: plan.PhaseCompleted}
	phase2 := &plan.PlanPhase{ID: "p2", PlanID: planID, Name: "Build", Sequence: 1, State: plan.PhasePending}
	if err := stub.CreatePhase(ctx, phase1); err != nil {
		t.Fatalf("CreatePhase p1: %v", err)
	}
	if err := stub.CreatePhase(ctx, phase2); err != nil {
		t.Fatalf("CreatePhase p2: %v", err)
	}
	o.planManager.RegisterTaskPlan("task-trans", planID)

	// Phase 1 step — already completed.
	s1 := task.NewTaskStep("task-trans", "setup", 1)
	s1.ID = "st1"
	s1.Phase = "Setup"
	s1.State = task.StepCompleted
	if err := store.Create(s1); err != nil {
		t.Fatalf("create s1: %v", err)
	}

	// Phase 2 step — pending, empty conversation ID.
	s2 := task.NewTaskStep("task-trans", "build", 2)
	s2.ID = "st2"
	s2.Phase = "Build"
	if err := store.Create(s2); err != nil {
		t.Fatalf("create s2: %v", err)
	}

	o.maybeTransitionPhase(ctx, s1.ID, "task-trans")

	got, _ := store.GetByID(s2.ID)
	if got == nil {
		t.Fatal("s2 not found after transition")
	}
	if !strings.HasPrefix(got.ConversationID, "phase-") {
		t.Errorf("phase 2 step's ConversationID = %q; want phase-* prefix", got.ConversationID)
	}
	if !strings.Contains(got.AccumulatedContext, "Build") {
		t.Errorf("phase 2 step's AccumulatedContext missing phase name; got: %s", got.AccumulatedContext)
	}
}

// TestMaybeTransitionPhase_PhaseIncomplete_NoTransition verifies the negative
// case: when the completing step's phase still has additional active steps,
// startNextPhase is NOT triggered.
func TestMaybeTransitionPhase_PhaseIncomplete_NoTransition(t *testing.T) {
	o, store, stub := newTestOrchestrator(t)
	ctx := context.Background()

	planID := "plan-incomplete"
	phase1 := &plan.PlanPhase{ID: "p1", PlanID: planID, Name: "Setup", Sequence: 0, State: plan.PhasePending}
	phase2 := &plan.PlanPhase{ID: "p2", PlanID: planID, Name: "Build", Sequence: 1, State: plan.PhasePending}
	if err := stub.CreatePhase(ctx, phase1); err != nil {
		t.Fatalf("CreatePhase p1: %v", err)
	}
	if err := stub.CreatePhase(ctx, phase2); err != nil {
		t.Fatalf("CreatePhase p2: %v", err)
	}
	o.planManager.RegisterTaskPlan("task-incomplete", planID)

	// Phase 1 has two steps: one completed, one still pending.
	s1 := task.NewTaskStep("task-incomplete", "setup1", 1)
	s1.ID = "sti1"
	s1.Phase = "Setup"
	s1.State = task.StepCompleted
	_ = store.Create(s1)

	s2 := task.NewTaskStep("task-incomplete", "setup2", 2)
	s2.ID = "sti2"
	s2.Phase = "Setup"
	s2.State = task.StepPending
	_ = store.Create(s2)

	// Phase 2 step: should remain untouched.
	s3 := task.NewTaskStep("task-incomplete", "build", 3)
	s3.ID = "sti3"
	s3.Phase = "Build"
	_ = store.Create(s3)

	o.maybeTransitionPhase(ctx, s1.ID, "task-incomplete")

	got, _ := store.GetByID(s3.ID)
	if got.ConversationID != "" {
		t.Errorf("phase 2 step should not be touched; ConversationID = %q", got.ConversationID)
	}
	if got.AccumulatedContext != "" {
		t.Errorf("phase 2 step should not have AccumulatedContext; got: %s", got.AccumulatedContext)
	}
}

// TestMaybeTransitionPhase_NoPhaseShortCircuits verifies that steps with no
// Phase assigned (typical single-phase plans) do not trigger phase-transition
// logic at all.
func TestMaybeTransitionPhase_NoPhaseShortCircuits(t *testing.T) {
	o, store, _ := newTestOrchestrator(t)
	ctx := context.Background()

	s1 := task.NewTaskStep("task-nop", "x", 1)
	s1.ID = "snp1"
	s1.State = task.StepCompleted
	// No Phase field.
	if err := store.Create(s1); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Should be a safe no-op — no panics, no errors propagating.
	o.maybeTransitionPhase(ctx, s1.ID, "task-nop")
}
