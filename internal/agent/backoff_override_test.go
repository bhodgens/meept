package agent

import (
	"context"
	"testing"
	"time"
)

func TestSetDefaultBackoffOverride_AppliedToLLMConfig(t *testing.T) {
	t.Cleanup(clearDefaultBackoffOverride)

	override := BackoffConfig{
		BaseDelay:   3 * time.Second,
		MaxDelay:    2 * time.Minute,
		Multiplier:  3.5,
		Jitter:      0.8,
		MaxAttempts: 12,
	}
	SetDefaultBackoffOverride(override)

	cfg := LLMBackoffConfig()

	if cfg.BaseDelay != 3*time.Second {
		t.Errorf("BaseDelay: got %v, want 3s", cfg.BaseDelay)
	}
	if cfg.MaxDelay != 2*time.Minute {
		t.Errorf("MaxDelay: got %v, want 2m", cfg.MaxDelay)
	}
	if cfg.Multiplier != 3.5 {
		t.Errorf("Multiplier: got %v, want 3.5", cfg.Multiplier)
	}
	if cfg.Jitter != 0.8 {
		t.Errorf("Jitter: got %v, want 0.8", cfg.Jitter)
	}
	if cfg.MaxAttempts != 12 {
		t.Errorf("MaxAttempts: got %d, want 12", cfg.MaxAttempts)
	}
}

func TestSetDefaultBackoffOverride_AppliedToDefaultConfig(t *testing.T) {
	t.Cleanup(clearDefaultBackoffOverride)

	override := BackoffConfig{
		BaseDelay: 750 * time.Millisecond,
		MaxDelay:  90 * time.Second,
	}
	SetDefaultBackoffOverride(override)

	cfg := DefaultBackoffConfig()

	if cfg.BaseDelay != 750*time.Millisecond {
		t.Errorf("BaseDelay: got %v, want 750ms", cfg.BaseDelay)
	}
	if cfg.MaxDelay != 90*time.Second {
		t.Errorf("MaxDelay: got %v, want 90s", cfg.MaxDelay)
	}
	// Non-overridden fields should keep preset values
	if cfg.Multiplier != 2.0 {
		t.Errorf("Multiplier: got %v, want 2.0 (preset)", cfg.Multiplier)
	}
	if cfg.Jitter != 0.3 {
		t.Errorf("Jitter: got %v, want 0.3 (preset)", cfg.Jitter)
	}
	if cfg.MaxAttempts != 10 {
		t.Errorf("MaxAttempts: got %d, want 10 (preset)", cfg.MaxAttempts)
	}
}

func TestSetDefaultBackoffOverride_ZeroFieldsIgnored(t *testing.T) {
	t.Cleanup(clearDefaultBackoffOverride)

	// Only set BaseDelay; everything else is zero and should be ignored.
	override := BackoffConfig{
		BaseDelay: 7 * time.Second,
	}
	SetDefaultBackoffOverride(override)

	cfg := LLMBackoffConfig()

	if cfg.BaseDelay != 7*time.Second {
		t.Errorf("BaseDelay: got %v, want 7s", cfg.BaseDelay)
	}
	// These should be the LLM preset values
	if cfg.MaxDelay != 30*time.Second {
		t.Errorf("MaxDelay: got %v, want 30s (preset)", cfg.MaxDelay)
	}
	if cfg.Multiplier != 2.0 {
		t.Errorf("Multiplier: got %v, want 2.0 (preset)", cfg.Multiplier)
	}
	if cfg.Jitter != 0.3 {
		t.Errorf("Jitter: got %v, want 0.3 (preset)", cfg.Jitter)
	}
	if cfg.MaxAttempts != 5 {
		t.Errorf("MaxAttempts: got %d, want 5 (preset)", cfg.MaxAttempts)
	}
}

