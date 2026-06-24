package employee

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/llm"
)

// ---------------------------------------------------------------------------
// Test stubs
// ---------------------------------------------------------------------------

// stubReflector is a controllable Reflector (llm.Chatter) for tests. It
// returns canned responses based on the label set via queueResponse. Each
// Chat call pops the next response. If no responses are queued, it returns
// the default response.
type stubReflector struct {
	mu        sync.Mutex
	responses []*llm.Response
	errs      []error
	calls     int32
	default_  *llm.Response
}

func newStubReflector() *stubReflector {
	return &stubReflector{
		default_: &llm.Response{Content: `{"candidates":[]}`},
	}
}

func (s *stubReflector) queueResponse(content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responses = append(s.responses, &llm.Response{Content: content})
}

func (s *stubReflector) queueError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errs = append(s.errs, err)
}

func (s *stubReflector) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	atomic.AddInt32(&s.calls, 1)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.errs) > 0 {
		err := s.errs[0]
		s.errs = s.errs[1:]
		return nil, err
	}
	if len(s.responses) > 0 {
		resp := s.responses[0]
		s.responses = s.responses[1:]
		return resp, nil
	}
	return s.default_, nil
}

func (s *stubReflector) Config() *llm.ModelConfig { return nil }

func (s *stubReflector) ChatWithProgress(_ context.Context, msgs []llm.ChatMessage, _ llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return s.Chat(context.Background(), msgs, opts...)
}

func (s *stubReflector) CallCount() int32 { return atomic.LoadInt32(&s.calls) }

// stubExecutor is a controllable BotExecutor for tests.
type stubExecutor struct {
	mu     sync.Mutex
	output string
	tokens int
	err    error
	calls  int32
	execFn func(ctx context.Context, systemPrompt, userMessage string) (string, int, error)
}

func newStubExecutor() *stubExecutor {
	return &stubExecutor{
		output: "execution succeeded",
		tokens: 100,
	}
}

func (e *stubExecutor) ExecuteBot(ctx context.Context, systemPrompt, userMessage string) (string, int, error) {
	atomic.AddInt32(&e.calls, 1)
	e.mu.Lock()
	execFn := e.execFn
	e.mu.Unlock()
	if execFn != nil {
		return execFn(ctx, systemPrompt, userMessage)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return "", 0, e.err
	}
	return e.output, e.tokens, nil
}

func (e *stubExecutor) failWith(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.err = err
}

func (e *stubExecutor) succeedWith(output string, tokens int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.output = output
	e.tokens = tokens
	e.err = nil
}

func (e *stubExecutor) CallCount() int32 { return atomic.LoadInt32(&e.calls) }

// stubPlanner is a controllable PlanCreator for tests.
type stubPlanner struct {
	mu       sync.Mutex
	created  []CandidatePlan
	err      error
	nextID   int
	idPrefix string
}

func newStubPlanner() *stubPlanner {
	return &stubPlanner{idPrefix: "plan-test-"}
}

func (p *stubPlanner) CreatePlan(_ context.Context, title, description, _, _ string) (PlanRef, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return PlanRef{}, p.err
	}
	p.nextID++
	p.created = append(p.created, CandidatePlan{Title: title, Description: description})
	return PlanRef{
		ID:    fmt.Sprintf("%s%03d", p.idPrefix, p.nextID),
		State: "pending_approval",
	}, nil
}

func (p *stubPlanner) CreatedCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.created)
}

func (p *stubPlanner) CreatedTitles() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.created))
	for i, c := range p.created {
		out[i] = c.Title
	}
	return out
}

// pauseRecorder captures auto-pause calls for assertion.
type pauseRecorder struct {
	mu     sync.Mutex
	called bool
	empID  string
	reason string
}

func (p *pauseRecorder) fn() PauseFunc {
	return func(employeeID, reason string) error {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.called = true
		p.empID = employeeID
		p.reason = reason
		return nil
	}
}

func (p *pauseRecorder) wasCalled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.called
}

// ---------------------------------------------------------------------------
// Shared test fixtures
// ---------------------------------------------------------------------------

