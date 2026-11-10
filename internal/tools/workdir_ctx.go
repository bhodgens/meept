package tools

import (
	"context"
	"errors"
	"fmt"
)

// workingDirCtxKey is the context key for the session-scoped working
// directory injected by the agent loop before tool execution.
type workingDirCtxKey struct{}

// sessionIDCtxKey is the context key for the caller's session id injected by
// the agent loop before tool execution (DSH-6). Tools that register state
// under a parent session (delegate_task's C1 Spawn) read it so children key
// to the real caller session; an absent/empty value stays the legacy
// sessionless posture. This is the shared production seam — the builtin
// package's own sessionIDContextKey predates it and its readers migrate to
// this key so ONE injection site serves every consumer.
type sessionIDCtxKey struct{}

// SessionIDFromContext extracts the caller's session id from ctx, if set.
// Returns empty string when not present (no session attribution).
func SessionIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(sessionIDCtxKey{}).(string); ok {
		return v
	}
	return ""
}

// ContextWithSessionID returns a new context carrying the caller's session
// id. An empty id stores nothing (no attribution = legacy behavior).
func ContextWithSessionID(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionIDCtxKey{}, sessionID)
}

// agentIDCtxKey is the context key for the calling agent id injected by
// the agent loop before tool execution. Effects claimants read it so a
// resolve can be checked against the agent that claimed the key.
type agentIDCtxKey struct{}

// AgentIDFromContext extracts the caller's agent id from ctx, if set.
// Returns empty string when not present.
func AgentIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(agentIDCtxKey{}).(string); ok {
		return v
	}
	return ""
}

// ContextWithAgentID returns a new context carrying the caller's agent id.
// An empty id stores nothing.
func ContextWithAgentID(ctx context.Context, agentID string) context.Context {
	if agentID == "" {
		return ctx
	}
	return context.WithValue(ctx, agentIDCtxKey{}, agentID)
}

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

// ErrNoWorkingDir is the sentinel error for a filesystem call that needed a
// path, got no explicit path argument, and had no session working directory
// to default to. Callers detect it with errors.Is / IsNoWorkingDir.
//
// Before this existed the tools returned the bare "no path specified", which
// told the model nothing about WHY the call could not proceed: it retried the
// identical call and the cycle guard killed the turn (fresh-rig daemon11,
// 2026-09-13 — the daemon served turns whose session had no project and no
// client CWD, so every relative-path tool failed). The message now names the
// missing context and the one action that unblocks the call.
var ErrNoWorkingDir = errors.New("no working directory for this session; pass an explicit path")

// ErrNoPath is the sentinel error for a filesystem call that requires an
// explicit path argument and received none, on a tool that does NOT fall
// back to the session working directory (read_file, write_file, delete_file,
// file_edit). The session may well have a working directory; the caller
// simply omitted the argument.
var ErrNoPath = errors.New("no path specified; pass an explicit path")

// NoWorkingDirError reports that tool needed a path, had no path argument,
// and the session has no working directory to default to. The returned error
// wraps ErrNoWorkingDir (%w), so callers can detect it with
// IsNoWorkingDir / errors.Is.
func NoWorkingDirError(tool string) error {
	if tool == "" {
		return ErrNoWorkingDir
	}
	return fmt.Errorf("%s: %w", tool, ErrNoWorkingDir)
}

// NoPathError reports that tool requires an explicit path argument and got
// none. The returned error wraps ErrNoPath (%w).
func NoPathError(tool string) error {
	if tool == "" {
		return ErrNoPath
	}
	return fmt.Errorf("%s: %w", tool, ErrNoPath)
}

// IsNoWorkingDir reports whether err is, or wraps, ErrNoWorkingDir. This is
// the detection surface for callers that want to surface a "bind a working
// directory" hint (or a distinct user-language message) instead of a generic
// tool failure.
//
// STATUS (deadcode wave-3): available-but-unused API surface — zero
// production callers (tests only). The sentinel-detection contract
// (internal/agent/AGENTS.md) names this as how a CLIENT should detect the
// unbound-session refusal, but the TUI/GUI surfaces render the sentinel's
// message text verbatim today and no RPC/comm layer special-cases it.
// First caller (a client binding hint, a WS event classifier case, etc.)
// should keep errors.Is-compatible semantics.
func IsNoWorkingDir(err error) bool {
	return errors.Is(err, ErrNoWorkingDir)
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
