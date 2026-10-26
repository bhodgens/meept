// Gate tests for the llama-hang-watchdog tree (docs/plans/llama-hang-watchdog).
// Written BEFORE the implementation exists (leaf 00T): this file must fail to
// compile naming ONLY the new contract symbols (HealthCheckConfig.Canary /
// CanaryConfig via JSON5, RuntimeConfig.CanaryEnabled / CanaryInterval /
// CanaryTimeout / CanaryThreshold / CanaryPrompt). Leaf 01 turns it GREEN.
package llm

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// canaryGateModelFile creates a fake GGUF file ValidateAndNormalize can stat.
// (createTempModelFile lives in package llm_test and is not reachable from
// this internal test file.)
func canaryGateModelFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "canary-model.gguf")
	if err := os.WriteFile(p, []byte("fake model"), 0o644); err != nil {
		t.Fatalf("write temp model file: %v", err)
	}
	return p
}

// loadCanaryGateConfig drives the real JSON5 loader (the path models.json5
// takes through LoadProvidersConfig) and normalizes the lifecycle block,
// mirroring TestAutoStopOnExitJSON5Decode in runtime_config_test.go.
// canaryBlock is inserted verbatim into health_check (e.g. `"canary": {},`);
// pass "" for an ABSENT canary key.
//
// Loopback-signaling seam: ValidateAndNormalize only sees the lifecycle
// block, so the fixture declares the endpoint address in BOTH options.baseURL
// and health_check.endpoint — resolution must read the endpoint's loopback
// status from the lifecycle-visible address. If leaf 01 instead resolves
// CanaryEnabled from RegisterConfig's baseURL (RuntimeManager seam), the
// orchestrator's delta protocol adjusts this fixture; flagged in the 00T
// report as a deviation candidate.
func loadCanaryGateConfig(t *testing.T, baseURL, healthEndpoint, canaryBlock string) *RuntimeConfig {
	t.Helper()
	modelPath := canaryGateModelFile(t)
	pidPath := filepath.Join(t.TempDir(), "pid", "llama.pid")
	content := fmt.Sprintf(`{
  "providers": {
    "local": {
      "api": "openai",
      "options": { "baseURL": %q },
      "lifecycle": {
        "runtime": "llama-cpp",
        "model_path": %q,
        "pid_file": %q,
        "health_check": {
          "endpoint": %q,
          "interval_seconds": 10,
          "timeout_seconds": 5,
          "unhealthy_threshold": 3,
          %s
        },
      },
    },
  },
}
`, baseURL, modelPath, pidPath, healthEndpoint, canaryBlock)
	path := filepath.Join(t.TempDir(), "models.json5")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write models.json5: %v", err)
	}
	cfg, err := LoadProvidersConfig(path)
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
	rc, err := ValidateAndNormalize(*provider.Lifecycle)
	if err != nil {
		t.Fatalf("ValidateAndNormalize: %v", err)
	}
	return rc
}

// TestCanaryConfigDefaults — gate matrix F1: an empty (or absent) canary
// block on a loopback llama-cpp lifecycle endpoint resolves to the Contract-1
// defaults: enabled=true, interval=60s, timeout=15s, threshold=3,
// prompt="ping". Zero/absent values must never survive as zero.
func TestCanaryConfigDefaults(t *testing.T) {
	cases := []struct {
		name        string
		canaryBlock string
	}{
		{"empty canary object", `"canary": {},`},
		{"absent canary key", ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rc := loadCanaryGateConfig(t,
				"http://127.0.0.1:8080/v1",
				"http://127.0.0.1:8080/health",
				tc.canaryBlock)

			if !rc.CanaryEnabled {
				t.Error("CanaryEnabled = false, want true (loopback llama-cpp lifecycle default)")
			}
			if rc.CanaryInterval != 60*time.Second {
				t.Errorf("CanaryInterval = %v, want %v", rc.CanaryInterval, 60*time.Second)
			}
			if rc.CanaryTimeout != 15*time.Second {
				t.Errorf("CanaryTimeout = %v, want %v", rc.CanaryTimeout, 15*time.Second)
			}
			if rc.CanaryThreshold != 3 {
				t.Errorf("CanaryThreshold = %d, want 3", rc.CanaryThreshold)
			}
			if rc.CanaryPrompt != "ping" {
				t.Errorf("CanaryPrompt = %q, want %q", rc.CanaryPrompt, "ping")
			}
		})
	}
}

// TestCanaryConfigExplicit — gate matrix F1: explicit canary values survive
// resolution verbatim (interval 30s, timeout 5s, threshold 2, prompt "hi"),
// and an explicit enabled=false on a LOOPBACK endpoint stays false (operator
// opt-out wins over the loopback default).
func TestCanaryConfigExplicit(t *testing.T) {
	rc := loadCanaryGateConfig(t,
		"http://127.0.0.1:8080/v1",
		"http://127.0.0.1:8080/health",
		`"canary": {
		   "enabled": false,
		   "interval_seconds": 30,
		   "timeout_seconds": 5,
		   "threshold": 2,
		   "prompt": "hi",
		 },`)

	if rc.CanaryEnabled {
		t.Error("CanaryEnabled = true, want false (explicit opt-out must win)")
	}
	if rc.CanaryInterval != 30*time.Second {
		t.Errorf("CanaryInterval = %v, want %v", rc.CanaryInterval, 30*time.Second)
	}
	if rc.CanaryTimeout != 5*time.Second {
		t.Errorf("CanaryTimeout = %v, want %v", rc.CanaryTimeout, 5*time.Second)
	}
	if rc.CanaryThreshold != 2 {
		t.Errorf("CanaryThreshold = %d, want 2", rc.CanaryThreshold)
	}
	if rc.CanaryPrompt != "hi" {
		t.Errorf("CanaryPrompt = %q, want %q", rc.CanaryPrompt, "hi")
	}
}

// TestCanaryDisabledNonLoopback — gate matrix F1: a non-loopback endpoint
// (cloud/remote) with the canary key ABSENT resolves CanaryEnabled=false
// (default-off unless loopback+lifecycle), while an explicit "enabled": true
// keeps it on (operator override wins in both directions). Non-loopback
// runtimes must otherwise be untouched by the canary feature.
func TestCanaryDisabledNonLoopback(t *testing.T) {
	cases := []struct {
		name        string
		canaryBlock string
		want        bool
	}{
		{"absent canary defaults off for non-loopback", ``, false},
		{"explicit enabled true overrides", `"canary": { "enabled": true },`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rc := loadCanaryGateConfig(t,
				"https://api.example.com/v1",
				"https://api.example.com/health",
				tc.canaryBlock)
			if rc.CanaryEnabled != tc.want {
				t.Errorf("CanaryEnabled = %v, want %v", rc.CanaryEnabled, tc.want)
			}
		})
	}
}
