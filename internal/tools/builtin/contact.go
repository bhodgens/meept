package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// ContactChainRunner is the injected chain seam (C6, gate-decided
// shape): plain strings in, rendered report out. The daemon wiring
// adapts contact.ChainRunner + the contact Store's Resolve onto it, so
// the builtin package carries NO internal/contact import edge (the
// same no-type rule voice_call pins for internal/voice).
type ContactChainRunner interface {
	// Run resolves person (contact id / E.164 phone / Signal uuid),
	// walks the record's channel policy (channels non-empty = override
	// for this run), and returns the per-channel outcome report, one
	// lowercase line per attempted channel.
	Run(person, message string, channels []string) string
}

// ContactTool is the policy-driven contact path: ONE employee action
// ("reach this person with this message") that tries the configured
// channels in order and reports per-channel outcomes. The raw channel
// tools (voice_call, the direct dial path) stay available for explicit
// control; this tool owns the fallback policy.
type ContactTool struct {
	tools.ToolDefaults
	chain ContactChainRunner
}

// NewContactTool constructs the tool. A nil chain fails closed at
// Execute (wiring error surfaced to the model, never a panic).
func NewContactTool(chain ContactChainRunner) *ContactTool {
	return &ContactTool{chain: chain}
}

// SetContactFunc arms (or replaces) the chain seam. Nil fn is ignored
// (every Set* nil-guarded per repo rule).
func (t *ContactTool) SetContactFunc(fn ContactChainRunner) {
	if fn == nil {
		return
	}
	t.chain = fn
}

func (t *ContactTool) Name() string { return "contact" }

func (t *ContactTool) Category() string { return "platform" }

func (t *ContactTool) Description() string {
	return "Reach a person with a message over the configured contact channels, in fallback order: e.g. phone call, then signal voice call, then signal message, then SMS. person is a contact id, E.164 phone (+1555...), or signal uuid. Per-channel outcomes come back one per line (answered calls stop the chain and continue on their own session; no-answer/busy/unavailable advance to the next channel). Optionally pass channels to override the fallback order for this one message."
}

func (t *ContactTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"person": {
				Type:        "string",
				Description: "Who to reach: contact id, E.164 phone (+1555...), or signal uuid.",
			},
			"message": {
				Type:        "string",
				Description: "The message to deliver (spoken on voice channels, sent verbatim on message channels).",
			},
			"channels": {
				Type:        "array",
				Description: "Optional channel override for THIS message, in order (telnyx_voice | signal_voice | signal_msg | telnyx_sms). Empty/omitted = the contact's stored policy, falling back to the employee default chain.",
			},
		},
		Required: []string{"person", "message"},
	}
}

// Execute validates args and runs the chain seam. A connected voice
// call does NOT block the result: "answered" is the chain's terminal
// outcome and the live call proceeds on its own session.
func (t *ContactTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.chain == nil {
		return tools.NewErrorResult("contact: contact chain is not wired (contact feature disabled)"), nil
	}
	person, _ := args["person"].(string)
	person = strings.TrimSpace(person)
	if person == "" {
		return tools.NewErrorResult("contact: person is required (contact id, E.164 phone, or signal uuid)"), nil
	}
	message, _ := args["message"].(string)

	var channels []string
	switch raw := args["channels"].(type) {
	case []any:
		for _, c := range raw {
			if s, ok := c.(string); ok && strings.TrimSpace(s) != "" {
				channels = append(channels, strings.TrimSpace(s))
			}
		}
	case []string:
		for _, s := range raw {
			if strings.TrimSpace(s) != "" {
				channels = append(channels, strings.TrimSpace(s))
			}
		}
	}

	report := t.chain.Run(person, message, channels)
	if report == "" {
		report = fmt.Sprintf("contact: no channels attempted for %s (no configured channels)", person)
	}
	return tools.NewSuccessResult(report), nil
}

func (t *ContactTool) IsReadOnly(input map[string]any) bool { return false }

func (t *ContactTool) IsConcurrencySafe(input map[string]any) bool { return false }
