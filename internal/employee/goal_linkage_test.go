package employee

// Goal-linkage tests (plan tree 20261008-goal-completion, leaf 02):
// SourceImplicit handling, PlanRef/Plan goal stamping, and
// GoalStore.EnsureImplicitGoal idempotency.

import (
	"context"
	"sync"
	"testing"
)

// goalRecordingPlanner wraps stubPlanner and records SetPlanGoal calls,
// simulating a PlanCreator adapter that persists the goal linkage onto the
// stored plan row.
type goalRecordingPlanner struct {
	stubPlanner
	mu        sync.Mutex
	planGoals map[string]string
}

func newGoalRecordingPlanner() *goalRecordingPlanner {
	return &goalRecordingPlanner{
		stubPlanner: stubPlanner{idPrefix: "plan-goal-"},
		planGoals:   map[string]string{},
	}
}

// SetPlanGoal satisfies the PlanGoalSetter optional interface.
func (p *goalRecordingPlanner) SetPlanGoal(_ context.Context, planID, goalID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.planGoals[planID] = goalID
	return nil
}

// TestGoalLoopPlanCarriesGoalID verifies that a tier-3 decide with a seeded
// active goal produces a plan whose GoalID matches the active goal, both on
// the returned PlanRef and via the PlanGoalSetter persistence hook.
func TestGoalLoopPlanCarriesGoalID(t *testing.T) {
	store := testGoalStore(t)
	seedBot(t, store, "emp-goal-link")

	g := &Goal{
		ID:         "goal_link_1",
		EmployeeID: "emp-goal-link",
		Title:      "linkage",
		Mandate:    "plans trace to goals",
		State:      GoalActive,
		Source:     SourceUser,
	}
	if err := store.Create(context.Background(), g); err != nil {
		t.Fatalf("Create goal: %v", err)
	}

	reflector := newStubReflector()
	reflector.queueResponse(`{"candidates":[{"title":"link","description":"d","prompt":"p"}]}`)
	planner := newGoalRecordingPlanner()

	loop := NewGoalLoop("emp-goal-link", testTier3Constitution(), store, nil).
		WithReflector(reflector).
		WithPlanner(planner).
		WithEscalationGate(func(_ *Constitution, _ CandidatePlan) (bool, string) {
			return true, "test"
		})

	if err := loop.Decide(context.Background(), basicTrigger()); err != nil {
		t.Fatalf("Decide: %v", err)
	}

	if len(planner.planGoals) != 1 {
		t.Fatalf("SetPlanGoal calls = %d, want 1", len(planner.planGoals))
	}
	for planID, goalID := range planner.planGoals {
		if goalID != "goal_link_1" {
			t.Errorf("plan %s: GoalID = %q, want %q", planID, goalID, "goal_link_1")
		}
	}

	// A direct Plan() call also stamps GoalID on the returned ref.
	ref, err := loop.Plan(context.Background(), CandidatePlan{Title: "direct"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if ref.GoalID != "goal_link_1" {
		t.Errorf("PlanRef.GoalID = %q, want %q", ref.GoalID, "goal_link_1")
	}
}

// TestGoalLoopPlan_NoGoal_EmptyGoalID verifies the legacy-legal case: with no
// active goal the PlanRef carries an empty GoalID and SetPlanGoal is skipped.
func TestGoalLoopPlan_NoGoal_EmptyGoalID(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueResponse(`{"candidates":[{"title":"x","description":"d","prompt":"p"}]}`)
	planner := newGoalRecordingPlanner()

	loop := NewGoalLoop("emp-no-goal", testTier2Constitution(), nil, nil).
		WithReflector(reflector).
		WithPlanner(planner)

	ref, err := loop.Plan(context.Background(), CandidatePlan{Title: "x"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if ref.GoalID != "" {
		t.Errorf("PlanRef.GoalID = %q, want empty (no active goal)", ref.GoalID)
	}
	if len(planner.planGoals) != 0 {
		t.Errorf("SetPlanGoal called %d times, want 0", len(planner.planGoals))
	}
}

// TestImplicitGoal_CanExecutePlan_SystemApprover verifies SourceImplicit goals
// pass CanExecutePlan with ApproverID "system" at every tier (no signoff
// requirement, same as trigger goals).
func TestImplicitGoal_CanExecutePlan_SystemApprover(t *testing.T) {
	store := testGoalStore(t)
	seedBot(t, store, "emp-implicit")

	g, err := store.EnsureImplicitGoal(context.Background(), "emp-implicit", "anchor chat work")
	if err != nil {
		t.Fatalf("EnsureImplicitGoal: %v", err)
	}
	if g.Source != SourceImplicit {
		t.Errorf("Source = %q, want %q", g.Source, SourceImplicit)
	}

	for _, c := range []*Constitution{testTier1Constitution(), testTier2Constitution(), testTier3Constitution()} {
		loop := NewGoalLoop("emp-implicit", c, store, nil)
		if !loop.CanExecutePlan(PlanRef{ID: "p1", ApproverID: "system"}) {
			t.Errorf("tier %d: CanExecutePlan(approver=system) = false, want true for implicit goal", c.AutonomyTier)
		}
	}
}

// TestDecide_Tier2_ImplicitGoal_TreatedLikeTrigger verifies a tier-2 decide
// with an active SourceImplicit goal creates and tracks a pending plan exactly
// like a trigger-sourced goal (no additional signoff gate).
func TestDecide_Tier2_ImplicitGoal_TreatedLikeTrigger(t *testing.T) {
	store := testGoalStore(t)
	seedBot(t, store, "emp-impl-t2")

	g, err := store.EnsureImplicitGoal(context.Background(), "emp-impl-t2", "chat work")
	if err != nil {
		t.Fatalf("EnsureImplicitGoal: %v", err)
	}

	reflector := newStubReflector()
	reflector.queueResponse(`{"candidates":[{"title":"t2 work","description":"d","prompt":"p"}]}`)
	planner := newGoalRecordingPlanner()

	loop := NewGoalLoop("emp-impl-t2", testTier2Constitution(), store, nil).
		WithReflector(reflector).
		WithPlanner(planner)

	if err := loop.Decide(context.Background(), basicTrigger()); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if planner.CreatedCount() != 1 {
		t.Fatalf("planner created %d plans, want 1", planner.CreatedCount())
	}
	loaded, err := store.Get(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("Get goal: %v", err)
	}
	if len(loaded.ActivePlans()) == 0 {
		t.Error("pending plan not tracked on implicit goal")
	}
	for planID, goalID := range planner.planGoals {
		if goalID != g.ID {
			t.Errorf("plan %s: GoalID = %q, want %q", planID, goalID, g.ID)
		}
	}
}

// TestEnsureImplicitGoal_Idempotent verifies two calls return the same goal ID
// and that the created goal carries Source=implicit, State=active,
// Health=unknown, and Title/Mandate from the title argument.
func TestEnsureImplicitGoal_Idempotent(t *testing.T) {
	store := testGoalStore(t)
	seedBot(t, store, "emp-impl-idem")

	ctx := context.Background()
	g1, err := store.EnsureImplicitGoal(ctx, "emp-impl-idem", "anchor chat work")
	if err != nil {
		t.Fatalf("first EnsureImplicitGoal: %v", err)
	}
	if g1.ID == "" {
		t.Fatal("first call returned empty goal ID")
	}
	if g1.Source != SourceImplicit {
		t.Errorf("Source = %q, want %q", g1.Source, SourceImplicit)
	}
	if g1.State != GoalActive {
		t.Errorf("State = %d, want GoalActive", g1.State)
	}
	if g1.Health != GoalUnknown {
		t.Errorf("Health = %d, want GoalUnknown", g1.Health)
	}
	if g1.Title != "anchor chat work" || g1.Mandate != "anchor chat work" {
		t.Errorf("Title/Mandate = %q/%q, want both %q", g1.Title, g1.Mandate, "anchor chat work")
	}
	if g1.CreatedAt.IsZero() {
		t.Error("CreatedAt not set")
	}

	g2, err := store.EnsureImplicitGoal(ctx, "emp-impl-idem", "anchor chat work")
	if err != nil {
		t.Fatalf("second EnsureImplicitGoal: %v", err)
	}
	if g2.ID != g1.ID {
		t.Errorf("idempotency: second call returned ID %q, want %q", g2.ID, g1.ID)
	}

	// The goal round-trips through the store with the implicit source intact.
	loaded, err := store.Get(ctx, g1.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if loaded.Source != SourceImplicit {
		t.Errorf("persisted Source = %q, want %q", loaded.Source, SourceImplicit)
	}
}

// TestEnsureImplicitGoal_PerEmployeeAndSourceScoped verifies the filter only
// matches the employee's own active SourceImplicit goals: another employee's
// implicit goal and a non-implicit goal do not satisfy the lookup.
func TestEnsureImplicitGoal_PerEmployeeAndSourceScoped(t *testing.T) {
	store := testGoalStore(t)
	seedBot(t, store, "emp-scope-a")
	seedBot(t, store, "emp-scope-b")

	ctx := context.Background()

	// Employee A has an active non-implicit goal; the helper must create a
	// separate implicit goal rather than reuse it.
	if err := store.Create(ctx, &Goal{
		ID: "goal_scope_user", EmployeeID: "emp-scope-a",
		Title: "user goal", Mandate: "user goal",
		State: GoalActive, Source: SourceUser,
	}); err != nil {
		t.Fatalf("Create user goal: %v", err)
	}
	ga, err := store.EnsureImplicitGoal(ctx, "emp-scope-a", "implicit a")
	if err != nil {
		t.Fatalf("EnsureImplicitGoal(a): %v", err)
	}
	if ga.ID == "goal_scope_user" {
		t.Error("reused non-implicit goal as implicit goal")
	}

	// Employee B gets its own goal, distinct from A's.
	gb, err := store.EnsureImplicitGoal(ctx, "emp-scope-b", "implicit b")
	if err != nil {
		t.Fatalf("EnsureImplicitGoal(b): %v", err)
	}
	if gb.ID == ga.ID {
		t.Error("employee B reused employee A's implicit goal")
	}

	// Repeat calls for both stay idempotent.
	ga2, err := store.EnsureImplicitGoal(ctx, "emp-scope-a", "implicit a")
	if err != nil {
		t.Fatalf("EnsureImplicitGoal(a) again: %v", err)
	}
	if ga2.ID != ga.ID {
		t.Errorf("second call for A returned %q, want %q", ga2.ID, ga.ID)
	}
}
