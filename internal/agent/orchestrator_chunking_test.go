package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
)

func TestEstimateStepTokens_Heuristic(t *testing.T) {
	step := &task.TaskStep{Description: "refactor the auth middleware", ToolHint: "code"}
	cfg := &llm.ModelConfig{ContextLimit: 32000}
	cost := estimateStepTokens(step, cfg)
	if cost <= 0 {
		t.Errorf("cost = %d; want > 0", cost)
	}
	// tool output budget for "code" is 8K; description ~10 tokens; cost should be ~8K
	if cost < 7000 || cost > 9000 {
		t.Errorf("cost = %d; want ~8000", cost)
	}
}

func TestExecutorBudget_PercentOfContext(t *testing.T) {
	cfg := &llm.ModelConfig{ContextLimit: 32000}
	b := executorBudget(cfg)
	if b != 12800 { // 40%
		t.Errorf("budget = %d; want 12800", b)
	}
}

func TestExecutorBudget_NilOrDefault(t *testing.T) {
	b := executorBudget(nil)
	if b != 12000 {
		t.Errorf("nil budget = %d; want 12000 (safe default)", b)
	}
	cfg := &llm.ModelConfig{ContextLimit: 0}
	b = executorBudget(cfg)
	if b != 12000 {
		t.Errorf("zero-limit budget = %d; want 12000 (safe default)", b)
	}
}

func TestToolOutputBudget(t *testing.T) {
	cases := map[string]int{
		"code":     8000,
		"refactor": 8000,
		"debug":    4000,
		"fix":      4000,
		"git":      1000,
		"commit":   1000,
		"chat":     1000,
		"unknown":  2000,
		"":         2000,
	}
	for hint, want := range cases {
		got := toolOutputBudget(hint)
		if got != want {
			t.Errorf("toolOutputBudget(%q) = %d; want %d", hint, got, want)
		}
	}
}
