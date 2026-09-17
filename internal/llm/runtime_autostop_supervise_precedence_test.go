package llm_test

// Pin for audit finding F24: the supervision default must not defeat an
// explicit auto_stop_on_exit:false. A supervised runtime is terminated when
// the daemon exits NORMALLY too (the parent-death pipe closes on any exit),
// so default-on supervision would kill a runtime the operator explicitly opted
// to preserve. The precedence is resolved in ValidateAndNormalize; the four
// combinations below are pinned end to end through the real JSON5 loader.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// TestAutoStopSupervisePrecedence pins the table:
//
//	supervise      auto_stop_on_exit   Supervised() after normalization
//	absent         absent              true   (both defaults)
//	absent         explicit false      false  (default yields to preserve opt-out — the F24 fix)
//	explicit true  explicit false      true   (operator override keeps supervision)
//	explicit false absent              false  (supervise opt-out unchanged)
func TestAutoStopSupervisePrecedence(t *testing.T) {
	modelPath := writeSuperviseModelFile(t)
	pidPath := filepath.Join(t.TempDir(), "pid", "llama.pid")

	cases := []struct {
		name          string
		superviseKey  string
		autoStopKey   string
		wantSupervise bool
	}{
		{"both absent: supervised", "", "", true},
		{"supervise absent, auto_stop_on_exit false: DIRECT (F24)", "", `"auto_stop_on_exit": false,`, false},
		{"supervise true, auto_stop_on_exit false: supervised (operator override)", `"supervise": true,`, `"auto_stop_on_exit": false,`, true},
		{"supervise false: DIRECT", `"supervise": false,`, "", false},
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
        %s
      },
    },
  },
}
`, modelPath, pidPath, modelPath, tc.superviseKey, tc.autoStopKey)
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatalf("write models.json5: %v", err)
			}

			cfg, err := llm.LoadProvidersConfig(path)
			if err != nil {
				t.Fatalf("LoadProvidersConfig: %v", err)
			}
			provider, ok := cfg.Providers["local"]
			if !ok || provider.Lifecycle == nil {
				t.Fatal("provider 'local' with a lifecycle block missing after decode")
			}

			runtimeCfg, err := llm.ValidateAndNormalize(*provider.Lifecycle)
			if err != nil {
				t.Fatalf("ValidateAndNormalize: %v", err)
			}

			wantAutoStop := true
			if tc.autoStopKey != "" {
				wantAutoStop = false
			}
			if runtimeCfg.AutoStop != wantAutoStop {
				t.Errorf("AutoStop = %v, want %v", runtimeCfg.AutoStop, wantAutoStop)
			}
			if got := runtimeCfg.Supervised(); got != tc.wantSupervise {
				t.Errorf("Supervised() = %v, want %v (precedence table violated)", got, tc.wantSupervise)
			}
		})
	}
}
