package daemon

import (
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
)

// deterministicToolsWiringState records what applyDeterministicToolsFromConfig
// did on its most recent invocation, so tests can assert the wiring without
// inspecting registry internals.
type deterministicToolsWiringState struct {
	wrapped []string
}

var lastDeterministicWiring deterministicToolsWiringState

// applyDeterministicToolsFromConfig wraps the live-web builtin tools
// (web_fetch/web_search) in builtin.CachedFetchTool when
// [agent.tools].deterministic_tools (or the MEEPT_DETERMINISTIC_TOOLS env
// override) is on. Called after registerBuiltinTools in NewComponents; the
// registry's Register replaces same-name tools by design (registry.go:69
// warns and overwrites), so wrapping is an in-place upgrade of the already
// registered tool. When the gate is off nothing is wrapped: behavior is
// byte-identical to the unwrapped tools.
func applyDeterministicToolsFromConfig(registry *tools.Registry, cfg config.AgentToolsConfig) {
	enabled := cfg.DeterministicTools || builtin.DeterministicToolsEnvEnabled()
	if !enabled || registry == nil {
		lastDeterministicWiring = deterministicToolsWiringState{}
		return
	}
	var wrapped []string
	for _, name := range []string{"web_fetch", "websearch", "web_search"} {
		if !builtin.DeterministicFetchNames(name) {
			continue
		}
		inner := registry.Get(name)
		if inner == nil {
			continue
		}
		// Idempotence: Register replaces by name, but don't double-wrap if
		// apply runs twice (e.g. config reload paths).
		if _, already := inner.(*builtin.CachedFetchTool); already {
			wrapped = append(wrapped, name)
			continue
		}
		registry.Register(builtin.NewCachedFetchTool(inner, cfg.DeterministicTools))
		wrapped = append(wrapped, name)
	}
	lastDeterministicWiring = deterministicToolsWiringState{wrapped: wrapped}
}
