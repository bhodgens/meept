package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
)

// DefaultToolViewLRUSize is the default capacity of the tool_view definition
// cache when the constructor receives a non-positive size.
const DefaultToolViewLRUSize = 32

// ToolViewTool returns the full parameter schema for a registered tool by
// name. It is the expansion mechanism for indexed schema mode: the registry
// stubs non-core tools to a one-line description, and the model calls
// tool_view{name} to load the complete definition on demand.
//
// Expansions are LRU-cached so repeated views of the same tool do not
// re-marshal large schemas. Cached values are captured from reg.Get(name) —
// the full-fidelity tool itself — so definitions are guaranteed complete even
// while the registry is in indexed mode (stubbing happens only in
// GetDefinitions/ToLLMDefinitions, a different path).
type ToolViewTool struct {
	tools.ToolDefaults
	reg *tools.Registry

	mu      sync.Mutex
	entries map[string][]byte // tool name -> JSON-marshalled definition
	order   []string          // LRU order; oldest first
	size    int               // capacity
}

// NewToolViewTool creates a new tool_view tool. lruSize <= 0 defaults to
// DefaultToolViewLRUSize (32).
func NewToolViewTool(reg *tools.Registry, lruSize int) *ToolViewTool {
	if lruSize <= 0 {
		lruSize = DefaultToolViewLRUSize
	}
	return &ToolViewTool{
		reg:     reg,
		entries: make(map[string][]byte, lruSize),
		order:   make([]string, 0, lruSize),
		size:    lruSize,
	}
}

func (t *ToolViewTool) Name() string { return "tool_view" }

func (t *ToolViewTool) Category() string { return "meta" }

func (t *ToolViewTool) Description() string {
	return "Return the full parameter schema for a named tool as JSON. " +
		"Use this to expand a tool whose schema was summarized to a one-line description before calling it."
}

func (t *ToolViewTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropName: {
				Type:        schemaTypeString,
				Description: "The registered tool name to view, e.g. \"web_fetch\".",
			},
		},
		Required: []string{schemaPropName},
	}
}

// Execute expands a tool name to its full llm.ToolDefinition JSON.
//
// Mutex scope: the LRU map is snapshotted/mutated under t.mu, but JSON
// marshaling and the registry lookup happen outside it (mutexio).
func (t *ToolViewTool) Execute(_ context.Context, args map[string]any) (any, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	t.mu.Lock()
	cached, ok := t.entries[name]
	t.mu.Unlock()
	if ok {
		return t.viewResult(name, cached), nil
	}

	if t.reg == nil {
		return nil, fmt.Errorf("tool registry not available")
	}

	tool := t.reg.Get(name)
	if tool == nil {
		return nil, fmt.Errorf("tool not found: %s", name)
	}

	// Build and marshal the full-fidelity definition outside the LRU lock.
	// reg.Get returns the live tool, so the definition is complete even in
	// indexed schema mode.
	def := llm.NewToolDefinition(tool.Name(), tool.Description(), tool.Parameters())
	raw, err := json.Marshal(def)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tool definition for %s: %w", name, err)
	}

	t.mu.Lock()
	t.record(name, raw)
	t.mu.Unlock()

	return t.viewResult(name, raw), nil
}

// viewResult wraps the definition JSON in the standard tool-result envelope
// with an evidence record, mirroring the value-type ToolResult pattern used
// by other builtins (web_fetch).
func (t *ToolViewTool) viewResult(name string, raw []byte) tools.ToolResult {
	return tools.ToolResult{
		Success: true,
		Result:  string(raw),
		Evidence: []models.Evidence{
			models.NewEvidence(
				models.EvidenceAPIResponse,
				name,
				fmt.Sprintf("bytes=%d", len(raw)),
				t.Name(),
			),
		},
	}
}

// record inserts or refreshes an entry and evicts the least-recently-used
// item when over capacity. Caller must hold t.mu.
func (t *ToolViewTool) record(name string, raw []byte) {
	if _, exists := t.entries[name]; !exists && len(t.entries) >= t.size {
		oldest := t.order[0]
		delete(t.entries, oldest)
		t.order = t.order[1:]
	}
	if _, exists := t.entries[name]; !exists {
		t.order = append(t.order, name)
	} else {
		t.touchLocked(name)
	}
	t.entries[name] = raw
}

// touchLocked moves name to the most-recently-used end of the order slice.
// Caller must hold t.mu.
func (t *ToolViewTool) touchLocked(name string) {
	for i, n := range t.order {
		if n == name {
			t.order = append(t.order[:i], t.order[i+1:]...)
			break
		}
	}
	t.order = append(t.order, name)
}

// lruLen reports the current cache entry count. Test-only introspection.
func (t *ToolViewTool) lruLen() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.entries)
}

// IsReadOnly reports that viewing a tool schema never mutates state.
func (t *ToolViewTool) IsReadOnly(map[string]any) bool { return true }

// IsConcurrencySafe reports that concurrent views are safe.
func (t *ToolViewTool) IsConcurrencySafe(map[string]any) bool { return true }

// Ensure ToolViewTool implements the Tool interface.
var _ tools.Tool = (*ToolViewTool)(nil)