func TestSetDefaultBackoffOverride_DoesNotAffectOperationSpecificPresets(t *testing.T) {
	t.Cleanup(clearDefaultBackoffOverride)

	override := BackoffConfig{
		BaseDelay:   42 * time.Second,
		MaxAttempts: 99,
	}
	SetDefaultBackoffOverride(override)

	// Tool, Aggressive, Conservative, HTTPHook should NOT be affected
	toolCfg := ToolBackoffConfig()
	if toolCfg.BaseDelay == 42*time.Second {
		t.Error("ToolBackoffConfig should not inherit override, but BaseDelay was overridden")
	}
	if toolCfg.MaxAttempts == 99 {
		t.Error("ToolBackoffConfig should not inherit override, but MaxAttempts was overridden")
	}

	aggCfg := AggressiveBackoffConfig()
	if aggCfg.BaseDelay == 42*time.Second {
		t.Error("AggressiveBackoffConfig should not inherit override")
	}

	consCfg := ConservativeBackoffConfig()
	if consCfg.BaseDelay == 42*time.Second {
		t.Error("ConservativeBackoffConfig should not inherit override")
	}

	hookCfg := HTTPHookBackoffConfig(3)
	if hookCfg.BaseDelay == 42*time.Second {
		t.Error("HTTPHookBackoffConfig should not inherit override")
	}
}

func TestSetDefaultBackoffOverride_Clear(t *testing.T) {
	// Set an override
	SetDefaultBackoffOverride(BackoffConfig{
		BaseDelay:   42 * time.Second,
		MaxAttempts: 99,
	})

	// Verify it's active
	cfg := LLMBackoffConfig()
	if cfg.BaseDelay != 42*time.Second {
		t.Fatalf("override not active: got BaseDelay %v, want 42s", cfg.BaseDelay)
	}

	// Clear it
	clearDefaultBackoffOverride()

	// Verify presets are restored
	cfg = LLMBackoffConfig()
	if cfg.BaseDelay != 1*time.Second {
		t.Errorf("after clear, BaseDelay: got %v, want 1s (preset)", cfg.BaseDelay)
	}
	if cfg.MaxAttempts != 5 {
		t.Errorf("after clear, MaxAttempts: got %d, want 5 (preset)", cfg.MaxAttempts)
	}
}

// ---------------------------------------------------------------------------
// Per-operation override tests
// ---------------------------------------------------------------------------

// TestPerOperationOverride_LLM verifies that an "llm" per-operation override
// takes precedence over the hardcoded preset, and that clearing the override
// restores the preset.
func TestPerOperationOverride_LLM(t *testing.T) {
	t.Cleanup(func() {
		clearPerOperationOverrides()
		clearDefaultBackoffOverride()
	})

	SetPerOperationBackoffOverride("llm", BackoffConfig{
		BaseDelay: 7 * time.Second,
	})

	cfg := LLMBackoffConfig()
	if cfg.BaseDelay != 7*time.Second {
		t.Errorf("BaseDelay: got %v, want 7s", cfg.BaseDelay)
	}
	// Non-overridden fields should retain preset values.
	if cfg.MaxAttempts != 5 {
		t.Errorf("MaxAttempts: got %d, want 5 (preset)", cfg.MaxAttempts)
	}

	// Clear per-op overrides and verify fallback to preset.
	clearPerOperationOverrides()
	cfg = LLMBackoffConfig()
	if cfg.BaseDelay != 1*time.Second {
		t.Errorf("after clear, BaseDelay: got %v, want 1s (preset)", cfg.BaseDelay)
	}
}

// TestPerOperationOverride_ToolWeb verifies that a "tool_web" per-operation
// override is consumed by getRetryConfigForTool for web tools.
func TestPerOperationOverride_ToolWeb(t *testing.T) {
	t.Cleanup(func() {
		clearPerOperationOverrides()
		clearDefaultBackoffOverride()
	})

	SetPerOperationBackoffOverride("tool_web", BackoffConfig{
		MaxAttempts: 9,
	})

	e := &Executor{}
	cfg := e.getRetryConfigForTool("web_search")
	if cfg.MaxAttempts != 9 {
		t.Errorf("web_search MaxAttempts: got %d, want 9", cfg.MaxAttempts)
	}

	cfg = e.getRetryConfigForTool("web_fetch")
	if cfg.MaxAttempts != 9 {
		t.Errorf("web_fetch MaxAttempts: got %d, want 9", cfg.MaxAttempts)
	}
}

