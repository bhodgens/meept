package agent

import (
	"context"
	"math"
	"sync"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// depthGate is an optional interface that marks a tool as depth-gated.
// When implemented, the depth gating layer strips the tool at
// depth == maxDepth so the agent never sees it in its registry.
type depthGate interface {
	tools.Tool

	// GatedDepth returns the maximum depth at which this tool is
	// visible. At depths greater than or equal to this value the
	// tool is invisible to the agent ("make illegal states
	// unrepresentable").
	GatedDepth() int
}

// DepthToolRegistry provides structural tool gating by depth.
//
// Tools are registered per-depth gate so that agents at the maximum
// depth simply don't have the spawn tool in their registry. This is
// the cleanest possible enforcement: an agent cannot spawn a child
// because it doesn't know spawning is possible.
//
// Leaf tools are available at all depths; depth-gated tools (such as
// the subagent spawn tool) are visible only below the depth limit.
type DepthToolRegistry struct {
	maxDepth     int
	leafTools    []tools.Tool
	gatedTools   []depthGate
	toolsByDepth []*toolsEntry
}

// toolsEntry holds the resolved tool list for a given depth.
type toolsEntry struct {
	tools         []tools.Tool
	definitions   []llm.ToolDefinition
	nameSet       map[string]struct{}
	definitionSet map[string]struct{}
}

// NewDepthToolRegistry creates a registry with tools gated by depth.
//
// leafTools are available at all depths. gatedTools are only visible
// when depth < their GatedDepth() value, so they disappear once the
// agent reaches its depth limit.
//
//nolint:prealloc // depth is small; allocation cost is negligible
func NewDepthToolRegistry(maxDepth int, leafTools []tools.Tool, gatedTools []depthGate) *DepthToolRegistry {
	if maxDepth <= 0 {
		maxDepth = 3 // reasonable default
	}

	dtr := &DepthToolRegistry{
		maxDepth:     maxDepth,
		leafTools:    leafTools,
		gatedTools:   gatedTools,
		toolsByDepth: make([]*toolsEntry, maxDepth+1),
	}

	// Pre-compute tool sets for each depth slice.
	for d := 0; d <= maxDepth; d++ {
		visible := dtr.visibleTools(d)
		entry := &toolsEntry{
			tools:         visible,
			nameSet:       make(map[string]struct{}),
			definitionSet: make(map[string]struct{}),
		}

		// Build definitions list.
		entry.definitions = make([]llm.ToolDefinition, 0, len(visible))
		for _, t := range visible {
			entry.nameSet[t.Name()] = struct{}{}
			entry.definitionSet[t.Name()] = struct{}{}
			entry.definitions = append(entry.definitions, llm.NewToolDefinition(
				t.Name(),
				t.Description(),
				t.Parameters(),
			))
		}

		dtr.toolsByDepth[d] = entry
	}

	return dtr
}

// visibleTools returns tools visible at the given depth.
// Leaf tools are always included; gated tools are included only when
// depth < their GatedDepth() value.
func (dtr *DepthToolRegistry) visibleTools(depth int) []tools.Tool {
	if depth < 0 {
		depth = 0
	}
	if depth > dtr.maxDepth {
		depth = dtr.maxDepth
	}

	result := make([]tools.Tool, 0, len(dtr.leafTools)+len(dtr.gatedTools))

	// Always add leaf tools.
	//lint:ignore S1011 loop preserves future expansion logic
	for _, t := range dtr.leafTools {
		result = append(result, t)
	}

	// Add gated tools that are still visible at this depth.
	for _, t := range dtr.gatedTools {
		if depth < t.GatedDepth() {
			result = append(result, t)
		}
	}

	return result
}

// HasTool reports whether a tool with the given name is visible at depth.
func (dtr *DepthToolRegistry) HasTool(depth int, name string) bool {
	entry := dtr.entryForDepth(depth)
	_, ok := entry.nameSet[name]
	return ok
}

// HasDefinition reports whether a tool definition with the given name
// is visible at depth (useful for checking function-calling definitions).
func (dtr *DepthToolRegistry) HasDefinition(depth int, name string) bool {
	entry := dtr.entryForDepth(depth)
	_, ok := entry.definitionSet[name]
	return ok
}

// ToolsAtDepth returns all tools visible to an agent at the given depth.
func (dtr *DepthToolRegistry) ToolsAtDepth(depth int) []tools.Tool {
	entry := dtr.entryForDepth(depth)
	return entry.tools
}

// DefinitionsAtDepth returns LLM tool definitions for the given depth.
func (dtr *DepthToolRegistry) DefinitionsAtDepth(depth int) []llm.ToolDefinition {
	entry := dtr.entryForDepth(depth)
	return entry.definitions
}

// GetTool returns the tool with the given name at the given depth, or
// nil if the tool is not visible at that depth.
func (dtr *DepthToolRegistry) GetTool(depth int, name string) tools.Tool {
	entry := dtr.entryForDepth(depth)
	for _, t := range entry.tools {
		if t.Name() == name {
			return t
		}
	}
	return nil
}

// ListAllLeaves returns the registered leaf tools (independent of depth).
func (dtr *DepthToolRegistry) ListAllLeaves() []tools.Tool {
	return dtr.leafTools
}

// ListAllGated returns all depth-gated tools regardless of visibility.
func (dtr *DepthToolRegistry) ListAllGated() []depthGate {
	return dtr.gatedTools
}

// MaxDepth returns the configured maximum depth.
func (dtr *DepthToolRegistry) MaxDepth() int {
	return dtr.maxDepth
}

// entryForDepth returns the precomputed tools entry for the given depth,
// clamping to valid bounds and falling back to the maxDepth entry if
// the requested depth is out of range.
func (dtr *DepthToolRegistry) entryForDepth(depth int) *toolsEntry {
	if depth < 0 {
		depth = 0
	}
	if depth > dtr.maxDepth {
		depth = dtr.maxDepth
	}
	return dtr.toolsByDepth[depth]
}

// --- Example leaf tool implementations below ---
// These satisfy the tools.Tool interface for use as placeholder
// leaf tools in the registry. In production they would wrap real
// tool implementations from internal/tools.

// noopLeaf is a minimal no-op tool for testing the registry.
type noopLeaf struct {
	name        string
	description string
}

func (t *noopLeaf) Name() string        { return t.name }
func (t *noopLeaf) Description() string { return t.description }
func (t *noopLeaf) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{}
}
func (t *noopLeaf) Execute(ctx context.Context, args map[string]any) (any, error) {
	return "ok", nil
}
func (t *noopLeaf) IsReadOnly(map[string]any) bool        { return false }
func (t *noopLeaf) IsConcurrencySafe(map[string]any) bool { return false }

