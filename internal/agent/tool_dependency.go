package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/caimlas/meept/internal/llm"
)

// ToolDependencyGraph represents dependencies between tool calls.
//
// Nodes are tool calls keyed by their ID. Edges express "depends-on"
// relationships: an edge from A to B means A depends on B (B must execute
// before A). IndependentGroups uses Kahn's algorithm to produce wave-based
// topological groups for parallel scheduling.
type ToolDependencyGraph struct {
	// nodes maps toolCallID -> tool call definition.
	nodes map[string]llm.ToolCall
	// edges maps toolCallID -> list of toolCallIDs that this one depends on
	// (i.e., edges[A] contains B means A depends on B; B must run first).
	edges map[string][]string
}

// NewToolDependencyGraph creates an empty dependency graph.
func NewToolDependencyGraph() *ToolDependencyGraph {
	return &ToolDependencyGraph{
		nodes: make(map[string]llm.ToolCall),
		edges: make(map[string][]string),
	}
}

// AddCall adds a tool call to the graph. Calls with duplicate IDs overwrite
// the previous node definition (last-wins) to mirror map semantics, though
// callers should generally use unique IDs.
func (g *ToolDependencyGraph) AddCall(call llm.ToolCall) {
	g.nodes[call.ID] = call
	// Ensure an entry exists for the node so IndependentGroups can discover
	// isolated nodes even when no edges reference them.
	if _, ok := g.edges[call.ID]; !ok {
		g.edges[call.ID] = nil
	}
}

// AddDependency records that 'from' depends on 'to' — 'to' must execute
// before 'from'. Duplicate edges are silently ignored. Self-edges are
// rejected (a call never depends on itself).
func (g *ToolDependencyGraph) AddDependency(from, to string) {
	if from == to {
		return
	}
	for _, existing := range g.edges[from] {
		if existing == to {
			return // already recorded
		}
	}
	g.edges[from] = append(g.edges[from], to)
}

// GetDependencies returns the IDs that 'id' depends on (must execute before
// 'id'). Returns nil for unknown IDs.
func (g *ToolDependencyGraph) GetDependencies(id string) []string {
	return g.edges[id]
}

// Dependents returns the IDs that depend on 'id' (reverse lookup of edges).
// Returns nil for unknown IDs or when nothing depends on 'id'.
func (g *ToolDependencyGraph) Dependents(id string) []string {
	var dependents []string
	for node, deps := range g.edges {
		for _, dep := range deps {
			if dep == id {
				dependents = append(dependents, node)
				break
			}
		}
	}
	return dependents
}

