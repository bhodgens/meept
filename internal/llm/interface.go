// Package llm provides LLM client functionality for OpenAI-compatible APIs.
package llm

import "context"

// ProgressStage represents the current stage of an LLM request.
type ProgressStage int

const (
	// ProgressStageStarting is the initial stage when the request is being prepared.
	ProgressStageStarting ProgressStage = iota
	// ProgressStageThinking is when the model is processing the request.
	ProgressStageThinking
	// ProgressStageStreaming is when the response is being received.
	ProgressStageStreaming
	// ProgressStageToolCall is when the model is making tool calls.
	ProgressStageToolCall
	// ProgressStageDone is when the request is complete.
	ProgressStageDone
)

// ProgressCallback is a function that reports progress during an LLM request.
// The stage parameter indicates the current stage, and detail provides
// human-readable information about the progress.
type ProgressCallback func(stage ProgressStage, detail string)

// Chatter is the interface for LLM chat operations.
// Both Client and ProviderManager implement this interface.
type Chatter interface {
	// Chat sends a chat completion request and returns the parsed response.
	Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error)

	// ChatWithProgress sends a chat completion request with progress reporting.
	// The progress callback is invoked at various stages of the request lifecycle.
	// If progress is nil, no progress reporting is done.
	ChatWithProgress(ctx context.Context, messages []ChatMessage, progress ProgressCallback, opts ...ChatOption) (*Response, error)
}

// Ensure implementations satisfy the interface
var (
	_ Chatter = (*Client)(nil)
	_ Chatter = (*ProviderManager)(nil)
)
