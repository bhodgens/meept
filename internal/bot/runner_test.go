package bot

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBotRunner_BuildSystemPrompt(t *testing.T) {
	def := BotDefinition{
		ID:     "test-bot",
		Name:   "Test Bot",
		Prompt: "You are a monitoring bot. Check the CI status.",
		Tools:  []string{"web_fetch", "memory_store", "memory_search"},
		Constraints: BotConstraints{
			MaxIterations:    5,
			Timeout:          2 * time.Minute,
			MaxTokensPerTurn: 2048,
			DailyBudgetCents: 50,
		},
	}

	runner := NewBotRunner(def)
	prompt := runner.BuildSystemPrompt("Check CI for project X")

	if prompt == "" {
		t.Fatal("expected non-empty system prompt")
	}
	if !strings.Contains(prompt, "You are a monitoring bot") {
		t.Error("system prompt should contain bot's behavioral instructions")
	}
	if !strings.Contains(prompt, "Check CI for project X") {
		t.Error("system prompt should contain trigger context")
	}
}

func TestBotRunner_ShouldRun_BudgetCheck(t *testing.T) {
	def := BotDefinition{
		ID:     "test-bot",
		Prompt: "test",
		Constraints: BotConstraints{
			DailyBudgetCents: 100,
		},
	}

	runner := NewBotRunner(def)

	state := &BotState{TodayCostCents: 50, TodayDate: time.Now().Format("2006-01-02")}
	if !runner.ShouldRun(state) {
		t.Error("should allow run when under budget")
	}

	state.TodayCostCents = 100
	if runner.ShouldRun(state) {
		t.Error("should deny run when at budget")
	}

	state.TodayCostCents = 150
	if runner.ShouldRun(state) {
		t.Error("should deny run when over budget")
	}
}

func TestBotRunner_ShouldRun_InvocationCap(t *testing.T) {
	def := BotDefinition{
		ID:     "test-bot",
		Prompt: "test",
		Constraints: BotConstraints{
			MaxInvocationsPerDay: 10,
		},
	}

	runner := NewBotRunner(def)

	state := &BotState{TodayRuns: 9, TodayDate: time.Now().Format("2006-01-02")}
	if !runner.ShouldRun(state) {
		t.Error("should allow run when under invocation cap")
	}

	state.TodayRuns = 10
	if runner.ShouldRun(state) {
		t.Error("should deny run when at invocation cap")
	}
}

func TestBotRunner_ShouldRun_ConsecutiveFailures(t *testing.T) {
	def := BotDefinition{
		ID:     "test-bot",
		Prompt: "test",
	}

	runner := NewBotRunner(def)

	state := &BotState{ConsecutiveFailures: 10}
	if runner.ShouldRun(state) {
		t.Error("should deny run after 10 consecutive failures")
	}

	state.ConsecutiveFailures = 5
	if !runner.ShouldRun(state) {
		t.Error("should allow run with only 5 consecutive failures")
	}
}

func TestBotRunner_ShouldRun_NilState(t *testing.T) {
	def := BotDefinition{ID: "test", Prompt: "test"}
	runner := NewBotRunner(def)

	if !runner.ShouldRun(nil) {
		t.Error("should allow run with nil state")
	}
}

func TestBotRunner_BuildUserMessage(t *testing.T) {
	def := BotDefinition{ID: "ci-monitor", Prompt: "test"}
	runner := NewBotRunner(def)

	msg := runner.BuildUserMessage("cron fired at 12:00")
	if !strings.Contains(msg, "ci-monitor") {
		t.Error("user message should contain bot ID")
	}
	if !strings.Contains(msg, "cron fired at 12:00") {
		t.Error("user message should contain trigger context")
	}
}

// ---------------------------------------------------------------------------
// H2: RetryPolicy tests
// ---------------------------------------------------------------------------

// countingExecutor wraps stubExecutor-like behavior for retry tests.
type countingExecutor struct {
	calls    int32
	failN    int32 // fail the first N calls, then succeed
	output   string
	tokens   int
	lastErr  error
	execFunc func(ctx context.Context, systemPrompt, userMessage string) (string, int, error)
}

func (e *countingExecutor) ExecuteBot(ctx context.Context, systemPrompt, userMessage string) (string, int, error) {
	n := atomic.AddInt32(&e.calls, 1)
	if e.execFunc != nil {
		return e.execFunc(ctx, systemPrompt, userMessage)
	}
	if n <= e.failN {
		return "", 0, errors.New("transient LLM error")
	}
	return e.output, e.tokens, nil
}

