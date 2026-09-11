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

// autonomousCtxKey is the context key for the autonomous-execution flag.
type autonomousCtxKey struct{}

// ContextWithAutonomous marks the context as AUTONOMOUS execution: a
// job-driven, headless run where no human and no later interactive turn
// can follow up. Staging tools (file_write, file_edit) consult this to
// bypass the preview/accept workflow — a staged change in an autonomous
// run is a silent no-op (e2e run 8, 2026-09-11: file_write direct:"False"
// staged into the pending-changes registry, nothing ever resolved it, the
// step "completed", and the file never existed on disk).
func ContextWithAutonomous(ctx context.Context) context.Context {
	return context.WithValue(ctx, autonomousCtxKey{}, true)
}

// AutonomousFromContext reports whether ctx was marked autonomous via
// ContextWithAutonomous. Absent marker = interactive: staging applies.
func AutonomousFromContext(ctx context.Context) bool {
	v, ok := ctx.Value(autonomousCtxKey{}).(bool)
	return ok && v
}
