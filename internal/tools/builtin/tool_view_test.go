package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// toolViewFixtureTool is a minimal Tool stub for building fixture registries
// in the tool_view tests.
type toolViewFixtureTool struct {
	tools.ToolDefaults
	name   string
	desc   string
	params llm.FunctionParameters
}

func (t *toolViewFixtureTool) Name() string                       { return t.name }
func (t *toolViewFixtureTool) Description() string                { return t.desc }
func (t *toolViewFixtureTool) Parameters() llm.FunctionParameters { return t.params }
func (t *toolViewFixtureTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	return "ok", nil
}

// newToolViewFixture registers one stub tool per name and returns the
// registry alongside the full-fidelity definition each tool is expected to
// expand to (built directly from the tool, so the test is independent of
// registry definition-building paths).
func newToolViewFixture(t *testing.T, names ...string) (*tools.Registry, map[string]llm.ToolDefinition) {
	t.Helper()
	reg := tools.NewRegistry(nil)
	expected := make(map[string]llm.ToolDefinition, len(names))
	for i, name := range names {
		tool := &toolViewFixtureTool{
			name: name,
			desc: fmt.Sprintf("fixture stub tool %d", i),
			params: llm.FunctionParameters{
				Type: schemaTypeObject,
				Properties: map[string]llm.ParameterProperty{
					schemaPropQuery: {
						Type:        schemaTypeString,
						Description: fmt.Sprintf("the query for %s", name),
					},
				},
				Required: []string{schemaPropQuery},
			},
		}
		reg.Register(tool)
		expected[name] = llm.NewToolDefinition(tool.Name(), tool.Description(), tool.Parameters())
	}
	return reg, expected
}

// unmarshalToolViewDef parses a tool_view result payload into a
// llm.ToolDefinition.
func unmarshalToolViewDef(t *testing.T, raw string) llm.ToolDefinition {
	t.Helper()
	var def llm.ToolDefinition
	if err := json.Unmarshal([]byte(raw), &def); err != nil {
		t.Fatalf("tool_view result is not valid JSON: %v\npayload: %s", err, raw)
	}
	return def
}

func TestToolViewKnownTool(t *testing.T) {
	reg, expected := newToolViewFixture(t, "alpha_tool")
	tv := NewToolViewTool(reg, 0) // 0 -> default capacity

	resAny, err := tv.Execute(context.Background(), map[string]any{"name": "alpha_tool"})
	if err != nil {
		t.Fatalf("Execute returned error for known tool: %v", err)
	}
	res, ok := resAny.(tools.ToolResult)
	if !ok {
		t.Fatalf("expected tools.ToolResult, got %T", resAny)
	}
	if !res.Success {
		t.Fatalf("expected successful result, got %+v", res)
	}
	raw, ok := res.Result.(string)
	if !ok || raw == "" {
		t.Fatalf("expected non-empty string result, got %T %v", res.Result, res.Result)
	}

	def := unmarshalToolViewDef(t, raw)
	if def.Type != "function" {
		t.Errorf("definition type = %q, want %q", def.Type, "function")
	}
	if def.Function.Name != "alpha_tool" {
		t.Errorf("definition name = %q, want %q", def.Function.Name, "alpha_tool")
	}
	if def.Function.Description == "" {
		t.Error("definition description is empty")
	}
	if len(def.Function.Parameters.Properties) == 0 {
		t.Error("definition has empty parameter properties; expected non-empty schema")
	}

	// No schema loss: parsed definition must equal the full-fidelity
	// definition built directly from the live tool.
	if !reflect.DeepEqual(def, expected["alpha_tool"]) {
		t.Errorf("definition mismatch:\n got: %#v\nwant: %#v", def, expected["alpha_tool"])
	}

	// JSON round-trip: re-marshaling and unmarshaling the parsed definition
	// must yield an equal value (no schema loss in the JSON itself).
	reMarshaled, err := json.Marshal(def)
	if err != nil {
		t.Fatalf("re-marshal failed: %v", err)
	}
	roundTripped := unmarshalToolViewDef(t, string(reMarshaled))
	if !reflect.DeepEqual(roundTripped, def) {
		t.Errorf("round-trip mismatch:\n got: %#v\nwant: %#v", roundTripped, def)
	}
}

func TestToolViewUnknownTool(t *testing.T) {
	reg, _ := newToolViewFixture(t, "alpha_tool")
	tv := NewToolViewTool(reg, 0)

	res, err := tv.Execute(context.Background(), map[string]any{"name": "missing_tool"})
	if err == nil {
		t.Fatalf("expected error for unknown tool, got result %+v", res)
	}
	if got := err.Error(); got != "tool not found: missing_tool" {
		t.Errorf("error = %q, want %q", got, "tool not found: missing_tool")
	}
}

