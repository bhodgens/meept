package daemon

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/tools"
)

// TestDefaultAlwaysFullToolsAllRegistered walks config.DefaultAlwaysFullTools()
// against a REAL daemon tool registry and fails on any entry that names no
// registered tool.
//
// This is the pin for F29 (bughunt 2026-09-12 wave): the list carried
// "websearch" while the search tool registers as "web_search", and registry
// normalization is lower+trim ONLY — so the entry silently never matched and
// web_search shipped with a one-line description and an empty parameter schema
// under indexed mode (the default). The same defect class hit json_extract
// before it. A name in this list is a promise that the tool keeps its full
// schema; a name that matches nothing is a silent stub.
func TestDefaultAlwaysFullToolsAllRegistered(t *testing.T) {
	cfg, _ := skillToolsTestConfig(t)
	// Two entries register only under non-default config; enable both so the
	// always-full promise is tested against a registry that actually carries
	// them:
	//   transcript_fetch — [transcript] enabled
	//   platform_status  — [multiagent] enabled (registerPlatformTools,
	//                      components.go, inside `if cfg.MultiAgent.Enabled`)
	cfg.Transcript = config.TranscriptConfig{Enabled: true}
	cfg.MultiAgent.Enabled = true
	comps := newTranscriptWiringComponents(t, cfg)
	if comps.ToolRegistry == nil {
		t.Fatal("ToolRegistry nil")
	}
	registry := comps.ToolRegistry

	alwaysFull := config.DefaultAlwaysFullTools()
	if len(alwaysFull) == 0 {
		t.Fatal("DefaultAlwaysFullTools() is empty")
	}

	// 1. The spelling pin: the legacy "websearch" alias must not come back.
	for _, name := range alwaysFull {
		if name == "websearch" {
			t.Errorf(`DefaultAlwaysFullTools lists "websearch", but the tool registers as "web_search" (internal/tools/builtin/tool_web_search.go); the entry would never match and web_search would ship stubbed`)
		}
	}

	// 2. Every entry must name a registered tool.
	for _, name := range alwaysFull {
		if registry.Get(name) == nil {
			t.Errorf("DefaultAlwaysFullTools lists %q, which is not a registered tool name (known names: %s)",
				name, strings.Join(registry.Names(), ", "))
		}
	}

	// 3. Functional consequence: under the DEFAULT indexed schema mode every
	//    entry that has parameters must still ship them — a stubbed entry is
	//    exactly what the list exists to prevent.
	registry.SetSchemaMode(tools.SchemaModeIndexed, alwaysFull)
	defs := make(map[string]struct{ props int }, len(alwaysFull))
	for _, def := range registry.ToLLMDefinitions() {
		defs[def.Function.Name] = struct{ props int }{props: len(def.Function.Parameters.Properties)}
	}
	for _, name := range alwaysFull {
		tool := registry.Get(name)
		if tool == nil {
			continue // already reported above
		}
		def, ok := defs[name]
		if !ok {
			t.Errorf("no LLM definition produced for always-full tool %q", name)
			continue
		}
		if want := len(tool.Parameters().Properties); want > 0 && def.props == 0 {
			t.Errorf("always-full tool %q is STUBBED under indexed schema mode (definition properties = 0, tool has %d): the model must pay a tool_view round-trip", name, want)
		}
	}
}
