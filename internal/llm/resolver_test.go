package llm

import (
	"log/slog"
	"os"
	"testing"
	"time"
)

func createTestConfig() *ProvidersConfig {
	return &ProvidersConfig{
		Model:      "zai/glm-4.7",
		SmallModel: "zai/glm-4.5-air",
		Providers: map[string]ProviderConfig{
			"zai": {
				API: "openai",
				Options: ProviderOptionsConfig{
					BaseURL: "https://api.z.ai/test",
					APIKey:  "test-key",
				},
				Models: map[string]ModelDef{
					"glm-4.7": {
						Name:         "glm-4.7",
						Capabilities: []string{"completion", "code", "reasoning", "tool_use"},
						InputCost:    0.0,
						OutputCost:   0.0,
						ContextLimit: 128000,
						MaxOutput:    8192,
						Temperature:  0.7,
					},
					"glm-4.5-air": {
						Name:         "glm-4.5-air",
						Capabilities: []string{"completion", "code", "reasoning"},
						InputCost:    0.0,
						OutputCost:   0.0,
						ContextLimit: 32000,
						MaxOutput:    4096,
						Temperature:  0.7,
					},
				},
			},
			"ollama": {
				API: "openai",
				Options: ProviderOptionsConfig{
					BaseURL: "http://localhost:11434/v1",
				},
				Models: map[string]ModelDef{
					"llama3.2": {
						Name:         "llama3.2",
						Capabilities: []string{"code", "tool_use", "reasoning"},
						InputCost:    0.0,
						OutputCost:   0.0,
						ContextLimit: 128000,
						MaxOutput:    4096,
						Temperature:  0.7,
					},
				},
			},
		},
		ModelAliases: map[string]ModelAliasEntry{
			"coder": {
				Models:   []string{"zai/glm-4.7", "ollama/llama3.2"},
				Timeout:  30,
				MaxFails: 3,
			},
			"planner": {
				Models:   []string{"zai/glm-4.5-air", "ollama/llama3.2"},
				Timeout:  15,
				MaxFails: 2,
			},
		},
	}
}

func TestResolver_NewResolver_LoadsAliases(t *testing.T) {
	cfg := createTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	
	resolver := NewResolver(cfg, logger)

	// Verify aliases were loaded
	if len(resolver.aliases) != 2 {
		t.Fatalf("Expected 2 aliases, got %d", len(resolver.aliases))
	}

	// Verify coder alias
	coderAlias, ok := resolver.aliases["coder"]
	if !ok {
		t.Fatal("Expected 'coder' alias to exist")
	}
	if len(coderAlias.Models) != 2 {
		t.Fatalf("Expected 'coder' alias to have 2 models, got %d", len(coderAlias.Models))
	}
	if coderAlias.Timeout != 30*time.Second {
		t.Errorf("Expected 'coder' timeout to be 30s, got %v", coderAlias.Timeout)
	}
	if coderAlias.MaxFails != 3 {
		t.Errorf("Expected 'coder' max_fails to be 3, got %d", coderAlias.MaxFails)
	}

	// Verify planner alias
	plannerAlias, ok := resolver.aliases["planner"]
	if !ok {
		t.Fatal("Expected 'planner' alias to exist")
	}
	if len(plannerAlias.Models) != 2 {
		t.Fatalf("Expected 'planner' alias to have 2 models, got %d", len(plannerAlias.Models))
	}
	if plannerAlias.Timeout != 15*time.Second {
		t.Errorf("Expected 'planner' timeout to be 15s, got %v", plannerAlias.Timeout)
	}
}

func TestResolver_ResolveForAlias(t *testing.T) {
	cfg := createTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	
	resolver := NewResolver(cfg, logger)

	// Should return the first model initially
	modelConfig, err := resolver.ResolveForAlias("coder")
	if err != nil {
		t.Fatalf("ResolveForAlias failed: %v", err)
	}
	if modelConfig.ModelID != "glm-4.7" {
		t.Errorf("Expected first model 'glm-4.7', got '%s'", modelConfig.ModelID)
	}
}

