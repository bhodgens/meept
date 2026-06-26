package tools

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

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

// CanonicalName returns the canonical registration name for a tool.
// The canonical name is the value returned by Tool.Name(), normalized to
// lowercase with surrounding whitespace trimmed. This is the single source
// of truth for how tool names appear in constitution tool references
// (tools_allowed / tools_forbidden) and in the tool registry's lookup key.
//
// C3: This function exists so that constitution tool references can be
// validated against canonical names at load time. Callers should use this
// function when comparing user-provided or constitution-declared tool
// names to the registry — never compare raw strings directly.
func CanonicalName(tool Tool) string {
	if tool == nil {
		return ""
	}
	return normalizeToolName(tool.Name())
}

// normalizeToolName lowercases and trims a tool name string so that
// constitution references (which may have inconsistent casing or
// whitespace) match against the canonical registry key. Non-nil tool
// objects already return canonical-cased names from Name(), but
// user-supplied strings in constitution JSON may not.
func normalizeToolName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// CanonicalNames returns a sorted list of all canonical tool names
// currently registered. This is the set against which constitution
// tool references should be validated.
func (r *Registry) CanonicalNames() map[string]struct{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make(map[string]struct{}, len(r.tools))
	for name := range r.tools {
		out[normalizeToolName(name)] = struct{}{}
	}
	return out
}

// IsCanonicalName reports whether the given name matches a registered
// tool after normalization. Returns false for empty strings.
func (r *Registry) IsCanonicalName(name string) bool {
	key := normalizeToolName(name)
	if key == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for registered := range r.tools {
		if normalizeToolName(registered) == key {
			return true
		}
	}
	return false
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
		return NewErrorResultErr(err), nil
	}

	// If result is already a ToolResult, return it directly to preserve evidence
	if tr, ok := result.(*ToolResult); ok {
		return tr, nil
	}
	// Also handle value-type ToolResult (some tools return value not pointer)
	if tr, ok := result.(ToolResult); ok {
		return &tr, nil
	}

	return NewSuccessResult(result), nil
}

// GetDefinitions returns tool definitions for the LLM.
// This is an alias for ToLLMDefinitions for compatibility with agent.ToolRegistry.
func (r *Registry) GetDefinitions() []llm.ToolDefinition {
	return r.ToLLMDefinitions()
}

// ToolInfo holds basic tool metadata for listing/reporting.
type ToolInfo struct {
	Name        string
	Description string
}

// CategoryTools groups tools by their category.
type CategoryTools struct {
	Category string
	Tools    []ToolInfo
}

// ToolsByCategory returns all registered tools grouped by their category.
// Tools that don't implement Categorizer are placed in the "general" category.
// Returns a map of category name -> sorted list of tool names.
func (r *Registry) ToolsByCategory() map[string][]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string][]string)
	for _, tool := range r.tools {
		cat := GetCategory(tool)
		result[cat] = append(result[cat], tool.Name())
	}

	// Sort tool names within each category
	for cat := range result {
		sort.Strings(result[cat])
	}

	return result
}

