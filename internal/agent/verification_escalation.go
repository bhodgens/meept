package agent

import (
	"context"
	"log/slog"

	"github.com/caimlas/meept/internal/llm"
)

// Compile-time proof that *llm.Resolver satisfies the ModelResolver seam
// (leaf 04): ResolveEscalationRef on *llm.Resolver implements this.
var _ ModelResolver = (*llm.Resolver)(nil)

// ModelResolver resolves an alias name or "provider/model" ref to a
// usable model reference. Satisfied by *llm.Resolver (leaf 04) and tests.
type ModelResolver interface {
	// ResolveEscalationRef returns a model ref string usable as a loop
	// model ref. Named ResolveEscalationRef (audit B2): *llm.Resolver
	// already has ResolveRef(ref string) *ModelConfig — a same-name /
	// different-signature method would not compile. Returns an error
	// when the ref names no alias/model.
	ResolveEscalationRef(ref string) (string, error)
}

// EscalationDecision is what handleFail should do on loop exhaustion.
type EscalationDecision struct {
	Escalate bool
	ModelRef string
	Reason   string // "fix_loops_exhausted" | "no_escalation_model" | "resolution_failed"
}

// TopicAgentModelEscalated is published on the bus when a fix-loop turn is
// switched to the escalation model. WS classification: agent_progress —
// NEVER chat_message (AGENTS.md invariant; internal/comm/http/server.go
// transformBusEventToWS). Payload keys (exact):
// {agent_id, from_model, to_model, reason, fix_loops}.
const TopicAgentModelEscalated = "agent.model_escalated"

// DecideEscalation never errors; resolution failure degrades to
// Escalate=false with Reason "resolution_failed".
func DecideEscalation(spec *AgentSpec, resolver ModelResolver) EscalationDecision {
	// No configured escalation target: not an error, just disabled (D3).
	if spec == nil || spec.EscalationModel == "" {
		return EscalationDecision{Escalate: false, Reason: "no_escalation_model"}
	}

	// Nil resolver with a configured target cannot resolve; degrade to
	// the legacy user-escalation path rather than panic.
	if resolver == nil {
		return EscalationDecision{Escalate: false, Reason: "resolution_failed"}
	}

	resolved, err := resolver.ResolveEscalationRef(spec.EscalationModel)
	if err != nil {
		return EscalationDecision{Escalate: false, Reason: "resolution_failed"}
	}

	return EscalationDecision{
		Escalate: true,
		ModelRef: resolved,
		Reason:   "fix_loops_exhausted",
	}
}

// AgentIDSource extracts the agent id for observability payloads. The loop
// is the natural source, but the hook must not import it, so the wiring
// site passes a closure over AgentLoop.agentID (SetAgentIDSource).
type AgentIDSource func() string

// EventPublisher publishes one bus event. Implemented at the loop wiring
// site as a closure over AgentLoop.bus + models.NewBusMessage, mirroring the
// established publish pattern in package agent (e.g.
// AgentLoop.publishSteeringInjected). The hook has no bus reference of its
// own, so observability travels through this seam; nil (unset) means the
// bus event is skipped — escalation still functions.
type EventPublisher func(topic string, payload map[string]any)

// effectiveMaxFixLoops returns the config value floored at 1 (mirrors
// handleFail's floor so every surface reports the same effective budget).
func effectiveMaxFixLoops(v int) int {
	if v < 1 {
		return 3
	}
	return v
}

