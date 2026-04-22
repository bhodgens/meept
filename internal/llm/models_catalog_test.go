package llm

import "testing"

func TestGetModelsForProvider(t *testing.T) {
	tests := []struct {
		providerID  string
		wantFound   bool
		wantMinLen  int
	}{
		{"anthropic", true, 3},
		{"openai", true, 2},
		{"openrouter", true, 1},
		{"ollama", true, 2},
		{"zai", true, 2},
		{"google", true, 2},
		{"deepseek", true, 2},
		{"unknown", false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.providerID, func(t *testing.T) {
			models, ok := GetModelsForProvider(tt.providerID)
			if ok != tt.wantFound {
				t.Fatalf("found = %v, want %v", ok, tt.wantFound)
			}
			if tt.wantFound && len(models) < tt.wantMinLen {
				t.Errorf("expected at least %d models, got %d", tt.wantMinLen, len(models))
			}
		})
	}
}

func TestGetModel(t *testing.T) {
	tests := []struct {
		providerID string
		modelID    string
		wantFound  bool
		wantName   string
	}{
		{"anthropic", "claude-sonnet-4-6", true, "Claude Sonnet 4.6"},
		{"openai", "gpt-5.4", true, "GPT-5.4"},
		{"zai", "glm-4.7", true, "GLM-4.7"},
		{"anthropic", "nonexistent", false, ""},
		{"unknown", "model", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.providerID+"/"+tt.modelID, func(t *testing.T) {
			model, ok := GetModel(tt.providerID, tt.modelID)
			if ok != tt.wantFound {
				t.Fatalf("found = %v, want %v", ok, tt.wantFound)
			}
			if tt.wantFound && model.Name != tt.wantName {
				t.Errorf("got name = %s, want %s", model.Name, tt.wantName)
			}
		})
	}
}

func TestGetAllCatalogModels(t *testing.T) {
	all := GetAllCatalogModels()
	if len(all) == 0 {
		t.Fatal("expected some models")
	}

	// Verify all models have required fields
	for _, m := range all {
		if m.ModelID == "" {
			t.Error("model missing ModelID")
		}
		if m.Name == "" {
			t.Error("model missing Name")
		}
		if m.ProviderID == "" {
			t.Error("model missing ProviderID")
		}
		if m.Capabilities == nil {
			t.Error("model missing Capabilities")
		}
	}
}

func TestModelCatalogEntry(t *testing.T) {
	// Test a specific model entry
	model, ok := GetModel("anthropic", "claude-sonnet-4-6")
	if !ok {
		t.Fatal("model not found")
	}

	if model.ContextWindow != 200000 {
		t.Errorf("wrong context window: %d, want 200000", model.ContextWindow)
	}
	if model.MaxOutput != 8192 {
		t.Errorf("wrong max output: %d, want 8192", model.MaxOutput)
	}
	if model.InputCost != 3.0 {
		t.Errorf("wrong input cost: %f, want 3.0", model.InputCost)
	}
	if model.OutputCost != 15.0 {
		t.Errorf("wrong output cost: %f, want 15.0", model.OutputCost)
	}
}
