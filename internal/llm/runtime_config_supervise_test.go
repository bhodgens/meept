package llm_test

// Tests for the per-endpoint `supervise` escape hatch: absent means supervised,
// only an explicit false opts out, and the resolved decision reaches the
// RuntimeConfig the spawn path reads.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// writeSuperviseModelFile writes a stand-in model file for config validation.
func writeSuperviseModelFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "supervise-model.gguf")
	if err := os.WriteFile(path, []byte("fake model"), 0o644); err != nil {
		t.Fatalf("write model file: %v", err)
	}
	return path
}

// TestRuntimeLifecycleConfig_SuperviseOrDefault pins the absent-means-true
// default mirroring auto_stop_on_exit.
func TestRuntimeLifecycleConfig_SuperviseOrDefault(t *testing.T) {
	cases := []struct {
		name string
		ptr  *bool
		want bool
	}{
		{"absent key", nil, true},
		{"explicit true", new(true), true},
		{"explicit false opts out", new(false), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := llm.RuntimeLifecycleConfig{Supervise: tc.ptr}
			if got := cfg.SuperviseOrDefault(); got != tc.want {
				t.Errorf("SuperviseOrDefault() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRuntimeConfig_Supervised pins the resolved flag: an absent value (every
// literal-constructed config, e.g. the local-models endpoint) is supervised, so
// silence cannot mean "leave a runtime behind after a hard kill".
func TestRuntimeConfig_Supervised(t *testing.T) {
	cases := []struct {
		name string
		ptr  *bool
		want bool
	}{
		{"absent value", nil, true},
		{"explicit true", new(true), true},
		{"explicit false", new(false), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &llm.RuntimeConfig{Supervise: tc.ptr}
			if got := cfg.Supervised(); got != tc.want {
				t.Errorf("Supervised() = %v, want %v", got, tc.want)
			}
		})
	}
	var nilCfg *llm.RuntimeConfig
	if nilCfg.Supervised() {
		t.Error("a nil config manages no runtime and must not report supervised")
	}
}

// TestSuperviseJSON5Decode drives the real JSON5 loader and the normalizer, the
// path models.json5 takes: the key must decode, default to true when absent,
// and reach RuntimeConfig.Supervise.
func TestSuperviseJSON5Decode(t *testing.T) {
	modelPath := writeSuperviseModelFile(t)
	pidPath := filepath.Join(t.TempDir(), "pid", "llama.pid")

	cases := []struct {
		name      string
		keyLine   string
		wantSuper bool
	}{
		{"absent key defaults to supervised", "", true},
		{"explicit true", `"supervise": true,`, true},
		{"explicit false opts out", `"supervise": false,`, false},
		{"explicit null behaves as absent", `"supervise": null,`, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "models.json5")
			content := fmt.Sprintf(`{
  "providers": {
    "local": {
      "api": "openai",
      "options": { "baseURL": "http://127.0.0.1:8082/v1" },
      "lifecycle": {
        "runtime": "llama-cpp",
        "model_path": %q,
        "pid_file": %q,
        "spawn_command": ["llama-server", "-m", %q, "--port", "8082"],
        %s
      },
    },
  },
}
`, modelPath, pidPath, modelPath, tc.keyLine)
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatalf("write models.json5: %v", err)
			}

			cfg, err := llm.LoadProvidersConfig(path)
			if err != nil {
				t.Fatalf("LoadProvidersConfig: %v", err)
			}
			provider, ok := cfg.Providers["local"]
			if !ok {
				t.Fatal("provider 'local' missing after decode")
			}
			if provider.Lifecycle == nil {
				t.Fatal("lifecycle block missing after decode")
			}
			if got := provider.IsSupervised(); got != tc.wantSuper {
				t.Errorf("IsSupervised() = %v, want %v", got, tc.wantSuper)
			}

			runtimeCfg, err := llm.ValidateAndNormalize(*provider.Lifecycle)
			if err != nil {
				t.Fatalf("ValidateAndNormalize: %v", err)
			}
			if got := runtimeCfg.Supervised(); got != tc.wantSuper {
				t.Errorf("normalized Supervised() = %v, want %v", got, tc.wantSuper)
			}
		})
	}
}
