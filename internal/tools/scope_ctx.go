package tools

import (
	"context"
)

// ScopeIdentity is the session memory-scope identity carried in the tool
// execution context (leaf 03-agent-recall-wiring T3/T4). The agent loop
// resolves the B1 tuple (agent MemoryMaxScope + session project/user
// identity) once per turn and injects it alongside the working directory;
// the memory tools stamp writes and filter reads from it. Zero value =
// no identity (legacy scope-blind behavior, byte-identical).
type ScopeIdentity struct {
	// MaxScope is the caller's widest recall scope: "project", "user", or
	// "global" (memory.Scope vocabulary).
	MaxScope string
	// ProjectID is the session's bound project ("" unbound — never a CWD
	// fallback).
	ProjectID string
	// UserID is the session's multiuser identity ("" single-user).
	UserID string
	// MultiUser is the Flag-3 bit: true under enabled multiuser, so an
	// empty UserID fails closed instead of matching user-scope rows.
	MultiUser bool
}

// scopeIdentityCtxKey is the context key for ScopeIdentity.
type scopeIdentityCtxKey struct{}

// ContextWithScopeIdentity returns ctx carrying the session's memory-scope
// identity. A zero MaxScope stores nothing (no identity = legacy behavior).
func ContextWithScopeIdentity(ctx context.Context, id ScopeIdentity) context.Context {
	if id.MaxScope == "" {
		return ctx
	}
	return context.WithValue(ctx, scopeIdentityCtxKey{}, id)
}

// ScopeIdentityFromContext extracts the session memory-scope identity from
// ctx. Returns the zero value when absent.
func ScopeIdentityFromContext(ctx context.Context) ScopeIdentity {
	if v, ok := ctx.Value(scopeIdentityCtxKey{}).(ScopeIdentity); ok {
		return v
	}
	return ScopeIdentity{}
}
