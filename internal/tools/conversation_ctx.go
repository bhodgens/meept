package tools

// conversation_ctx.go — the tool-context carrier for the calling loop's live
// parent conversation (DSH-1 fork-seed). It lives in package tools (not
// internal/agent) because internal/agent imports internal/tools, so an
// internal/agent home would be an import cycle; the builtin delegate tool
// already imports agent for DelegateManager/DelegateRequest, and reads the
// carrier through this package's key with an `any` value.
//
// Contract mirrors workdir_ctx.go's ContextWithSessionID (DSH-6): the agent
// loop injects the value before tool execution (executeToolCallsUngated);
// a context without it (legacy callers, direct tool tests) keeps the
// sessionless posture — fork then degrades to a fresh brief at the
// DelegateManager's gate, exactly as before this carrier existed.

import "context"

// conversationCtxKey is the context key for the calling loop's live
// conversation. The value is `any` holding *agent.Conversation: package
// tools cannot name the agent type (cycle), and the single consumer
// (builtin delegate_task) type-asserts it back.
type conversationCtxKey struct{}

// ConversationFromContext extracts the calling loop's live conversation
// from ctx, if set. Returns nil when not present (no fork-seed source).
// The value stays `any`: package tools cannot name *agent.Conversation
// (import cycle); the single consumer (builtin delegate_task) type-asserts.
func ConversationFromContext(ctx context.Context) any {
	return ctx.Value(conversationCtxKey{})
}

// ContextWithConversation returns a new context carrying the calling loop's
// live conversation (an `any`-boxed *agent.Conversation). A nil value
// stores nothing.
func ContextWithConversation(ctx context.Context, conv any) context.Context {
	if conv == nil {
		return ctx
	}
	return context.WithValue(ctx, conversationCtxKey{}, conv)
}
