package llm

import (
	"net/url"
	"slices"
	"testing"
)

func TestConfigLoads(t *testing.T) {
	cfg, err := LoadProvidersConfig("../../config/models.json5")
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify root-level model references (2026-09-12: the general default
	// moved from the mlx_lm endpoint to the same LFM2.5-8B weights on
	// llama.cpp, which is the runtime that extracts tool calls; the MLX
	// entry stays registered as the classifier alternate).
	if cfg.Model != "local-gguf/lfm-8b-gguf" {
		t.Errorf("model = %q, want local-gguf/lfm-8b-gguf", cfg.Model)
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
	// Classifier alias: the llama.cpp LFM2.5-8B primary (it is the general
	// driver too, so the classifier runs on the endpoint that can call
	// tools), then the same weights on mlx_lm — the 2026-09-06 A/B winner
	// (86.8% vs 54.4% corpus accuracy) — then combined-sft, then the fast
	// 1.2b.
	if len(classifierAlias.Models) == 0 || classifierAlias.Models[0] != "local-gguf/lfm-8b-gguf" {
		t.Errorf("classifier alias primary = %q, want local-gguf/lfm-8b-gguf", classifierAlias.Models[0])
	}
	if len(classifierAlias.Models) < 2 || classifierAlias.Models[1] != "local/lfm-8b-mlx-4bit" {
		t.Errorf("classifier alias alternate = %v, want local/lfm-8b-mlx-4bit at [1]", classifierAlias.Models)
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
	// LoadProvidersConfig always runs env expansion: with MEEPT_MODELS_DIR
	// unset (test env), the ${MEEPT_MODELS_DIR:-~/.meept/models} default in
	// the shipped config applies verbatim.
	if lfm8b.Name != "~/.meept/models/LiquidAI/LFM2.5-8B-A1B-MLX-4bit" {
		t.Errorf("lfm-8b-mlx-4bit name = %q, want ~/.meept/models/LiquidAI/LFM2.5-8B-A1B-MLX-4bit", lfm8b.Name)
	}
	if lfm8b.MaxConcurrency != 2 {
		t.Errorf("lfm-8b-mlx-4bit max_concurrency = %d, want 2", lfm8b.MaxConcurrency)
	}
	if lfm8b.ContextLimit != 16384 {
		t.Errorf("lfm-8b-mlx-4bit context_limit = %d, want 16384", lfm8b.ContextLimit)
	}

	// Verify the general driver is the llama.cpp endpoint, and that it can
	// actually call tools: --jinja is what makes llama-server render the
	// model's chat template (tool list included). Without it the daemon
	// boots a driver that answers in prose and never calls a tool.
	ggufProvider, ok := cfg.Providers["local-gguf"]
	if !ok {
		t.Fatal("local-gguf provider not found")
	}
	if ggufProvider.Options.BaseURL != "http://127.0.0.1:8080/v1" {
		t.Errorf("local-gguf baseURL = %q, want http://127.0.0.1:8080/v1", ggufProvider.Options.BaseURL)
	}
	if ggufProvider.Lifecycle == nil {
		t.Fatal("local-gguf lifecycle not configured")
	}
	if !slices.Contains(ggufProvider.Lifecycle.SpawnCommand, "--jinja") {
		t.Errorf("local-gguf spawn_command = %v, want --jinja (without it llama-server cannot emit tool calls)",
			ggufProvider.Lifecycle.SpawnCommand)
	}
	ggufModel, ok := ggufProvider.Models["lfm-8b-gguf"]
	if !ok {
		t.Fatal("lfm-8b-gguf model not found in local-gguf provider")
	}
	if !slices.Contains(ggufModel.Capabilities, "tool_use") {
		t.Errorf("lfm-8b-gguf capabilities = %v, want tool_use", ggufModel.Capabilities)
	}

	// Port guard. Two loopback endpoints sharing one port makes the daemon
	// adopt a foreign listener as its own runtime (2026-09-12: the MLX
	// endpoint claimed :8082, the prompt-router sidecar's port, so the
	// driver answered with the router's canned JSON). 8082 is reserved for
	// that sidecar and is not a provider port.
	seenPort := map[string]string{}
	for name, p := range cfg.Providers {
		port := loopbackPort(p.Options.BaseURL)
		if port == "" {
			continue
		}
		if port == "8082" {
			t.Errorf("provider %s uses :8082, reserved for the prompt-router sidecar", name)
		}
		if prev, dup := seenPort[port]; dup {
			t.Errorf("providers %s and %s both use loopback port %s", prev, name, port)
		}
		seenPort[port] = name
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

// loopbackPort returns the port of a loopback baseURL, or "" when the URL is
// remote or has no port. Remote providers may share :443 without conflict, so
// only loopback endpoints are compared by the port guard in TestConfigLoads.
func loopbackPort(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	if u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" {
		return ""
	}
	return u.Port()
}