// testTier1Constitution returns a minimal tier-1 constitution for reactive
// tests. Mirrors the shape expected by constitution.go (Phase 1).
func testTier1Constitution() *Constitution {
	return &Constitution{
		Purpose:      "respond to alerts",
		Role:         "alert responder",
		Charter:      "investigate and report",
		AutonomyTier: Tier1Reactive,
		EscalatesTo:  []string{"user"},
		Constraints: ConstitutionalConstraints{
			RiskCeiling: RiskCeilingLow,
			Never:       []string{"delete files"},
		},
	}
}

// testTier2Constitution returns a minimal tier-2 constitution for propose
// tests.
func testTier2Constitution() *Constitution {
	return &Constitution{
		Purpose:      "keep CI green",
		Role:         "CI Reliability Engineer",
		Charter:      "investigate failures, open issues",
		AutonomyTier: Tier2Propose,
		EscalatesTo:  []string{"user"},
		Constraints: ConstitutionalConstraints{
			RiskCeiling:          RiskCeilingMedium,
			DailyBudgetCents:     50,
			MaxInvocationsPerDay: 100,
			Never:                []string{"merge to main", "force push"},
		},
	}
}

func basicTrigger() TriggerEvent {
	return TriggerEvent{
		Source:  "cron",
		Topic:   "*/15 * * * *",
		Payload: []byte(`{"status":"ok"}`),
		FiredAt: time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC),
	}
}

// ---------------------------------------------------------------------------
// Tests: Assess
// ---------------------------------------------------------------------------

func TestAssess_ParsesCandidates(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueResponse(`{
		"candidates": [
			{"title": "fix flaky test", "description": "investigate test_X", "prompt": "run test_X with verbose"},
			{"title": "open issue", "description": "document the failure", "prompt": "create github issue"}
		]
	}`)

	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil).
		WithReflector(reflector)

	candidates, err := loop.Assess(context.Background(), basicTrigger())
	if err != nil {
		t.Fatalf("Assess returned error: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}
	if candidates[0].Title != "fix flaky test" {
		t.Errorf("first candidate title = %q, want %q", candidates[0].Title, "fix flaky test")
	}
	if candidates[1].Prompt != "create github issue" {
		t.Errorf("second candidate prompt = %q, want %q", candidates[1].Prompt, "create github issue")
	}
}

func TestAssess_NoCandidates(t *testing.T) {
	reflector := newStubReflector()
	// default returns {"candidates":[]}
	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil).
		WithReflector(reflector)

	candidates, err := loop.Assess(context.Background(), basicTrigger())
	if err != nil {
		t.Fatalf("Assess returned error: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("expected 0 candidates, got %d", len(candidates))
	}
}

func TestAssess_LLMError(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueError(errors.New("LLM unavailable"))

	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil).
		WithReflector(reflector)

	_, err := loop.Assess(context.Background(), basicTrigger())
	if err == nil {
		t.Fatal("expected error from Assess when LLM fails")
	}
}

// TestAssess_InvalidJSONFallback verifies spec line 590: invalid JSON from the
// LLM falls back to tier-1 behaviour (wraps raw output as single candidate).
func TestAssess_InvalidJSONFallback(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueResponse("this is not valid JSON, just free-form text")

	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil).
		WithReflector(reflector)

	candidates, err := loop.Assess(context.Background(), basicTrigger())
	if err != nil {
		t.Fatalf("Assess should not fail on invalid JSON (spec fallback): %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 fallback candidate, got %d", len(candidates))
	}
	if candidates[0].Prompt != "this is not valid JSON, just free-form text" {
		t.Errorf("fallback candidate prompt = %q, want raw LLM output", candidates[0].Prompt)
	}
}

func TestAssess_NilReflector(t *testing.T) {
	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil)
	_, err := loop.Assess(context.Background(), basicTrigger())
	if err == nil {
		t.Fatal("expected error when reflector is nil")
	}
}

