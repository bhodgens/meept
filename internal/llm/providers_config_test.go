package llm

import (
	"testing"
)

func TestConfigLoads(t *testing.T) {
	cfg, err := LoadProvidersConfig("../../config/models.json5")
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify root-level model references (2026-09-06 MLX platform design:
	// general default is the local MLX 4-bit model; classifier primary is
	// the fine-tuned combined-sft on the local-mlx runtime).
	if cfg.Model != "local/lfm-8b-mlx-4bit" {
		t.Errorf("model = %q, want local/lfm-8b-mlx-4bit", cfg.Model)
	}
	if cfg.SmallModel != "local/lfm-1.2b-q8" {
		t.Errorf("small_model = %q, want local/lfm-1.2b-q8", cfg.SmallModel)
	}
	if cfg.ClassifierModel != "classifier" {
		t.Errorf("classifier_model = %q, want classifier", cfg.ClassifierModel)
	}
	if cfg.ImageModel != "xai-oauth/grok-imagine-image-2.0" {
		t.Errorf("image_model = %q, want xai-oauth/grok-imagine-image-2.0", cfg.ImageModel)
	}

	// Verify classifier alias primary: the fine-tuned combined-sft on the
	// local-mlx runtime (platform-intrinsic; A/B testing only).
	classifierAlias, ok := cfg.ModelAliases["classifier"]
	if !ok {
		t.Fatal("classifier alias not found")
	}
	if len(classifierAlias.Models) == 0 || classifierAlias.Models[0] != "local-mlx/lfm-combined-sft" {
		t.Errorf("classifier alias primary = %q, want local-mlx/lfm-combined-sft", classifierAlias.Models[0])
	}

	// Verify coder alias uses glm-5.2
	coderAlias, ok := cfg.ModelAliases["coder"]
	if !ok {
		t.Fatal("coder alias not found")
	}
	if len(coderAlias.Models) == 0 || coderAlias.Models[0] != "zai/glm-5.2" {
		t.Errorf("coder alias primary = %q, want zai/glm-5.2", coderAlias.Models[0])
	}

	// Verify local provider has lfm-8b-mlx-4bit model (2026-09-06: the
	// GGUF F16 general model was retired in favor of the MLX 4-bit one).
	localProvider, ok := cfg.Providers["local"]
	if !ok {
		t.Fatal("local provider not found")
	}

	lfm8b, ok := localProvider.Models["lfm-8b-mlx-4bit"]
	if !ok {
		t.Fatal("lfm-8b-mlx-4bit model not found in local provider")
	}
	if lfm8b.Name != "/Volumes/LLMs/LiquidAI/LFM2.5-8B-A1B-MLX-4bit" {
		t.Errorf("lfm-8b-mlx-4bit name = %q, want /Volumes/LLMs/LiquidAI/LFM2.5-8B-A1B-MLX-4bit", lfm8b.Name)
	}
	if lfm8b.MaxConcurrency != 2 {
		t.Errorf("lfm-8b-mlx-4bit max_concurrency = %d, want 2", lfm8b.MaxConcurrency)
	}
	if lfm8b.ContextLimit != 16384 {
		t.Errorf("lfm-8b-mlx-4bit context_limit = %d, want 16384", lfm8b.ContextLimit)
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
