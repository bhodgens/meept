package agent

// Fresh-context evidence critic child (plan-20261101-fresh-context-critic
// leaf 02; contracts C2/C3/C4 in docs/plans/20261101-fresh-context-critic/
// master.md).
//
// Round 1 of the critique loop runs here when fresh_critic_enabled is on:
// a NEW child AgentLoop sees ONLY the draft markdown plus
// PlanCritiqueInput.Render() - no planner transcript, no session facts,
// no prior revise reasoning. The child has an EMPTY tool registry (this
// is a reader, not a runner: verification_hook.go's verifierAllowedTools
// intentionally carries shell_execute - the plan critic must never copy
// that allowlist) and a system prompt that states the reader has no
// earlier conversation.
//
// This child is NOT the adversarial verifier: it does not call
// SpawnVerifier, does not load prompts/verifier.go, and does not execute
// anything. Its reply is parsed under the C4 envelope, which requires a
// paraphrase before objections; a missing or empty paraphrase on a
// non-empty draft becomes ONE synthetic blocking objection so the round
// can never seal clean on an unread draft.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/id"
)

// freshCriticSystemPrompt is the child loop's SystemPromptOveride. It is
// an evidence-critic prompt written for THIS pass (not a copy of
// prompts/verifier.go, which is the post-execution hostile reviewer): it
// states the isolation contract, demands the C4 JSON object and nothing
// else, and never mentions shell/test execution.
const freshCriticSystemPrompt = `You are the fresh-context plan critic. You are reading this plan with NO earlier conversation: you did not write it and you have no history with it. Judge only what is on the page in front of you, against the evidence sections provided.

Reply with ONLY a JSON object of exactly this shape:

{"paraphrase":{"proposes":"...","system_context":"..."},"objections":[{"section":"...","objection":"...","severity":"blocking|advisory"}]}

"paraphrase.proposes" restates, in one or two sentences, what the draft actually proposes. "paraphrase.system_context" states the operating context the draft assumes. Every objection names a draft section, states the concrete problem, and carries severity "blocking" (must be fixed before sealing) or "advisory" (worth noting). If the draft is sound, "objections" is an empty array. No prose outside the JSON object.`

// planCriticChildRequest is contract C3's child-call shape: system prompt,
// single user prompt, and the chatter the child loop runs on.
type planCriticChildRequest struct {
	SystemPrompt string
	UserPrompt   string
	Chatter      llm.Chatter
}

// emptyToolRegistry is a ToolRegistry with no tools. The filteredToolRegistry
// pattern in verification_hook.go filters a parent down to an allow-map;
// the plan critic's allow-map is EMPTY by contract (no shell_execute, no
// file_read, no delegate tools), so this is the allow-map collapsed to a
// literal: List and GetDefinitions always return nothing.
type emptyToolRegistry struct{}

func (emptyToolRegistry) Get(string) tools.Tool                { return nil }
func (emptyToolRegistry) List() []tools.Tool                   { return nil }
func (emptyToolRegistry) GetDefinitions() []llm.ToolDefinition { return nil }

// freshCriticChildSessionID names the child loop's throwaway session (the
// loop is constructed per call and discarded after the single turn; it is
// not attached to any chat session).
func freshCriticChildSessionID() string {
	return fmt.Sprintf("plan-fresh-critic-%s", id.Generate(""))
}

// freshCriticChildConversationID namespaces the child's throwaway
// conversation (one turn, then the loop is discarded).
func freshCriticChildConversationID() string {
	return fmt.Sprintf("plan-fresh-critic-conv-%s", id.Generate(""))
}

// runIsolatedCritic runs one critic pass on a NEW child AgentLoop with an
// empty conversation, an empty tool registry, and the request's system
// prompt (contract C3 invariants). The reply text is returned raw; the
// C4 envelope parse happens in parseFreshCriticReply.
func (sp *StrategicPlanner) runIsolatedCritic(ctx context.Context, req planCriticChildRequest) (string, error) {
	if req.Chatter == nil {
		return "", fmt.Errorf("fresh critic: no chatter wired")
	}
	child := NewAgentLoop(
		freshCriticChildSessionID(),
		"",
		WithLLMChatter(req.Chatter),
		WithToolRegistry(emptyToolRegistry{}),
		WithAgentConfig(AgentConfig{
			MaxIterations:       1,
			SystemPromptOveride: req.SystemPrompt,
			Guards:              DefaultGuardConfig(),
		}),
		WithLoopLogger(sp.logger),
		WithSessionAttached(false),
		WithIsolatedChild(true),
	)
	if child == nil {
		return "", fmt.Errorf("fresh critic: child loop construction failed")
	}
	output, err := child.RunOnce(ctx, req.UserPrompt, freshCriticChildConversationID())
	if err != nil {
		return "", fmt.Errorf("fresh critic child call failed: %w", err)
	}
	return output, nil
}

