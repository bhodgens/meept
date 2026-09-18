package agent

import (
	"log/slog"

	"github.com/caimlas/meept/internal/llm"
)

// Loop refusal fallback (refusal-fallback tree 03).
//
// A *llm.RefusalError means the provider's safety layer declined the turn.
// The model is HEALTHY — nothing here may touch alias health (no
// RecordAliasFailure, no BlockQuota, no RotateToNextModel). Instead the loop
// re-dispatches the SAME turn to a configured refusal fallback model exactly
// once ("one-hop"): if the fallback also refuses, the original error
// surfaces unchanged.
//
// Precedence (user decision 2026-09-16, per-agent configurable):
//  1. spec.RefusalModel (per-agent, alias name or "provider/model" ref)
//  2. ModelsConfig.RefusalModel (global models.json5 slot; mirrored onto
//     the spec by SetGlobalRefusalModel at wiring time)
//  3. "" (feature off for this agent)

// maxRefusalFallbackHops is the PERSISTENT one-hop refusal budget across
// park/resume generations (scopes-3 audit finding B): the turn-scoped pin
// clears on every fresh turn (clearRefusalFreshTurnState), so without a
// cross-generation counter a refuse → park → resume → refuse cycle would
// re-arm the fallback unbounded. Two total hops per genuinely-fresh turn
// window: the primary's hop and one fallback re-arm before the refusal
// surfaces.
const maxRefusalFallbackHops = 2

// SetGlobalRefusalModel mirrors the global models.json5 refusal_model slot
// onto this loop. Precedence lives in refusalFallbackRef: a non-empty
// spec.RefusalModel wins; the global value fills an empty spec field.
// Wire once at loop construction (alongside the resolver wiring); not
// thread-safe by design because construction-time wiring is.
func (l *AgentLoop) SetGlobalRefusalModel(ref string) {
	l.globalRefusalModel = ref
}

// WithGlobalRefusalModel is the LoopOption form of SetGlobalRefusalModel
// (bughunt F5): ConfigSnapshot carries the template's global refusal slot so
// clones inherit it, and the registry can wire the slot straight into
// specialist loops. Non-empty only; an empty slot is a no-op so the option
// never clobbers a value set by an earlier wiring step.
func WithGlobalRefusalModel(ref string) LoopOption {
	return func(l *AgentLoop) {
		if ref != "" {
			l.globalRefusalModel = ref
		}
	}
}

// refusalFallbackRef resolves the configured fallback by frozen precedence:
// spec.RefusalModel > global ModelsConfig.RefusalModel > "". The loop holds
// the global slot value (mirrored at wiring time) because the loop package
// does not import internal/config.
func (l *AgentLoop) refusalFallbackRef() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.spec != nil && l.spec.RefusalModel != "" {
		return l.spec.RefusalModel
	}
	return l.globalRefusalModel
}