// TestPerOperationOverride_GlobalFallback verifies that when no per-op
// override is set, the global override still applies to LLMBackoffConfig.
func TestPerOperationOverride_GlobalFallback(t *testing.T) {
	t.Cleanup(func() {
		clearPerOperationOverrides()
		clearDefaultBackoffOverride()
	})

	SetDefaultBackoffOverride(BackoffConfig{
		BaseDelay: 9 * time.Second,
	})

	cfg := LLMBackoffConfig()
	if cfg.BaseDelay != 9*time.Second {
		t.Errorf("BaseDelay: got %v, want 9s (global fallback)", cfg.BaseDelay)
	}
}

// TestPerOperationOverride_PerOpWinsOverGlobal verifies that when both a
// per-op override and a global override are set, the per-op override wins
// for its operation category.
func TestPerOperationOverride_PerOpWinsOverGlobal(t *testing.T) {
	t.Cleanup(func() {
		clearPerOperationOverrides()
		clearDefaultBackoffOverride()
	})

	SetDefaultBackoffOverride(BackoffConfig{
		BaseDelay: 9 * time.Second,
	})
	SetPerOperationBackoffOverride("llm", BackoffConfig{
		BaseDelay: 7 * time.Second,
	})

	cfg := LLMBackoffConfig()
	if cfg.BaseDelay != 7*time.Second {
		t.Errorf("BaseDelay: got %v, want 7s (per-op wins)", cfg.BaseDelay)
	}
}

// TestPerOperationOverride_NilGuard verifies that applyOverride with a nil
// override pointer returns the preset unchanged.
func TestPerOperationOverride_NilGuard(t *testing.T) {
	preset := BackoffConfig{
		BaseDelay:   1 * time.Second,
		MaxDelay:    30 * time.Second,
		Multiplier:  2.0,
		Jitter:      0.3,
		MaxAttempts: 5,
	}
	result := applyOverride(preset, nil)
	if result != preset {
		t.Errorf("applyOverride with nil should return preset unchanged: got %+v", result)
	}
}

// TestPerOperationOverride_HTTP verifies that an "http" per-operation
// override is consumed by HTTPHookBackoffConfig.
func TestPerOperationOverride_HTTP(t *testing.T) {
	t.Cleanup(func() {
		clearPerOperationOverrides()
		clearDefaultBackoffOverride()
	})

	SetPerOperationBackoffOverride("http", BackoffConfig{
		BaseDelay: 3 * time.Second,
	})

	cfg := HTTPHookBackoffConfig(4)
	if cfg.BaseDelay != 3*time.Second {
		t.Errorf("HTTP BaseDelay: got %v, want 3s", cfg.BaseDelay)
	}
	// MaxAttempts should retain the passed count since override doesn't set it.
	if cfg.MaxAttempts != 4 {
		t.Errorf("HTTP MaxAttempts: got %d, want 4 (count param)", cfg.MaxAttempts)
	}
}

// ---------------------------------------------------------------------------
// RetryBudget LLM path attachment test
// ---------------------------------------------------------------------------

// TestRetryBudget_LLMPathBudgetAttached verifies that attachLLMBudget attaches
// a non-nil RetryBudget to the context, and that an already-attached budget is
// preserved.
func TestRetryBudget_LLMPathBudgetAttached(t *testing.T) {
	// Fresh context has no budget.
	ctx := context.Background()
	if b := GetRetryBudget(ctx); b != nil {
		t.Fatal("expected nil budget on fresh context")
	}

	// attachLLMBudget should attach a budget sized to maxAttempts.
	ctx = attachLLMBudget(ctx, 5)
	budget := GetRetryBudget(ctx)
	if budget == nil {
		t.Fatal("expected non-nil budget after attachLLMBudget")
	}
	if budget.Remaining() != 5 {
		t.Errorf("budget.Remaining(): got %d, want 5", budget.Remaining())
	}

	// Calling attachLLMBudget again should NOT replace the existing budget.
	existing := GetRetryBudget(ctx)
	ctx = attachLLMBudget(ctx, 99)
	if GetRetryBudget(ctx) != existing {
		t.Error("attachLLMBudget should preserve existing budget, not replace it")
	}
}
