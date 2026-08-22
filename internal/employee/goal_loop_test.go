package employee

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
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

func TestDecide_Tier3_WithCandidate_ExecutesImmediately(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueResponse(`{"candidates":[{"title":"auto","description":"d","prompt":"do it autonomously"}]}`)
	// Reflect will call the reflector; queue a healthy response.
	reflector.queueResponse(`{"health":"healthy","reasoning":"ok"}`)
	executor := newStubExecutor()
	executor.succeedWith("autonomous done", 42)

	c := testTier2Constitution()
	c.AutonomyTier = Tier3Autonomous

	loop := NewGoalLoop("emp-test", c, nil, nil).
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

// ---------------------------------------------------------------------------
// Tests: decideTier3 (tier-3 autonomous, leaf 01)
// ---------------------------------------------------------------------------

// testTier3Constitution returns a minimal tier-3 autonomous constitution.
func testTier3Constitution() *Constitution {
	c := testTier2Constitution()
	c.AutonomyTier = Tier3Autonomous
	return c
}

// capturingExecutor records the user messages (prompts) it was called with.
type capturingExecutor struct {
	stubExecutor
	mu      sync.Mutex
	prompts []string
}

func (e *capturingExecutor) ExecuteBot(ctx context.Context, systemPrompt, userMessage string) (string, int, error) {
	e.mu.Lock()
	e.prompts = append(e.prompts, userMessage)
	e.mu.Unlock()
	return e.stubExecutor.ExecuteBot(ctx, systemPrompt, userMessage)
}

func (e *capturingExecutor) Prompts() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.prompts))
	copy(out, e.prompts)
	return out
}

func TestGoalLoopEscalationGate_Setter(t *testing.T) {
	t.Run("nil default is pure autonomy", func(t *testing.T) {
		loop := NewGoalLoop("emp-test", testTier3Constitution(), nil, nil)
		if gate := loop.escalationGateSnapshot(); gate != nil {
			t.Fatalf("default gate = %v, want nil", gate)
		}
	})

	t.Run("set and read back", func(t *testing.T) {
		loop := NewGoalLoop("emp-test", testTier3Constitution(), nil, nil)
		gate := func(c *Constitution, cand CandidatePlan) (bool, string) { return true, "test" }
		loop.WithEscalationGate(gate)
		if got := loop.escalationGateSnapshot(); got == nil {
			t.Fatal("gate should be set after WithEscalationGate")
		}
	})

	t.Run("typed-nil is ignored", func(t *testing.T) {
		loop := NewGoalLoop("emp-test", testTier3Constitution(), nil, nil)
		loop.WithEscalationGate(nil)
		if gate := loop.escalationGateSnapshot(); gate != nil {
			t.Fatalf("typed-nil gate should be ignored, got %v", gate)
		}
	})
}

func TestDecide_Tier3_HappyPath_NoGate(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueResponse(`{"candidates":[{"title":"auto fix","description":"d","prompt":"fix the thing"}]}`)
	reflector.queueResponse(`{"health":"healthy","reasoning":"ok"}`)
	executor := &capturingExecutor{stubExecutor: *newStubExecutor()}
	executor.succeedWith("done", 10)

	loop := NewGoalLoop("emp-test", testTier3Constitution(), nil, nil).
		WithReflector(reflector).
		WithExecutor(executor)

	err := loop.Decide(context.Background(), basicTrigger())
	if err != nil {
		t.Fatalf("Decide error: %v", err)
	}

	prompts := executor.Prompts()
	if len(prompts) != 1 || prompts[0] != "fix the thing" {
		t.Errorf("executed prompts = %v, want [fix the thing]", prompts)
	}

	// G3: tier-3 immediate execution uses system approval. Verify via
	// CanExecutePlan on a synthesized ref (the loop builds refs with
	// ApproverID "system").
	if !loop.CanExecutePlan(PlanRef{ID: "x", ApproverID: "system"}) {
		t.Error("ApproverID \"system\" must be executable for tier-3")
	}

	// Reflect was invoked: reflector call count = assess + reflect = 2.
	if calls := reflector.CallCount(); calls != 2 {
		t.Errorf("reflector calls = %d, want 2 (assess + reflect)", calls)
	}

	// No pending plans left in ActivePlanIDs (no goal store → no tracking,
	// but the contract's removal pattern is exercised by the store-based
	// escalation test below).
}

