package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// schemaModeTool is a minimal inline Tool implementation for schema-mode
// tests. It satisfies the full Tool interface without pulling in any
// builtin-tool dependencies.
type schemaModeTool struct {
	name        string
	description string
	params      llm.FunctionParameters
}

func (t *schemaModeTool) Name() string        { return t.name }
func (t *schemaModeTool) Description() string { return t.description }
func (t *schemaModeTool) Parameters() llm.FunctionParameters {
	if t.params.Type == "" {
		return llm.FunctionParameters{
			Type:       "object",
			Properties: make(map[string]llm.ParameterProperty),
		}
	}
	return t.params
}
func (t *schemaModeTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	return map[string]any{"ok": true}, nil
}
func (t *schemaModeTool) IsReadOnly(map[string]any) bool        { return true }
func (t *schemaModeTool) IsConcurrencySafe(map[string]any) bool { return true }

// Compile-time assertion that the fake implements Tool.
var _ Tool = (*schemaModeTool)(nil)

// emptyObjectSchema returns the exact stub parameter schema that indexed
// mode must emit: an object with no properties and no required list.
func emptyObjectSchema() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type:       "object",
		Properties: make(map[string]llm.ParameterProperty),
	}
}

// findSchemaDef returns the definition for the named tool, failing the test
// if it is absent.
func findSchemaDef(t *testing.T, defs []llm.ToolDefinition, name string) llm.ToolDefinition {
	t.Helper()
	for _, d := range defs {
		if d.Function.Name == name {
			return d
		}
	}
	t.Fatalf("tool %q not found among %d definitions", name, len(defs))
	return llm.ToolDefinition{}
}

// richObjectSchema builds a realistic multi-property schema so full-mode
// payloads are non-trivially larger than stubs.
func richObjectSchema(props ...string) llm.FunctionParameters {
	p := make(map[string]llm.ParameterProperty, len(props))
	for _, name := range props {
		p[name] = llm.ParameterProperty{Type: "string", Description: "the " + name + " value"}
	}
	return llm.FunctionParameters{
		Type:       "object",
		Properties: p,
		Required:   props[:1],
	}
}

func TestRegistrySchemaMode_StubsNonCoreTools(t *testing.T) {
	r := NewRegistry(nil)
	r.Register(&schemaModeTool{
		name:        "file_read",
		description: "Read a file from disk.",
		params:      richObjectSchema("path", "encoding"),
	})
	r.Register(&schemaModeTool{
		name:        "obscure_widget",
		description: "Frobnicate a widget.",
		params:      richObjectSchema("widget_id", "mode", "force"),
	})

	r.SetSchemaMode(SchemaModeIndexed, []string{"file_read"})
	defs := r.GetDefinitions()

	t.Run("non-core tool is stubbed", func(t *testing.T) {
		got := findSchemaDef(t, defs, "obscure_widget")
		if got.Type != "function" {
			t.Errorf("expected Type %q, got %q", "function", got.Type)
		}
		if got.Function.Parameters.Type != "object" || len(got.Function.Parameters.Properties) != 0 {
			t.Errorf("expected empty object schema, got %+v", got.Function.Parameters)
		}
		if len(got.Function.Parameters.Required) != 0 {
			t.Errorf("expected no required params on stub, got %v", got.Function.Parameters.Required)
		}
		wantDesc := "Frobnicate a widget. use tool_view{obscure_widget}."
		if got.Function.Description != wantDesc {
			t.Errorf("expected description %q, got %q", wantDesc, got.Function.Description)
		}
	})

	t.Run("core (alwaysFull) tool keeps full schema", func(t *testing.T) {
		got := findSchemaDef(t, defs, "file_read")
		if got.Function.Description != "Read a file from disk." {
			t.Errorf("core tool description mutated: %q", got.Function.Description)
		}
		if len(got.Function.Parameters.Properties) != 2 {
			t.Errorf("expected 2 properties preserved, got %d", len(got.Function.Parameters.Properties))
		}
		if len(got.Function.Parameters.Required) != 1 || got.Function.Parameters.Required[0] != "path" {
			t.Errorf("expected required=[path], got %v", got.Function.Parameters.Required)
		}
	})
}

func TestRegistrySchemaMode_ToolViewImplicitlyAlwaysFull(t *testing.T) {
	r := NewRegistry(nil)
	r.Register(&schemaModeTool{
		name:        "tool_view",
		description: "Show the full schema of another tool.",
		params:      richObjectSchema("name"),
	})

	// Deliberately pass an empty alwaysFull list: tool_view must still be
	// exempt from stubbing because it is the meta tool for expanding stubs.
	r.SetSchemaMode(SchemaModeIndexed, nil)
	defs := r.GetDefinitions()

	got := findSchemaDef(t, defs, "tool_view")
	if got.Function.Description != "Show the full schema of another tool." {
		t.Errorf("tool_view description was stubbed: %q", got.Function.Description)
	}
	if len(got.Function.Parameters.Properties) != 1 {
		t.Errorf("tool_view parameters were stubbed: %+v", got.Function.Parameters)
	}
}

