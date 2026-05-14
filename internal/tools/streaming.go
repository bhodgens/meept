package tools

import (
	"context"
	"encoding/json"
)

// ProgressUpdate represents a streaming progress update from a tool.
type ProgressUpdate struct {
	// Message is a human-readable description of the current step.
	// Examples: "downloading file...", "parsing response...", "running command..."
	Message string `json:"message"`

	// Percent is completion progress from 0-100. Use -1 for indeterminate progress.
	Percent int `json:"percent"`

	// PartialResult is an optional partial result that the UI can display
	// before the final result arrives. For streaming outputs (shell, web_fetch),
	// this accumulates output incrementally.
	PartialResult json.RawMessage `json:"partial_result,omitempty"`

	// ToolCallID links this update to the specific tool call in the batch.
	ToolCallID string `json:"tool_call_id"`
}

// StreamingTool is an optional interface that tools can implement to emit
// progress updates during execution. The executor detects this interface
// at runtime and wires up the progress callback automatically.
type StreamingTool interface {
	// ExecuteStreaming runs the tool with a progress callback.
	// The tool calls onUpdate() with ProgressUpdate values during execution.
	// The tool MUST still satisfy Execute() semantics: return (result, error).
	ExecuteStreaming(ctx context.Context, args map[string]any, onUpdate func(ProgressUpdate)) (any, error)
}

// TerminatingTool is an optional interface that tools can implement to signal
// that their result does not need further LLM processing. The executor reads
// the TerminateHint() value and propagates it to ExecutionResult.
type TerminatingTool interface {
	// TerminateHint returns true if the tool's result is a final answer
	// that should be returned to the user without LLM follow-up.
	// This is advisory; the executor only acts on it when ALL tools in the
	// batch agree (unanimous consent).
	TerminateHint(args map[string]any) bool
}
