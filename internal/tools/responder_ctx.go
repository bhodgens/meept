package tools

import "context"

// turnResponderCtxKey carries the PER-TURN user-response callback injected
// by the agent loop (A6-6, bughunt 2026-11-25). Tools like builtin
// exit_plan_mode are registered ONCE on the shared tool registry and the
// loop's wireResponseSetters seam mutates their per-turn state; with two
// concurrent session loops a struct-field injection races and leaks
// cross-session responders. The loop therefore ALSO stamps the responder
// on the tool-execution context (the same seam as ContextWithSessionID /
// ContextWithWorkingDir) and the tool prefers the context value: every
// concurrent turn carries its OWN responder.
type turnResponderCtxKey struct{}

// TurnResponder is the per-turn user-response callback shape (matches
// builtin.ResponseFunc structurally: question + options in, free-text
// reply out; ctx-cancellable).
type TurnResponder func(ctx context.Context, question string, options []string) (string, error)

// ContextWithTurnResponder returns a new context carrying the loop's
// per-turn responder. A nil func stores nothing (no responder = the
// tool's legacy fallback field, unchanged behavior for direct callers).
func ContextWithTurnResponder(ctx context.Context, fn TurnResponder) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, turnResponderCtxKey{}, fn)
}

// TurnResponderFromContext extracts the per-turn responder from ctx, if
// set. Returns nil when not present (legacy/non-loop caller).
func TurnResponderFromContext(ctx context.Context) TurnResponder {
	if v, ok := ctx.Value(turnResponderCtxKey{}).(TurnResponder); ok {
		return v
	}
	return nil
}
