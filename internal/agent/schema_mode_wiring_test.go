package agent

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// schemaWiringTool is a minimal non-core fixture tool for schema-mode wiring
// tests (same Tool-interface pattern as mockGuardTool in guards_test.go).
type schemaWiringTool struct {
	name string
}

func (t *schemaWiringTool) Name() string        { return t.name }
func (t *schemaWiringTool) Description() string { return "wiring test tool" }
func (t *schemaWiringTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"q": {Type: "string", Description: "the query"},
		},
		Required: []string{"q"},
	}
}
func (t *schemaWiringTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	return &tools.ToolResult{Success: true, Result: "ok"}, nil
}
func (t *schemaWiringTool) IsReadOnly(map[string]any) bool        { return true }
func (t *schemaWiringTool) IsConcurrencySafe(map[string]any) bool { return true }

// schemaWiringResolver builds a one-provider resolver with no schema_mode
// overrides, so tests exercise the global/default resolution path.
func schemaWiringResolver() *llm.Resolver {
	cfg := &llm.ProvidersConfig{
		Model: "prov/alpha",
		Providers: map[string]llm.ProviderConfig{
			"prov": {
				API:     "openai",
				Options: llm.ProviderOptionsConfig{BaseURL: "http://localhost"},
				Models: map[string]llm.ModelDef{
					"alpha": {Name: "alpha-model", Capabilities: []string{llm.CapCompletion}},
				},
			},
		},
	}
	return llm.NewResolver(cfg, nil)
}

// findWiringDef returns the definition for name among defs.
func findWiringDef(t *testing.T, defs []llm.ToolDefinition, name string) llm.ToolDefinition {
	t.Helper()
	for _, d := range defs {
		if d.Function.Name == name {
			return d
		}
	}
	t.Fatalf("tool %q not found among %d definitions", name, len(defs))
	return llm.ToolDefinition{}
}

// TestSchemaModeWiring_IndexedDefaultOn verifies that a loop constructed with
// a plain registry gets indexed schema mode by default (leaf verdict: indexed
// is default-on): a non-core fixture tool is stubbed to an empty schema with
// a tool_view expansion instruction, and the payload stays well-formed.
func TestSchemaModeWiring_IndexedDefaultOn(t *testing.T) {
	registry := tools.NewRegistry(nil)
	registry.Register(&schemaWiringTool{name: "fixture_search"})

	loop := NewAgentLoop("test-session", "/tmp",
		WithLLMClient(llm.NewClient(&llm.ModelConfig{ProviderID: "prov", ModelID: "alpha"})),
		WithResolver(schemaWiringResolver()),
		WithToolRegistry(registry),
	)
	if loop == nil {
		t.Fatal("NewAgentLoop returned nil")
	}

	def := findWiringDef(t, registry.GetDefinitions(), "fixture_search")
	if len(def.Function.Parameters.Properties) != 0 {
		t.Fatalf("indexed mode must stub non-core tool schema; got %d properties",
			len(def.Function.Parameters.Properties))
	}
	// The stub description must point the model at the expansion tool.
	if !containsWiring(def.Function.Description, "tool_view{fixture_search}") {
		t.Fatalf("stubbed description %q must contain tool_view expansion instruction",
			def.Function.Description)
	}
	// Name must survive stubbing (the model needs it to call the tool).
	if def.Function.Name != "fixture_search" {
		t.Fatalf("stubbed definition name = %q, want fixture_search", def.Function.Name)
	}
}