func TestDecide_Tier3_BlocksAtMaxActivePlans(t *testing.T) {
	store := testGoalStore(t)
	seedBot(t, store, "emp-t3-max")

	// Pre-seed an active goal already at cap=1.
	g := &Goal{
		ID:         "goal_t3_max",
		EmployeeID: "emp-t3-max",
		Title:      "at cap",
		Mandate:    "test tier-3 max-plan blocking",
		State:      GoalActive,
		Source:     SourceUser,
	}
	g.SetActivePlan("plan-active")
	g.AddActivePlan("plan-active")
	if err := store.Create(context.Background(), g); err != nil {
		t.Fatalf("Create goal: %v", err)
	}

	c := testTier3Constitution()
	c.MaxActivePlans = 1

	reflector := newStubReflector()
	executor := newStubExecutor()
	planner := newStubPlanner()

	loop := NewGoalLoop("emp-t3-max", c, store, nil).
		WithReflector(reflector).
		WithExecutor(executor).
		WithPlanner(planner)

	err := loop.Decide(context.Background(), basicTrigger())
	if err != nil {
		t.Fatalf("Decide should not error when at cap: %v", err)
	}
	if executor.CallCount() != 0 {
		t.Errorf("executor called %d times, want 0 (assess skipped at cap)", executor.CallCount())
	}
	if reflector.CallCount() != 0 {
		t.Errorf("reflector called %d times, want 0 (ASSESS blocked)", reflector.CallCount())
	}
}

func TestDecide_Tier3_EscalationGate_RoutesToPlanSignoff(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueResponse(`{"candidates":[{"title":"risky op","description":"d","prompt":"do risky thing"}]}`)

	planner := newStubPlanner()
	executor := newStubExecutor()

	c := testTier3Constitution()

	loop := NewGoalLoop("emp-t3-esc", c, nil, nil).
		WithReflector(reflector).
		WithExecutor(executor).
		WithPlanner(planner).
		WithEscalationGate(func(_ *Constitution, _ CandidatePlan) (bool, string) {
			return true, "always escalate"
		})

	err := loop.Decide(context.Background(), basicTrigger())
	if err != nil {
		t.Fatalf("Decide error: %v", err)
	}

	if executor.CallCount() != 0 {
		t.Errorf("executor called %d times, want 0 (escalated, not executed)", executor.CallCount())
	}
	if planner.CreatedCount() != 1 {
		t.Fatalf("planner created %d plans, want 1 (escalated to signoff)", planner.CreatedCount())
	}
	if titles := planner.CreatedTitles(); len(titles) != 1 || titles[0] != "risky op" {
		t.Errorf("plan titles = %v, want [risky op]", titles)
	}
}

func TestDecide_Tier3_EscalatedPlan_TrackedInActivePlanIDs(t *testing.T) {
	store := testGoalStore(t)
	seedBot(t, store, "emp-t3-track")

	g := &Goal{
		ID:         "goal_t3_track",
		EmployeeID: "emp-t3-track",
		Title:      "tracking",
		Mandate:    "track escalated plans",
		State:      GoalActive,
		Source:     SourceUser,
	}
	if err := store.Create(context.Background(), g); err != nil {
		t.Fatalf("Create goal: %v", err)
	}

	reflector := newStubReflector()
	reflector.queueResponse(`{"candidates":[{"title":"esc","description":"d","prompt":"p"}]}`)

	c := testTier3Constitution()
	c.MaxActivePlans = 5

	loop := NewGoalLoop("emp-t3-track", c, store, nil).
		WithReflector(reflector).
		WithPlanner(&stubPlanner{idPrefix: "plan-esc-"}).
		WithEscalationGate(func(_ *Constitution, _ CandidatePlan) (bool, string) { return true, "test" })

	err := loop.Decide(context.Background(), basicTrigger())
	if err != nil {
		t.Fatalf("Decide error: %v", err)
	}

	loaded, getErr := store.Get(context.Background(), "goal_t3_track")
	if getErr != nil {
		t.Fatalf("Get goal: %v", getErr)
	}
	plans := loaded.ActivePlans()
	if len(plans) == 0 {
		t.Fatal("ActivePlanIDs empty; escalated plan not tracked")
	}
	if !strings.HasPrefix(plans[0], "plan-") {
		t.Errorf("tracked plan id %q missing plan prefix", plans[0])
	}
}

