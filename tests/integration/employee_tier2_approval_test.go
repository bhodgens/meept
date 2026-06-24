// Package integration — employee_tier2_approval_test.go drives the full
// tier-2 approval cycle described in spec line 644: "ASSESS produces
// plan → signoff via RPC → EXECUTE → REFLECT updates goal health."
//
// This test exercises two production code paths:
//  1. The GoalLoop itself: GoalLoop.Assess → GoalLoop.Plan →
//     GoalLoop.ApproveAndExecute, which internally runs Execute +
//     Reflect. The GoalStore holds the live goal, so health transitions
//     are observable after Reflect completes.
//  2. Manager.ApprovePlan and Manager.RejectPlan via a stub
//     PlanDisposer, verifying the Manager delegates to the disposer
//     and updates the goal's ActivePlanID on approval.
//
// This file re-declares the chatter/executor/planner stubs locally
// (same pattern as mcp_toggle_test.go uses for the stub script).
package integration

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/id"
)

// tier2Chatter is a queue-driven llm.Chatter stub for the tier-2 test.
// Each Chat call pops the next response; if the queue is empty it
// returns a default "no candidates" response. This mirrors the
// stubReflector pattern used in internal/employee/goal_loop_test.go.
type tier2Chatter struct {
	mu        sync.Mutex
	responses []*llm.Response
	calls     atomic.Int32
}

func (c *tier2Chatter) queue(content string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.responses = append(c.responses, &llm.Response{Content: content})
}

func (c *tier2Chatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	c.calls.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.responses) == 0 {
		// Default to "healthy" so a missing queue entry doesn't crash
		// Reflect.
		return &llm.Response{Content: `{"health":"healthy","reasoning":"default"}`}, nil
	}
	resp := c.responses[0]
	c.responses = c.responses[1:]
	return resp, nil
}

func (c *tier2Chatter) ChatWithProgress(_ context.Context, msgs []llm.ChatMessage, _ llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(context.Background(), msgs, opts...)
}

func (c *tier2Chatter) Config() *llm.ModelConfig { return nil }

// tier2Executor is a BotExecutor stub that records ExecuteBot calls and
// returns a canned successful output. Mirrors stubExecutor from
// internal/employee/goal_loop_test.go.
type tier2Executor struct {
	output string
	tokens int
	err    error
	calls  atomic.Int32
}

func (e *tier2Executor) ExecuteBot(_ context.Context, _, _ string) (string, int, error) {
	e.calls.Add(1)
	if e.err != nil {
		return "", 0, e.err
	}
	return e.output, e.tokens, nil
}

// tier2Planner is a PlanCreator stub. Each CreatePlan call returns a
// fresh plan ID and a pending_approval state. Mirrors stubPlanner from
// internal/employee/goal_loop_test.go.
type tier2Planner struct {
	mu       sync.Mutex
	nextID   int
	created  int32
	idPrefix string
}

func newTier2Planner() *tier2Planner {
	return &tier2Planner{idPrefix: "plan-t2-"}
}

func (p *tier2Planner) CreatePlan(_ context.Context, _, _, _, _ string) (employee.PlanRef, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextID++
	atomic.AddInt32(&p.created, 1)
	return employee.PlanRef{
		ID:    p.idPrefix + zeroPad(p.nextID),
		State: "pending_approval",
	}, nil
}

// stubPlanDisposer is a PlanDisposer that records approve/reject calls
// and optionally returns a canned error.
type stubPlanDisposer struct {
	mu          sync.Mutex
	approvedIDs []string
	rejectedIDs []string
	approveErr  error
	rejectErr   error
}

func (d *stubPlanDisposer) ApprovePlan(_ context.Context, planID, _, _ string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.approveErr != nil {
		return d.approveErr
	}
	d.approvedIDs = append(d.approvedIDs, planID)
	return nil
}

func (d *stubPlanDisposer) RejectPlan(_ context.Context, planID, _, _, _ string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.rejectErr != nil {
		return d.rejectErr
	}
	d.rejectedIDs = append(d.rejectedIDs, planID)
	return nil
}