// TestSchemaModeWiring_ConfigOverridesDefault verifies that an explicit
// [agent.tools] schema_mode="full" restores legacy full-schema payloads, and
// that a custom always_full list is honored.
func TestSchemaModeWiring_ConfigOverridesDefault(t *testing.T) {
	registry := tools.NewRegistry(nil)
	registry.Register(&schemaWiringTool{name: "fixture_search"})

	loop := NewAgentLoop("test-session", "/tmp",
		WithLLMClient(llm.NewClient(&llm.ModelConfig{ProviderID: "prov", ModelID: "alpha"})),
		WithResolver(schemaWiringResolver()),
		WithToolRegistry(registry),
	)
	loop.SetSchemaModeConfig(config.AgentToolsConfig{
		SchemaMode: "full",
		AlwaysFull: []string{"fixture_search"},
	})

	def := findWiringDef(t, registry.GetDefinitions(), "fixture_search")
	if len(def.Function.Parameters.Properties) != 1 {
		t.Fatalf("full mode must ship complete schemas; got %d properties",
			len(def.Function.Parameters.Properties))
	}
	if def.Function.Parameters.Required == nil || len(def.Function.Parameters.Required) != 1 {
		t.Fatalf("full mode must preserve required list, got %v", def.Function.Parameters.Required)
	}
}

// TestSchemaModeWiring_SetConfigBeforeRegistry verifies the config set on a
// loop before the registry is attached is applied once the registry arrives.
func TestSchemaModeWiring_SetConfigBeforeRegistry(t *testing.T) {
	loop := NewAgentLoop("test-session", "/tmp")
	loop.SetSchemaModeConfig(config.AgentToolsConfig{SchemaMode: "full"})

	registry := tools.NewRegistry(nil)
	registry.Register(&schemaWiringTool{name: "fixture_search"})
	loop.SetSchemaModeConfig(config.AgentToolsConfig{SchemaMode: "full"})
	// Attach through the same option path the daemon uses.
	WithToolRegistry(registry)(loop)

	def := findWiringDef(t, registry.GetDefinitions(), "fixture_search")
	if len(def.Function.Parameters.Properties) != 1 {
		t.Fatalf("deferred full-mode config must apply on registry attach; got %d properties",
			len(def.Function.Parameters.Properties))
	}
}

// TestFilteredToolRegistry_InheritsStubsViaParentDelegation proves that
// FilteredToolRegistry does not store schema mode of its own: wrapping an
// indexed-mode *tools.Registry yields stubbed defs for non-alwaysFull tools
// because GetDefinitions() delegates to parent.GetDefinitions() then
// name-filters. There is no SetSchemaMode on the wrapper.
func TestFilteredToolRegistry_InheritsStubsViaParentDelegation(t *testing.T) {
	registry := tools.NewRegistry(nil)
	registry.Register(&schemaWiringTool{name: "shell"})
	registry.Register(&schemaWiringTool{name: "rare_tool"})
	registry.Register(&schemaWiringTool{name: "hidden_tool"})

	registry.SetSchemaMode(tools.SchemaModeIndexed, []string{"shell"})

	filtered := NewFilteredToolRegistry(registry, []string{"shell", "rare_tool"})
	defs := filtered.GetDefinitions()

	shell := findWiringDef(t, defs, "shell")
	if len(shell.Function.Parameters.Properties) == 0 {
		t.Fatal("alwaysFull tool \"shell\" must keep its parameter properties through the filtered wrapper")
	}

	rare := findWiringDef(t, defs, "rare_tool")
	if len(rare.Function.Parameters.Properties) != 0 {
		t.Fatalf("non-alwaysFull tool must be stubbed via parent GetDefinitions; got %d properties",
			len(rare.Function.Parameters.Properties))
	}
	if !contains(rare.Function.Description, " use tool_view{rare_tool}.") {
		t.Fatalf("stubbed description %q must contain tool_view expansion instruction",
			rare.Function.Description)
	}
	if rare.Function.Name != "rare_tool" {
		t.Fatalf("stubbed definition name = %q, want rare_tool", rare.Function.Name)
	}

	for _, d := range defs {
		if d.Function.Name == "hidden_tool" {
			t.Fatal("tool not in the allowed list must not appear in filtered definitions")
		}
	}
}

// containsWiring is a tiny substring helper avoiding strings import churn.
func containsWiring(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOfWiring(s, sub) >= 0)
}

func indexOfWiring(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