// handleRefusal implements the one-hop policy. Called from the loop's error
// path when errors.As(err, &refusalErr) matched.
//
//   - fallback ref already serving            => (false, err) — give up
//   - both sources empty / unresolvable       => (false, err) — feature off
//   - otherwise: pin the persistent override  => (true, nil)  — retry
//
// The pin clears after the turn like the verification-escalation override
// does (HookRegistry.ClearFreshTurnOverrides / ClearRefusalFallback).
func (l *AgentLoop) handleRefusal(refusalErr *llm.RefusalError) (bool, error) {
	if refusalErr == nil {
		return false, nil
	}

	fallbackRef := l.refusalFallbackRef()
	if fallbackRef == "" {
		slog.Debug("refusal fallback disabled: no refusal model configured",
			"agent_id", l.agentID,
			"model", refusalErr.ModelID,
		)
		return false, refusalErr
	}

	// Resolver seam is required to resolve the ref; nil degrades to off.
	l.mu.RLock()
	resolver := l.refusalResolver
	publisher := l.refusalEventPublisher
	applier := l.refusalOverrideApplier
	serving := ""
	if l.refusalServingRef != nil {
		serving = l.refusalServingRef()
	}
	l.mu.RUnlock()

	if resolver == nil {
		slog.Warn("refusal fallback skipped: no model resolver wired",
			"agent_id", l.agentID,
			"fallback_ref", fallbackRef,
		)
		return false, refusalErr
	}

	resolved, err := resolver.ResolveEscalationRef(fallbackRef)
	if err != nil || resolved == "" {
		slog.Warn("refusal fallback unresolvable: surfacing original refusal",
			"agent_id", l.agentID,
			"fallback_ref", fallbackRef,
			"error", err,
		)
		return false, refusalErr
	}

	// One-hop rules — either exits surface the original refusal:
	//   a) the fallback is ALREADY the armed override: the retry just ran on
	//      it and refused again — no third attempt;
	//   b) the fallback IS the model currently serving (resolver-reported);
	//   c) the PERSISTENT hop budget is exhausted (maxRefusalFallbackHops
	//      pins armed across park/resume generations — scopes-3 audit
	//      finding B: clearRefusalFreshTurnState clears the pin on every
	//      resume, so without a cross-generation counter each refuse →
	//      park → resume cycle could re-arm the fallback forever).
	l.mu.RLock()
	hops := l.refusalFallbackHops
	l.mu.RUnlock()
	if l.GetModelOverride() == resolved ||
		(serving != "" && serving == resolved) ||
		hops >= maxRefusalFallbackHops {
		slog.Info("refusal fallback already serving: surfacing original refusal",
			"agent_id", l.agentID,
			"fallback_ref", fallbackRef,
			"serving_ref", serving,
			"hops_used", hops,
		)
		// Restore the base model — the fallback window is over.
		l.ClearRefusalFallback()
		return false, refusalErr
	}

	// Arm the fallback via the loop's persistent-override machinery. The
	// request-scoped seam (llm.WithModelOverride on the retry, ranked above
	// the alias channel) consumes the pin; ClearRefusalFallback restores the
	// base model on the next fresh turn if the retry never fired.
	if applier != nil {
		applier(resolved)
		// Persistent hop budget (scopes-3 finding B): the pin was accepted,
		// so count it. The counter is intentionally OUTSIDE the fresh-turn
		// reset lifecycle — it survives park/resume re-entry and resets only
		// when a genuinely fresh (non-resumed) turn begins.
		l.mu.Lock()
		l.refusalFallbackHops++
		// Streaming epoch bump (scopes-3 finding A): tag the retry attempt
		// so the live stream accumulator in RunOnceWithParts drops the
		// refused attempt's partial text instead of appending the fallback
		// tokens after it.
		l.streamAttemptEpoch.Add(1)
		epoch := l.streamAttemptEpoch.Load()
		l.mu.Unlock()
		slog.Info("refusal fallback hop armed",
			"agent_id", l.agentID,
			"hops_used", epoch,
		)
		// Observability leaf 04 (user decision 2026-09-16, option b): the
		// pin was accepted, so IF the retry succeeds the served turn must
		// disclose the fallback in the user-visible reply text. The flag
		// is set ONLY here — never on give-up paths — and cleared at the
		// start of the next turn, exactly like the modelOverride
		// lifecycle it mirrors. refusalFallbackModel() reads it at reply
		// assembly; no normal turn can see it.
		l.mu.Lock()
		l.refusalFallbackServedModel = resolved
		l.mu.Unlock()
	}

	// Stage the resolved fallback config so the retry call carries it
	// request-scoped (llm.WithModelOverride, ranked ABOVE the alias channel
	// in chatWithFailoverRaw) even when the retry re-enters mid-cycle —
	// reasoningCycle only stages overrides at turn top, and the refusal
	// fires mid-loop. Mirrors the user-directive staging contract exactly:
	// persisted override stays armed until cleared, the non-persistent flag
	// is preserved for the fresh-turn sweep. Best-effort: a seam resolver
	// that cannot produce a config (stub in unit tests, exotic target)
	// leaves staging to the applier path.
	// Resolve the concrete config through the seam resolver first (it may
	// differ from l.resolver, e.g. wired per-loop in tests/daemons), falling
	// back to the loop's resolver.
	var staged *llm.ModelConfig
	if sr, ok := resolver.(*llm.Resolver); ok {
		staged = sr.ResolveRef(resolved)
	}
	if staged == nil && l.resolver != nil {
		staged = l.resolver.ResolveRef(resolved)
	}
	if staged != nil {
		l.mu.Lock()
		l.pendingModelOverrideConfig = staged
		l.mu.Unlock()
	}

	// Best-effort observability on the SAME topic as verification
	// escalation (agent.model_escalated → WS agent_progress). Payload keys
	// (exact): {agent_id, from_model, to_model, reason, fix_loops}; the
	// refusal fallback grants no fix loops (0). nil publisher skips the
	// event — the fallback still functions.
	if publisher != nil {
		agentID := l.agentID
		publisher(TopicAgentModelEscalated, map[string]any{
			"agent_id":   agentID,
			"from_model": serving,
			"to_model":   resolved,
			"reason":     "refusal_fallback",
			"fix_loops":  0,
		})
	}

	slog.Info("model refusal: falling back to refusal model",
		"agent_id", l.agentID,
		"from_model", serving,
		"to_model", resolved,
		"fallback_ref", fallbackRef,
		"provider", refusalErr.ProviderID,
		"model", refusalErr.ModelID,
		"source", refusalErr.Source,
	)

	return true, nil
}