func TestResolver_ResolveForAlias_NotFound(t *testing.T) {
	cfg := createTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	
	resolver := NewResolver(cfg, logger)

	_, err := resolver.ResolveForAlias("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent alias")
	}
}

func TestResolver_RecordAliasFailure_Success(t *testing.T) {
	cfg := createTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	
	resolver := NewResolver(cfg, logger)

	// Record a failure
	resolver.RecordAliasFailure("coder", nil)

	// Check health state
	health := resolver.health["coder"]
	if health == nil {
		t.Fatal("Expected health tracking to exist")
	}
	if health.ConsecutiveFails != 1 {
		t.Errorf("Expected 1 consecutive failure, got %d", health.ConsecutiveFails)
	}
	// CooldownUntil should be set to a future time
	if health.CooldownUntil.IsZero() {
		t.Errorf("Expected cooldown until to be set")
	}
	if health.CooldownUntil.Before(time.Now()) {
		t.Errorf("Expected cooldown until to be in the future, got %v", health.CooldownUntil)
	}
}

func TestResolver_RecordAliasSuccess_ResetsFailures(t *testing.T) {
	cfg := createTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	
	resolver := NewResolver(cfg, logger)

	// Record some failures
	resolver.RecordAliasFailure("coder", nil)
	resolver.RecordAliasFailure("coder", nil)
	resolver.RecordAliasFailure("coder", nil)

	// Verify failures were recorded
	health := resolver.health["coder"]
	if health.ConsecutiveFails != 3 {
		t.Fatalf("Expected 3 consecutive failures, got %d", health.ConsecutiveFails)
	}

	// Record success
	resolver.RecordAliasSuccess("coder")

	// Verify resets
	if health.ConsecutiveFails != 0 {
		t.Errorf("Expected 0 consecutive failures after success, got %d", health.ConsecutiveFails)
	}
	if !health.CooldownUntil.IsZero() {
		t.Error("Expected cooldown to be reset")
	}
}

func TestResolver_Rotation(t *testing.T) {
	cfg := createTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	
	resolver := NewResolver(cfg, logger)

	// Get initial model
	model1, _ := resolver.ResolveForAlias("coder")
	if model1.ModelID != "glm-4.7" {
		t.Errorf("Expected first model 'glm-4.7', got '%s'", model1.ModelID)
	}

	// Simulate failures to trigger cooldown
	for i := 0; i < 3; i++ {
		resolver.RecordAliasFailure("coder", nil)
	}

	// Next resolution should trigger rotation
	model2, _ := resolver.ResolveForAlias("coder")
	if model2.ModelID != "llama3.2" {
		t.Errorf("Expected rotation to 'llama3.2', got '%s'", model2.ModelID)
	}
}

func TestResolver_HasAlias(t *testing.T) {
	cfg := createTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	
	resolver := NewResolver(cfg, logger)

	if !resolver.HasAlias("coder") {
		t.Error("Expected 'coder' alias to exist")
	}
	if !resolver.HasAlias("planner") {
		t.Error("Expected 'planner' alias to exist")
	}
	if resolver.HasAlias("nonexistent") {
		t.Error("Expected 'nonexistent' alias to not exist")
	}
}

func TestResolver_ExponentialBackoff(t *testing.T) {
	cfg := createTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	
	resolver := NewResolver(cfg, logger)

	// Record first failure
	resolver.RecordAliasFailure("coder", nil)
	health1 := resolver.health["coder"]
	cooldown1 := health1.CooldownUntil.Sub(time.Now())

	// Record second failure
	resolver.RecordAliasFailure("coder", nil)
	health2 := resolver.health["coder"]
	cooldown2 := health2.CooldownUntil.Sub(time.Now())

	// Cooldown should roughly double (30s -> 60s)
	if cooldown2 < cooldown1 + cooldown1 {
		t.Errorf("Expected exponential backoff: cooldown1=%v, cooldown2=%v", cooldown1, cooldown2)
	}
}