func TestAssess_NilConstitution(t *testing.T) {
	reflector := newStubReflector()
	loop := NewGoalLoop("emp-test", nil, nil, nil).WithReflector(reflector)
	_, err := loop.Assess(context.Background(), basicTrigger())
	if err == nil {
		t.Fatal("expected error when constitution is nil")
	}
}

// ---------------------------------------------------------------------------
// Tests: Plan
// ---------------------------------------------------------------------------

func TestPlan_Success(t *testing.T) {
	planner := newStubPlanner()
	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil).
		WithPlanner(planner)

	candidate := CandidatePlan{Title: "fix CI", Description: "investigate failure", Prompt: "run tests"}
	ref, err := loop.Plan(context.Background(), candidate)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if ref.ID == "" {
		t.Error("expected non-empty plan ID")
	}
	if ref.State != "pending_approval" {
		t.Errorf("plan state = %q, want %q", ref.State, "pending_approval")
	}
	if planner.CreatedCount() != 1 {
		t.Errorf("planner created %d plans, want 1", planner.CreatedCount())
	}
}

func TestPlan_NoPlanner(t *testing.T) {
	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil)
	_, err := loop.Plan(context.Background(), CandidatePlan{Title: "test"})
	if err == nil {
		t.Fatal("expected error when planner is nil")
	}
}

func TestPlan_PlannerError(t *testing.T) {
	planner := newStubPlanner()
	planner.err = errors.New("plan store unavailable")
	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil).
		WithPlanner(planner)

	_, err := loop.Plan(context.Background(), CandidatePlan{Title: "test"})
	if err == nil {
		t.Fatal("expected error when planner fails")
	}
}

// ---------------------------------------------------------------------------
// Tests: Execute
// ---------------------------------------------------------------------------

func TestExecute_Success(t *testing.T) {
	executor := newStubExecutor()
	executor.succeedWith("all good", 200)

	loop := NewGoalLoop("emp-test", testTier1Constitution(), nil, nil).
		WithExecutor(executor)

	result, err := loop.Execute(context.Background(), PlanRef{ID: "p1", State: "executing"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.Success {
		t.Error("expected success=true")
	}
	if result.TokensUsed != 200 {
		t.Errorf("tokens = %d, want 200", result.TokensUsed)
	}
	if result.Output != "all good" {
		t.Errorf("output = %q, want %q", result.Output, "all good")
	}
}

func TestExecute_Failure(t *testing.T) {
	executor := newStubExecutor()
	executor.failWith(errors.New("tool error"))

	loop := NewGoalLoop("emp-test", testTier1Constitution(), nil, nil).
		WithExecutor(executor)

	result, err := loop.Execute(context.Background(), PlanRef{ID: "p1"})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected success=false")
	}
	if result.Error == "" {
		t.Error("expected non-empty error string")
	}
}

func TestExecute_NoExecutor(t *testing.T) {
	loop := NewGoalLoop("emp-test", testTier1Constitution(), nil, nil)
	_, err := loop.Execute(context.Background(), PlanRef{ID: "p1"})
	if err == nil {
		t.Fatal("expected error when executor is nil")
	}
}

// ---------------------------------------------------------------------------
// Tests: Reflect
// ---------------------------------------------------------------------------

func TestReflect_Success_Healthy(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueResponse(`{"health":"healthy","reasoning":"CI is green"}`)

	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil).
		WithReflector(reflector)

	result := &bot.BotExecutionResult{
		BotID:      "emp-test",
		Output:     "tests passed",
		TokensUsed: 50,
		Success:    true,
	}
	health, err := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, result)
	if err != nil {
		t.Fatalf("Reflect returned error: %v", err)
	}
	if health != GoalHealthy {
		t.Errorf("health = %s, want %s", health.String(), GoalHealthy.String())
	}
	if loop.ConsecutiveFailures() != 0 {
		t.Errorf("consecutive failures = %d, want 0 after success", loop.ConsecutiveFailures())
	}
}