func TestRegistrySchemaMode_FullModeByteIdenticalRoundTrip(t *testing.T) {
	r := NewRegistry(nil)
	r.Register(&schemaModeTool{
		name:        "file_read",
		description: "Read a file from disk.",
		params:      richObjectSchema("path", "encoding"),
	})
	r.Register(&schemaModeTool{
		name:        "tool_view",
		description: "Show a tool's full schema.",
		params:      richObjectSchema("name"),
	})

	// Default (never-configured) registry must already be full mode.
	before, err := json.Marshal(r.ToLLMDefinitions())
	if err != nil {
		t.Fatalf("marshal before: %v", err)
	}

	r.SetSchemaMode(SchemaModeIndexed, nil)
	indexed, err := json.Marshal(r.ToLLMDefinitions())
	if err != nil {
		t.Fatalf("marshal indexed: %v", err)
	}
	if string(before) == string(indexed) {
		t.Error("indexed payload should differ from full payload")
	}

	// Switching back to full must restore the exact original bytes.
	r.SetSchemaMode(SchemaModeFull, []string{"irrelevant_entry"})
	after, err := json.Marshal(r.ToLLMDefinitions())
	if err != nil {
		t.Fatalf("marshal after: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("full mode payload not byte-identical after round trip:\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestRegistrySchemaMode_UnknownModeFallsBackToFull(t *testing.T) {
	r := NewRegistry(nil)
	r.Register(&schemaModeTool{
		name:        "obscure_widget",
		description: "Frobnicate a widget.",
		params:      richObjectSchema("widget_id"),
	})

	r.SetSchemaMode(SchemaMode("bogus_mode"), nil)
	defs := r.GetDefinitions()

	got := findSchemaDef(t, defs, "obscure_widget")
	if len(got.Function.Parameters.Properties) != 1 {
		t.Errorf("unknown mode should behave as full mode; params were stubbed: %+v", got.Function.Parameters)
	}
	if got.Function.Description != "Frobnicate a widget." {
		t.Errorf("unknown mode should behave as full mode; description mutated: %q", got.Function.Description)
	}
}

func TestRegistrySchemaMode_IndexedPayloadSmallerThanFull(t *testing.T) {
	r := NewRegistry(nil)
	for i := 0; i < 20; i++ {
		r.Register(&schemaModeTool{
			name:        fmt.Sprintf("size_tool_%02d", i),
			description: fmt.Sprintf("Fixture tool %d: does fixture things with fixture inputs.", i),
			params:      richObjectSchema("alpha", "beta", "gamma", "delta"),
		})
	}

	full, err := json.Marshal(r.ToLLMDefinitions())
	if err != nil {
		t.Fatalf("marshal full: %v", err)
	}

	r.SetSchemaMode(SchemaModeIndexed, []string{"size_tool_00"})
	indexed, err := json.Marshal(r.ToLLMDefinitions())
	if err != nil {
		t.Fatalf("marshal indexed: %v", err)
	}

	if len(indexed) >= len(full) {
		t.Errorf("expected indexed payload (%d bytes) < full payload (%d bytes)", len(indexed), len(full))
	}
	if len(indexed) == 0 {
		t.Fatal("indexed payload is empty")
	}
}

func TestRegistrySchemaMode_RaceSafeModeSwitching(t *testing.T) {
	r := NewRegistry(nil)
	for i := 0; i < 4; i++ {
		r.Register(&schemaModeTool{
			name:        fmt.Sprintf("race_tool_%d", i),
			description: "Race fixture tool.",
			params:      richObjectSchema("x", "y"),
		})
	}

	var wg sync.WaitGroup

	// Concurrent readers of the definitions.
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				defs := r.GetDefinitions()
				for _, d := range defs {
					if d.Function.Name == "" {
						t.Error("empty tool name in definition")
						return
					}
					if d.Type != "function" {
						t.Errorf("unexpected definition type %q", d.Type)
						return
					}
				}
			}
		}()
	}

	// Concurrent writer flipping modes mid-flight.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			if i%2 == 0 {
				r.SetSchemaMode(SchemaModeIndexed, []string{"race_tool_0"})
			} else {
				r.SetSchemaMode(SchemaModeFull, nil)
			}
		}
	}()

	wg.Wait()
}
