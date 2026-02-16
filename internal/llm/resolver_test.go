package llm

import (
	"testing"
)

func testProvidersConfig() *ProvidersConfig {
	return &ProvidersConfig{
		Model:      "provider1/model-a",
		SmallModel: "provider1/model-b",
		Providers: map[string]ProviderConfig{
			"provider1": {
				API: "openai",
				Options: ProviderOptionsConfig{
					BaseURL: "http://localhost:11434/v1",
				},
				Models: map[string]ModelDef{
					"model-a": {
						Name:         "model-a",
						Capabilities: []string{"code", "reasoning"},
						InputCost:    1.0,
						OutputCost:   2.0,
						ContextLimit: 128000,
						MaxOutput:    4096,
						Temperature:  0.7,
					},
					"model-b": {
						Name:         "model-b",
						Capabilities: []string{"code"},
						InputCost:    0.5,
						OutputCost:   1.0,
						ContextLimit: 32000,
						MaxOutput:    2048,
						Temperature:  0.5,
					},
				},
			},
			"provider2": {
				API: "openai",
				Options: ProviderOptionsConfig{
					BaseURL: "https://api.example.com/v1",
				},
				Models: map[string]ModelDef{
					"model-x": {
						Name:         "model-x",
						Capabilities: []string{"code", "reasoning", "tool_use"},
						InputCost:    3.0,
						OutputCost:   15.0,
						ContextLimit: 200000,
						MaxOutput:    8192,
						Temperature:  0.7,
					},
				},
			},
		},
	}
}

func TestResolverDefaultModel(t *testing.T) {
	cfg := testProvidersConfig()
	r := NewResolver(cfg, nil)

	defaultModel := r.DefaultModel()
	if defaultModel == nil {
		t.Fatal("Default model should not be nil")
	}

	if defaultModel.ModelID != "model-a" {
		t.Errorf("DefaultModel.ModelID = %q, want model-a", defaultModel.ModelID)
	}

	if defaultModel.ProviderID != "provider1" {
		t.Errorf("DefaultModel.ProviderID = %q, want provider1", defaultModel.ProviderID)
	}
}

func TestResolverSmallModel(t *testing.T) {
	cfg := testProvidersConfig()
	r := NewResolver(cfg, nil)

	smallModel := r.SmallModel()
	if smallModel == nil {
		t.Fatal("Small model should not be nil")
	}

	if smallModel.ModelID != "model-b" {
		t.Errorf("SmallModel.ModelID = %q, want model-b", smallModel.ModelID)
	}
}

func TestResolverResolveForSkillNoRequirements(t *testing.T) {
	cfg := testProvidersConfig()
	r := NewResolver(cfg, nil)

	// No skill - should return default
	model, err := r.ResolveForSkill(nil, nil)
	if err != nil {
		t.Fatalf("ResolveForSkill failed: %v", err)
	}

	if model.ModelID != "model-a" {
		t.Errorf("Model = %q, want model-a (default)", model.ModelID)
	}

	// Empty requirements - should return default
	model, err = r.ResolveForSkill(&SkillRequirements{Name: "test"}, nil)
	if err != nil {
		t.Fatalf("ResolveForSkill failed: %v", err)
	}

	if model.ModelID != "model-a" {
		t.Errorf("Model = %q, want model-a (default)", model.ModelID)
	}
}

func TestResolverResolveForSkillCurrentSatisfies(t *testing.T) {
	cfg := testProvidersConfig()
	r := NewResolver(cfg, nil)

	current := r.DefaultModel() // has code, reasoning

	skill := &SkillRequirements{
		Name:     "test-skill",
		Requires: []string{"code"},
	}

	model, err := r.ResolveForSkill(skill, current)
	if err != nil {
		t.Fatalf("ResolveForSkill failed: %v", err)
	}

	// Should use current model since it satisfies requirements
	if model.ModelID != current.ModelID {
		t.Errorf("Model = %q, want %q (current)", model.ModelID, current.ModelID)
	}
}

