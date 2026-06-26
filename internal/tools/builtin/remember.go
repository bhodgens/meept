package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// RememberTool lets an agent propose a permanent improvement (skill, prompt
// change, or rule) directly via the proposal queue. Source is tagged
// "manual:/remember" to distinguish agent-initiated proposals from automated
// reflection proposals (which use "turn:<sessionID>" or "session:<sessionID>").
//
// The tool does NOT apply the change. It queues the proposal for later review
// and application via /implement-improvements or `meept improvements list`.
type RememberTool struct {
	queue *agent.ProposalQueueExternal
}

// NewRememberTool creates a /remember tool bound to a proposal queue file path.
// The path's parent directories are created on first Append.
func NewRememberTool(queuePath string) *RememberTool {
	return &RememberTool{queue: agent.NewExternalProposalQueue(queuePath)}
}

func (t *RememberTool) Name() string { return "remember" }

// Category implements tools.Categorizer. Grouped with other agent-facing tools
// so the dispatcher can route /remember-style intents consistently.
func (t *RememberTool) Category() string { return "agent" }

func (t *RememberTool) Description() string {
	return "Propose a permanent improvement (skill, prompt change, or rule) for future sessions. " +
		"Use when you observe a reusable lesson worth saving. The proposal is queued for review " +
		"and will NOT be applied immediately. Targets under config/prompts/, AGENT.md, or CLAUDE.md " +
		"are always propose-only regardless of auto_apply settings."
}

func (t *RememberTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"target": {
				Type:        schemaTypeString,
				Description: "File path or skill name to modify (e.g., '.meept/skills/foo/SKILL.md', 'CLAUDE.md', 'config/agents/coder/AGENT.md'). For skill_create, use the skill directory path ending in SKILL.md.",
			},
			"change": {
				Type:        schemaTypeString,
				Description: "Full content for skill_create proposals, or rule/instruction text to add for project_instruction / agent_prompt / prompt_component proposals.",
			},
			"justification": {
				Type:        schemaTypeString,
				Description: "Why this improvement matters. What problem does it solve? What observation triggered this proposal?",
			},
		},
		Required: []string{"target", "change", "justification"},
	}
}

// RememberResult is the structured payload returned by a successful Execute.
type RememberResult struct {
	Target string `json:"target"`
	ID     string `json:"id"`       // assigned by the queue; useful for /improvements skip/apply
	Queued bool   `json:"queued"`   // always true on success
}

// TerminateHint implements tools.TerminatingTool. The remember tool returns a
// short confirmation that does not need LLM follow-up synthesis, so we signal
// termination. This mirrors the behavior of other "fire and forget" tools
// (memory_store, retain, reflect) that produce a final ack string.
func (t *RememberTool) TerminateHint(args map[string]any) bool { return true }

func (t *RememberTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	target, _ := args["target"].(string)
	change, _ := args["change"].(string)
	just, _ := args["justification"].(string)

	if target == "" || change == "" {
		return tools.NewErrorResult("target and change are required"), nil
	}
	if just == "" {
		just = "(no justification provided)"
	}

	proposal := agent.ReflectionProposal{
		ID:            agent.GenerateProposalID(),
		Type:          inferProposalType(target),
		Target:        target,
		Change:        change,
		Justification: just,
		Confidence:    0.9, // manual proposals get high confidence; the operator can skip if undesired
		Source:        "manual:/remember",
	}

	if err := t.queue.Append(proposal); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("failed to queue proposal: %v", err)), nil
	}

	return tools.NewSuccessResultWithTerminate(RememberResult{
		Target: target,
		ID:     proposal.ID,
		Queued: true,
	}), nil
}

// inferProposalType maps a target path to a proposal type. The mapping mirrors
// the type vocabulary understood by the applier
// (skill_create|skill_update|agent_prompt|project_instruction|prompt_component).
// Skill paths ending in SKILL.md are skill_create; AGENT.md targets are
// agent_prompt; CLAUDE.md is project_instruction; anything else under
// config/prompts/ is a prompt_component.
func inferProposalType(target string) string {
	clean := strings.TrimSpace(target)
	if strings.Contains(clean, "SKILL.md") || strings.Contains(clean, ".meept/skills/") {
		return "skill_create"
	}
	if strings.HasSuffix(clean, "AGENT.md") {
		return "agent_prompt"
	}
	if clean == "CLAUDE.md" {
		return "project_instruction"
	}
	return "prompt_component"
}

// Compile-time interface checks.
var (
	_ tools.Tool             = (*RememberTool)(nil)
	_ tools.Categorizer      = (*RememberTool)(nil)
	_ tools.TerminatingTool  = (*RememberTool)(nil)
)
