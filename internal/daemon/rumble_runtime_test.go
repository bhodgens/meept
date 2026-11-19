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