func zeroPad(n int) string {
	if n < 10 {
		return "00" + itoa(n)
	}
	if n < 100 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

// itoa is a dependency-free int → string converter (avoiding fmt to keep
// the stub allocation-free).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// TestEmployee_Tier2ApprovalCycle drives the tier-2 plan approval cycle
// end-to-end:
//
//  1. Hire a tier_2_propose employee with a GoalStore-backed goal.
//  2. GoalLoop.Assess produces a candidate plan from a stub LLM.
//  3. GoalLoop.Plan creates a pending plan via a stub PlanCreator.
//  4. GoalLoop.ApproveAndExecute runs Execute (stub BotExecutor) +
//     Reflect (stub LLM) and updates the goal's health.
//  5. The GoalStore shows the health transition and the plan ID in the
//     goal's history.
//  6. Manager.ApprovePlan delegates to a stub PlanDisposer and updates
//     the goal's ActivePlanID.
//  7. Manager.RejectPlan delegates to the stub PlanDisposer without
//     touching the goal.
func TestEmployee_Tier2ApprovalCycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping tier-2 approval integration test in short mode")
	}

	env := newEmployeeLifecycleEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// --- setup: hire a tier-2 employee and give it a goal ---
	employeeID := id.Generate("emp_t2_")

	tier2Constitution := validConstitutionMap()
	tier2Constitution["autonomy_tier"] = "tier_2_propose"
	tier2Constitution["purpose"] = "tier 2 approval poc"
	tier2Constitution["constraints"] = map[string]any{
		"risk_ceiling":        "medium",
		"assessment_interval": "15m",
	}

	_, err := env.empMgr.Hire(ctx, employee.HireRequest{
		ID:           employeeID,
		Name:         "tier-2-proposer",
		Description:  "tier-2 propose employee",
		Prompt:       "monitor and propose plans",
		Model:        "stub-model",
		Triggers:     []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
		Tools:        []string{"file_read"},
		Enabled:      true,
		Constitution: tier2Constitution,
	})
	if err != nil {
		t.Fatalf("Hire tier-2: %v", err)
	}

	emp, err := env.empMgr.GetEmployee(ctx, employeeID)
	if err != nil {
		t.Fatalf("GetEmployee: %v", err)
	}
	constitution := emp.Constitution
	if constitution.AutonomyTier != employee.Tier2Propose {
		t.Fatalf("autonomy tier = %v, want Tier2Propose", constitution.AutonomyTier)
	}

	// Seed a goal for the employee. GoalStore.Create is called directly
	// (production path is via Manager.ListGoals / GoalLoop wiring).
	goal := &employee.Goal{
		ID:         employee.NewGoalID(),
		EmployeeID: employeeID,
		Title:      "keep CI green",
		Mandate:    "all PR builds should pass",
		State:      employee.GoalActive,
		Source:     employee.SourceUser,
		Health:     employee.GoalHealthy,
	}
	if err := env.goalStore.Create(ctx, goal); err != nil {
		t.Fatalf("goalStore.Create: %v", err)
	}

	// --- wire GoalLoop with stubs ---
	chatter := &tier2Chatter{}
	// ASSESS response: a single candidate plan.
	chatter.queue(`{"candidates":[{"title":"revert failing commit","description":"revert the commit that broke main","prompt":"run git revert"}]}`)
	// REFLECT response: healthy outcome.
	chatter.queue(`{"health":"healthy","reasoning":"revert restored green builds"}`)

	executor := &tier2Executor{output: "reverted commit abc123", tokens: 250}
	planner := newTier2Planner()

	loop := employee.NewGoalLoop(employeeID, &constitution, env.goalStore, nil).
		WithExecutor(executor).
		WithPlanner(planner).
		WithReflector(chatter)

	// --- ASSESS ---
	var candidates []employee.CandidatePlan
	t.Run("assess produces candidate", func(t *testing.T) {
		candidates, err = loop.Assess(ctx, employee.TriggerEvent{
			Source:   "webhook",
			Topic:    "github.push",
			Payload:  []byte(`{"status":"failure"}`),
			FiredAt:  time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("Assess: %v", err)
		}
		if len(candidates) == 0 {
			t.Fatal("expected at least one candidate from Assess")
		}
		if candidates[0].Title == "" {
			t.Error("candidate Title is empty")
		}
	})

	// --- PLAN ---
	var planRef employee.PlanRef
	t.Run("plan creates pending plan", func(t *testing.T) {
		planRef, err = loop.Plan(ctx, candidates[0])
		if err != nil {
			t.Fatalf("Plan: %v", err)
		}
		if planRef.ID == "" {
			t.Fatal("Plan returned empty ID")
		}
		if planRef.State != "pending_approval" {
			t.Errorf("plan state = %q, want 'pending_approval'", planRef.State)
		}
	})

	// --- APPROVE & EXECUTE ---
	t.Run("approve and execute updates goal health", func(t *testing.T) {
		result, health, err := loop.ApproveAndExecute(ctx, planRef)
		if err != nil {
			t.Fatalf("ApproveAndExecute: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil BotExecutionResult")
		}
		if !result.Success {
			t.Errorf("result.Success = false; error = %q", result.Error)
		}
		if executor.calls.Load() == 0 {
			t.Error("expected BotExecutor.ExecuteBot to be called")
		}
		if health != employee.GoalHealthy {
			t.Errorf("post-execute health = %q, want %q",
				health.String(), employee.GoalHealthy.String())
		}

		// The goal must now carry the plan in its history and have
		// LastAssessed populated. Fetch the latest row from the store.
		updated, err := env.goalStore.Get(ctx, goal.ID)
		if err != nil {
			t.Fatalf("goalStore.Get after ApproveAndExecute: %v", err)
		}
		history := updated.History()
		if len(history) == 0 || history[len(history)-1] != planRef.ID {
			t.Errorf("plan %q not in goal history %v", planRef.ID, history)
		}
		if updated.ActivePlan() != "" {
			t.Errorf("active plan = %q, want empty (cleared after execute)",
				updated.ActivePlan())
		}
		if updated.LastAssessed.IsZero() {
			t.Error("LastAssessed should be non-zero after Reflect")
		}
		if updated.Health != employee.GoalHealthy {
			t.Errorf("persisted goal health = %q, want %q",
				updated.Health.String(), employee.GoalHealthy.String())
		}
	})

	// --- Manager.ApprovePlan / RejectPlan without disposer configured ---
	// When no PlanDisposer is wired, the methods should return a clear
	// "not configured" error, not ErrNotImplemented.
	t.Run("manager.ApprovePlan without disposer returns not-configured error", func(t *testing.T) {
		err := env.empMgr.ApprovePlan(ctx, goal.ID, "plan-fake", "approved")
		if err == nil {
			t.Fatal("expected error when plan disposer not configured, got nil")
		}
		if errors.Is(err, employee.ErrNotImplemented) {
			t.Errorf("should not return ErrNotImplemented now that the stub is implemented: %v", err)
		}
	})

	t.Run("manager.RejectPlan without disposer returns not-configured error", func(t *testing.T) {
		err := env.empMgr.RejectPlan(ctx, goal.ID, "plan-fake", "rejected")
		if err == nil {
			t.Fatal("expected error when plan disposer not configured, got nil")
		}
		if errors.Is(err, employee.ErrNotImplemented) {
			t.Errorf("should not return ErrNotImplemented now that the stub is implemented: %v", err)
		}
	})

	// --- Manager.ApprovePlan / RejectPlan with disposer wired ---
	// Seed a second goal for the disposer-backed subtests so the
	// ActivePlanID assertion is against a clean goal.
	disposerGoal := &employee.Goal{
		ID:         employee.NewGoalID(),
		EmployeeID: employeeID,
		Title:      "disposer test goal",
		Mandate:    "exercises Manager.ApprovePlan via disposer",
		State:      employee.GoalActive,
		Source:     employee.SourceUser,
		Health:     employee.GoalHealthy,
	}
	if err := env.goalStore.Create(ctx, disposerGoal); err != nil {
		t.Fatalf("goalStore.Create disposerGoal: %v", err)
	}

	disposer := &stubPlanDisposer{}
	env.empMgr.SetPlanDisposer(disposer)

	t.Run("manager.ApprovePlan delegates to disposer and sets active plan", func(t *testing.T) {
		planID := "plan-approve-test"
		if err := env.empMgr.ApprovePlan(ctx, disposerGoal.ID, planID, "looks good"); err != nil {
			t.Fatalf("ApprovePlan: %v", err)
		}
		// Disposer recorded the approval.
		disposer.mu.Lock()
		found := false
		for _, id := range disposer.approvedIDs {
			if id == planID {
				found = true
				break
			}
		}
		disposer.mu.Unlock()
		if !found {
			t.Errorf("plan %q not in disposer.approvedIDs", planID)
		}
		// Goal's ActivePlanID was set.
		updated, err := env.goalStore.Get(ctx, disposerGoal.ID)
		if err != nil {
			t.Fatalf("goalStore.Get: %v", err)
		}
		if updated.ActivePlan() != planID {
			t.Errorf("active plan = %q, want %q", updated.ActivePlan(), planID)
		}
	})

	t.Run("manager.RejectPlan delegates to disposer without touching goal", func(t *testing.T) {
		// Seed a third goal for the reject subtest.
		rejectGoal := &employee.Goal{
			ID:         employee.NewGoalID(),
			EmployeeID: employeeID,
			Title:      "reject test goal",
			Mandate:    "exercises Manager.RejectPlan",
			State:      employee.GoalActive,
			Source:     employee.SourceUser,
			Health:     employee.GoalHealthy,
		}
		if err := env.goalStore.Create(ctx, rejectGoal); err != nil {
			t.Fatalf("goalStore.Create rejectGoal: %v", err)
		}

		planID := "plan-reject-test"
		if err := env.empMgr.RejectPlan(ctx, rejectGoal.ID, planID, "bad idea"); err != nil {
			t.Fatalf("RejectPlan: %v", err)
		}
		// Disposer recorded the rejection.
		disposer.mu.Lock()
		found := false
		for _, id := range disposer.rejectedIDs {
			if id == planID {
				found = true
				break
			}
		}
		disposer.mu.Unlock()
		if !found {
			t.Errorf("plan %q not in disposer.rejectedIDs", planID)
		}
		// Goal's ActivePlanID should remain empty.
		updated, err := env.goalStore.Get(ctx, rejectGoal.ID)
		if err != nil {
			t.Fatalf("goalStore.Get: %v", err)
		}
		if updated.ActivePlan() != "" {
			t.Errorf("active plan = %q, want empty after reject", updated.ActivePlan())
		}
	})
}

