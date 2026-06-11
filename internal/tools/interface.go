// Package tools provides tool registration and execution for the meept daemon.
//
// Tools are the primary mechanism by which the LLM agent interacts with the
// system. Each tool defines its parameters via JSON Schema and implements an
// Execute method that performs the actual work.
package tools

import (
	"context"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/models"
)

// Tool is the interface that all meept tools must implement.
//
// A tool provides metadata (name, description, parameters) that is sent to the
// LLM so it can decide when to invoke the tool, and an Execute method that
// performs the actual action.
//
//nolint:revive // stutter with package name is intentional for API clarity
type Tool interface {
	// Name returns the unique identifier for this tool.
	// Tool names should be snake_case (e.g., "file_read", "shell_execute").
	Name() string

	// Description returns a human-readable description of what the tool does.
	// This is included in the LLM prompt to help it decide when to use the tool.
	Description() string

	// Parameters returns the JSON Schema parameters for this tool.
	// The schema uses the OpenAI function calling format.
	Parameters() llm.FunctionParameters

	// Execute runs the tool with the given arguments.
	// Arguments are parsed from the JSON provided by the LLM.
	// Returns the result as a map, or an error if execution fails.
	Execute(ctx context.Context, args map[string]any) (any, error)
}

// ToolResult is the standardized result envelope returned by tool execution.
//
//nolint:revive // stutter with package name is intentional for API clarity
type ToolResult struct {
	Success   bool              `json:"success"`
	Result    any               `json:"result,omitempty"`
	Error     string            `json:"error,omitempty"`
	Evidence  []models.Evidence `json:"evidence,omitempty"`
	Terminate bool              `json:"terminate,omitempty"` // Advisory: hint that result is final and needs no LLM follow-up
}

// NewSuccessResult creates a successful tool result.
func NewSuccessResult(result any) *ToolResult {
	return &ToolResult{
		Success: true,
		Result:  result,
	}
}

// NewSuccessResultWithTerminate creates a successful tool result that signals
// the agent loop to skip LLM follow-up processing.
func NewSuccessResultWithTerminate(result any) *ToolResult {
	return &ToolResult{
		Success:   true,
		Result:    result,
		Terminate: true,
	}
}

// NewErrorResult creates a failed tool result.
func NewErrorResult(err string) *ToolResult {
	return &ToolResult{
		Success: false,
		Error:   err,
	}
}

// ToolExecutor wraps a Tool and provides permission checking and auditing.
//
//nolint:revive // stutter with package name is intentional for API clarity
type ToolExecutor interface {
	// Execute runs a tool by name with the given arguments.
	// Returns the result or an error if the tool is not found or execution fails.
	Execute(ctx context.Context, toolName string, args map[string]any) (*ToolResult, error)
}

// PreviewResult describes a deferred action awaiting approval.
type PreviewResult struct {
	// Description is a human-readable summary of what the action will do.
	Description string `json:"description"`
	// Diff is an optional diff or preview content showing the proposed changes.
	Diff string `json:"diff,omitempty"`
	// ToolName is the name of the tool that produced this preview.
	ToolName string `json:"tool_name"`
	// ToolArgs are the original args for Apply/Discard.
	ToolArgs map[string]any `json:"tool_args"`
}

// Deferrable is an optional interface that tools implement to support a
// preview-then-apply workflow. When the agent loop encounters a tool that
// implements Deferrable, it calls Preview first. The result is staged until
// the agent explicitly resolves it via the "resolve" tool.
type Deferrable interface {
	// Preview describes what the tool would do without performing the action.
	Preview(ctx context.Context, args map[string]any) (PreviewResult, error)
	// Apply executes the deferred action.
	Apply(ctx context.Context, args map[string]any) (any, error)
	// Discard cleans up any staged state for the deferred action.
	Discard(ctx context.Context, args map[string]any) error
}

// Categorizer is an optional interface that tools can implement to declare
// their functional category. Tools that don't implement Categorizer are
// assigned to the "general" category.
type Categorizer interface {
	Category() string
}

// GetCategory returns the tool's category, or "general" if it doesn't implement Categorizer.
func GetCategory(t Tool) string {
	if c, ok := t.(Categorizer); ok {
		cat := c.Category()
		if cat != "" {
			return cat
		}
	}
	return "general"
}

// PTYTool is a tool that supports interactive PTY sessions for real-time
// streaming (e.g. gdb, ipython, long-running servers).
type PTYTool interface {
	Tool

	// CreateSession creates a new PTY session.
	CreateSession(sessionID string, config PTYSessionConfig) (*PTYSessionInfo, error)

	// WriteToSession sends input to a PTY session.
	WriteToSession(sessionID string, input []byte) error

	// ReadFromSession reads output from a PTY session (context-aware).
	ReadFromSession(ctx context.Context, sessionID string) ([]byte, error)

	// CloseSession terminates a PTY session.
	CloseSession(sessionID string) error

	// SessionOutput returns a channel for streaming session output.
	SessionOutput(sessionID string) (<-chan []byte, error)
}

// PTYSessionConfig holds PTY session configuration.
type PTYSessionConfig struct {
	Cmd     string            `json:"cmd"`
	Args    []string          `json:"args,omitempty"`
	Dir     string            `json:"dir,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Rows    int               `json:"rows"`
	Cols    int               `json:"cols"`
	Timeout time.Duration     `json:"-"`
}

// PTYSessionInfo holds session metadata returned to clients.
type PTYSessionInfo struct {
	ID        string    `json:"id"`
	Cmd       string    `json:"cmd"`
	Args      []string  `json:"args,omitempty"`
	Dir       string    `json:"dir,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Rows      int       `json:"rows"`
	Cols      int       `json:"cols"`
	IsRunning bool      `json:"is_running"`
}