// buildFreshCriticUserPrompt renders the child's SINGLE user message: the
// draft markdown plus PlanCritiqueInput.Render() ONLY (contract C3). No
// planner transcript, no session facts, no prior revise reasoning.
func buildFreshCriticUserPrompt(draft *PlanDraft, input *PlanCritiqueInput) string {
	var sb strings.Builder
	sb.WriteString("## Draft under critique\n\n")
	sb.WriteString(draft.Markdown)
	sb.WriteString("\n\n")
	if evidence := input.Render(); evidence != "" {
		sb.WriteString("## Evidence\n\n")
		sb.WriteString(evidence)
		sb.WriteString("\n\n")
	}
	sb.WriteString("Reply with ONLY the JSON object the system prompt specifies.")
	return sb.String()
}

// runFreshCriticRound is the flag-on round body: one isolated child pass
// plus the C4 envelope parse. The round-1 chatter is SetCriticChatter's
// seam; nil falls back to the planner-side critique chatter (contract C2:
// the planner chatter is allowed for round 1 ONLY - the hostile pass,
// leaf 03, has no such fallback).
func (sp *StrategicPlanner) runFreshCriticRound(ctx context.Context, draft *PlanDraft, input *PlanCritiqueInput) ([]criticObjection, error) {
	chatter := sp.criticChatterFor()
	if chatter == nil {
		chatter = sp.critiqueChatterFor()
	}
	planCtx, cancel := context.WithTimeout(ctx, sp.plannerTimeout)
	defer cancel()
	output, err := sp.runIsolatedCritic(planCtx, planCriticChildRequest{
		SystemPrompt: freshCriticSystemPrompt,
		UserPrompt:   buildFreshCriticUserPrompt(draft, input),
		Chatter:      chatter,
	})
	if err != nil {
		return nil, err
	}
	return parseFreshCriticReply(output, strings.TrimSpace(draft.Markdown) != "")
}

// freshCriticReply is the C4 round-1 envelope.
type freshCriticReply struct {
	Paraphrase struct {
		Proposes      string `json:"proposes"`
		SystemContext string `json:"system_context"`
	} `json:"paraphrase"`
	Objections []criticObjection `json:"objections"`
}

// freshCriticEmptyParaphraseObjection is the synthetic blocking objection
// stamped when the C4 paraphrase is missing or empty on a non-empty draft
// (contract C4): the fresh reader did not demonstrate it read the draft,
// so the round is not clean.
func freshCriticEmptyParaphraseObjection() criticObjection {
	return criticObjection{
		Section:   "## Meta",
		Objection: "the fresh-context critic returned no paraphrase of the draft; it did not demonstrate it read the plan, so its verdict is unusable (re-run required)",
		Severity:  "blocking",
	}
}

// parseFreshCriticReply parses the C4 envelope from a fresh-critic child
// reply. A missing/empty paraphrase on a non-empty draft yields the one
// synthetic blocking objection (on top of any parsed objections). An
// unparsable payload is an error: the caller fail-opens per the loop's
// degradation rules.
func parseFreshCriticReply(raw string, draftNonEmpty bool) ([]criticObjection, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("fresh critic output empty")
	}
	var reply freshCriticReply
	if jsonStr := ExtractJSON(raw); jsonStr != "" {
		if err := json.Unmarshal([]byte(jsonStr), &reply); err != nil {
			return nil, fmt.Errorf("fresh critic output failed to parse: %w", err)
		}
	} else if err := json.Unmarshal([]byte(trimmed), &reply); err != nil {
		return nil, fmt.Errorf("fresh critic output failed to parse: %w", err)
	}
	objections := reply.Objections
	if draftNonEmpty && strings.TrimSpace(reply.Paraphrase.Proposes) == "" {
		objections = append(objections, freshCriticEmptyParaphraseObjection())
	}
	return objections, nil
}
