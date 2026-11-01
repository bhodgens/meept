package daemon

import (
	"log/slog"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/toolprov"
)

// providerConfigFilter is the package-level hook that lets wiring sites
// without a full *config.Config handle (hermes_register.go, acp_register.go)
// consult the [tool_providers] block. Set once at daemon startup from the
// loaded config; nil (tests, non-daemon binaries) = no filtering.
var providerConfigFilter func(name string) bool

// SetProviderConfigFilter installs the [tool_providers] filter hook. The
// predicate returns true when the named provider is enabled. Call once
// from NewComponents after the config is in scope; nil clears it.
func SetProviderConfigFilter(enabled func(name string) bool) {
	providerConfigFilter = enabled
}

// enabledToolProviderNames filters `groupNames` (the per-group list a
// wiring site passes to toolprov.RegisterAll) against [tool_providers]
// in the config. Absent block/entries default to enabled (default-true:
// existing installs keep working unchanged); an explicit enabled=false
// skips that provider with a debug log naming it. Unknown names pass
// through - RegisterAll rejects them with the known-names list.
//
// Each migrated group's wiring site calls this immediately before its
// RegisterAll and uses the returned slice as the name list. When the
// result is empty the site should skip RegisterAll entirely (an empty
// list would succeed while registering nothing, which is correct but
// wasteful; skipping also avoids the zero-tools warn).
func enabledToolProviderNames(cfg *config.Config, groupNames []string, logger *slog.Logger) []string {
	if cfg == nil {
		return groupNames
	}
	// toolprov.Names() is the full registered-factory universe; passing
	// it as `all` maps the whole config block once. The per-group filter
	// is then a set intersection, so a wiring site only ever touches the
	// names it owns.
	enabled := map[string]bool{}
	for _, name := range cfg.ToolProviders.EnabledNames(toolprov.Names()) {
		enabled[name] = true
	}
	out := make([]string, 0, len(groupNames))
	for _, name := range groupNames {
		if !enabled[name] {
			logger.Debug("tool provider disabled by [tool_providers] config; skipping registration",
				"provider", name)
			continue
		}
		out = append(out, name)
	}
	return out
}