// spawnLeaf is a depth-gated tool representing the subagent-spawn capability.
// It is visible at all depths less than its gate depth, making it invisible
// once the agent reaches max depth.
type spawnLeaf struct {
	base       tools.Tool
	gatedDepth int
}

func (t *spawnLeaf) GatedDepth() int { return t.gatedDepth }
func (t *spawnLeaf) Name() string {
	return t.base.Name()
}
func (t *spawnLeaf) Description() string {
	return t.base.Description()
}
func (t *spawnLeaf) Parameters() llm.FunctionParameters {
	return t.base.Parameters()
}
func (t *spawnLeaf) Execute(ctx context.Context, args map[string]any) (any, error) {
	return t.base.Execute(ctx, args)
}
func (t *spawnLeaf) IsReadOnly(input map[string]any) bool { return t.base.IsReadOnly(input) }
func (t *spawnLeaf) IsConcurrencySafe(input map[string]any) bool {
	return t.base.IsConcurrencySafe(input)
}

// -----------------------------------------------------------------------
// GatedToolRegistry: multi-dimensional tool gating
// -----------------------------------------------------------------------

// ToolDescriptor describes a tool with gating constraints across
// multiple dimensions: depth, invocation count, and runtime state.
type ToolDescriptor struct {
	Name        string
	Description string
	InputSchema llm.FunctionParameters
	// AvailableAtDepths lists the depths at which this tool is
	// available. If nil or empty, the tool is available at all depths.
	AvailableAtDepths []int
	// MaxUses limits total invocations across the agent run.
	// Zero means unlimited.
	MaxUses int
	// RequiresState, if non-empty, specifies the agent state
	// in which the tool becomes available. Empty means "any state".
	RequiresState string
}