func TestDecide_Tier3_FailurePath_SyntheticResultAndContinue(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueResponse(`{"candidates":[
		{"title":"first","description":"d","prompt":"fails"},
		{"title":"second","description":"d","prompt":"succeeds"}
	]}`)
	// Two reflect responses (one per candidate); failures default healthy.
	reflector.queueResponse(`{"health":"at_risk","reasoning":"failed once"}`)
	reflector.queueResponse(`{"health":"healthy","reasoning":"recovered"}`)

	executor := &capturingExecutor{stubExecutor: *newStubExecutor()}
	callNum := int32(0)
	baseFn := func(ctx context.Context, systemPrompt, userMessage string) (string, int, error) {
		n := atomic.AddInt32(&callNum, 1)
		if n == 1 {
			return "", 0, errors.New("boom: first execution fails")
		}
		return "ok", 5, nil
	}

	c := testTier3Constitution()
	c.MaxActivePlans = 5

	var mu sync.Mutex
	failures := 0
	loop := NewGoalLoop("emp-t3-fail", c, nil, nil).
		WithReflector(reflector).
		WithExecutor(executor)
	loop.SetEmitMetricFunc(func(name string, value float64, tags map[string]string) {
		if name != "employee.invocations" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if tags["outcome"] == "failure" {
			failures++
		}
	})
	// Inject the per-call failure behavior after construction.
	executor.execFn = baseFn
	_ = executor

	err := loop.Decide(context.Background(), basicTrigger())
	if err != nil {
		t.Fatalf("Decide error: %v", err)
	}

	// Both candidates processed despite first failing.
	if prompts := executor.Prompts(); len(prompts) != 2 {
		t.Fatalf("executed prompts = %v, want 2 candidates processed", prompts)
	} else if prompts[0] != "fails" || prompts[1] != "succeeds" {
		t.Errorf("prompts = %v, want [fails succeeds]", prompts)
	}

	// Synthetic failure result reached Reflect → consecutive-failure
	// counter incremented then reset by the success. Verify counter state:
	loop.mu.Lock()
	cf := loop.consecutiveFailures
	cs := loop.consecutiveSuccesses
	loop.mu.Unlock()
	if cf != 0 || cs != 1 {
		t.Errorf("after fail-then-succeed: consecutiveFailures=%d consecutiveSuccesses=%d, want 0/1", cf, cs)
	}

	// Failure metric emitted exactly once with tier="3".
	mu.Lock()
	f := failures
	mu.Unlock()
	if f != 1 {
		t.Errorf("failure invocations metrics emitted = %d, want 1", f)
	}
}