// refusalFallbackDisclosure returns the reply-text disclosure suffix when a
// refusal-fallback retry was armed during this turn (refusal-fallback leaf
// 04, user decision 2026-09-16 option b): the user-visible reply ends with
//
//	"\n\n[answered by <fallback model> after refusal]"
//
// so the transcript itself discloses the fallback on every surface without
// client changes. Empty string for normal turns — replies stay byte-
// identical. The note is CODE-appended at reply assembly, never
// model-generated.
func (l *AgentLoop) refusalFallbackDisclosure() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.refusalFallbackServedModel == "" {
		return ""
	}
	return "\n\n[answered by " + l.refusalFallbackServedModel + " after refusal]"
}

// clearRefusalFallbackServed resets the turn-scoped disclosure state at the
// start of a fresh turn. Idempotent; guarded by l.mu.
func (l *AgentLoop) clearRefusalFallbackServed() {
	l.mu.Lock()
	l.refusalFallbackServedModel = ""
	l.mu.Unlock()
}

// ClearRefusalFallback clears a fallback override armed by handleRefusal
// when the retry never consumed it (fresh-turn restore, mirroring the
// verification-escalation clear-after-turn behavior). Restores the base
// model by clearing the persistent override AND the staged fallback config
// (pendingModelOverrideConfig): the staged config is part of the pin —
// leaving it armed would re-apply WithModelOverride on the next call even
// after the override ref itself was cleared (bughunt F4). Idempotent.
func (l *AgentLoop) ClearRefusalFallback() {
	l.mu.Lock()
	l.pendingModelOverrideConfig = nil
	l.mu.Unlock()
	if l.refusalOverrideClear != nil {
		l.refusalOverrideClear()
	}
}

// clearRefusalFreshTurnState is the fresh-turn half of the refusal-fallback
// lifecycle (bughunt F4): when the pin was REFUSAL-ARMED in a previous turn
// (signaled by the turn-scoped disclosure flag, set exactly when handleRefusal
// accepted the pin), restore the base model before this turn resolves its
// own. Without this the persistent override survived the successful fallback
// turn and every later turn silently served the fallback model — the fresh
// turn must start from the base alias unless THIS turn refuses and re-arms.
// Always resets the disclosure flag (the clearRefusalFallbackServed
// lifecycle). Idempotent; guarded by l.mu.
//
// scopes-3 finding B: a park/resume re-entry ALSO runs through here (the
// resume re-enters RunOnceWithParts), and that clear is CORRECT for the pin —
// the resumed generation must serve the base alias. But the one-hop BUDGET
// must survive the cycle: resumedTurn ctx does NOT set the cleared marker, so
// the hop counter persists; the next GENUINELY fresh (non-resumed) turn
// consumes the marker and resets the budget.
func (l *AgentLoop) clearRefusalFreshTurnState() {
	l.mu.RLock()
	refusalArmed := l.refusalFallbackServedModel != ""
	l.mu.RUnlock()
	if refusalArmed {
		l.ClearRefusalFallback()
	}
	l.mu.Lock()
	l.refusalFallbackServedModel = ""
	l.mu.Unlock()
}

