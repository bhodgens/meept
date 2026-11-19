package daemon

// F5 gate: the Rumble vLLM sidecar config pins an explicit low GPU memory
// fraction (0.20-0.25), never vLLM's unsafe 0.92 default, so it coexists
// with qwen3-tts on the RTX 3060. RED until leaf 02 lands the config.

import "testing"

func TestRumbleRuntimeConfigCap(t *testing.T) {
	cfg := DefaultRumbleRuntimeConfig()
	if cfg.GPUMemoryUtilization <= 0 {
		t.Fatal("Rumble runtime config must set gpu_memory_utilization explicitly")
	}
	if cfg.GPUMemoryUtilization > 0.25 {
		t.Fatalf("gpu_memory_utilization = %.2f, want <= 0.25 (never 0.92)", cfg.GPUMemoryUtilization)
	}
	if cfg.MaxModelLen <= 0 {
		t.Fatal("Rumble runtime config must set max_model_len explicitly")
	}
	if cfg.MaxModelLen > 4096 {
		t.Fatalf("max_model_len = %d, want <= 4096", cfg.MaxModelLen)
	}
	if cfg.HealthPath != "/health" {
		t.Fatalf("health path = %q, want /health", cfg.HealthPath)
	}
	if !cfg.AutoStopOnExit {
		t.Fatal("Rumble sidecar must auto-stop on daemon exit")
	}
}

// TestRumbleSpawnArgsClamp pins bughunt 2026-11-19 M18/H1 at the render
// seam: SpawnArgs must NEVER emit an out-of-range --gpu-memory-utilization,
// even from a hand-built config that skipped the load-boundary normalizer.
// The safe range is the documented 0.20-0.25 coexistence window.
func TestRumbleSpawnArgsClamp(t *testing.T) {
	gpuArg := func(args []string) string {
		for i, a := range args {
			if a == "--gpu-memory-utilization" && i+1 < len(args) {
				return args[i+1]
			}
		}
		t.Fatalf("spawn args missing --gpu-memory-utilization: %v", args)
		return ""
	}
	cases := []struct {
		name string
		in   float64
		want string
	}{
		{"vllm_unsafe_default", 0.92, "0.25"},
		{"above_range", 0.50, "0.25"},
		{"zero", 0, "0.20"},
		{"negative", -0.3, "0.20"},
		{"in_range_low", 0.20, "0.20"},
		{"in_range_mid", 0.22, "0.22"},
		{"in_range_max", 0.25, "0.25"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := RumbleRuntimeConfig{GPUMemoryUtilization: tc.in, MaxModelLen: 4096}
			if got := gpuArg(cfg.SpawnArgs()); got != tc.want {
				t.Fatalf("gpu fraction %v rendered %q, want %q", tc.in, got, tc.want)
			}
		})
	}
	// The frozen default config renders the frozen safe fraction.
	if got := gpuArg(DefaultRumbleRuntimeConfig().SpawnArgs()); got != "0.20" {
		t.Fatalf("default spawn args gpu fraction = %q, want 0.20", got)
	}
}