func TestDecide_Tier3_EmitsInvocationMetrics(t *testing.T) {
	type metric struct {
		name string
		tags map[string]string
	}
	var mu sync.Mutex
	var metrics []metric

	reflector := newStubReflector()
	reflector.queueResponse(`{"candidates":[{"title":"a","description":"d","prompt":"p"}]}`)
	reflector.queueResponse(`{"health":"healthy","reasoning":"ok"}`)
	executor := newStubExecutor()
	executor.succeedWith("ok", 1)

	c := testTier3Constitution()
	loop := NewGoalLoop("emp-t3-metric", c, nil, nil).
		WithReflector(reflector).
		WithExecutor(executor)
	loop.SetEmitMetricFunc(func(name string, value float64, tags map[string]string) {
		mu.Lock()
		defer mu.Unlock()
		if name == "employee.invocations" {
			cp := make(map[string]string, len(tags))
			for k, v := range tags {
				cp[k] = v
			}
			metrics = append(metrics, metric{name: name, tags: cp})
		}
	})

	err := loop.Decide(context.Background(), basicTrigger())
	if err != nil {
		t.Fatalf("Decide error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(metrics) != 1 {
		t.Fatalf("employee.invocations metrics = %d, want 1", len(metrics))
	}
	tags := metrics[0].tags
	if tags["tier"] != "3" {
		t.Errorf("tier tag = %q, want \"3\"", tags["tier"])
	}
	if tags["outcome"] != "success" {
		t.Errorf("outcome tag = %q, want success", tags["outcome"])
	}
	if tags["employee_id"] != "emp-t3-metric" {
		t.Errorf("employee_id tag = %q, want emp-t3-metric", tags["employee_id"])
	}
}

// ---------------------------------------------------------------------------
// Leaf 03: employee.tier3.escalated metric + escalation audit record.
// ---------------------------------------------------------------------------

// TestDecide_Tier3_EmitsEscalatedMetric verifies that an escalated candidate
// emits the dedicated "employee.tier3.escalated" counter tagged with
// employee_id (leaf 03 contract), alongside employee.invocations.
func TestDecide_Tier3_EmitsEscalatedMetric(t *testing.T) {
	type metric struct {
		name string
		tags map[string]string
	}
	var mu sync.Mutex
	var metrics []metric

	c := testTier3Constitution()
	c.Constraints.EscalationTriggers = []EscalationTrigger{
		{On: EscalateOnTool, Match: "shell_execute", Reason: "shell requires approval"},
	}

	reflector := newStubReflector()
	reflector.queueResponse(`{"candidates":[{"title":"run shell","description":"d","prompt":"use shell_execute now"}]}`)
	executor := newStubExecutor()
	planner := newStubPlanner()

	loop := NewGoalLoop("emp-t3-esc-metric", c, nil, nil).
		WithReflector(reflector).
		WithExecutor(executor).
		WithPlanner(planner).
		WithEscalationGate(func(c *Constitution, cand CandidatePlan) (bool, string) {
			escalate, reason := ShouldEscalate(c, cand)
			return escalate, reason
		})
	loop.SetEmitMetricFunc(func(name string, value float64, tags map[string]string) {
		mu.Lock()
		defer mu.Unlock()
		cp := make(map[string]string, len(tags))
		for k, v := range tags {
			cp[k] = v
		}
		metrics = append(metrics, metric{name: name, tags: cp})
	})

	if err := loop.Decide(context.Background(), basicTrigger()); err != nil {
		t.Fatalf("Decide error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	var escalated *metric
	for i := range metrics {
		if metrics[i].name == "employee.tier3.escalated" {
			escalated = &metrics[i]
			break
		}
	}
	if escalated == nil {
		t.Fatalf("employee.tier3.escalated not emitted; metrics = %+v", metrics)
	}
	if escalated.tags["employee_id"] != "emp-t3-esc-metric" {
		t.Errorf("employee_id tag = %q, want emp-t3-esc-metric", escalated.tags["employee_id"])
	}
}

// TestDecide_Tier3_EscalationWritesAuditRecord verifies that decideTier3's
// escalation branch persists an AuditFinding carrying the pending plan ID and
// trigger reason into a real AuditStore (leaf 03 Task 2).
func TestDecide_Tier3_EscalationWritesAuditRecord(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	store, err := NewAuditStore(dbPath)
	if err != nil {
		t.Fatalf("NewAuditStore: %v", err)
	}
	defer store.Close()

	c := testTier3Constitution()
	const triggerReason = "shell requires approval"
	c.Constraints.EscalationTriggers = []EscalationTrigger{
		{On: EscalateOnTool, Match: "shell_execute", Reason: triggerReason},
	}

	reflector := newStubReflector()
	reflector.queueResponse(`{"candidates":[{"title":"run shell","description":"d","prompt":"use shell_execute now"}]}`)
	executor := newStubExecutor()
	planner := newStubPlanner()

	loop := NewGoalLoop("emp-t3-audit", c, nil, nil).
		WithReflector(reflector).
		WithExecutor(executor).
		WithPlanner(planner).
		WithAuditStore(store).
		WithEscalationGate(func(c *Constitution, cand CandidatePlan) (bool, string) {
			escalate, reason := ShouldEscalate(c, cand)
			return escalate, reason
		})

	if err := loop.Decide(context.Background(), basicTrigger()); err != nil {
		t.Fatalf("Decide error: %v", err)
	}

	findings, err := store.List(context.Background(), AuditListFilter{EmployeeID: "emp-t3-audit"})
	if err != nil {
		t.Fatalf("List findings: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("audit findings = %d, want 1 escalation record", len(findings))
	}
	f := findings[0]
	if f.PlanID == "" {
		t.Error("escalation finding missing plan_id")
	}
	if f.Severity != SeverityInfo {
		t.Errorf("severity = %q, want info", f.Severity)
	}
	if f.Checkpoint != CheckpointPreExec {
		t.Errorf("checkpoint = %q, want pre_exec", f.Checkpoint)
	}
	if f.EmployeeID != "emp-t3-audit" {
		t.Errorf("employee_id = %q, want emp-t3-audit", f.EmployeeID)
	}
	// The trigger Reason must be carried in the record.
	if !strings.Contains(f.Evidence, triggerReason) {
		t.Errorf("evidence = %q, want it to contain trigger reason %q", f.Evidence, triggerReason)
	}
}