func TestResolverResolveForSkillEscalate(t *testing.T) {
	cfg := testProvidersConfig()
	r := NewResolver(cfg, nil)

	// model-b only has "code", not "tool_use"
	current := r.SmallModel()

	skill := &SkillRequirements{
		Name:     "tool-skill",
		Requires: []string{"tool_use"},
	}

	model, err := r.ResolveForSkill(skill, current)
	if err != nil {
		t.Fatalf("ResolveForSkill failed: %v", err)
	}

	// Should escalate to model-x which has tool_use
	if model.ModelID != "model-x" {
		t.Errorf("Model = %q, want model-x", model.ModelID)
	}
}

func TestResolverResolveForSkillNoMatch(t *testing.T) {
	cfg := testProvidersConfig()
	r := NewResolver(cfg, nil)

	skill := &SkillRequirements{
		Name:     "impossible-skill",
		Requires: []string{"vision", "magic"},
	}

	_, err := r.ResolveForSkill(skill, nil)
	if err == nil {
		t.Fatal("Expected CapabilityError")
	}

	capErr, ok := err.(*CapabilityError)
	if !ok {
		t.Fatalf("Expected CapabilityError, got %T", err)
	}

	if capErr.SkillName != "impossible-skill" {
		t.Errorf("SkillName = %q, want impossible-skill", capErr.SkillName)
	}
}

func TestResolverFindCheapest(t *testing.T) {
	cfg := testProvidersConfig()
	r := NewResolver(cfg, nil)

	// Find cheapest with "code" capability
	model := r.FindCheapest([]string{"code"})
	if model == nil {
		t.Fatal("Should find a model")
	}

	// model-b is cheapest (0.5 + 1.0 = 1.5)
	if model.ModelID != "model-b" {
		t.Errorf("Model = %q, want model-b (cheapest)", model.ModelID)
	}
}

func TestResolverFindByProvider(t *testing.T) {
	cfg := testProvidersConfig()
	r := NewResolver(cfg, nil)

	models := r.FindByProvider("provider1")
	if len(models) != 2 {
		t.Errorf("Expected 2 models from provider1, got %d", len(models))
	}

	models = r.FindByProvider("provider2")
	if len(models) != 1 {
		t.Errorf("Expected 1 model from provider2, got %d", len(models))
	}

	models = r.FindByProvider("nonexistent")
	if len(models) != 0 {
		t.Errorf("Expected 0 models from nonexistent provider, got %d", len(models))
	}
}

func TestResolverAllModels(t *testing.T) {
	cfg := testProvidersConfig()
	r := NewResolver(cfg, nil)

	models := r.AllModels()
	if len(models) != 3 {
		t.Errorf("Expected 3 total models, got %d", len(models))
	}
}

func TestResolverResolveRef(t *testing.T) {
	cfg := testProvidersConfig()
	r := NewResolver(cfg, nil)

	model := r.ResolveRef("provider2/model-x")
	if model == nil {
		t.Fatal("Should resolve model")
	}

	if model.ModelID != "model-x" {
		t.Errorf("ModelID = %q, want model-x", model.ModelID)
	}

	// Invalid ref
	model = r.ResolveRef("invalid")
	if model != nil {
		t.Error("Should return nil for invalid ref")
	}
}

func TestModelConfigHasCapabilities(t *testing.T) {
	m := &ModelConfig{
		Capabilities: map[string]bool{
			"code":      true,
			"reasoning": true,
		},
	}

	if !m.HasCapability("code") {
		t.Error("Should have code capability")
	}

	if m.HasCapability("vision") {
		t.Error("Should not have vision capability")
	}

	if !m.HasCapabilities([]string{"code", "reasoning"}) {
		t.Error("Should have both code and reasoning")
	}

	if m.HasCapabilities([]string{"code", "vision"}) {
		t.Error("Should not have code and vision")
	}
}
