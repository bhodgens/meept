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
func (t *fakeLeafTool) IsReadOnly(map[string]any) bool        { return false }
func (t *fakeLeafTool) IsConcurrencySafe(map[string]any) bool { return false }

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
func (t *fakeSpawnTool) IsReadOnly(input map[string]any) bool        { return t.base.IsReadOnly(input) }
func (t *fakeSpawnTool) IsConcurrencySafe(input map[string]any) bool { return t.base.IsConcurrencySafe(input) }

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

// -----------------------------------------------------------------------
// GatedToolRegistry tests (Phase 5: structural tool gating)
// -----------------------------------------------------------------------

func TestGatedToolRegistry_DepthGating(t *testing.T) {
	descs := []ToolDescriptor{
		{
			Name:                "call_subagent",
			Description:         "spawn a subagent",
			AvailableAtDepths:   []int{0, 1},
			MaxUses:             0,
			RequiresState:       "",
		},
		{
			Name:                "view_trace",
			Description:         "view trace details",
			AvailableAtDepths:   nil, // all depths
		},
		{
			Name:                "generate_report",
			Description:         "generate synthesis report",
			AvailableAtDepths:   []int{0}, // top-level only
		},
	}
	leafNames := []string{"read_file", "write_file", "shell"}
	reg := NewGatedToolRegistryWithDescriptors(2, descs, leafNames)

	// call_subagent available at depths 0,1
	if !reg.HasTool(0, "call_subagent") {
		t.Error("call_subagent should be available at depth 0")
	}
	if !reg.HasTool(1, "call_subagent") {
		t.Error("call_subagent should be available at depth 1")
	}
	if reg.HasTool(2, "call_subagent") {
		t.Error("call_subagent should NOT be available at depth 2 (maxDepth)")
	}

	// generate_report only at depth 0
	if !reg.HasTool(0, "generate_report") {
		t.Error("generate_report should be available at depth 0")
	}
	// HasTool checks DepthToolRegistry; generate_report maps to depthGate
	// with GatedDepth=1, so it's visible at depth 0 only in DTR.

	// AvailableTools checks full gating
	tools0 := reg.GetAvailableTools(0, "")
	toolNames0 := make(map[string]bool)
	for _, td := range tools0 {
		toolNames0[td.Name] = true
	}
	if !toolNames0["call_subagent"] {
		t.Error("GetAvailableTools(0) missing call_subagent")
	}
	if !toolNames0["generate_report"] {
		t.Error("GetAvailableTools(0) missing generate_report")
	}

	tools2 := reg.GetAvailableTools(2, "")
	toolNames2 := make(map[string]bool)
	for _, td := range tools2 {
		toolNames2[td.Name] = true
	}
	if toolNames2["call_subagent"] {
		t.Error("GetAvailableTools(2) should NOT have call_subagent")
	}
	if toolNames2["generate_report"] {
		t.Error("GetAvailableTools(2) should NOT have generate_report")
	}
}

func TestGatedToolRegistry_MaxUses(t *testing.T) {
	descs := []ToolDescriptor{
		{
			Name:            "limited_tool",
			Description:     "tool with max uses",
			AvailableAtDepths: []int{0, 1, 2},
			MaxUses:         3,
		},
	}
	reg := NewGatedToolRegistryWithDescriptors(2, descs, nil)

	// Available, use it 3 times
	if !reg.IsAvailable(0, "limited_tool", "") {
		t.Error("limited_tool should be available")
	}
	reg.RecordUse("limited_tool")
	reg.RecordUse("limited_tool")
	reg.RecordUse("limited_tool")

	// After 3 uses, should be unavailable
	if reg.IsAvailable(0, "limited_tool", "") {
		t.Error("limited_tool should be unavailable after 3 uses")
	}
	if reg.UsageCount("limited_tool") != 3 {
		t.Errorf("UsageCount = %d, want 3", reg.UsageCount("limited_tool"))
	}
}

func TestGatedToolRegistry_StateGating(t *testing.T) {
	descs := []ToolDescriptor{
		{
			Name:          "synthesis_tool",
			Description:   "only available during synthesis",
			AvailableAtDepths: []int{0},
			RequiresState:  "planning",
		},
	}
	reg := NewGatedToolRegistryWithDescriptors(2, descs, nil)

	// Available in "planning" state
	if !reg.IsAvailable(0, "synthesis_tool", "planning") {
		t.Error("synthesis_tool should be available in planning state")
	}

	// Unavailable in other states
	if reg.IsAvailable(0, "synthesis_tool", "executing") {
		t.Error("synthesis_tool should NOT be available in executing state")
	}
	if reg.IsAvailable(0, "synthesis_tool", "") {
		t.Error("synthesis_tool should NOT be available in empty state")
	}
}

func TestGatedToolRegistry_GetAvailableToolsFiltersByMaxUses(t *testing.T) {
	descs := []ToolDescriptor{
		{
			Name:            "bounded_reporter",
			Description:     "report with bounded uses",
			AvailableAtDepths: []int{0, 1, 2},
			MaxUses:         1,
		},
	}
	reg := NewGatedToolRegistryWithDescriptors(2, descs, nil)

	// First call: should be available
	tools := reg.GetAvailableTools(0, "")
	if len(tools) != 1 || tools[0].Name != "bounded_reporter" {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	// Use it once
	reg.RecordUse("bounded_reporter")

	// Now should be unavailable in GetAvailableTools
	tools = reg.GetAvailableTools(0, "")
	if len(tools) != 0 {
		t.Errorf("expected 0 tools after use, got %d", len(tools))
	}
}

func TestGatedToolRegistry_StateAndDepthGating(t *testing.T) {
	descs := []ToolDescriptor{
		{
			Name:                "depth_and_state_tool",
			Description:         "gated by both depth and state",
			AvailableAtDepths:   []int{0, 1},
			MaxUses:             0,
			RequiresState:       "analysis",
		},
	}
	reg := NewGatedToolRegistryWithDescriptors(2, descs, nil)

	// Available at depth 1 in analysis state
	if !reg.IsAvailable(1, "depth_and_state_tool", "analysis") {
		t.Error("should be available at depth 1 in analysis state")
	}

	// Unavailable at depth 2 (depth gate) even in correct state
	if reg.IsAvailable(2, "depth_and_state_tool", "analysis") {
		t.Error("should NOT be available at depth 2 even in correct state")
	}

	// Unavailable in wrong state even at valid depth
	if reg.IsAvailable(0, "depth_and_state_tool", "executing") {
		t.Error("should NOT be available in executing state even at valid depth")
	}
}
