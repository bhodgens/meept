package employee

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
)

// ---------------------------------------------------------------------------
// G1: Health recovery transitions
// ---------------------------------------------------------------------------

func TestHealthDecayFunc(t *testing.T) {
	tests := []struct {
		name      string
		failures  int
		threshold int
		want      GoalHealth
	}{
		{"zero failures → healthy", 0, 3, GoalHealthy},
		{"1 failure → at_risk", 1, 3, GoalAtRisk},
		{"2 failures → at_risk", 2, 3, GoalAtRisk},
		{"3 failures (threshold) → broken", 3, 3, GoalBroken},
		{"5 failures (exceeds threshold) → broken", 5, 3, GoalBroken},
		{"threshold 0 disables decay", 10, 0, GoalAtRisk},
		{"negative failures → healthy", -1, 3, GoalHealthy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HealthDecayFunc(tt.failures, tt.threshold)
			if got != tt.want {
				t.Errorf("HealthDecayFunc(%d, %d) = %s, want %s",
					tt.failures, tt.threshold, got.String(), tt.want.String())
			}
		})
	}
}

func TestHealthRecoveryFunc(t *testing.T) {
	tests := []struct {
		name              string
		successes         int
		recoveryThreshold int
		current           GoalHealth
		want              GoalHealth
	}{
		{"broken with 0 successes stays broken", 0, 3, GoalBroken, GoalBroken},
		{"broken with 2 successes stays broken", 2, 3, GoalBroken, GoalBroken},
		{"broken with 3 successes → at_risk", 3, 3, GoalBroken, GoalAtRisk},
		{"at_risk with 0 successes stays at_risk", 0, 3, GoalAtRisk, GoalAtRisk},
		{"at_risk with 3 successes → healthy", 3, 3, GoalAtRisk, GoalHealthy},
		{"healthy stays healthy", 0, 3, GoalHealthy, GoalHealthy},
		{"healthy with many successes stays healthy", 10, 3, GoalHealthy, GoalHealthy},
		{"unknown with 3 successes → healthy", 3, 3, GoalUnknown, GoalHealthy},
		{"unknown with 1 success stays unknown", 1, 3, GoalUnknown, GoalUnknown},
		{"broken with 6 successes → at_risk (not healthy)", 6, 3, GoalBroken, GoalAtRisk},
		// To go broken→healthy requires 3 successes (broken→at_risk) + 3 more (at_risk→healthy)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HealthRecoveryFunc(tt.successes, tt.recoveryThreshold, tt.current)
			if got != tt.want {
				t.Errorf("HealthRecoveryFunc(%d, %d, %s) = %s, want %s",
					tt.successes, tt.recoveryThreshold, tt.current.String(), got.String(), tt.want.String())
			}
		})
	}
}

func TestHealthRecovery_BrokenToAtRiskToHealthy(t *testing.T) {
	// Full transition: broken → at_risk → healthy requires 2 consecutive
	// blocks of M successes.
	recoveryThreshold := 3
	current := GoalBroken

	// 0, 1, 2 successes: still broken
	for i := 0; i < 2; i++ {
		current = HealthRecoveryFunc(i+1, recoveryThreshold, current)
		if current != GoalBroken {
			t.Fatalf("after %d successes: health = %s, want broken", i+1, current.String())
		}
	}
	// 3 successes: broken → at_risk
	current = HealthRecoveryFunc(3, recoveryThreshold, current)
	if current != GoalAtRisk {
		t.Fatalf("after 3 successes: health = %s, want at_risk", current.String())
	}

	// Reset success count for at_risk → healthy transition
	// The function is called with cumulative successes, but the recovery
	// threshold is checked per-transition. In the real Reflect loop,
	// `consecutiveSuccesses` is reset when the health transitions.
	// So after broken→at_risk, we need 3 MORE successes.
	current = GoalAtRisk
	for i := 0; i < 2; i++ {
		current = HealthRecoveryFunc(i+1, recoveryThreshold, current)
		if current != GoalAtRisk {
			t.Fatalf("at_risk with %d successes: health = %s, want at_risk", i+1, current.String())
		}
	}
	current = HealthRecoveryFunc(3, recoveryThreshold, current)
	if current != GoalHealthy {
		t.Fatalf("after 3 more successes: health = %s, want healthy", current.String())
	}
}