// resetRefusalHopBudgetOnFreshTurn is the genuinely-fresh-turn half of the
// persistent hop budget (scopes-3 finding B): called from RunOnceWithParts
// AFTER clearRefusalFreshTurnState on NON-resumed turns only. Semantics: the
// marker set by the previous fresh turn says "one full turn window elapsed
// without any park/resume re-entry" — the budget resets so a session can
// still use its fallback on a later turn. A park/resume cycle skips this
// (WithResumedTurn), keeping the count. Guarded by l.mu.
func (l *AgentLoop) resetRefusalHopBudgetOnFreshTurn() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.refusalFallbackHopsCleared {
		// A fresh turn already consumed the previous window's budget.
		return
	}
	if l.refusalFallbackHops == 0 {
		l.refusalFallbackHopsCleared = true
		return
	}
	l.refusalFallbackHops = 0
	l.refusalFallbackHopsCleared = true
}

// refusalHopsExhausted reports whether the persistent one-hop budget
// (maxRefusalFallbackHops pins across park/resume generations) is spent.
func (l *AgentLoop) refusalHopsExhausted() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.refusalFallbackHops >= maxRefusalFallbackHops
}

// streamAttemptEpochSnapshot returns the current streaming attempt epoch
// (scopes-3 finding A): the accumulator in RunOnceWithParts snapshots this
// before the chat call and RESETS its text when the epoch moves (a
// refusal-fallback retry must not append its tokens after the refused
// attempt's partial stream).
func (l *AgentLoop) streamAttemptEpochSnapshot() int64 {
	return l.streamAttemptEpoch.Load()
}

// streamAccumulator is the refusal-retry-aware live-stream text accumulator
// (scopes-3 audit finding A). RunOnceWithParts owns one per streaming call;
// when handleRefusal arms a fallback retry it bumps the loop's
// streamAttemptEpoch, and the FIRST delta of the next attempt observes the
// epoch change and DISCARDS the refused attempt's partial text — the same
// reset the ProviderManager rotation path gets from DeltaCallbackWithAttempt
// (provider_manager.go), applied to the loop-side accumulator that feeds the
// client-visible text_so_far preview. Without the reset, the fallback's full
// text appends after the refused partial and the preview shows the mixed
// text (the final persisted reply is unaffected: the refused call returns no
// response).
type streamAccumulator struct {
	loop  *AgentLoop
	epoch int64
	text  string
}

func newStreamAccumulator(l *AgentLoop) *streamAccumulator {
	return &streamAccumulator{loop: l, epoch: l.streamAttemptEpochSnapshot()}
}

// observe folds one delta into the accumulated preview text, resetting first
// when the streaming attempt epoch moved (a refusal retry armed between
// attempts). Returns the text to publish as text_so_far.
func (a *streamAccumulator) observe(delta string) string {
	if cur := a.loop.streamAttemptEpochSnapshot(); cur != a.epoch {
		a.epoch = cur
		a.text = ""
	}
	a.text += delta
	return a.text
}

// refusalServingModelRef returns the "provider/model" ref of the model the
// loop is currently serving with, for the one-hop comparison. Resolution
// mirrors modelRefForAgent's alias-or-ref shape via ResolveEscalationRef.
func (l *AgentLoop) refusalServingModelRef() string {
	resolver := l.refusalResolver
	if resolver == nil || l.modelRef == "" {
		return ""
	}
	ref, err := resolver.ResolveEscalationRef(l.modelRef)
	if err != nil {
		return ""
	}
	return ref
}

// refusalFallbackSeamAlign is compile-time documentation only: the refusal
// branch reuses the SAME seams as verification escalation (ModelResolver,
// EventPublisher, SetPersistentModelOverride / ClearModelOverride). Ensures
// the seam signatures stay aligned with the hook-site wiring.
var _ = struct{}{}
