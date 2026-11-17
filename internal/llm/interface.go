// Package llm provides LLM client functionality for OpenAI-compatible APIs.
package llm

import (
	"context"
)

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

	// Config returns the current model configuration.
	Config() *ModelConfig
}

// WarmChatter is the warm-seam contract (plan
// 20261113-filler-clips-and-context-warm C4, hard rules 1 and 5): every
// client class the voice turn rides implements it so the daemon's
// context-warm closure can dispatch a cache-warming probe WITHOUT
// knowing the concrete provider. Warm is fire-and-forget: it NEVER
// returns a completion, NEVER re-dispatches (no retry loop), NEVER
// reads/writes a response cache, and NEVER records alias/endpoint/quota
// state. Failures are logged at DEBUG and swallowed by design.
//
// The signature is EXACTLY ChatOption-opts shaped; the alias / target
// resolution lives at the Resolver/ProviderManager layer (WithResolvedModel
// steers the ProviderManager's routing; a bare *Client serves whatever
// its own config holds). Callers may stamp WithMaxTokens(1) themselves
// or let the implementation force it.
type WarmChatter interface {
	Chatter
	Warm(ctx context.Context, messages []ChatMessage, opts ...ChatOption) error
}

// StreamingChatter is the interface for LLMs that support per-delta streaming.
// Time-traveling stream rules (TTSR) use this to abort mid-generation and
// retry with injected rule enforcement content.
type StreamingChatter interface {
	Chatter
	ChatWithDeltaCallback(ctx context.Context, messages []ChatMessage, onDelta DeltaCallback, opts ...ChatOption) (*Response, error)
}

// AsStreamingChatter attempts to cast a Chatter to a StreamingChatter.
// Returns nil, false if the underlying implementation does not support streaming.
func AsStreamingChatter(c Chatter) (StreamingChatter, bool) {
	sc, ok := c.(StreamingChatter)
	return sc, ok
}

// Ensure implementations satisfy the interface
var (
	_ Chatter = (*Client)(nil)
	_ Chatter = (*ProviderManager)(nil)
	_ Chatter = (*ContextFirewall)(nil)

	_ WarmChatter = (*Client)(nil)
	_ WarmChatter = (*ProviderManager)(nil)
	_ WarmChatter = (*AnthropicClient)(nil)
	_ WarmChatter = (*CodexClient)(nil)
)

// TokenResolver is implemented by token stores that can provide a valid access
// token for an OAuth provider. The llm package depends on this interface
// rather than importing the auth package directly, avoiding circular imports.
type TokenResolver interface {
	// ResolveToken returns a valid (possibly refreshed) access token for the
	// given provider. Implementations should handle token expiry and refresh
	// transparently.
	ResolveToken(ctx context.Context, provider string) (string, error)
}

// UploadStore provides access to uploaded file storage. The LLM client uses it
// to resolve file:// references into base64 data URLs for provider APIs.
type UploadStore interface {
	Load(ctx context.Context, id string) (data []byte, mimeType string, err error)
}