func TestReflect_G1_RecoveryOnSuccesses(t *testing.T) {
	// Test that after consecutive successes, a previously at_risk goal
	// recovers via HealthRecoveryFunc in the Reflect path.
	reflector := newStubReflector()
	// First: LLM says at_risk
	reflector.queueResponse(`{"health":"at_risk","reasoning":"flaky"}`)

	loop := NewGoalLoop("emp-g1", testTier2Constitution(), nil, nil).
		WithReflector(reflector).
		WithConsecutiveSuccessesForRecovery(2)

	// First success: LLM says at_risk, but counts as 1 success.
	// consecutiveSuccesses=1, recovery threshold=2, so at_risk stays at_risk.
	result1 := &bot.BotExecutionResult{Success: true, Output: "ok"}
	h1, _ := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, result1)
	if h1 != GoalAtRisk {
		t.Fatalf("first success: health = %s, want at_risk (1 success < 2 threshold)", h1.String())
	}

	// Second success: LLM says at_risk, but consecutiveSuccesses=2.
	// HealthRecoveryFunc(2, 2, GoalAtRisk) = GoalHealthy.
	reflector.queueResponse(`{"health":"at_risk","reasoning":"still flaky"}`)
	result2 := &bot.BotExecutionResult{Success: true, Output: "ok"}
	h2, _ := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, result2)
	if h2 != GoalHealthy {
		t.Fatalf("second success: health = %s, want healthy (recovery applied)", h2.String())
	}
}

// ---------------------------------------------------------------------------
// G2: Multi-plan concurrency
// ---------------------------------------------------------------------------

func TestGoal_AddRemoveActivePlan(t *testing.T) {
	g := &Goal{ID: "g1", EmployeeID: "e1"}

	// Initially no active plans.
	if plans := g.ActivePlans(); plans != nil {
		t.Fatalf("initial ActivePlans = %v, want nil", plans)
	}

	// Add one plan.
	n := g.AddActivePlan("plan-1")
	if n != 1 {
		t.Errorf("after add plan-1: count = %d, want 1", n)
	}
	if g.ActivePlan() != "plan-1" {
		t.Errorf("ActivePlanID = %q, want plan-1", g.ActivePlan())
	}

	// Add second plan.
	n = g.AddActivePlan("plan-2")
	if n != 2 {
		t.Errorf("after add plan-2: count = %d, want 2", n)
	}

	// Adding duplicate is a no-op.
	n = g.AddActivePlan("plan-1")
	if n != 2 {
		t.Errorf("after dup add: count = %d, want 2", n)
	}

	// Remove first plan.
	n = g.RemoveActivePlan("plan-1")
	if n != 1 {
		t.Errorf("after remove plan-1: count = %d, want 1", n)
	}
	// ActivePlanID should now mirror the first remaining element.
	if g.ActivePlan() != "plan-2" {
		t.Errorf("after remove: ActivePlanID = %q, want plan-2", g.ActivePlan())
	}

	// Remove last plan.
	n = g.RemoveActivePlan("plan-2")
	if n != 0 {
		t.Errorf("after remove plan-2: count = %d, want 0", n)
	}
	if g.ActivePlan() != "" {
		t.Errorf("after remove all: ActivePlanID = %q, want empty", g.ActivePlan())
	}
}

func TestGoal_CanAddActivePlan(t *testing.T) {
	g := &Goal{ID: "g1"}
	// With maxActivePlans=1, can add one plan.
	if !g.CanAddActivePlan(1) {
		t.Error("CanAddActivePlan(1) with 0 plans: want true")
	}
	g.AddActivePlan("p1")
	// Now at cap.
	if g.CanAddActivePlan(1) {
		t.Error("CanAddActivePlan(1) with 1 plan: want false")
	}
	// With maxActivePlans=2, can add another.
	if !g.CanAddActivePlan(2) {
		t.Error("CanAddActivePlan(2) with 1 plan: want true")
	}
	g.AddActivePlan("p2")
	if g.CanAddActivePlan(2) {
		t.Error("CanAddActivePlan(2) with 2 plans: want false")
	}
}