// ApplyEscalation consumes h.pendingEscalation (leaf 03's consumption seam)
// and prepares the escalated fix iteration. It mutates the CALLER'S
// TurnModification in place (master Contract 4 signature) so the escalation
// turn keeps handleFail's full message shape (verifier findings included)
// while the override and observability fire exactly once:
//
//   - Pending decision nil → returns false, mod untouched (idempotent no-op).
//   - Sets mod.ModelOverride = decision.ModelRef (the EXISTING
//     TurnModification field — no struct extension) and mod.Modified = true.
//   - Arms the loop override PERSISTENTLY (R1): the overrideApplier seam
//     invokes AgentLoop.SetPersistentModelOverride, giving the escalated
//     model the FULL max_fix_loops budget — one-shot (SetModelOverride)
//     would grant exactly one turn. The persistent override is cleared on
//     FRESH-TURN DETECTION: the next turn that arrives WITHOUT a pending
//     escalation (see ClearEscalation). Escalation is per-fix-loop, never
//     sticky across turns (master Contract 4, audit R1).
//   - Emits bus topic TopicAgentModelEscalated with payload
//     {agent_id, from_model, to_model, reason, fix_loops} (bus reason is
//     "fix_loops_exhausted"; the routing-log reason is "escalation").
//   - Routing-log reason "escalation" is recorded RESOLVER-side:
//     Resolver.ResolveEscalationRef persists the RoutingDecision when a
//     RoutingLogger is attached (mirroring ResolveRef's "explicit"
//     recording), so the decision row exists even if application is
//     skipped. No second logging seam is introduced here.
//
// Clears h.pendingEscalation on success — consumed exactly once; a second
// call is a no-op. Cites D3: hook placement keeps loop.go unmodified except
// at the wiring site.
func ApplyEscalation(h *VerificationAutoTrigger, mod *TurnModification) bool {
	// Idempotent: nil receiver or nothing pending → no-op.
	if h == nil || h.pendingEscalation == nil {
		return false
	}
	decision := h.pendingEscalation
	h.pendingEscalation = nil // consumed exactly once (Q2 already reset fixCount)

	fromRef := h.escalatedModelRef
	if fromRef == "" {
		// First application this window: report the pre-escalation base
		// ref the agent actually ran (Contract 4 payload semantics).
		fromRef = h.baseModelRef
	}

	// Arm the loop override PERSISTENTLY before the turn runs: the escalated
	// model keeps the override across its full budget and the engine does
	// not auto-clear a persistent override (loop.go consumption site honors
	// IsModelOverridePersistent).
	if h.overrideApplier != nil {
		h.overrideApplier(decision.ModelRef)
	}

	h.escalatedModelRef = decision.ModelRef

	if mod != nil {
		mod.Modified = true
		mod.ModelOverride = decision.ModelRef
		if mod.Reason == "" {
			mod.Reason = "verification model escalation"
		}
	}

	// Best-effort observability — never fail the turn over an event or log.
	if h.publishEvent != nil {
		agentID := ""
		if h.agentIDSource != nil {
			agentID = h.agentIDSource()
		}
		h.publishEvent(TopicAgentModelEscalated, map[string]any{
			"agent_id":   agentID,
			"from_model": fromRef,
			"to_model":   decision.ModelRef,
			"reason":     decision.Reason,
			// Effective budget, not the raw field: MaxFixLoops < 1 floors
			// to 3 everywhere else in the hook (handleFail does the same).
			"fix_loops": effectiveMaxFixLoops(h.config.MaxFixLoops),
		})
	}
	slog.Info("verification escalation applied",
		"from_model", fromRef,
		"to_model", decision.ModelRef,
		"reason", decision.Reason,
	)

	return true
}

// ClearModelOverride implements ModelOverrideFreshTurnHook (leaf 04, R1): a
// persistent override armed by ApplyEscalation is cleared when a turn
// arrives WITHOUT a pending escalation and verification passed — the fresh
// turn must run on the BASE model again (no sticky escalation across
// turns). Swept by HookRegistry.ClearFreshTurnOverrides at the start of
// every turn; clearing is idempotent.
func (h *VerificationAutoTrigger) ClearModelOverride(ctx context.Context) {
	if h == nil || h.escalatedModelRef == "" {
		return
	}
	if h.clearOverride != nil {
		h.clearOverride()
	}
	slog.Info("verification escalation cleared on fresh turn", "restored_model", h.escalatedModelRef)
	h.escalatedModelRef = ""
	h.baseModelRef = ""
}
