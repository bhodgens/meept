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