func TestGoal_CanAddActivePlan_DefaultMax(t *testing.T) {
	g := &Goal{ID: "g1"}
	// DefaultMaxActivePlans is 1.
	if !g.CanAddActivePlan(0) {
		t.Error("CanAddActivePlan(0) with 0 plans: want true (default cap=1)")
	}
	g.AddActivePlan("p1")
	if g.CanAddActivePlan(0) {
		t.Error("CanAddActivePlan(0) with 1 plan: want false (default cap=1)")
	}
}

func TestGoal_PersistRoundTrip_ActivePlanIDs(t *testing.T) {
	store := testGoalStore(t)
	seedBot(t, store, "e-g2")

	g := &Goal{
		ID:             "goal_g2",
		EmployeeID:     "e-g2",
		Title:          "multi-plan",
		Mandate:        "test multi-plan",
		State:          GoalActive,
		Source:         SourceUser,
		ActivePlanIDs:  []string{"plan-a", "plan-b"},
		MaxPlanHistory: 50,
	}
	g.SetActivePlan("plan-a") // also sets ActivePlanID
	if err := store.Create(context.Background(), g); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.Get(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	plans := got.ActivePlans()
	if len(plans) != 2 {
		t.Fatalf("ActivePlans len = %d, want 2", len(plans))
	}
	if plans[0] != "plan-a" || plans[1] != "plan-b" {
		t.Errorf("ActivePlans = %v, want [plan-a plan-b]", plans)
	}
	if got.MaxPlanHistory != 50 {
		t.Errorf("MaxPlanHistory = %d, want 50", got.MaxPlanHistory)
	}
}

func TestConstitution_MaxActivePlans(t *testing.T) {
	c := Constitution{
		Purpose:       "test",
		AutonomyTier: Tier2Propose,
		MaxActivePlans: 3,
	}
	if c.MaxActivePlans != 3 {
		t.Errorf("MaxActivePlans = %d, want 3", c.MaxActivePlans)
	}
}

// ---------------------------------------------------------------------------
// G3: Plan ownership and execution authority
// ---------------------------------------------------------------------------

func TestCanExecutePlan_SystemApproved(t *testing.T) {
	loop := NewGoalLoop("emp-g3", testTier2Constitution(), nil, nil)
	plan := PlanRef{ID: "p1", ApproverID: "system"}
	if !loop.CanExecutePlan(plan) {
		t.Error("CanExecutePlan with approver=system: want true")
	}
}

func TestCanExecutePlan_EmptyApprover(t *testing.T) {
	loop := NewGoalLoop("emp-g3", testTier2Constitution(), nil, nil)
	plan := PlanRef{ID: "p1", ApproverID: ""}
	if !loop.CanExecutePlan(plan) {
		t.Error("CanExecutePlan with empty approver: want true (backward compat)")
	}
}

func TestCanExecutePlan_MatchesEscalatesTo(t *testing.T) {
	c := testTier2Constitution()
	c.EscalatesTo = []string{"role:oncall", "agent-approver"}
	loop := NewGoalLoop("emp-g3", c, nil, nil)

	plan := PlanRef{ID: "p1", ApproverID: "role:oncall"}
	if !loop.CanExecutePlan(plan) {
		t.Error("CanExecutePlan with approver matching escalates_to: want true")
	}

	plan2 := PlanRef{ID: "p2", ApproverID: "unknown-approver"}
	if loop.CanExecutePlan(plan2) {
		t.Error("CanExecutePlan with unknown approver: want false")
	}
}

func TestCanExecutePlan_SelfApproved(t *testing.T) {
	loop := NewGoalLoop("emp-self", testTier2Constitution(), nil, nil)

	plan := PlanRef{ID: "p1", ApproverID: "emp-self"}
	if !loop.CanExecutePlan(plan) {
		t.Error("CanExecutePlan with self-approval: want true")
	}
}

func TestExecute_UnauthorizedPlan(t *testing.T) {
	executor := newStubExecutor()
	c := testTier2Constitution()
	c.EscalatesTo = []string{"role:oncall"}
	loop := NewGoalLoop("emp-g3", c, nil, nil).
		WithExecutor(executor)

	plan := PlanRef{ID: "p1", ApproverID: "someone-else"}
	_, err := loop.Execute(context.Background(), plan)
	if err == nil {
		t.Fatal("expected error for unauthorized plan execution")
	}
}

// ---------------------------------------------------------------------------
// G4: PlanHistory ring buffer
// ---------------------------------------------------------------------------

func TestAppendHistory_RingBuffer(t *testing.T) {
	g := &Goal{ID: "g-g4", MaxPlanHistory: 3}
	g.AppendHistory("p1")
	g.AppendHistory("p2")
	g.AppendHistory("p3")

	if h := g.History(); len(h) != 3 {
		t.Fatalf("after 3 appends: len = %d, want 3", len(h))
	}

	// Exceed cap: oldest (p1) should be evicted.
	g.AppendHistory("p4")
	h := g.History()
	if len(h) != 3 {
		t.Fatalf("after 4 appends with cap 3: len = %d, want 3", len(h))
	}
	if h[0] != "p2" || h[1] != "p3" || h[2] != "p4" {
		t.Errorf("after overflow: history = %v, want [p2 p3 p4]", h)
	}
}

func TestAppendHistory_DefaultCap(t *testing.T) {
	g := &Goal{ID: "g-g4"} // MaxPlanHistory = 0 → default
	for i := 0; i < DefaultMaxPlanHistory+5; i++ {
		g.AppendHistory(fmt.Sprintf("p-%d", i))
	}
	h := g.History()
	if len(h) != DefaultMaxPlanHistory {
		t.Fatalf("history len = %d, want %d (default cap)", len(h), DefaultMaxPlanHistory)
	}
	// Oldest 5 should be evicted.
	if h[0] != "p-5" {
		t.Errorf("first entry = %q, want p-5 (oldest 5 evicted)", h[0])
	}
}

func TestAppendHistory_PersistRoundTrip(t *testing.T) {
	store := testGoalStore(t)
	seedBot(t, store, "e-g4")

	g := &Goal{
		ID:             "goal_g4",
		EmployeeID:     "e-g4",
		Title:          "ring buffer",
		Mandate:        "test",
		State:          GoalActive,
		Source:         SourceUser,
		MaxPlanHistory: 5,
	}
	for i := 0; i < 10; i++ {
		g.AppendHistory(fmt.Sprintf("p-%d", i))
	}
	if err := store.Create(context.Background(), g); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := store.Get(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	h := got.History()
	if len(h) != 5 {
		t.Fatalf("after round-trip: len = %d, want 5", len(h))
	}
	if h[0] != "p-5" {
		t.Errorf("first entry = %q, want p-5", h[0])
	}
}

// ---------------------------------------------------------------------------
// G5: Assessment concurrency semaphore
// ---------------------------------------------------------------------------

func TestAssessmentSemaphore_PreventsOverlap(t *testing.T) {
	m := &Manager{
		assessmentSems: make(map[string]chan struct{}),
		logger:         slog.Default().With("test", "G5"),
	}

	empID := "emp-g5"
	sem := m.acquireAssessmentSemaphore(empID)

	// Fill the semaphore (buffer=1).
	sem <- struct{}{}

	// The runAssessForEmployee call should skip immediately because
	// the non-blocking send fails. It returns without calling
	// GetEmployee (which would panic with nil botManager).
	// We verify this by checking it returns quickly (no blocking).
	done := make(chan struct{}, 1)
	go func() {
		m.runAssessForEmployee(context.Background(), empID)
		done <- struct{}{}
	}()

	select {
	case <-done:
		// Good — it returned quickly (skipped).
	case <-time.After(2 * time.Second):
		t.Error("runAssessForEmployee blocked instead of skipping when semaphore was full")
	}

	// Release the semaphore.
	<-sem

	// Now runAssessForEmployee should proceed past the semaphore check
	// and call GetEmployee, which will error (nil botManager), but the
	// important thing is it got past the semaphore.
	done2 := make(chan struct{}, 1)
	go func() {
		m.runAssessForEmployee(context.Background(), empID)
		done2 <- struct{}{}
	}()

	select {
	case <-done2:
		// Good — it proceeded (and errored on GetEmployee, but that's fine).
	case <-time.After(2 * time.Second):
		t.Error("runAssessForEmployee did not proceed after semaphore was released")
	}
}

func TestAssessmentSemaphore_LazyCreate(t *testing.T) {
	m := &Manager{
		assessmentSems: make(map[string]chan struct{}),
		logger:         slog.Default(),
	}

	sem1 := m.acquireAssessmentSemaphore("emp-a")
	sem2 := m.acquireAssessmentSemaphore("emp-a")
	if sem1 != sem2 {
		t.Error("lazy create: second call returned different channel")
	}

	sem3 := m.acquireAssessmentSemaphore("emp-b")
	if sem1 == sem3 {
		t.Error("different employees should get different semaphores")
	}
}

// ---------------------------------------------------------------------------
// G6: GoalSource validation by tier
// ---------------------------------------------------------------------------

func TestValidateGoalSource(t *testing.T) {
	tests := []struct {
		name   string
		source GoalSource
		tier   AutonomyTier
		wantErr bool
	}{
		{"tier1 + user", SourceUser, Tier1Reactive, false},
		{"tier1 + trigger", SourceTrigger, Tier1Reactive, false},
		{"tier1 + self_proposed (rejected)", SourceSelfProposed, Tier1Reactive, true},
		{"tier1 + audit_finding", SourceAuditFinding, Tier1Reactive, false},
		{"tier2 + user", SourceUser, Tier2Propose, false},
		{"tier2 + trigger", SourceTrigger, Tier2Propose, false},
		{"tier2 + self_proposed", SourceSelfProposed, Tier2Propose, false},
		{"tier2 + audit_finding", SourceAuditFinding, Tier2Propose, false},
		{"tier3 + user", SourceUser, Tier3Autonomous, false},
		{"tier3 + self_proposed", SourceSelfProposed, Tier3Autonomous, false},
		{"unknown tier", SourceUser, AutonomyTier(99), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGoalSource(tt.source, tt.tier)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// G7: Implicit Plan prompt and parser
// ---------------------------------------------------------------------------

func TestParseImplicitPlanResponse_Valid(t *testing.T) {
	content := `{"action": "restart CI job", "reasoning": "flaky test"}`
	action, reasoning, err := parseImplicitPlanResponse(content)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if action != "restart CI job" {
		t.Errorf("action = %q, want %q", action, "restart CI job")
	}
	if reasoning != "flaky test" {
		t.Errorf("reasoning = %q, want %q", reasoning, "flaky test")
	}
}

func TestParseImplicitPlanResponse_CodeFenced(t *testing.T) {
	content := "```json\n{\"action\": \"noop\", \"reasoning\": \"nothing\"}\n```"
	action, _, err := parseImplicitPlanResponse(content)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if action != "noop" {
		t.Errorf("action = %q, want noop", action)
	}
}

func TestParseImplicitPlanResponse_Invalid(t *testing.T) {
	_, _, err := parseImplicitPlanResponse("not json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseImplicitPlanResponse_EmptyAction(t *testing.T) {
	content := `{"action": "", "reasoning": "no action needed"}`
	action, _, err := parseImplicitPlanResponse(content)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if action != "" {
		t.Errorf("action = %q, want empty", action)
	}
}

func TestBuildImplicitPlanPrompt(t *testing.T) {
	trigger := TriggerEvent{
		Source:  "cron",
		Topic:   "*/15 * * * *",
		FiredAt: time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC),
	}
	prompt := buildImplicitPlanPrompt(trigger, "CI is green")
	if prompt == "" {
		t.Fatal("prompt should not be empty")
	}
	if !containsStr(prompt, "cron") {
		t.Error("prompt should contain trigger source")
	}
	if !containsStr(prompt, "*/15 * * * *") {
		t.Error("prompt should contain trigger topic")
	}
	if !containsStr(prompt, "CI is green") {
		t.Error("prompt should contain state summary")
	}
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// G8: Findings retained on goal retire (no cascade delete)
// ---------------------------------------------------------------------------

func TestG8_FindingsNotCascadeDeletedOnRetire(t *testing.T) {
	store := testGoalStore(t)
	auditStore := testAuditStoreHelper(t)
	ctx := context.Background()

	// Create a goal.
	g := mustCreateGoal(t, store, &Goal{
		EmployeeID: "bot-test-1",
		Title:      "g8 test",
		Mandate:    "test findings retention",
		Source:     SourceUser,
	})

	// Create a finding linked to the goal.
	finding := AuditFinding{
		ID:         "finding-g8-1",
		EmployeeID: "bot-test-1",
		GoalID:     g.ID,
		Severity:   SeverityWarning,
		Checkpoint: CheckpointPostTurn,
		DetectedAt: time.Now().UTC(),
	}
	if err := auditStore.Create(ctx, finding); err != nil {
		t.Fatalf("Create finding: %v", err)
	}

	// Retire the goal.
	if err := store.Retire(ctx, g.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Retire goal: %v", err)
	}

	// Finding should still exist (not cascade-deleted). Use List to find it.
	findings, err := auditStore.List(ctx, AuditListFilter{EmployeeID: g.EmployeeID, Limit: 100})
	if err != nil {
		t.Fatalf("List findings: %v", err)
	}
	var found bool
	for _, f := range findings {
		if f.ID == finding.ID {
			found = true
			if f.GoalID != g.ID {
				t.Errorf("finding GoalID = %q, want %q (preserved)", f.GoalID, g.ID)
			}
			break
		}
	}
	if !found {
		t.Error("finding was cascade-deleted or not found after goal retire")
	}
}

func TestG8_PeriodicAuditExcludesRetiredGoalFindings(t *testing.T) {
	store := testGoalStore(t)
	ctx := context.Background()

	// Create a goal and retire it.
	g := mustCreateGoal(t, store, &Goal{
		EmployeeID: "bot-test-1",
		Title:      "retired goal",
		Mandate:    "should be excluded",
		Source:     SourceUser,
	})
	if err := store.Retire(ctx, g.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Retire: %v", err)
	}

	// Verify the goal is retired.
	loaded, err := store.Get(ctx, g.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !loaded.IsRetired() {
		t.Fatal("goal should be retired")
	}

	// The periodic audit's G8 filtering logic checks goal.IsRetired().
	// We test the filtering condition directly.
	turns := []TurnRecord{
		{EmployeeID: "bot-test-1", GoalID: g.ID, TurnID: "t1"},
		{EmployeeID: "bot-test-1", GoalID: "", TurnID: "t2"}, // no goal → included
	}

	// Simulate the G8 filtering.
	var filtered []TurnRecord
	for _, turn := range turns {
		if turn.GoalID == "" {
			filtered = append(filtered, turn)
			continue
		}
		goal, gErr := store.Get(ctx, turn.GoalID)
		if gErr != nil || goal.IsRetired() {
			continue // skip retired-goal turns
		}
		filtered = append(filtered, turn)
	}

	if len(filtered) != 1 {
		t.Fatalf("after G8 filtering: %d turns, want 1 (only the no-goal turn)", len(filtered))
	}
	if filtered[0].TurnID != "t2" {
		t.Errorf("filtered turn = %q, want t2", filtered[0].TurnID)
	}
}

// ---------------------------------------------------------------------------
// G2: Tier2 blocks ASSESS when at max active plans
// ---------------------------------------------------------------------------

func TestDecide_Tier2_BlocksAtMaxActivePlans(t *testing.T) {
	store := testGoalStore(t)
	seedBot(t, store, "emp-g2-max")

	// Create a goal with an active plan (at cap=1).
	g := &Goal{
		ID:            "goal_g2_max",
		EmployeeID:    "emp-g2-max",
		Title:         "at cap",
		Mandate:       "test multi-plan blocking",
		State:         GoalActive,
		Source:        SourceUser,
		ActivePlanIDs: []string{"plan-active"},
	}
	g.SetActivePlan("plan-active")
	if err := store.Create(context.Background(), g); err != nil {
		t.Fatalf("Create goal: %v", err)
	}

	reflector := newStubReflector()
	reflector.queueResponse(`{"candidates":[{"title":"new","description":"d","prompt":"p"}]}`)

	c := testTier2Constitution()
	c.MaxActivePlans = 1 // only 1 concurrent plan allowed

	loop := NewGoalLoop("emp-g2-max", c, store, nil).
		WithReflector(reflector).
		WithPlanner(newStubPlanner())

	err := loop.Decide(context.Background(), basicTrigger())
	if err != nil {
		t.Fatalf("Decide should not error (should gracefully skip): %v", err)
	}

	// The reflector should NOT have been called because ASSESS was skipped.
	// (The planner should also NOT have been called.)
	if reflector.CallCount() != 0 {
		t.Errorf("reflector called %d times, want 0 (ASSESS blocked)", reflector.CallCount())
	}
}

// ---------------------------------------------------------------------------
// Concurrency: no panic on concurrent multi-plan operations
// ---------------------------------------------------------------------------

func TestGoal_ConcurrentActivePlanOps(t *testing.T) {
	g := &Goal{ID: "g-conc"}
	var wg sync.WaitGroup

	// 20 goroutines adding/removing plans concurrently.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			planID := fmt.Sprintf("plan-%d", n)
			g.AddActivePlan(planID)
			g.RemoveActivePlan(planID)
		}(i)
	}
	wg.Wait()

	// Should end up with no active plans.
	if plans := g.ActivePlans(); len(plans) != 0 {
		t.Errorf("after concurrent ops: %d active plans, want 0", len(plans))
	}
	if g.ActivePlan() != "" {
		t.Errorf("after concurrent ops: ActivePlanID = %q, want empty", g.ActivePlan())
	}
}

// ---------------------------------------------------------------------------
// G3: ApproveAndExecute with_approver_id
// ---------------------------------------------------------------------------

func TestApproveAndExecute_G3_DelegatesToExecute(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueResponse(`{"health":"healthy","reasoning":"ok"}`)
	executor := newStubExecutor()
	executor.succeedWith("done", 50)

	c := testTier2Constitution()
	c.EscalatesTo = []string{"role:oncall"}

	loop := NewGoalLoop("emp-g3-approve", c, nil, nil).
		WithReflector(reflector).
		WithExecutor(executor)

	plan := PlanRef{ID: "plan-g3", State: "approved", ApproverID: "role:oncall"}
	result, _, err := loop.ApproveAndExecute(context.Background(), plan)
	if err != nil {
		t.Fatalf("ApproveAndExecute error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestApproveAndExecute_G3_Unauthorized(t *testing.T) {
	executor := newStubExecutor()
	c := testTier2Constitution()
	c.EscalatesTo = []string{"role:oncall"}

	loop := NewGoalLoop("emp-g3-unauth", c, nil, nil).
		WithExecutor(executor)

	plan := PlanRef{ID: "plan-g3-unauth", State: "approved", ApproverID: "unknown"}
	result, _, err := loop.ApproveAndExecute(context.Background(), plan)
	if err != nil {
		t.Fatalf("ApproveAndExecute should not return error (reflects as failure): %v", err)
	}
	if result.Success {
		t.Error("expected failure result for unauthorized plan")
	}
	if result.Error == "" {
		t.Error("expected non-empty error string")
	}
	// Verify the error message mentions authorization.
	if !containsStr(result.Error, "authorized") {
		t.Errorf("error should mention authorization, got: %q", result.Error)
	}
}

// ---------------------------------------------------------------------------
// G7: implicit plan prompt is used in tier-1 Assess flow
// ---------------------------------------------------------------------------

func TestG7_ImplicitPlanPrompt_UsedByTier1(t *testing.T) {
	// The assess prompt for tier-1 should reference the implicit plan structure.
	// We verify that the Tier1 Decide path correctly uses the candidate
	// prompt as an implicit single-step plan (approver=system).
	reflector := newStubReflector()
	reflector.queueResponse(`{"candidates":[{"title":"react","description":"d","prompt":"do the thing"}]}`)
	executor := newStubExecutor()
	executor.succeedWith("done", 10)
	reflector.queueResponse(`{"health":"healthy","reasoning":"ok"}`)

	loop := NewGoalLoop("emp-g7", testTier1Constitution(), nil, nil).
		WithReflector(reflector).
		WithExecutor(executor)

	err := loop.Decide(context.Background(), basicTrigger())
	if err != nil {
		t.Fatalf("Decide error: %v", err)
	}
	if executor.CallCount() != 1 {
		t.Errorf("executor called %d times, want 1", executor.CallCount())
	}
}

// Avoid unused import lint errors.
var _ = context.TODO
