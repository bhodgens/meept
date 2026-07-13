package agent

import (
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
