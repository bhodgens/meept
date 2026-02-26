// Package llm provides LLM client functionality for OpenAI-compatible APIs.
package llm

import "context"

// Chatter is the interface for LLM chat operations.
// Both Client and ProviderManager implement this interface.
type Chatter interface {
	// Chat sends a chat completion request and returns the parsed response.
	Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error)
}

// Ensure implementations satisfy the interface
var (
	_ Chatter = (*Client)(nil)
	_ Chatter = (*ProviderManager)(nil)
)