func TestBotRunner_RetryPolicy_NoRetry(t *testing.T) {
	exec := &countingExecutor{failN: 0, output: "ok", tokens: 10}
	def := BotDefinition{ID: "test-bot", Prompt: "test"}
	runner := NewBotRunner(def).
		WithExecutor(exec).
		WithRetryPolicy(RetryPolicy{MaxRetries: 0, RetryBackoff: 1 * time.Millisecond})

	result, err := runner.Execute(context.Background(), &BotState{}, "trigger")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if exec.calls != 1 {
		t.Errorf("expected 1 call, got %d", exec.calls)
	}
}

func TestBotRunner_RetryPolicy_RetriesThenSucceeds(t *testing.T) {
	exec := &countingExecutor{failN: 2, output: "recovered", tokens: 50}
	def := BotDefinition{ID: "test-bot", Prompt: "test"}
	runner := NewBotRunner(def).
		WithExecutor(exec).
		WithRetryPolicy(RetryPolicy{MaxRetries: 3, RetryBackoff: 1 * time.Millisecond})

	result, err := runner.Execute(context.Background(), &BotState{}, "trigger")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success after retries, got: %s", result.Error)
	}
	if exec.calls != 3 { // 2 failures + 1 success
		t.Errorf("expected 3 calls (2 fails + 1 success), got %d", exec.calls)
	}
	if result.Output != "recovered" {
		t.Errorf("expected output 'recovered', got %q", result.Output)
	}
}

func TestBotRunner_RetryPolicy_ExhaustedAllRetries(t *testing.T) {
	exec := &countingExecutor{failN: 99, output: "ok", tokens: 10}
	def := BotDefinition{ID: "test-bot", Prompt: "test"}
	runner := NewBotRunner(def).
		WithExecutor(exec).
		WithRetryPolicy(RetryPolicy{MaxRetries: 2, RetryBackoff: 1 * time.Millisecond})

	result, err := runner.Execute(context.Background(), &BotState{}, "trigger")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure after all retries exhausted")
	}
	// 1 initial + 2 retries = 3 total calls
	if exec.calls != 3 {
		t.Errorf("expected 3 calls (1 + 2 retries), got %d", exec.calls)
	}
}

func TestBotRunner_RetryPolicy_ContextCancelNoRetry(t *testing.T) {
	exec := &countingExecutor{failN: 99, output: "ok", tokens: 10}
	def := BotDefinition{ID: "test-bot", Prompt: "test"}
	runner := NewBotRunner(def).
		WithExecutor(exec).
		WithRetryPolicy(RetryPolicy{MaxRetries: 5, RetryBackoff: 100 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	result, err := runner.Execute(ctx, &BotState{}, "trigger")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure due to context cancellation")
	}
	// Should not have retried many times since context expired quickly.
	if exec.calls > 2 {
		t.Errorf("expected at most 2 calls before context cancel, got %d", exec.calls)
	}
}

func TestBotRunner_RetryPolicy_ExponentialBackoff(t *testing.T) {
	var timestamps []time.Time
	var callCount int32
	exec := &countingExecutor{}
	exec.execFunc = func(ctx context.Context, _, _ string) (string, int, error) {
		n := atomic.AddInt32(&callCount, 1)
		timestamps = append(timestamps, time.Now())
		if n <= 2 {
			return "", 0, errors.New("transient error")
		}
		return "ok", 10, nil
	}
	def := BotDefinition{ID: "test-bot", Prompt: "test"}
	runner := NewBotRunner(def).
		WithExecutor(exec).
		WithRetryPolicy(RetryPolicy{MaxRetries: 3, RetryBackoff: 10 * time.Millisecond})

	result, _ := runner.Execute(context.Background(), &BotState{}, "trigger")
	if !result.Success {
		t.Fatalf("expected success after retries")
	}
	if len(timestamps) != 3 {
		t.Fatalf("expected 3 timestamps, got %d", len(timestamps))
	}
	// First backoff: ~10ms, second backoff: ~20ms
	gap1 := timestamps[1].Sub(timestamps[0])
	gap2 := timestamps[2].Sub(timestamps[1])
	if gap1 < 5*time.Millisecond {
		t.Errorf("first backoff too short: %v (expected >= ~10ms)", gap1)
	}
	if gap2 < 15*time.Millisecond {
		t.Errorf("second backoff too short: %v (expected >= ~20ms)", gap2)
	}
	if gap2 <= gap1 {
		t.Errorf("expected exponential growth: gap1=%v gap2=%v", gap1, gap2)
	}
}