// GatedToolRegistry extends DepthToolRegistry with usage-based and
// state-based gating. Tools are filtered by three dimensions:
//
//   - Depth: AvailableAtDepths gates which depths may use a tool
//   - Usage: MaxUses limits total invocations
//   - State: RequiresState gates tools by agent runtime state
type GatedToolRegistry struct {
	*DepthToolRegistry
	mu                sync.Mutex
	usageCount        map[string]int
	stateGateRegistry map[string]ToolDescriptor // name → descriptor
}

// NewGatedToolRegistry creates a gating registry. depthGate tools
// (those implementing GatedDepth()) are still applied on top of
// the DepthToolRegistry layer.
func NewGatedToolRegistry(maxDepth int, leafTools []tools.Tool, gatedTools []depthGate) *GatedToolRegistry {
	dtr := NewDepthToolRegistry(maxDepth, leafTools, gatedTools)
	return &GatedToolRegistry{
		DepthToolRegistry: dtr,
		usageCount:        make(map[string]int),
		stateGateRegistry: make(map[string]ToolDescriptor),
	}
}

// NewGatedToolRegistryWithDescriptors creates a GatedToolRegistry where
// each tool is described by a ToolDescriptor giving precise gating
// per depth, usage count, and state requirement.
//
// "maxDepth" is still used for the clamp boundary; depthGate tools
// are derived from the gating descriptors rather than GatedDepth().
func NewGatedToolRegistryWithDescriptors(maxDepth int, descriptors []ToolDescriptor, leafNames []string) *GatedToolRegistry {
	if maxDepth <= 0 {
		maxDepth = 3
	}

	stateRegistry := make(map[string]ToolDescriptor)
	usage := make(map[string]int)

	for _, desc := range descriptors {
		stateRegistry[desc.Name] = desc
	}

	// Gather gated tools (those with depth restrictions) for the underlying DTR.
	allGatedTools := make([]depthGate, 0, len(descriptors))
	for _, desc := range descriptors {
		if len(desc.AvailableAtDepths) > 0 {
			allGatedTools = append(allGatedTools, &descriptorTool{desc})
		}
	}

	// Build leaf tools from names not already in the descriptor list.
	leafTools := make([]tools.Tool, 0, len(leafNames))
	for _, name := range leafNames {
		if _, hasDesc := stateRegistry[name]; !hasDesc {
			leafTools = append(leafTools, &noopLeaf{name: name, description: name + " tool"})
		}
	}

	dtr := NewDepthToolRegistry(maxDepth, leafTools, allGatedTools)

	return &GatedToolRegistry{
		DepthToolRegistry: dtr,
		usageCount:        usage,
		stateGateRegistry: stateRegistry,
	}
}

// getAvailableAtDepth returns the list of tool names visible at the
// depth level (from DepthToolRegistry).
func (g *GatedToolRegistry) getAvailableAtDepth(depth int) []string {
	tools := g.ToolsAtDepth(depth)
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name())
	}
	return names
}