func TestReflect_Failure_AtRisk(t *testing.T) {
	reflector := newStubReflector()
	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil).
		WithReflector(reflector)

	result := &bot.BotExecutionResult{
		BotID:   "emp-test",
		Success: false,
		Error:   "tool unavailable",
	}
	health, err := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, result)
	if err != nil {
		t.Fatalf("Reflect returned error: %v", err)
	}
	if health != GoalAtRisk {
		t.Errorf("health = %s, want %s", health.String(), GoalAtRisk.String())
	}
	if loop.ConsecutiveFailures() != 1 {
		t.Errorf("consecutive failures = %d, want 1", loop.ConsecutiveFailures())
	}
}

// TestReflect_ConsecutiveFailures_Broken verifies that after N (default 3)
// consecutive failures, the goal is marked broken and the employee is
// auto-paused (spec lines 588-591).
func TestReflect_ConsecutiveFailures_Broken(t *testing.T) {
	reflector := newStubReflector()
	recorder := &pauseRecorder{}

	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil).
		WithReflector(reflector).
		WithPauseFunc(recorder.fn())

	result := &bot.BotExecutionResult{
		Success: false,
		Error:   "persistent failure",
	}

	// Fail 1: at_risk
	h1, _ := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, result)
	if h1 != GoalAtRisk {
		t.Fatalf("failure 1 health = %s, want at_risk", h1.String())
	}
	if recorder.wasCalled() {
		t.Fatal("auto-pause should not fire on failure 1")
	}

	// Fail 2: at_risk
	h2, _ := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, result)
	if h2 != GoalAtRisk {
		t.Fatalf("failure 2 health = %s, want at_risk", h2.String())
	}
	if recorder.wasCalled() {
		t.Fatal("auto-pause should not fire on failure 2")
	}

	// Fail 3: broken + auto-pause
	h3, _ := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, result)
	if h3 != GoalBroken {
		t.Fatalf("failure 3 health = %s, want broken", h3.String())
	}
	if !recorder.wasCalled() {
		t.Fatal("auto-pause should fire on failure 3 (threshold reached)")
	}
	if recorder.empID != "emp-test" {
		t.Errorf("pause empID = %q, want %q", recorder.empID, "emp-test")
	}
}

// TestReflect_ResetOnSuccess verifies that a success resets the failure counter.
func TestReflect_ResetOnSuccess(t *testing.T) {
	reflector := newStubReflector()
	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil).
		WithReflector(reflector)

	failResult := &bot.BotExecutionResult{Success: false, Error: "err"}
	okResult := &bot.BotExecutionResult{Success: true, Output: "ok"}

	// Two failures.
	loop.Reflect(context.Background(), PlanRef{ID: "p1"}, failResult)
	loop.Reflect(context.Background(), PlanRef{ID: "p1"}, failResult)
	if loop.ConsecutiveFailures() != 2 {
		t.Fatalf("expected 2 consecutive failures, got %d", loop.ConsecutiveFailures())
	}

	// Success resets.
	loop.Reflect(context.Background(), PlanRef{ID: "p1"}, okResult)
	if loop.ConsecutiveFailures() != 0 {
		t.Fatalf("expected 0 failures after success, got %d", loop.ConsecutiveFailures())
	}
}

func TestReflect_InvalidJSON_DefaultsHealthy(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueResponse("not valid JSON")

	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil).
		WithReflector(reflector)

	result := &bot.BotExecutionResult{Success: true, Output: "ok"}
	health, err := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, result)
	if err != nil {
		t.Fatalf("Reflect error: %v", err)
	}
	if health != GoalHealthy {
		t.Errorf("health = %s, want healthy (fallback for unparseable reflect JSON)", health.String())
	}
}

func TestReflect_CustomThreshold(t *testing.T) {
	reflector := newStubReflector()
	recorder := &pauseRecorder{}

	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil).
		WithReflector(reflector).
		WithPauseFunc(recorder.fn()).
		WithMaxConsecutiveFailures(2)

	fail := &bot.BotExecutionResult{Success: false, Error: "err"}

	h1, _ := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, fail)
	if h1 != GoalAtRisk {
		t.Fatalf("failure 1: health = %s, want at_risk", h1.String())
	}

	h2, _ := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, fail)
	if h2 != GoalBroken {
		t.Fatalf("failure 2: health = %s, want broken (threshold=2)", h2.String())
	}
	if !recorder.wasCalled() {
		t.Fatal("auto-pause should fire at threshold=2")
	}
}

