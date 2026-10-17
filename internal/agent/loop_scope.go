package agent

import "github.com/caimlas/meept/internal/llm"

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

// SetScopeProvider wires the owning agent loop so fix-request LLM calls
// carry the loop's current task/session scope (leaf 01
// session-affinity). Nil guard prevents typed-nil panic.
func (re *ReflectionEngine) SetScopeProvider(provider chatScopeProvider) {
	if provider == nil {
		return
	}
	re.scopeProvider = provider
}