// IndependentGroups returns groups of tool calls that can execute in parallel.
//
// Groups are computed using Kahn's algorithm, wave-by-wave:
//   - Group 0 contains all nodes with no dependencies (in-degree 0).
//   - After "removing" group 0, group 1 contains nodes whose dependencies are
//     all in earlier groups.
//   - This continues until all nodes are consumed.
//
// All calls within a single group can run in parallel. Group N depends on
// groups 0..N-1 completing first.
//
// If a cycle is detected (nodes remain but none have in-degree 0), the
// remaining nodes are returned as a final fallback group and a warning is
// logged. This prevents infinite loops while still surfacing the problematic
// cycle for diagnosis.
func (g *ToolDependencyGraph) IndependentGroups() [][]llm.ToolCall {
	// inDegree counts how many unsatisfied dependencies each node has.
	inDegree := make(map[string]int, len(g.nodes))
	// dependents maps a dependency ID -> nodes that depend on it.
	dependents := make(map[string][]string)

	for id := range g.nodes {
		inDegree[id] = 0
	}
	for node, deps := range g.edges {
		for _, dep := range deps {
			// Only count dependencies that exist as nodes; phantom edges
			// (pointing to calls never added) are ignored to avoid
			// permanently blocking a node.
			if _, ok := g.nodes[dep]; ok {
				inDegree[node]++
				dependents[dep] = append(dependents[dep], node)
			}
		}
	}

	var groups [][]llm.ToolCall
	processed := make(map[string]bool, len(g.nodes))

	for len(processed) < len(g.nodes) {
		// Find all nodes currently at in-degree 0 that haven't been processed.
		var currentWave []string
		for id, deg := range inDegree {
			if deg == 0 && !processed[id] {
				currentWave = append(currentWave, id)
			}
		}

		if len(currentWave) == 0 {
			// Cycle detected: remaining nodes all have in-degree > 0.
			// Collect all unprocessed nodes as a fallback group.
			var cycleNodes []llm.ToolCall
			for id := range g.nodes {
				if !processed[id] {
					cycleNodes = append(cycleNodes, g.nodes[id])
					processed[id] = true
				}
			}
			if len(cycleNodes) > 0 {
				cycleDesc := describeCycle(g.edges, cycleNodes)
				slog.Warn("tool dependency graph contains a cycle; returning remaining nodes as fallback group",
					"node_count", len(cycleNodes),
					"cycle_hint", cycleDesc,
				)
				groups = append(groups, cycleNodes)
			}
			break
		}

		// Build the wave group and mark nodes processed.
		wave := make([]llm.ToolCall, 0, len(currentWave))
		for _, id := range currentWave {
			wave = append(wave, g.nodes[id])
			processed[id] = true
		}
		groups = append(groups, wave)

		// Decrement in-degree for dependents of processed nodes.
		for _, id := range currentWave {
			for _, dependent := range dependents[id] {
				if inDegree[dependent] > 0 {
					inDegree[dependent]--
				}
			}
		}
	}

	return groups
}

// describeCycle produces a human-readable hint about which nodes participate
// in a cycle, to aid debugging. It lists each node and its edges.
func describeCycle(edges map[string][]string, nodes []llm.ToolCall) string {
	parts := make([]string, 0, len(nodes))
	for _, n := range nodes {
		deps := edges[n.ID]
		if len(deps) == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s->[%s]", n.ID, strings.Join(deps, ",")))
	}
	return strings.Join(parts, " ")
}

// ---------------------------------------------------------------------------
// DependencyInferrer
// ---------------------------------------------------------------------------

// DependencyInferrer analyzes tool calls to infer data dependencies using
// heuristic rules. It populates a ToolDependencyGraph that can then be used
// for wave-based parallel scheduling.
type DependencyInferrer struct {
	toolRegistry ToolRegistry
	logger       *slog.Logger
}

// NewDependencyInferrer creates an inferrer with the given tool registry and
// logger. A nil logger defaults to slog.Default.
func NewDependencyInferrer(reg ToolRegistry, logger *slog.Logger) *DependencyInferrer {
	if logger == nil {
		logger = slog.Default()
	}
	return &DependencyInferrer{
		toolRegistry: reg,
		logger:       logger,
	}
}