// TestEmployee_Tier2AssessNoCandidates covers the ASSESS no-op path
// (spec line 623): when ASSESS finds nothing to do, Decide returns nil
// and the goal's health is left unchanged. This guards against
// regressions where an empty candidates slice gets misinterpreted as a
// failure.
func TestEmployee_Tier2AssessNoCandidates(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping tier-2 no-op integration test in short mode")
	}

	env := newEmployeeLifecycleEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	employeeID := id.Generate("emp_t2_")
	tier2Constitution := validConstitutionMap()
	tier2Constitution["autonomy_tier"] = "tier_2_propose"
	if _, err := env.empMgr.Hire(ctx, employee.HireRequest{
		ID:           employeeID,
		Name:         "tier-2-noop",
		Description:  "tier-2 noop employee",
		Prompt:       "monitor and propose plans",
		Triggers:     []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
		Enabled:      true,
		Constitution: tier2Constitution,
	}); err != nil {
		t.Fatalf("Hire: %v", err)
	}

	emp, err := env.empMgr.GetEmployee(ctx, employeeID)
	if err != nil {
		t.Fatalf("GetEmployee: %v", err)
	}
	constitution := emp.Constitution

	// Chatter returns no candidates.
	chatter := &tier2Chatter{}
	chatter.queue(`{"candidates":[]}`)

	executor := &tier2Executor{output: "should not run"}
	planner := newTier2Planner()

	loop := employee.NewGoalLoop(employeeID, &constitution, env.goalStore, nil).
		WithExecutor(executor).
		WithPlanner(planner).
		WithReflector(chatter)

	// Decide dispatches to decideTier2 which calls Assess; if no
	// candidates come back it logs debug and returns nil (spec line
	// 623).
	if err := loop.Decide(ctx, employee.TriggerEvent{
		Source:  "cron",
		Topic:   "15m",
		FiredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Decide with no candidates: %v", err)
	}

	if executor.calls.Load() != 0 {
		t.Errorf("executor was called %d times; expected 0 on no-op Assess",
			executor.calls.Load())
	}
	if atomic.LoadInt32(&planner.created) != 0 {
		t.Errorf("planner created %d plans; expected 0 on no-op Assess",
			atomic.LoadInt32(&planner.created))
	}
}