// ---------------------------------------------------------------------------
// Tests: Decide (tier dispatch)
// ---------------------------------------------------------------------------

func TestDecide_Tier1_NoCandidates_NoOp(t *testing.T) {
	reflector := newStubReflector()
	// default: {"candidates":[]}
	loop := NewGoalLoop("emp-test", testTier1Constitution(), nil, nil).
		WithReflector(reflector)

	err := loop.Decide(context.Background(), basicTrigger())
	if err != nil {
		t.Fatalf("Decide error: %v", err)
	}
	// No executor configured; if candidates were produced it would error.
	// With zero candidates, no execution happens.
}

func TestDecide_Tier1_WithCandidate_ExecutesImmediately(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueResponse(`{"candidates":[{"title":"react","description":"d","prompt":"do it"}]}`)
	executor := newStubExecutor()
	executor.succeedWith("done", 10)
	// Reflect will call the reflector; queue a healthy response.
	reflector.queueResponse(`{"health":"healthy","reasoning":"ok"}`)

	loop := NewGoalLoop("emp-test", testTier1Constitution(), nil, nil).
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

func TestDecide_Tier2_CreatesPendingPlans(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueResponse(`{
		"candidates": [
			{"title":"fix A","description":"desc A","prompt":"prompt A"},
			{"title":"fix B","description":"desc B","prompt":"prompt B"}
		]
	}`)
	planner := newStubPlanner()

	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil).
		WithReflector(reflector).
		WithPlanner(planner)

	err := loop.Decide(context.Background(), basicTrigger())
	if err != nil {
		t.Fatalf("Decide error: %v", err)
	}
	if planner.CreatedCount() != 2 {
		t.Errorf("planner created %d plans, want 2", planner.CreatedCount())
	}
	titles := planner.CreatedTitles()
	if titles[0] != "fix A" || titles[1] != "fix B" {
		t.Errorf("plan titles = %v, want [fix A, fix B]", titles)
	}
}

func TestDecide_Tier2_NoPlanner_Error(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueResponse(`{"candidates":[{"title":"x","description":"d","prompt":"p"}]}`)

	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil).
		WithReflector(reflector)
	// No planner wired.

	err := loop.Decide(context.Background(), basicTrigger())
	if err == nil {
		t.Fatal("expected error when tier-2 has no planner")
	}
}

func TestDecide_Tier2_NoCandidates_NoOp(t *testing.T) {
	reflector := newStubReflector()
	planner := newStubPlanner()

	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil).
		WithReflector(reflector).
		WithPlanner(planner)

	err := loop.Decide(context.Background(), basicTrigger())
	if err != nil {
		t.Fatalf("Decide error: %v", err)
	}
	if planner.CreatedCount() != 0 {
		t.Errorf("expected 0 plans, got %d", planner.CreatedCount())
	}
}

func TestDecide_Tier3_NotImplemented(t *testing.T) {
	reflector := newStubReflector()
	c := testTier2Constitution()
	c.AutonomyTier = Tier3Autonomous

	loop := NewGoalLoop("emp-test", c, nil, nil).WithReflector(reflector)

	err := loop.Decide(context.Background(), basicTrigger())
	if err == nil {
		t.Fatal("expected error for tier 3")
	}
	if err.Error() != "tier 3 not yet implemented" {
		t.Errorf("error = %q, want %q", err.Error(), "tier 3 not yet implemented")
	}
}

func TestDecide_NilConstitution_Error(t *testing.T) {
	loop := NewGoalLoop("emp-test", nil, nil, nil)
	err := loop.Decide(context.Background(), basicTrigger())
	if err == nil {
		t.Fatal("expected error for nil constitution")
	}
}