// InferDependencies analyzes a list of tool calls and returns a populated
// dependency graph.
//
// Heuristic rules applied (best-effort, conservative — false dependencies
// only reduce parallelism, never correctness):
//  1. File path overlap: if call A writes a file and call B reads or writes
//     the same path (and A appears earlier in the list), B depends on A.
//  2. Shell command substitution: if a shell/bash command argument contains
//     "$(<priorCallID...)", it depends on that prior call.
//  3. Explicit argument references: if any argument value stringified
//     contains "$<callID>.result" or "<callID>.result", add dependency.
//  4. Same-resource writes: two write calls to the same path are ordered
//     (B depends on A) to preserve intended sequencing.
func (r *DependencyInferrer) InferDependencies(calls []llm.ToolCall) *ToolDependencyGraph {
	graph := NewToolDependencyGraph()

	// Add all calls as nodes first.
	for _, call := range calls {
		graph.AddCall(call)
	}

	// Pre-compute parsed args and file paths for each call.
	type callInfo struct {
		call     llm.ToolCall
		args     map[string]any
		filePath string
		isWrite  bool
		isRead   bool
	}
	infos := make([]callInfo, len(calls))
	for i, call := range calls {
		ci := callInfo{call: call}
		ci.args = parseArgs(call.Function.Arguments)
		ci.filePath = extractFilePath(ci.args)
		ci.isWrite = isWriteTool(call.Function.Name)
		ci.isRead = r.isReadOnly(call.Function.Name, ci.args)
		infos[i] = ci
	}

	// Pairwise comparison: for each (A earlier, B later), check heuristics.
	for i := 0; i < len(infos); i++ {
		for j := i + 1; j < len(infos); j++ {
			a := infos[i]
			b := infos[j]
			aID := a.call.ID
			bID := b.call.ID

			// Rule 1: File path overlap — A writes, B reads or writes same path.
			if a.filePath != "" && a.filePath == b.filePath && (b.isRead || b.isWrite) && a.isWrite {
				graph.AddDependency(bID, aID)
				continue // avoid adding duplicate edge from other rules
			}

			// Rule 4: Same-resource writes — both write same path.
			if a.filePath != "" && a.filePath == b.filePath && a.isWrite && b.isWrite {
				// This is subsumed by Rule 1 when isWrite covers it, but we
				// keep the explicit rule for clarity in case Rule 1's
				// conditions diverge. AddDependency is idempotent.
				graph.AddDependency(bID, aID)
				continue
			}

			// Rule 2: Shell command substitution referencing prior call ID.
			if isShellTool(b.call.Function.Name) {
				cmdStr := stringifyArg(b.args["command"])
				if strings.Contains(cmdStr, "$("+aID) {
					graph.AddDependency(bID, aID)
					continue
				}
			}

			// Rule 3: Explicit argument references in B pointing to A.
			if referencesCallID(b.args, aID) {
				graph.AddDependency(bID, aID)
				continue
			}
		}
	}

	return graph
}

// parseArgs unmarshals a JSON arguments string into a map. Returns an empty
// map on error (best-effort; malformed args don't block dependency analysis).
func parseArgs(arguments string) map[string]any {
	args := make(map[string]any)
	if arguments == "" {
		return args
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return args
	}
	return args
}

// extractFilePath attempts to locate a file path from common argument keys.
// Checks "file", "path", "filename" in order. Returns "" if none found.
func extractFilePath(args map[string]any) string {
	for _, key := range []string{"file", "path", "filename"} {
		if v, ok := args[key]; ok {
			s := stringifyArg(v)
			if s != "" {
				return filepath.Clean(s)
			}
		}
	}
	return ""
}

// isReadOnly reports whether the named tool is read-only for the given input.
// It prefers the tool's own declaration via the registry, falling back to the
// name-based heuristic when the tool is not found.
func (r *DependencyInferrer) isReadOnly(toolName string, input map[string]any) bool {
	if tool := r.toolRegistry.Get(toolName); tool != nil {
		return tool.IsReadOnly(input)
	}
	return isReadTool(toolName)
}

// isWriteTool returns true if the tool name suggests it writes/modifies a file.
func isWriteTool(name string) bool {
	name = strings.ToLower(name)
	return strings.Contains(name, "write") || strings.Contains(name, "edit")
}

// isReadTool returns true if the tool name suggests it reads a file.
func isReadTool(name string) bool {
	name = strings.ToLower(name)
	return strings.Contains(name, "read")
}

// isShellTool returns true if the tool name is shell or bash.
func isShellTool(name string) bool {
	name = strings.ToLower(name)
	return name == "shell" || name == "bash" || name == "shell_execute"
}

// stringifyArg converts an any argument value to its string representation.
func stringifyArg(v any) string {
	return fmt.Sprintf("%v", v)
}

// referencesCallID checks whether any argument value (when stringified)
// contains a reference to the given call ID. Recognized patterns:
//   - "$<callID>.result"
//   - "<callID>.result"
func referencesCallID(args map[string]any, callID string) bool {
	if callID == "" {
		return false
	}
	needle1 := "$" + callID + ".result"
	needle2 := callID + ".result"
	for _, v := range args {
		s := stringifyArg(v)
		if strings.Contains(s, needle1) || strings.Contains(s, needle2) {
			return true
		}
	}
	return false
}
