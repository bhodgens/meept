package agent

import (
	"context"

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

// _ ensure compile-time interface satisfaction
var _ depthGate = (*spawnLeaf)(nil)
var _ tools.Tool = (*noopLeaf)(nil)
var _ tools.Tool = (*spawnLeaf)(nil)
