package agent

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// fakeLeafTool is a depth-agnostic leaf tool for testing.
type fakeLeafTool struct {
	name        string
	description string
}

func (t *fakeLeafTool) Name() string        { return t.name }
func (t *fakeLeafTool) Description() string { return t.description }
func (t *fakeLeafTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"target": {Type: "string"},
		},
	}
}
func (t *fakeLeafTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return map[string]any{"tool": t.name, "args": args}, nil
}

// fakeSpawnTool is a depth-gated spawn tool with gate at maxDepth.
type fakeSpawnTool struct {
	base       tools.Tool
	gatedDepth int
}

func (t *fakeSpawnTool) GatedDepth() int { return t.gatedDepth }
func (t *fakeSpawnTool) Name() string    { return t.base.Name() }
func (t *fakeSpawnTool) Description() string {
	return t.base.Description()
}
func (t *fakeSpawnTool) Parameters() llm.FunctionParameters {
	return t.base.Parameters()
}
func (t *fakeSpawnTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return t.base.Execute(ctx, args)
}

var _ depthGate = (*fakeSpawnTool)(nil)

func newLeaf(name string) *fakeLeafTool {
	return &fakeLeafTool{name: name, description: name + " tool"}
}

func newSpawn(base tools.Tool, gatedDepth int) *fakeSpawnTool {
	return &fakeSpawnTool{base: base, gatedDepth: gatedDepth}
}

func hasTool(tools []tools.Tool, name string) bool {
	for _, t := range tools {
		if t.Name() == name {
			return true
		}
	}
	return false
}

func TestToolRegistry_StructuralGating(t *testing.T) {
	leaves := []tools.Tool{
		newLeaf("get_dataset_overview"),
		newLeaf("query_traces"),
		newLeaf("view_trace"),
	}
	spawn := newSpawn(&fakeLeafTool{name: "call_subagent", description: "spawn a subagent"}, 2)
	registry := NewDepthToolRegistry(2, leaves, []depthGate{spawn})

	// Depth 0: has spawn tool
	if !registry.HasTool(0, "call_subagent") {
		t.Error("depth 0 should have call_subagent tool")
	}

	// Depth 1: has spawn tool
	if !registry.HasTool(1, "call_subagent") {
		t.Error("depth 1 should have call_subagent tool")
	}

	// Depth 2 (maxDepth): NO call_subagent tool
	if registry.HasTool(2, "call_subagent") {
		t.Error("depth 2 (maxDepth) should NOT have call_subagent tool")
	}
}

func TestToolRegistry_LeafToolsAtAllDepths(t *testing.T) {
	leaves := []tools.Tool{
		newLeaf("get_dataset_overview"),
		newLeaf("query_traces"),
		newLeaf("view_trace"),
		newLeaf("view_spans"),
		newLeaf("search_trace"),
		newLeaf("search_span"),
	}
	registry := NewDepthToolRegistry(2, leaves, nil)

	for d := 0; d <= 2; d++ {
		tools := registry.ToolsAtDepth(d)
		for _, name := range []string{
			"get_dataset_overview", "query_traces", "view_trace",
			"view_spans", "search_trace", "search_span",
		} {
			if !hasTool(tools, name) {
				t.Errorf("depth %d missing leaf tool: %s", d, name)
			}
		}
	}
}

func TestToolRegistry_ToolsAtDepth(t *testing.T) {
	leaves := []tools.Tool{newLeaf("read_file"), newLeaf("write_file")}
	spawn := newSpawn(&fakeLeafTool{name: "spawn"}, 2)
	registry := NewDepthToolRegistry(2, leaves, []depthGate{spawn})

	tools0 := registry.ToolsAtDepth(0)
	if len(tools0) != 3 {
		t.Errorf("depth 0: got %d tools, want 3", len(tools0))
	}

	tools2 := registry.ToolsAtDepth(2)
	if len(tools2) != 2 {
		t.Errorf("depth 2 (maxDepth): got %d tools, want 2", len(tools2))
	}
}

func TestToolRegistry_GetToolReturnsNilWhenGated(t *testing.T) {
	leaves := []tools.Tool{newLeaf("read_file")}
	spawn := newSpawn(&fakeLeafTool{name: "spawn"}, 2)
	registry := NewDepthToolRegistry(2, leaves, []depthGate{spawn})

	if registry.GetTool(2, "spawn") != nil {
		t.Error("GetTool at maxDepth should return nil for gated tool")
	}
	if registry.GetTool(0, "spawn") == nil {
		t.Error("GetTool below maxDepth should NOT return nil for non-gated tool")
	}
}

func TestToolRegistry_DefinitionsAtDepth(t *testing.T) {
	leaves := []tools.Tool{newLeaf("read_file")}
	spawn := newSpawn(&fakeLeafTool{name: "spawn"}, 2)
	registry := NewDepthToolRegistry(2, leaves, []depthGate{spawn})

	defs0 := registry.DefinitionsAtDepth(0)
	if len(defs0) != 2 {
		t.Errorf("depth 0 definitions: got %d, want 2", len(defs0))
	}

	defs2 := registry.DefinitionsAtDepth(2)
	if len(defs2) != 1 {
		t.Errorf("depth 2 definitions: got %d, want 1", len(defs2))
	}
}

func TestToolRegistry_HasDefinition(t *testing.T) {
	leaves := []tools.Tool{newLeaf("read_file")}
	spawn := newSpawn(&fakeLeafTool{name: "spawn"}, 2)
	registry := NewDepthToolRegistry(2, leaves, []depthGate{spawn})

	if !registry.HasDefinition(0, "spawn") {
		t.Error("depth 0 should have spawn definition")
	}
	if registry.HasDefinition(2, "spawn") {
		t.Error("depth 2 should NOT have spawn definition")
	}
}

