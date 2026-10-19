package daemon

import (
	"log/slog"
	"os"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/hermes"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
)

// RegisterHermesTools registers the hermes builtin tool when [hermes]
// interop is enabled AND the API key resolves from the env var named by
// hermes.api_key_env. No-op (nil-safe) for a nil registry, a disabled
// config, or an absent key — a missing key means the gateway cannot be
// authenticated against, so the tool family stays absent (leaf 02 task 4;
// the absent-key case is a valid configuration state, logged at Debug).
//
// The API key is read into the client here and NEVER logged: only the base
// URL travels in errors.
func RegisterHermesTools(reg *tools.Registry, cfg config.HermesConfig) {
	if reg == nil || !cfg.Enabled {
		return
	}
	apiKey := ""
	if cfg.APIKeyEnv != "" {
		apiKey = os.Getenv(cfg.APIKeyEnv)
	}
	if apiKey == "" {
		slog.Debug("hermes interop enabled but api key env unset; hermes tool not registered",
			"api_key_env", cfg.APIKeyEnv)
		return
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	client := hermes.NewClient(cfg.BaseURL, apiKey, timeout)
	reg.Register(builtin.NewHermesTool(client, cfg.AutoApprove))
	slog.Info("hermes tool registered", "base_url", cfg.BaseURL, "auto_approve", cfg.AutoApprove)
}
