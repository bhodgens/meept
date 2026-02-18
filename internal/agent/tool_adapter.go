// Package agent provides the agent loop and related components.
package agent

import (
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// ToolRegistryAdapter wraps a tools.Registry to implement ToolRegistry.
type ToolRegistryAdapter struct {
	registry *tools.Registry
}

// NewToolRegistryAdapter creates a new adapter.
func NewToolRegistryAdapter(registry *tools.Registry) *ToolRegistryAdapter {
	return &ToolRegistryAdapter{registry: registry}
}

// Get retrieves a tool by name.
func (a *ToolRegistryAdapter) Get(name string) Tool {
	t := a.registry.Get(name)
	if t == nil {
		return nil
	}
	return &toolWrapper{t}
}

// List returns all available tools.
func (a *ToolRegistryAdapter) List() []Tool {
	toolsList := a.registry.List()
	result := make([]Tool, len(toolsList))
	for i, t := range toolsList {
		result[i] = &toolWrapper{t}
	}
	return result
}

// GetDefinitions returns tool definitions for the LLM.
func (a *ToolRegistryAdapter) GetDefinitions() []llm.ToolDefinition {
	return a.registry.ToLLMDefinitions()
}

// toolWrapper wraps tools.Tool to implement agent.Tool.
type toolWrapper struct {
	tools.Tool
}

// Ensure toolWrapper implements Tool
var _ Tool = (*toolWrapper)(nil)
