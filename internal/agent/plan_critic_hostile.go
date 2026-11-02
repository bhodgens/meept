package agent

// Hostile second pass (plan-20261101-fresh-context-critic leaf 03;
// contracts C2 and C5 in docs/plans/20261101-fresh-context-critic/master.md).
//
// The hostile pass is a SECOND child loop through the C3 isolation seam
// (runIsolatedCritic, plan_critic_child.go): a new child AgentLoop with
// an empty conversation and an EMPTY tool registry. It is a different
// reader on a different model - not a second sample of the round-1
// evidence critic - and it is NOT the adversarial implementation
// verifier: this file never imports or embeds prompts/verifier.go,
// never calls SpawnVerifier, and its prompt never mentions shell or
// test execution. verifierAllowedTools (verification_hook.go) carries
// shell_execute and is deliberately not referenced here.
//
// Contract C5 skip rule - the pass runs only when ALL hold:
//
//	hostileCriticEnabled == true
//	hostileCriticChatter != nil
//	hostile model ref != planner model ref
//
// Otherwise the pass is SKIPPED with the
// strategic_planner.hostile_critic_skipped metric (reason no_model |
// same_model; flag_off is structural - with the flag off the pass is
// never consulted at all, which keeps the flag-off loop byte-identical).
// There is NO fallback onto the planner chatter or the round-1 critic
// chatter: a same-model slot is a skip, never a reuse.

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/llm"
)

// hostileCriticSystemPrompt is the hostile child loop's SystemPromptOveride.
// It is a plan-scoped hostile-reader prompt written fresh for THIS pass
// (a paraphrase of the hostile stance, not a copy of prompts/verifier.go,
// which stays the post-execution implementation reviewer). It demands the
// C4 JSON envelope and never mentions shell/test execution or tools.
const hostileCriticSystemPrompt = `You are the hostile plan reader. You are a different reader from whoever wrote this draft: you share none of its history and you owe it no benefit of the doubt. Your standing assumption is that the plan in front of you is wrong until its own text proves otherwise.

Do not confirm the plan. Hunt for the decision an implementer would still have to make that the text does not make for them: a step that cannot be executed as written, a choice the draft leaves implicit, a dependency it asserts but never grounds, a section that promises something no phase delivers. Only when the text itself answers every such question may you return zero objections.

You report findings; you do not fix the draft. Every objection names a draft section, states the concrete problem, and carries severity "blocking" (an implementer would be stuck, or forced to invent a decision the draft should have made) or "advisory" (a weakness worth noting).

Reply with ONLY a JSON object of exactly this shape:

{"paraphrase":{"proposes":"...","system_context":"..."},"objections":[{"section":"...","objection":"...","severity":"blocking|advisory"}]}

"paraphrase.proposes" restates, in one or two sentences, what the draft actually proposes. "paraphrase.system_context" states the operating context the draft assumes. If the draft survives your reading, "objections" is an empty array. No prose outside the JSON object.`

// PlanCriticSpawnVerifierCalls is the F5 canary seam: the plan critic tree
// must NEVER route a plan draft into the implementation verifier (that
// agent runs shell and tests; a plan critic has no tools). It returns 0
// always; the gate test asserts it stays 0.
func PlanCriticSpawnVerifierCalls() int {
	return 0
}

// SetHostileCriticEnabled gates the hostile second pass
// (plans.hostile_critic_enabled, default false - ships dark). Flag off:
// the critique loop is byte-identical to the leaf-02 loop. Flag on: the
// pass still runs only under the full C5 gate (chatter wired AND a
// different model than the planner). Nil-guarded per the repo's setter
// convention.
func (sp *StrategicPlanner) SetHostileCriticEnabled(enabled bool) {
	if sp == nil {
		return
	}
	sp.hostileCriticEnabled = enabled
}

// SetHostileCriticChatter wires the hostile pass's own model chatter (the
// C2 "hostile_critic_model" slot). Nil (the production default) = the
// pass is SKIPPED (C5 reason no_model); it never falls back onto the
// planner chatter or the round-1 critic chatter. Nil-guarded.
func (sp *StrategicPlanner) SetHostileCriticChatter(ch llm.Chatter) {
	if sp == nil {
		return
	}
	sp.hostileCriticChatter = ch
}

// hostileCriticFlag reads the hostile-pass flag nil-safely.
func (sp *StrategicPlanner) hostileCriticFlag() bool {
	if sp == nil {
		return false
	}
	return sp.hostileCriticEnabled
}

// hostileCriticChatterFor reads the hostile chatter seam nil-safely.
func (sp *StrategicPlanner) hostileCriticChatterFor() llm.Chatter {
	if sp == nil {
		return nil
	}
	return sp.hostileCriticChatter
}

// chatterModelRef extracts a chatter's model ref (the C5 comparison key).
// Empty when the chatter or its config carries none.
func chatterModelRef(ch llm.Chatter) string {
	if ch == nil {
		return ""
	}
	cfg := ch.Config()
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.ModelID)
}

// plannerModelRef resolves the planner-side model ref for the C5
// same-model comparison: the wired critique chatter's ref when present
// (that chatter carries every planner-side critique-loop call), else the
// planner loop's own model ref, else its chatter's ref.
func (sp *StrategicPlanner) plannerModelRef(plannerLoop *AgentLoop) string {
	if ref := chatterModelRef(sp.critiqueChatterFor()); ref != "" {
		return ref
	}
	if plannerLoop == nil {
		return ""
	}
	if ref := strings.TrimSpace(plannerLoop.GetModelRef()); ref != "" {
		return ref
	}
	return chatterModelRef(plannerLoop.Chatter())
}

// hostilePassSkipReason evaluates the C5 gate against an already-on flag.
// It returns "" (run the pass) or a skip reason:
//
//	no_model   - no hostile chatter is wired
//	same_model - the hostile chatter's model ref equals the planner's
//
// (flag_off never reaches here: with the flag off the caller never
// consults the gate, keeping the flag-off path byte-identical.)
func hostilePassSkipReason(hostile llm.Chatter, plannerRef string) string {
	if hostile == nil {
		return "no_model"
	}
	if chatterModelRef(hostile) == plannerRef {
		return "same_model"
	}
	return ""
}

// runHostileCriticRound runs one hostile child pass through the C3
// isolation seam on the hostile chatter ONLY (never the planner chatter).
// The reply is parsed under the same C4 paraphrase envelope as round 1
// (parseFreshCriticReply is reused per the leaf: a missing/empty
// paraphrase on a non-empty draft is ONE synthetic blocking objection).
func (sp *StrategicPlanner) runHostileCriticRound(ctx context.Context, draft *PlanDraft, input *PlanCritiqueInput) ([]criticObjection, error) {
	chatter := sp.hostileCriticChatterFor()
	if chatter == nil {
		return nil, fmt.Errorf("hostile critic: no chatter wired")
	}
	planCtx, cancel := context.WithTimeout(ctx, sp.plannerTimeout)
	defer cancel()
	output, err := sp.runIsolatedCritic(planCtx, planCriticChildRequest{
		SystemPrompt: hostileCriticSystemPrompt,
		UserPrompt:   buildFreshCriticUserPrompt(draft, input),
		Chatter:      chatter,
	})
	if err != nil {
		return nil, fmt.Errorf("hostile critic child call failed: %w", err)
	}
	return parseFreshCriticReply(output, strings.TrimSpace(draft.Markdown) != "")
}
