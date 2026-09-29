package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBreaker_CustomBudget_ExhaustsAtConfiguredLimit pins the
// agent.guards.repeat_error_limit plumbing at the breaker level: a breaker
// built with an explicit budget exhausts exactly there (not at the default
// 3), which is what lets the e2e breakers-04 suite raise the loop-level
// budget above the tool breaker's 5-strike veto.
func TestBreaker_CustomBudget_ExhaustsAtConfiguredLimit(t *testing.T) {
	b := newRepeatErrorBreakerWithBudget(10)
	hash := repeatErrorArgsHash(map[string]any{"path": "doomed"})

	for i := 1; i <= 9; i++ {
		exhausted, summary := b.Observe("file_write", hash, "not a directory")
		assert.False(t, exhausted, "failure %d must not exhaust a budget of 10", i)
		assert.Empty(t, summary)
		assert.True(t, b.Allow("file_write", hash), "must stay allowed through failure %d", i)
	}

	exhausted, summary := b.Observe("file_write", hash, "not a directory")
	assert.True(t, exhausted, "the 10th identical failure must exhaust the budget")
	assert.Contains(t, summary, "rejected the identical input 10 times")
	assert.False(t, b.Allow("file_write", hash))
}

// TestBreaker_NonPositiveBudgetFallsBackToDefault pins the fallback
// contract: zero AND negative budgets resolve to maxIdenticalToolErrors (3)
// — the loader passes raw config through, so the <=0 fallback must live at
// the breaker.
func TestBreaker_NonPositiveBudgetFallsBackToDefault(t *testing.T) {
	for _, budget := range []int{0, -1, -99} {
		b := newRepeatErrorBreakerWithBudget(budget)
		hash := repeatErrorArgsHash(map[string]any{"k": budget})

		for i := 1; i <= 2; i++ {
			exhausted, _ := b.Observe("task_create", hash, "boom")
			assert.False(t, exhausted, "budget %d: failure %d must not exhaust", budget, i)
		}
		exhausted, summary := b.Observe("task_create", hash, "boom")
		assert.True(t, exhausted, "budget %d: the 3rd failure must exhaust at the default", budget)
		assert.Contains(t, summary, "rejected the identical input 3 times")
	}
}

// TestRepeatErrorBudget_FromNormalizedGuards pins the loop-construction
// wiring: an unnormalized zero GuardConfig falls back to the default budget,
// and a configured GuardConfig value lands on the loop via
// WithRepeatErrorBudget.
func TestRepeatErrorBudget_FromNormalizedGuards(t *testing.T) {
	// Zero-value config normalizes to the ship-on default budget.
	loop := NewAgentLoop("budget-defaults", "")
	defer loop.wg.Wait()
	loop.repeatErr.Observe("t", repeatErrorArgsHash(map[string]any{"a": 1}), "e")
	assert.Equal(t, DefaultRepeatErrorLimit, loop.repeatErrorBudget,
		"an unconfigured loop must carry the default repeat-error budget")
	assert.Equal(t, DefaultRepeatErrorLimit, loop.repeatErr.budgetOrDefault())

	// An explicit WithRepeatErrorBudget (daemon wiring) is honored
	// alongside AgentConfig carrying the normalized guards value.
	loop2 := NewAgentLoop("budget-explicit", "",
		WithAgentConfig(AgentConfig{Guards: GuardConfig{RepeatErrorLimit: 10}}),
		WithRepeatErrorBudget(10))
	defer loop2.wg.Wait()
	assert.Equal(t, 10, loop2.repeatErrorBudget)
	assert.Equal(t, 10, loop2.repeatErr.budgetOrDefault())

	// A raw <=0 option value falls back to the default at the breaker even
	// when the guards value is present (option wins only when non-zero —
	// but a non-positive option leaves the guards-derived budget intact).
	loop3 := NewAgentLoop("budget-negative", "",
		WithAgentConfig(AgentConfig{Guards: GuardConfig{RepeatErrorLimit: 7}}),
		WithRepeatErrorBudget(-1))
	defer loop3.wg.Wait()
	// The option overwrote the field with -1; the breaker itself must
	// still fall back to the shipped default rather than disabling the
	// breaker.
	assert.Equal(t, maxIdenticalToolErrors, loop3.repeatErr.budgetOrDefault(),
		"a non-positive option value must never disable the breaker")
}
