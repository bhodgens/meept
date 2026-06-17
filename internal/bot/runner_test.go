package bot

import (
	"strings"
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