func TestToolRegistry_MaxDepth(t *testing.T) {
	registry := NewDepthToolRegistry(3, nil, nil)
	if registry.MaxDepth() != 3 {
		t.Errorf("maxDepth = %d, want 3", registry.MaxDepth())
	}
}

func TestToolRegistry_EmptyToolRegistry(t *testing.T) {
	registry := NewDepthToolRegistry(2, nil, nil)

	tools0 := registry.ToolsAtDepth(0)
	if len(tools0) != 0 {
		t.Errorf("depth 0: got %d tools, want 0", len(tools0))
	}

	if registry.HasTool(0, "anything") {
		t.Error("empty registry should have no tools")
	}
}

func TestToolRegistry_ClampedToMaxDepth(t *testing.T) {
	leaves := []tools.Tool{newLeaf("read_file")}
	registry := NewDepthToolRegistry(2, leaves, nil)

	// Requesting tools at depth beyond max should clamp to maxDepth.
	tools10 := registry.ToolsAtDepth(10)
	if len(tools10) != 1 {
		t.Errorf("depth 10: got %d tools, want 1 (clamped to maxDepth)", len(tools10))
	}
	if !hasTool(tools10, "read_file") {
		t.Error("depth 10 should have leaf tool (clamped)")
	}
}

func TestToolRegistry_ClampedToZero(t *testing.T) {
	leaves := []tools.Tool{newLeaf("read_file")}
	registry := NewDepthToolRegistry(2, leaves, nil)

	// Requesting tools at negative depth should clamp to 0.
	toolsNeg := registry.ToolsAtDepth(-1)
	if len(toolsNeg) != 1 {
		t.Errorf("depth -1: got %d tools, want 1 (clamped to 0)", len(toolsNeg))
	}
}

func TestToolRegistry_NilDefaultDepth(t *testing.T) {
	// maxDepth <= 0 should default to 3.
	registry := NewDepthToolRegistry(0, nil, nil)
	if registry.MaxDepth() != 3 {
		t.Errorf("default maxDepth = %d, want 3", registry.MaxDepth())
	}
}

func TestToolRegistry_DifferentGateDepths(t *testing.T) {
	leaves := []tools.Tool{newLeaf("read_file")}
	gate1 := newSpawn(&fakeLeafTool{name: "early_spawn"}, 1)
	gate3 := newSpawn(&fakeLeafTool{name: "late_spawn"}, 3)
	registry := NewDepthToolRegistry(3, leaves, []depthGate{gate1, gate3})

	// Depth 0: both gated tools visible
	if !registry.HasTool(0, "early_spawn") || !registry.HasTool(0, "late_spawn") {
		t.Error("depth 0: both gated tools should be visible")
	}

	// Depth 1: only late_spawn visible (early_spawn gated at 1)
	if registry.HasTool(1, "early_spawn") {
		t.Error("depth 1: early_spawn should be hidden (gated at 1)")
	}
	if !registry.HasTool(1, "late_spawn") {
		t.Error("depth 1: late_spawn should still be visible")
	}

	// Depth 2: early_spawn hidden, late_spawn still visible (gates at 3)
	if registry.HasTool(2, "early_spawn") {
		t.Error("depth 2: early_spawn should be hidden (gated at 1)")
	}
	if !registry.HasTool(2, "late_spawn") {
		t.Error("depth 2: late_spawn should still be visible (gated at 3)")
	}

	// Depth 3 (maxDepth): both hidden
	if registry.HasTool(3, "early_spawn") || registry.HasTool(3, "late_spawn") {
		t.Error("depth 3: both gated tools should be hidden at maxDepth")
	}
}

func TestToolRegistry_DispatcherIntegrationNilRegistry(t *testing.T) {
	// Regression: dispatcher with nil tool registry should still work.
	d := NewDispatcher(DispatcherConfig{})
	if d.ToolRegistry() != nil {
		t.Error("new dispatcher should have nil tool registry by default")
	}
}

func TestToolRegistry_DispatcherSetToolRegistry(t *testing.T) {
	leaves := []tools.Tool{newLeaf("read_file")}
	registry := NewDepthToolRegistry(2, leaves, nil)

	d := NewDispatcher(DispatcherConfig{})
	d.SetToolRegistry(registry)

	if d.ToolRegistry() != registry {
		t.Error("SetToolRegistry should persist the registry")
	}
}

func TestToolRegistry_LeavesAndGatedLists(t *testing.T) {
	leaves := []tools.Tool{newLeaf("read_file"), newLeaf("write_file")}
	spawn := newSpawn(&fakeLeafTool{name: "spawn"}, 2)
	registry := NewDepthToolRegistry(2, leaves, []depthGate{spawn})

	leafList := registry.ListAllLeaves()
	if len(leafList) != 2 {
		t.Errorf("leaves: got %d, want 2", len(leafList))
	}

	gatedList := registry.ListAllGated()
	if len(gatedList) != 1 {
		t.Errorf("gated: got %d, want 1", len(gatedList))
	}
}

func TestToolRegistry_ExecuteGatedToolBelowGatedDepth(t *testing.T) {
	base := &fakeLeafTool{name: "spawn", description: "spawn tool"}
	spawn := newSpawn(base, 2)
	registry := NewDepthToolRegistry(2, nil, []depthGate{spawn})

	tool := registry.GetTool(1, "spawn")
	if tool == nil {
		t.Fatal("spawn should be visible at depth 1")
	}

	result, err := tool.Execute(context.Background(), map[string]any{"target": "child"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result.(map[string]any)["tool"]; !ok {
		t.Errorf("unexpected result: %v", result)
	}
}
