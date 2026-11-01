package main

import (
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/toolprov"
)

// Tool provider report (04-config.md T3): one line per registered
// toolprov factory with its enabled state from [tool_providers] in the
// config. Report-only: nothing is installed, prompted, or mutated, and
// the checks never flip the doctor exit status (ok is always true).
//
// State values:
//   - "enabled"  - registered factory, config defaults it on
//   - "enabled (config)" - explicitly enabled=true in config
//   - "disabled" - explicit enabled=false in config
//   - "unknown"  - name present in config but no registered factory
//     (RegisterAll would reject it with the known-names list)
func providerChecksDoctor(cfg *config.Config) []doctorCheck {
	checks := make([]doctorCheck, 0, 8)

	names := toolprov.Names()
	enabled := map[string]bool{}
	explicit := map[string]bool{}
	if cfg != nil {
		for _, name := range cfg.ToolProviders.EnabledNames(names) {
			enabled[name] = true
		}
		if cfg.ToolProviders.Providers != nil {
			for name, entry := range cfg.ToolProviders.Providers {
				if entry.Enabled != nil {
					explicit[name] = *entry.Enabled
				}
			}
		}
	}

	registered := map[string]bool{}
	for _, name := range names {
		registered[name] = true
	}

	for _, name := range names {
		state := "enabled"
		if !enabled[name] {
			state = "disabled"
		} else if explicit[name] {
			state = "enabled (config)"
		}
		checks = append(checks, doctorCheck{
			name:   "tool-provider",
			ok:     true,
			detail: fmt.Sprintf("%s: %s", name, state),
		})
	}

	// Config names that are not registered factories: report them so a
	// typo does not silently do nothing (RegisterAll errors on them at
	// boot; doctor surfaces them read-only, exit status unchanged).
	if cfg != nil {
		for name, entry := range cfg.ToolProviders.Providers {
			if registered[name] {
				continue
			}
			state := "unknown (no registered factory)"
			if entry.Enabled != nil && !*entry.Enabled {
				state = "unknown (no registered factory; disabled in config)"
			}
			checks = append(checks, doctorCheck{
				name:   "tool-provider",
				ok:     true,
				warn:   true,
				detail: fmt.Sprintf("%s: %s", name, state),
			})
		}
	}

	if len(checks) == 0 {
		checks = append(checks, doctorCheck{
			name:   "tool-provider",
			ok:     true,
			detail: "no providers registered (builtin tools unaffected)",
		})
	}
	return checks
}

// sortProviderCheckNames is a test seam helper: returns the provider
// names in the order providerChecksDoctor would report them (sorted,
// since toolprov.Names() is sorted and the config map walk is sorted
// separately by the caller when determinism matters).
func sortProviderCheckNames(names []string) []string {
	out := make([]string, len(names))
	copy(out, names)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && strings.Compare(out[j], out[j-1]) < 0; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
