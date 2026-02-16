package tools

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/caimlas/meept/internal/llm"
)

// Registry maintains a collection of registered tools.
//
// The registry is thread-safe and can be used concurrently by multiple
// goroutines. Tools can be registered and unregistered at runtime.
type Registry struct {
	mu     sync.RWMutex
	tools  map[string]Tool
	logger *slog.Logger
}

// NewRegistry creates a new empty tool registry.
func NewRegistry(logger *slog.Logger) *Registry {
	if logger == nil {
		logger = slog.Default()
	}
	return &Registry{
		tools:  make(map[string]Tool),
		logger: logger,
	}
}

// Register adds a tool to the registry.
// If a tool with the same name already exists, it will be replaced.
func (r *Registry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := tool.Name()
	if _, exists := r.tools[name]; exists {
		r.logger.Warn("replacing existing tool registration", "name", name)
	}
	r.tools[name] = tool
	r.logger.Info("registered tool", "name", name)
}

// Unregister removes a tool from the registry.
// Returns an error if the tool is not found.
func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[name]; !exists {
		return fmt.Errorf("tool not found: %s", name)
	}
	delete(r.tools, name)
	r.logger.Info("unregistered tool", "name", name)
	return nil
}

// Get returns a tool by name, or nil if not found.
func (r *Registry) Get(name string) Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name]
}

// List returns all registered tools.
func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := make([]Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}

	// Sort by name for consistent ordering
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name() < tools[j].Name()
	})

	return tools
}

// Names returns a sorted list of all registered tool names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Count returns the number of registered tools.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// ToLLMDefinitions converts all registered tools to LLM tool definitions.
// This format is suitable for passing to the LLM client's tools parameter.
func (r *Registry) ToLLMDefinitions() []llm.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	definitions := make([]llm.ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		def := llm.NewToolDefinition(
			tool.Name(),
			tool.Description(),
			tool.Parameters(),
		)
		definitions = append(definitions, def)
	}

	// Sort by name for consistent ordering
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Function.Name < definitions[j].Function.Name
	})

	return definitions
}

// Execute runs a tool by name with the given arguments.
// Returns an error if the tool is not found.
func (r *Registry) Execute(ctx context.Context, name string, args map[string]any) (*ToolResult, error) {
	tool := r.Get(name)
	if tool == nil {
		return nil, fmt.Errorf("tool not found: %s", name)
	}

	r.logger.Debug("executing tool",
		"name", name,
		"args", args,
	)

	result, err := tool.Execute(ctx, args)
	if err != nil {
		r.logger.Warn("tool execution failed",
			"name", name,
			"error", err,
		)
		return NewErrorResult(err.Error()), nil
	}

	return NewSuccessResult(result), nil
}

// Executor is a Registry that implements ToolExecutor.
var _ ToolExecutor = (*Registry)(nil)
