package daemon

import (
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
)

// applyCodemodeToolsFromConfig registers the `codemode` builtin tool on the
// shared tool registry when [agent.tools].codemode_enabled is on
// (token-efficiency branch 02 leaf 02). It mirrors the
// applyDeterministicToolsFromConfig deterministic-wiring pattern: a
// config-gated, call-site registration that is a no-op (tool ABSENT) when
// the gate is off, so the default install behaves byte-identically to
// pre-codemode. Registration happens on c.ToolRegistry (the shared parent
// registry) so the codemode tool is present in the registry every agent's
// filtered view (agent filterTools) derives from; the agent-side grant
// (unioning codemode into a scoped agent's allow-list) is driven by the
// same flag via agent.RegistryConfig.CodemodeEnabled, wired at the
// NewAgentRegistry call site.
//
// Idempotent: the registry's Register replaces by name, and a second call
// (config reload) just re-registers the same tool — harmless.
func applyCodemodeToolsFromConfig(registry *tools.Registry, cfg config.AgentToolsConfig) {
	if !cfg.CodemodeEnabled || registry == nil {
		return
	}
	registry.Register(builtin.NewCodemodeTool())
}
