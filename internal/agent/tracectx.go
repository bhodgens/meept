package agent

import "context"

// traceIDContextKey is the unexported context key for the request-scoped
// trace identifier (llm-hop-tracing leaf 01). Unexported so only this
// package can mint trace-scoped contexts; readers everywhere use
// TraceIDFromContext.
type traceIDContextKey struct{}

// WithTraceContext attaches the turn's trace ID to the turn's context --
// the request-scoped channel from the handler's mint seam into the agent
// loop and the tool-executor hand-off. An empty traceID returns ctx
// unchanged (no scope = the pre-leaf posture; never a sentinel value).
// This mirrors the llm.WithTraceID ChatOption contract: per-request state
// on the context, never a field on the shared loop.
func WithTraceContext(ctx context.Context, traceID string) context.Context {
	if traceID == "" {
		return ctx
	}
	return context.WithValue(ctx, traceIDContextKey{}, traceID)
}

// TraceIDFromContext reports the request's trace ID ("" when the context
// carries none). Readers: the loop's tool-executor hand-off stamps the
// same turn trace onto ToolExecutionEndData; leaf 03's span producers
// subscribe to those events.
func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(traceIDContextKey{}).(string)
	return v
}
