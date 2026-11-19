package daemon

import (
	"fmt"
	"strconv"
)

// RumbleRuntimeConfig pins the supervised vLLM Rumble STT sidecar: an explicitly
// low GPU memory fraction (never vLLM's unsafe 0.92 default) so the sidecar can
// coexist with qwen3-tts on the same RTX 3060 (~5.2 GB free). It is opt-in via
// voice.stt_provider = "rumble"; the whisper default is unchanged.
type RumbleRuntimeConfig struct {
	GPUMemoryUtilization float64  `json:"gpu_memory_utilization"`
	MaxModelLen          int      `json:"max_model_len"`
	HealthPath           string   `json:"health_path"`
	AutoStopOnExit       bool     `json:"auto_stop_on_exit"`
	EndpointKey          string   `json:"endpoint_key"`
	SpawnCommand         []string `json:"spawn_command"`
}

const rumbleModelID = "Reson8/Rumble-2-Flash"
const rumblePort = "8091"

// DefaultRumbleRuntimeConfig returns the frozen Rumble vLLM sidecar defaults.
// gpu_memory_utilization 0.20 and max_model_len 4096 are the safe coexistence
// values for an RTX 3060 that also runs qwen3-tts; never raise the fraction
// toward vLLM's unsafe 0.92 default. SpawnCommand is derived from SpawnArgs
// so the GPU cap is emitted from ONE source of truth.
func DefaultRumbleRuntimeConfig() RumbleRuntimeConfig {
	cfg := RumbleRuntimeConfig{
		GPUMemoryUtilization: 0.20,
		MaxModelLen:          4096,
		HealthPath:           "/health",
		AutoStopOnExit:       true,
		EndpointKey:          "rumble:127.0.0.1:" + rumblePort,
	}
	cfg.SpawnCommand = cfg.SpawnArgs()
	return cfg
}

// SpawnArgs renders the vLLM serve command from this config. It is the single
// source of truth for the explicit GPU cap and context limit: the daemon and
// operator both use these args so --gpu-memory-utilization can never silently
// drift back to vLLM's unsafe 0.92 default. The Rumble vLLM process is spawned
// and supervised by the daemon runtime lifecycle (RuntimeProcess.Start port
// probe / supervisor / ownership-scoped Stop / startup orphan sweep) once the
// operator selects voice.stt_provider = "rumble"; this function never spawns.
func (c RumbleRuntimeConfig) SpawnArgs() []string {
	gpu := strconv.FormatFloat(c.GPUMemoryUtilization, 'f', 2, 64)
	maxLen := strconv.Itoa(c.MaxModelLen)
	return []string{
		"vllm", "serve", rumbleModelID,
		"--max-model-len", maxLen,
		"--gpu-memory-utilization", gpu,
		"--port", rumblePort,
	}
}

// BaseURL returns the loopback endpoint the Rumble STT adapter dials. It is
// derived from the same port as the spawn command so the adapter and sidecar
// stay in sync.
func (c RumbleRuntimeConfig) BaseURL() string {
	return fmt.Sprintf("http://127.0.0.1:%s", rumblePort)
}
