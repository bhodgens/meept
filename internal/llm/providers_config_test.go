package llm

import (
	"testing"
)

func TestConfigLoads(t *testing.T) {
	cfg, err := LoadProvidersConfig("../../config/models.json5")
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify root-level model references
	if cfg.Model != "zai/glm-5.2" {
		t.Errorf("model = %q, want zai/glm-5.2", cfg.Model)
	}
	if cfg.SmallModel != "local/lfm-1.2b-q8" {
		t.Errorf("small_model = %q, want local/lfm-1.2b-q8", cfg.SmallModel)
	}
	if cfg.ClassifierModel != "classifier" {
		t.Errorf("classifier_model = %q, want classifier", cfg.ClassifierModel)
	}

	// Verify classifier alias uses lfm-1.2b-q8 as primary
	classifierAlias, ok := cfg.ModelAliases["classifier"]
	if !ok {
		t.Fatal("classifier alias not found")
	}
	if len(classifierAlias.Models) == 0 || classifierAlias.Models[0] != "local/lfm-1.2b-q8" {
		t.Errorf("classifier alias primary = %q, want local/lfm-1.2b-q8", classifierAlias.Models[0])
	}

	// Verify coder alias uses glm-5.2
	coderAlias, ok := cfg.ModelAliases["coder"]
	if !ok {
		t.Fatal("coder alias not found")
	}
	if len(coderAlias.Models) == 0 || coderAlias.Models[0] != "zai/glm-5.2" {
		t.Errorf("coder alias primary = %q, want zai/glm-5.2", coderAlias.Models[0])
	}

	// Verify local provider has lfm-8b-f16 model
	localProvider, ok := cfg.Providers["local"]
	if !ok {
		t.Fatal("local provider not found")
	}

	lfm8b, ok := localProvider.Models["lfm-8b-f16"]
	if !ok {
		t.Fatal("lfm-8b-f16 model not found in local provider")
	}
	if lfm8b.Name != "/Volumes/LLMs/LiquidAI/LFM2.5-8B-A1B-GGUF/LFM2.5-8B-A1B-F16.gguf" {
		t.Errorf("lfm-8b-f16 name = %q, want /Volumes/LLMs/LiquidAI/LFM2.5-8B-A1B-GGUF/LFM2.5-8B-A1B-F16.gguf", lfm8b.Name)
	}
	if lfm8b.MaxConcurrency != 2 {
		t.Errorf("lfm-8b-f16 max_concurrency = %d, want 2", lfm8b.MaxConcurrency)
	}
	if lfm8b.ContextLimit != 16384 {
		t.Errorf("lfm-8b-f16 context_limit = %d, want 16384", lfm8b.ContextLimit)
	}

	// Verify lfm-1.2b-q8 model (primary small model)
	lfm12b, ok := localProvider.Models["lfm-1.2b-q8"]
	if !ok {
		t.Fatal("lfm-1.2b-q8 model not found in local provider")
	}
	if lfm12b.ContextLimit != 8192 {
		t.Errorf("lfm-1.2b-q8 context_limit = %d, want 8192", lfm12b.ContextLimit)
	}

	// Verify zai provider has glm-5.2
	zaiProvider, ok := cfg.Providers["zai"]
	if !ok {
		t.Fatal("zai provider not found")
	}
	glm52, ok := zaiProvider.Models["glm-5.2"]
	if !ok {
		t.Fatal("glm-5.2 model not found in zai provider")
	}
	if glm52.Name != "glm-5.2" {
		t.Errorf("glm-5.2 name = %q, want glm-5.2", glm52.Name)
	}
}
