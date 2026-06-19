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
	if cfg.SmallModel != "local/lfm-8b-4bit" {
		t.Errorf("small_model = %q, want local/lfm-8b-4bit", cfg.SmallModel)
	}
	if cfg.ClassifierModel != "classifier" {
		t.Errorf("classifier_model = %q, want classifier", cfg.ClassifierModel)
	}

	// Verify classifier alias uses lfm-8b-4bit as primary
	classifierAlias, ok := cfg.ModelAliases["classifier"]
	if !ok {
		t.Fatal("classifier alias not found")
	}
	if len(classifierAlias.Models) == 0 || classifierAlias.Models[0] != "local/lfm-8b-4bit" {
		t.Errorf("classifier alias primary = %q, want local/lfm-8b-4bit", classifierAlias.Models[0])
	}

	// Verify coder alias uses glm-5.2
	coderAlias, ok := cfg.ModelAliases["coder"]
	if !ok {
		t.Fatal("coder alias not found")
	}
	if len(coderAlias.Models) == 0 || coderAlias.Models[0] != "zai/glm-5.2" {
		t.Errorf("coder alias primary = %q, want zai/glm-5.2", coderAlias.Models[0])
	}

	// Verify local provider has lfm-8b-4bit model
	localProvider, ok := cfg.Providers["local"]
	if !ok {
		t.Fatal("local provider not found")
	}

	lfm8b, ok := localProvider.Models["lfm-8b-4bit"]
	if !ok {
		t.Fatal("lfm-8b-4bit model not found in local provider")
	}
	if lfm8b.Name != "LFM2.5-8B-A1B-Instruct-MLX-4bit" {
		t.Errorf("lfm-8b-4bit name = %q, want LFM2.5-8B-A1B-Instruct-MLX-4bit", lfm8b.Name)
	}
	if lfm8b.MaxConcurrency != 2 {
		t.Errorf("lfm-8b-4bit max_concurrency = %d, want 2", lfm8b.MaxConcurrency)
	}
	if lfm8b.ContextLimit != 16384 {
		t.Errorf("lfm-8b-4bit context_limit = %d, want 16384", lfm8b.ContextLimit)
	}

	// Verify vision model
	vlmModel, ok := localProvider.Models["lfm-vl-1.6b-6bit"]
	if !ok {
		t.Fatal("lfm-vl-1.6b-6bit model not found")
	}
	if vlmModel.Name != "LFM2.5-VL-1.6B-MLX-6bit" {
		t.Errorf("lfm-vl-1.6b-6bit name = %q", vlmModel.Name)
	}
	hasVision := false
	for _, cap := range vlmModel.Capabilities {
		if cap == "vision" {
			hasVision = true
			break
		}
	}
	if !hasVision {
		t.Error("lfm-vl-1.6b-6bit missing vision capability")
	}
	if vlmModel.MaxConcurrency != 1 {
		t.Errorf("lfm-vl-1.6b-6bit max_concurrency = %d, want 1", vlmModel.MaxConcurrency)
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
