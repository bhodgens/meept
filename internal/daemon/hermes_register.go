package daemon

import (
	"log/slog"
	"os"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/hermes"
	"github.com/caimlas/meept/internal/toolprov"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
)

// RegisterHermesTools registers the hermes builtin tool when [hermes]
// interop is enabled AND the API key resolves from the env var named by
// hermes.api_key_env. No-op (nil-safe) for a nil registry, a disabled
// config, or an absent key - a missing key means the gateway cannot be
// authenticated against, so the tool family stays absent (leaf 02 task 4;
// the absent-key case is a valid configuration state, logged at Debug).
//
// Since the tool-provider seam (docs/plans/20261031-tool-provider-seam/
// 03-migrate.md T2) the tool arrives through the "hermes" provider:
// this function applies the SAME enabled+key gating as before, binds
// the provider via builtin.SetHermesProvider, and walks
// toolprov.RegisterAll. An unbound factory fails closed (nil Provider
// => zero tools), so the disabled/absent-key outcomes are unchanged.
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
	builtin.SetHermesProvider(client, cfg.AutoApprove, nil)
	// [tool_providers] config filter (04-config.md T2). cfg here is
	// config.HermesConfig; the helper needs the top-level Config, so the
	// filter is applied by the caller through the package-level hook the
	// daemon sets at startup (nil hook = no config filtering, tests keep
	// working).
	if providerConfigFilter != nil && !providerConfigFilter("hermes") {
		slog.Debug("hermes provider disabled by [tool_providers]; skipping registration")
		return
	}
	if _, err := toolprov.RegisterAll(reg, []string{"hermes"}); err != nil {
		slog.Error("hermes tool registration failed", "error", err)
		return
	}
	slog.Info("hermes tool registered", "base_url", cfg.BaseURL, "auto_approve", cfg.AutoApprove)
}