func TestDecide_Tier2_AssessError_Propagates(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueError(errors.New("LLM down"))

	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil).
		WithReflector(reflector).
		WithPlanner(newStubPlanner())

	err := loop.Decide(context.Background(), basicTrigger())
	if err == nil {
		t.Fatal("expected error from tier-2 when Assess fails")
	}
}

// ---------------------------------------------------------------------------
// Tests: ApproveAndExecute
// ---------------------------------------------------------------------------

func TestApproveAndExecute_Success(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueResponse(`{"health":"healthy","reasoning":"ok"}`)
	executor := newStubExecutor()
	executor.succeedWith("fixed", 150)

	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil).
		WithReflector(reflector).
		WithExecutor(executor)

	result, health, err := loop.ApproveAndExecute(context.Background(), PlanRef{ID: "plan-001", State: "approved"})
	if err != nil {
		t.Fatalf("ApproveAndExecute error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if health != GoalHealthy {
		t.Errorf("health = %s, want healthy", health.String())
	}
}

func TestApproveAndExecute_ExecuteError_StillReflects(t *testing.T) {
	reflector := newStubReflector()
	executor := newStubExecutor()
	executor.failWith(errors.New("exec failed"))

	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil).
		WithReflector(reflector).
		WithExecutor(executor)

	result, health, err := loop.ApproveAndExecute(context.Background(), PlanRef{ID: "plan-002"})
	if err != nil {
		t.Fatalf("ApproveAndExecute error: %v", err)
	}
	// Even on exec error, we get a synthetic failure result + reflect.
	if result.Success {
		t.Error("expected failure result")
	}
	if health != GoalAtRisk {
		t.Errorf("health = %s, want at_risk", health.String())
	}
}

// ---------------------------------------------------------------------------
// Tests: SetConstitution (atomic swap)
// ---------------------------------------------------------------------------

func TestSetConstitution(t *testing.T) {
	loop := NewGoalLoop("emp-test", testTier1Constitution(), nil, nil)

	newC := testTier2Constitution()
	loop.SetConstitution(newC)

	loop.mu.Lock()
	got := loop.constitution
	loop.mu.Unlock()

	if got.AutonomyTier != Tier2Propose {
		t.Errorf("after SetConstitution, tier = %d, want %d", got.AutonomyTier, Tier2Propose)
	}
}

// ---------------------------------------------------------------------------
// Tests: ResetFailureCounter
// ---------------------------------------------------------------------------

func TestResetFailureCounter(t *testing.T) {
	reflector := newStubReflector()
	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil).
		WithReflector(reflector)

	fail := &bot.BotExecutionResult{Success: false, Error: "err"}
	loop.Reflect(context.Background(), PlanRef{ID: "p1"}, fail)
	loop.Reflect(context.Background(), PlanRef{ID: "p1"}, fail)
	if loop.ConsecutiveFailures() != 2 {
		t.Fatalf("expected 2 failures before reset, got %d", loop.ConsecutiveFailures())
	}

	loop.ResetFailureCounter()
	if loop.ConsecutiveFailures() != 0 {
		t.Errorf("after reset, failures = %d, want 0", loop.ConsecutiveFailures())
	}
}

// ---------------------------------------------------------------------------
// Tests: parseAssessResponse / parseReflectResponse (table-driven)
// ---------------------------------------------------------------------------

func TestParseAssessResponse(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantCount int
		wantErr   bool
	}{
		{
			name:      "valid two candidates",
			content:   `{"candidates":[{"title":"a","description":"d","prompt":"p"},{"title":"b","description":"d2","prompt":"p2"}]}`,
			wantCount: 2,
		},
		{
			name:      "empty candidates",
			content:   `{"candidates":[]}`,
			wantCount: 0,
		},
		{
			name:      "code-fenced JSON",
			content:   "```json\n{\"candidates\":[]}\n```",
			wantCount: 0,
		},
		{
			name:    "invalid JSON",
			content: "not json at all",
			wantErr: true,
		},
		{
			name:    "empty string",
			content: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates, err := parseAssessResponse(tt.content)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.wantErr && len(candidates) != tt.wantCount {
				t.Errorf("candidate count = %d, want %d", len(candidates), tt.wantCount)
			}
		})
	}
}