// CategorizedTools returns all registered tools grouped by category with their descriptions.
// Returns a sorted slice of CategoryTools, ordered by category name.
func (r *Registry) CategorizedTools() []CategoryTools {
	r.mu.RLock()
	defer r.mu.RUnlock()

	catMap := make(map[string][]ToolInfo)
	for _, tool := range r.tools {
		cat := GetCategory(tool)
		catMap[cat] = append(catMap[cat], ToolInfo{
			Name:        tool.Name(),
			Description: tool.Description(),
		})
	}

	result := make([]CategoryTools, 0, len(catMap))
	for cat, tools := range catMap {
		sort.Slice(tools, func(i, j int) bool {
			return tools[i].Name < tools[j].Name
		})
		result = append(result, CategoryTools{
			Category: cat,
			Tools:    tools,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Category < result[j].Category
	})

	return result
}

// ToolRetryPolicy defines retry semantics for a specific tool.
//
//nolint:revive // stutter with package name is intentional for API clarity
type ToolRetryPolicy struct {
	MaxRetries      int              // Maximum number of retry attempts
	RetryDelay      time.Duration    // Base delay between retries
	Exponential     bool             // Use exponential backoff (delay * 2^attempt)
	Retryable       bool             // Whether retries are allowed
	RetryableErrors []*regexp.Regexp // Patterns for retryable errors (nil = all errors retryable)
}

// defaultRetryPolicies defines retry semantics for builtin tools.
var defaultRetryPolicies = map[string]ToolRetryPolicy{
	// File operations - writes are not retryable (side effects)
	"file_read": {
		MaxRetries:  1,
		RetryDelay:  100 * time.Millisecond,
		Exponential: false,
		Retryable:   true,
	},
	"file_write": {
		MaxRetries: 0,
		Retryable:  false, // Side effects - may cause duplication
	},
	"delete_file": {
		MaxRetries: 0,
		Retryable:  false, // Side effects
	},
	"list_directory": {
		MaxRetries:  1,
		RetryDelay:  100 * time.Millisecond,
		Exponential: false,
		Retryable:   true,
	},

	// Shell execution - not retryable due to side effects
	"shell": {
		MaxRetries: 0,
		Retryable:  false,
	},

	// Web operations - highly retryable (network failures)
	"web_fetch": {
		MaxRetries:  2,
		RetryDelay:  1 * time.Second,
		Exponential: true,
		Retryable:   true,
	},
	"web_search": {
		MaxRetries:  2,
		RetryDelay:  1 * time.Second,
		Exponential: true,
		Retryable:   true,
	},

	// Memory operations - retryable (transient DB locks)
	"memory_read": {
		MaxRetries: 1,
		RetryDelay: 100 * time.Millisecond,
		Retryable:  true,
	},
	"memory_write": {
		MaxRetries: 1,
		RetryDelay: 100 * time.Millisecond,
		Retryable:  true,
	},
	"memory_search": {
		MaxRetries: 1,
		RetryDelay: 100 * time.Millisecond,
		Retryable:  true,
	},

	// Task operations - not retryable (state changes)
	"task_create": {
		MaxRetries: 0,
		Retryable:  false,
	},
	"task_update": {
		MaxRetries: 0,
		Retryable:  false,
	},

	// Platform operations - depends on operation
	"platform_agents": {
		MaxRetries: 1,
		RetryDelay: 100 * time.Millisecond,
		Retryable:  true,
	},
	"platform_tools": {
		MaxRetries: 1,
		RetryDelay: 100 * time.Millisecond,
		Retryable:  true,
	},
	"platform_status": {
		MaxRetries: 1,
		RetryDelay: 100 * time.Millisecond,
		Retryable:  true,
	},

	// Default - conservative retry for unknown tools
	"default": {
		MaxRetries: 0,
		Retryable:  false,
	},
}

// getRetryPolicy returns the retry policy for a tool.
func getRetryPolicy(toolName string) ToolRetryPolicy {
	policy, ok := defaultRetryPolicies[toolName]
	if !ok {
		policy = defaultRetryPolicies["default"]
	}
	return policy
}

// isRetryableError checks if an error matches retryable patterns.
func isRetryableError(errMsg string, patterns []*regexp.Regexp) bool {
	if len(patterns) == 0 {
		return true // All errors retryable
	}
	for _, pattern := range patterns {
		if pattern.MatchString(errMsg) {
			return true
		}
	}
	return false
}

// ExecuteWithRetry executes a tool with retry semantics based on tool-specific policies.
// Returns the result of the first successful execution or the last error.
func (r *Registry) ExecuteWithRetry(ctx context.Context, name string, args map[string]any) (*ToolResult, error) {
	policy := getRetryPolicy(name)

	if !policy.Retryable {
		// No retry - execute once
		return r.Execute(ctx, name, args)
	}

	var lastErr error
	var lastResult *ToolResult

	for attempt := 0; attempt <= policy.MaxRetries; attempt++ {
		result, err := r.Execute(ctx, name, args)

		if err == nil && result != nil && result.Success {
			// Success - return immediately
			return result, nil
		}

		// Record error for potential return
		if err != nil {
			lastErr = err
		} else if result != nil && result.Error != "" {
			lastErr = fmt.Errorf("%s", result.Error)
			lastResult = result
		}

		// Guard against nil lastErr (shouldn't happen, but defensive)
		if lastErr == nil {
			lastErr = fmt.Errorf("unknown error during tool execution")
		}

		// Check if error is retryable
		if !isRetryableError(lastErr.Error(), policy.RetryableErrors) {
			// Non-retryable error - fail immediately
			r.logger.Debug("Tool execution failed with non-retryable error",
				"name", name,
				"attempt", attempt+1,
				"error", lastErr,
			)
			if lastResult != nil {
				return lastResult, nil
			}
			return NewErrorResultErr(lastErr), nil
		}

		// Wait before retry (if not last attempt)
		if attempt < policy.MaxRetries {
			delay := policy.RetryDelay
			if policy.Exponential {
				// Cap the shift exponent to prevent overflow for very high
				// MaxRetries values. 1 << 30 (~17 minutes at 1s base) is the
				// practical ceiling before time.Duration int64 overflow.
				shift := min(attempt, 30)
				delay *= time.Duration(1 << uint(shift))
			}

			select {
			case <-ctx.Done():
				return NewErrorResultErr(ctx.Err()), ctx.Err()
			case <-time.After(delay):
				r.logger.Debug("Retrying tool execution",
					"name", name,
					"attempt", attempt+2,
					"delay", delay,
				)
			}
		}
	}

	// All retries exhausted - guard against nil lastErr
	if lastErr == nil {
		lastErr = fmt.Errorf("tool execution failed with no error recorded")
	}

	r.logger.Warn("Tool execution failed after all retries",
		"name", name,
		"max_retries", policy.MaxRetries,
		"error", lastErr,
	)

	if lastResult != nil {
		return lastResult, nil
	}
	return NewErrorResultErr(lastErr), nil
}

// Executor is a Registry that implements ToolExecutor.
var _ ToolExecutor = (*Registry)(nil)
