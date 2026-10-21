package agent

import (
	"context"

	"github.com/caimlas/meept/internal/llm"
)

// scopeOptions returns the loop's current task/session scope as prepended
// ChatOptions. Mirrors chatWithFailoverRaw's prepending (loop.go, task-scope
// block): same fields, same lock, same AUDIT FIX H11 restriction — the
// ACCOUNTING session id comes from the dedicated per-turn field
// (turnAccountingSessionID); currentSessionID is a session-identity field
// and is deliberately NOT consulted. Secondary LLM call sites that bypass
// chatWithFailoverRaw (reflection, RLM analysis, trace synthesis, intent
// analysis, classification, reflection collection, skill state runtime) call
// this so their requests carry the same WithTaskScope option. Returns nil
// when both IDs are empty — downstream clients skip empty scope values, and
// no placeholder IDs are invented.
func (l *AgentLoop) scopeOptions() []llm.ChatOption {
	l.mu.RLock()
	taskID, sessionID := l.currentTaskID, l.turnAccountingSessionID
	l.mu.RUnlock()
	if taskID == "" && sessionID == "" {
		return nil
	}
	return []llm.ChatOption{llm.WithTaskScope(taskID, sessionID)}
}

// chatScopeProvider is the seam secondary clients use to obtain the owning
// loop's task/session scope. *AgentLoop satisfies it via scopeOptions.
// Secondary components (ReflectionEngine, RLMAnalyzer, SynthesizeTracesTool,
// IntentAnalyzer, LLMClassifier, ReflectionCollector, SkillStateRuntime)
// hold one optionally: nil means no scope is known — their calls stay
// unchanged, matching the pre-threading behavior instead of inventing IDs.
type chatScopeProvider interface {
	scopeOptions() []llm.ChatOption
}

// prependScope returns caller opts with the provider's scope options
// PREPENDED — scope first in the slice, mirroring chatWithFailoverRaw's
// append order (loop.go task-scope block). A nil provider or nil/empty
// scope leaves the caller opts untouched. Used by every secondary LLM call
// site threaded by leaf 01 of docs/plans/20261016-session-affinity.
func prependScope(provider chatScopeProvider, opts []llm.ChatOption) []llm.ChatOption {
	if provider == nil {
		return opts
	}
	scope := provider.scopeOptions()
	if len(scope) == 0 {
		return opts
	}
	return append(scope, opts...)
}

// --- Per-call scope threading (context seam) ---
//
// Singleton components (ReflectionEngine, RLMAnalyzer,
// SynthesizeTracesTool, ReflectionCollector) are shared across sessions,
// so a wire-once scopeProvider on them is either unwired (dead) or a
// cross-session hazard (the ReflectionCollector race the leaf-01 audit
// found). The per-call alternative: the CALLER — which knows the serving
// session — attaches the captured scope options to the context, and the
// secondary call site prefers them over any wired provider.

type scopeOptionsCtxKey struct{}

// WithScopeOptions attaches caller-captured task/session scope options to
// ctx for secondary LLM call sites (resolveCallScope). Nil/empty opts are
// a no-op passthrough.
func WithScopeOptions(ctx context.Context, opts []llm.ChatOption) context.Context {
	if ctx == nil || len(opts) == 0 {
		return ctx
	}
	return context.WithValue(ctx, scopeOptionsCtxKey{}, opts)
}

// resolveCallScope returns the per-call scope (WithScopeOptions) prepended
// to opts when present, else falls back to the component's wired
// scopeProvider (prependScope). Same ordering contract: scope options
// first.
func resolveCallScope(ctx context.Context, provider chatScopeProvider, opts []llm.ChatOption) []llm.ChatOption {
	if ctx != nil {
		if scoped, ok := ctx.Value(scopeOptionsCtxKey{}).([]llm.ChatOption); ok && len(scoped) > 0 {
			return append(append([]llm.ChatOption(nil), scoped...), opts...)
		}
	}
	return prependScope(provider, opts)
}

// SetScopeProvider wires the owning agent loop so fix-request LLM calls
// carry the loop's current task/session scope (leaf 01
// session-affinity). Nil guard prevents typed-nil panic.
func (re *ReflectionEngine) SetScopeProvider(provider chatScopeProvider) {
	if provider == nil {
		return
	}
	re.scopeProvider = provider
}
