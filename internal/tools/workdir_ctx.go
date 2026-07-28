package tools

import "context"

// workingDirCtxKey is the context key for the session-scoped working
// directory injected by the agent loop before tool execution.
type workingDirCtxKey struct{}

// WorkingDirFromContext extracts the working directory from ctx, if set.
// Returns empty string when not present.
func WorkingDirFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(workingDirCtxKey{}).(string); ok {
		return v
	}
	return ""
}

// ContextWithWorkingDir returns a new context with the working directory
// injected. Used by the agent loop to give tools access to the session's
// project path without shared mutable state.
func ContextWithWorkingDir(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, workingDirCtxKey{}, dir)
}