func TestParseReflectResponse(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantHealth GoalHealth
		wantErr    bool
	}{
		{
			name:       "healthy",
			content:    `{"health":"healthy","reasoning":"CI is green"}`,
			wantHealth: GoalHealthy,
		},
		{
			name:       "at_risk",
			content:    `{"health":"at_risk","reasoning":"flaky tests"}`,
			wantHealth: GoalAtRisk,
		},
		{
			name:       "broken",
			content:    `{"health":"broken","reasoning":"main is red"}`,
			wantHealth: GoalBroken,
		},
		{
			name:       "unknown health maps to GoalUnknown",
			content:    `{"health":"unknown","reasoning":"not assessed"}`,
			wantHealth: GoalUnknown,
		},
		{
			name:    "invalid JSON",
			content: "garbage",
			wantErr: true,
		},
		{
			name:    "invalid health value",
			content: `{"health":"on_fire","reasoning":"???"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			health, err := parseReflectResponse(tt.content)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if health != tt.wantHealth {
				t.Errorf("health = %s, want %s", health.String(), tt.wantHealth.String())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests: prompt builders
// ---------------------------------------------------------------------------

func TestBuildAssessUserPrompt_IncludesConstitution(t *testing.T) {
	c := testTier2Constitution()
	prompt := buildAssessUserPrompt(c, basicTrigger())

	// Spot-check key fields appear in the prompt.
	for _, want := range []string{c.Purpose, c.Role, "cron", "*/15 * * * *"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("assess prompt missing %q", want)
		}
	}
	for _, never := range c.Constraints.Never {
		if !strings.Contains(prompt, never) {
			t.Errorf("assess prompt missing never-rule %q", never)
		}
	}
}

func TestBuildReflectUserPrompt_IncludesOutcome(t *testing.T) {
	c := testTier2Constitution()
	result := &bot.BotExecutionResult{
		Success:    true,
		Output:     "all tests pass",
		TokensUsed: 42,
		Duration:   5 * time.Second,
	}
	prompt := buildReflectUserPrompt(c, result)

	for _, want := range []string{"all tests pass", "42", "true"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("reflect prompt missing %q", want)
		}
	}
}

func TestBuildReflectUserPrompt_NilResult(t *testing.T) {
	prompt := buildReflectUserPrompt(testTier2Constitution(), nil)
	if !strings.Contains(prompt, "no result") {
		t.Error("reflect prompt should mention 'no result' when result is nil")
	}
}

// ---------------------------------------------------------------------------
// Tests: Tier-1 Decide with assess failure (resilience)
// ---------------------------------------------------------------------------

// TestDecide_Tier1_AssessError_Propagates verifies that when the LLM is
// unreachable, tier-1 Decide surfaces the error rather than silently no-op'ing.
func TestDecide_Tier1_AssessError_Propagates(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueError(errors.New("network unreachable"))

	loop := NewGoalLoop("emp-test", testTier1Constitution(), nil, nil).
		WithReflector(reflector).
		WithExecutor(newStubExecutor())

	err := loop.Decide(context.Background(), basicTrigger())
	if err == nil {
		t.Fatal("expected error when assess fails in tier-1")
	}
}

// ---------------------------------------------------------------------------
// Tests: GoalStore integration (optional -- nil store is valid)
// ---------------------------------------------------------------------------

// TestReflect_NilGoalStore verifies that a nil GoalStore does not panic
// during Reflect.
func TestReflect_NilGoalStore(t *testing.T) {
	reflector := newStubReflector()
	loop := NewGoalLoop("emp-test", testTier2Constitution(), nil, nil).
		WithReflector(reflector)
	// goalStore is nil.

	result := &bot.BotExecutionResult{Success: true, Output: "ok"}
	_, err := loop.Reflect(context.Background(), PlanRef{ID: "p1"}, result)
	if err != nil {
		t.Fatalf("Reflect with nil goalStore should not error: %v", err)
	}
}