// IsAvailable checks if a tool is available given the current depth
// and state, respecting MaxUses and RequiresState constraints.
func (g *GatedToolRegistry) IsAvailable(depth int, toolName string, agentState string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Check depth gating (from DepthToolRegistry).
	if !g.HasTool(depth, toolName) {
		return false
	}

	// Check state gating.
	desc, hasDesc := g.stateGateRegistry[toolName]
	if hasDesc {
		if desc.RequiresState != "" && desc.RequiresState != agentState {
			return false
		}
		if len(desc.AvailableAtDepths) > 0 {
			depthOk := false
			for _, d := range desc.AvailableAtDepths {
				if d == depth {
					depthOk = true
					break
				}
			}
			if !depthOk {
				return false
			}
		}
	}

	// Check usage gating.
	if desc.MaxUses > 0 {
		if g.usageCount[toolName] >= desc.MaxUses {
			return false
		}
	}

	return true
}

// RecordUse increments the invocation count for a tool.
func (g *GatedToolRegistry) RecordUse(toolName string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.usageCount[toolName]++
}

// UsageCount returns the current invocation count for a tool.
func (g *GatedToolRegistry) UsageCount(toolName string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.usageCount[toolName]
}

// GetAvailableTools returns all tools that are available at the given
// depth and state, filtered by MaxUses.
func (g *GatedToolRegistry) GetAvailableTools(depth int, agentState string) []ToolDescriptor {
	g.mu.Lock()
	defer g.mu.Unlock()

	var result []ToolDescriptor
	toolNames := g.getAvailableAtDepth(depth)

	for _, name := range toolNames {
		desc, hasDesc := g.stateGateRegistry[name]
		if !hasDesc {
			// Default: no extra gating, always available at this depth.
			desc = ToolDescriptor{Name: name, Description: name + " tool"}
		}

		// State gate check.
		if desc.RequiresState != "" && desc.RequiresState != agentState {
			continue
		}

		// Explicit depth gate from descriptor.
		if len(desc.AvailableAtDepths) > 0 {
			depthOk := false
			for _, d := range desc.AvailableAtDepths {
				if d == depth {
					depthOk = true
					break
				}
			}
			if !depthOk {
				continue
			}
		}

		// Usage gate check.
		if desc.MaxUses > 0 {
			if g.usageCount[name] >= desc.MaxUses {
				continue
			}
		}

		result = append(result, desc)
	}

	return result
}

// _ ensure compile-time interface satisfaction
var _ depthGate = (*spawnLeaf)(nil)
var _ tools.Tool = (*noopLeaf)(nil)
var _ tools.Tool = (*spawnLeaf)(nil)

// descriptorTool wraps a ToolDescriptor as a tools.Tool for use
// inside DepthToolRegistry's leaf/gated tool lists.
type descriptorTool struct {
	desc ToolDescriptor
}

func (t *descriptorTool) Name() string                       { return t.desc.Name }
func (t *descriptorTool) Description() string                { return t.desc.Description }
func (t *descriptorTool) Parameters() llm.FunctionParameters { return t.desc.InputSchema }
func (t *descriptorTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return map[string]any{"tool": t.desc.Name, "args": args}, nil
}
func (t *descriptorTool) IsReadOnly(map[string]any) bool        { return false }
func (t *descriptorTool) IsConcurrencySafe(map[string]any) bool { return false }

// GatedDepth returns the maximum depth at which the tool is visible.
// For descriptor-based gating we derive this from AvailableAtDepths:
// the gate fires at max(available_depths)+1, meaning the tool disappears
// at the first depth beyond its allowed range.
func (t *descriptorTool) GatedDepth() int {
	depths := t.desc.AvailableAtDepths
	if len(depths) == 0 {
		return math.MaxInt32 // always visible
	}
	maxD := 0
	for _, d := range depths {
		if d > maxD {
			maxD = d
		}
	}
	return maxD + 1 // hidden at maxD+1 and beyond
}

var _ depthGate = (*descriptorTool)(nil)
var _ tools.Tool = (*descriptorTool)(nil)