func TestToolViewRequiresName(t *testing.T) {
	reg, _ := newToolViewFixture(t, "alpha_tool")
	tv := NewToolViewTool(reg, 0)

	if _, err := tv.Execute(context.Background(), map[string]any{}); err == nil {
		t.Error("expected error when name argument is missing")
	}
	if _, err := tv.Execute(context.Background(), map[string]any{"name": ""}); err == nil {
		t.Error("expected error when name argument is empty")
	}
}

func TestToolViewLRUEviction(t *testing.T) {
	reg, _ := newToolViewFixture(t, "tool_a", "tool_b", "tool_c")
	tv := NewToolViewTool(reg, 2)

	names := []string{"tool_a", "tool_b", "tool_c"}
	for _, name := range names {
		if _, err := tv.Execute(context.Background(), map[string]any{"name": name}); err != nil {
			t.Fatalf("Execute(%q) failed: %v", name, err)
		}
		if got := tv.lruLen(); got > 2 {
			t.Fatalf("cache size %d exceeds capacity 2 after expanding %q", got, name)
		}
	}
	if got := tv.lruLen(); got != 2 {
		t.Errorf("cache size = %d, want 2 after expanding 3 tools with capacity 2", got)
	}
}

func TestToolViewCacheHit(t *testing.T) {
	reg := tools.NewRegistry(nil)
	// countingTool changes its schema on every Parameters() call; a cache
	// hit must serve the schema captured at first expansion.
	tool := &toolViewCountingTool{}
	reg.Register(tool)
	tv := NewToolViewTool(reg, 0)

	res1Any, err := tv.Execute(context.Background(), map[string]any{"name": tool.Name()})
	if err != nil {
		t.Fatalf("first Execute failed: %v", err)
	}
	raw1, _ := res1Any.(tools.ToolResult).Result.(string)
	first := unmarshalToolViewDef(t, raw1)

	tool.calls = 99 // mutate subsequent Parameters() output
	res2Any, err := tv.Execute(context.Background(), map[string]any{"name": tool.Name()})
	if err != nil {
		t.Fatalf("second Execute failed: %v", err)
	}
	raw2, _ := res2Any.(tools.ToolResult).Result.(string)

	if raw1 == "" || raw1 != raw2 {
		t.Errorf("expected cached payload on second expansion:\nfirst:  %s\nsecond: %s", raw1, raw2)
	}
	if def := unmarshalToolViewDef(t, raw2); !reflect.DeepEqual(def, first) {
		t.Errorf("cached definition drifted from first expansion: %#v", def)
	}
	// calls staying at 99 proves the second expansion never consulted
	// tool.Parameters() — the cache served it.
	if tool.calls != 99 {
		t.Errorf("cache hit consulted tool.Parameters(): calls=%d", tool.calls)
	}
}

// toolViewCountingTool embeds its call count in the schema so cache hits are
// observable in test.
type toolViewCountingTool struct {
	tools.ToolDefaults
	calls int
}

func (t *toolViewCountingTool) Name() string        { return "counting_tool" }
func (t *toolViewCountingTool) Description() string { return "counting fixture tool" }
func (t *toolViewCountingTool) Parameters() llm.FunctionParameters {
	t.calls++
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropCount: {
				Type:        schemaTypeInteger,
				Description: fmt.Sprintf("calls=%d", t.calls),
			},
		},
	}
}
func (t *toolViewCountingTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	return t.calls, nil
}

func TestToolViewNilRegistry(t *testing.T) {
	tv := NewToolViewTool(nil, 0)

	// Must error, not panic.
	res, err := tv.Execute(context.Background(), map[string]any{"name": "alpha_tool"})
	if err == nil {
		t.Fatalf("expected error with nil registry, got result %+v", res)
	}
}

func TestToolViewConcurrentExecute(t *testing.T) {
	reg, _ := newToolViewFixture(t, "alpha_tool", "beta_tool", "gamma_tool")
	tv := NewToolViewTool(reg, 2) // tiny cache to force eviction under contention

	names := []string{"alpha_tool", "beta_tool", "gamma_tool", "missing_tool"}

	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				name := names[(g+i)%len(names)]
				_, err := tv.Execute(context.Background(), map[string]any{"name": name})
				if err != nil && name != "missing_tool" {
					// testing.T is safe for concurrent use.
					t.Errorf("Execute(%q) failed: %v", name, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}
