package builtin

// voice_call (leaf 14 T5): the employee-callable outbound dial. The tool is
// a THIN argument shim over the leaf-11 Dialer: every policy gate
// (voice.enabled, allowlist, capacity, duplicate target), the audit row,
// and the effects reservation live in voice.Dialer.Dial, so this tool adds
// no policy of its own - it validates argument shapes, calls the injected
// DialFunc, and reports the reserved result.
//
// Registration goes through the toolprov provider seam (the "voice"
// provider, same pattern as schedule_register.go): the dialer is a runtime
// value only the daemon can supply, so this file registers a provider
// FACTORY whose handle is captured at Discover time via SetVoiceProvider.
// An unbound factory fails closed (nil Provider -> RegisterAll error).
//
// Permission surface: pkg/security BuiltinRules gains one "voice_call"
// action row (unknown actions DENY by default, so the row is mandatory);
// agent.ToolActionMap maps the tool name onto it, and the employee
// constitution tools_allowed list grants it per employee (the tool gate
// applies to every execution - the tool never checks grants itself).

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/secrets"
	"github.com/caimlas/meept/internal/toolprov"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/id"
)

// DialerFunc is the injected dial seam: voice.Dialer.Dial adapted by the
// daemon wiring (employee id travels from the executor's agent context).
// Result is the ToolResult envelope so both error shapes round-trip.
type DialerFunc func(ctx context.Context, employeeID, target, requestID string) (callControlID string, err error)

// AgentIDFunc resolves the calling employee's id from the request context.
// The daemon wiring injects the executor's agent-id seam; a nil func makes
// every call fail closed with a nameable error (never a dial without an
// attributable employee).
type AgentIDFunc func(ctx context.Context) string

// VoiceCallTool places one outbound call on behalf of the calling employee.
type VoiceCallTool struct {
	tools.ToolDefaults
	dial    DialerFunc
	agentID AgentIDFunc
}

// NewVoiceCallTool constructs the tool. A nil dial func fails closed at
// Execute (config error surfaced to the model, never a panic).
func NewVoiceCallTool(dial DialerFunc, agentID AgentIDFunc) *VoiceCallTool {
	return &VoiceCallTool{dial: dial, agentID: agentID}
}

func (t *VoiceCallTool) Name() string { return "voice_call" }

func (t *VoiceCallTool) Category() string { return "platform" }

func (t *VoiceCallTool) Description() string {
	return "Place an outbound phone call to an allowlisted number on behalf of the calling employee. The call is gated by the voice allowlist, the concurrency registry, and the effects ledger (a retry of the same request_id never rings twice). Returns the call_control_id; the call's lifecycle surfaces via voice.call.* progress events."
}

func (t *VoiceCallTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"target": {
				Type:        "string",
				Description: "Phone number to dial, E.164 format (+1555...). Must be in voice.allowed_numbers.",
			},
			"request_id": {
				Type:        "string",
				Description: "Idempotency key for this dial attempt. Reuse the same value to retry a failed dial without ringing twice. Omit to have one generated.",
			},
			"instructions": {
				Type:        "string",
				Description: "Optional first-turn context for the employee answering the call (why it is calling).",
			},
		},
		Required: []string{"target"},
	}
}

// VoiceCallResult is the structured tool result.
type VoiceCallResult struct {
	CallControlID string `json:"call_control_id"`
	Target        string `json:"target"`
	State         string `json:"state"` // dialing (terminal outcome via events)
}

func (t *VoiceCallTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.dial == nil {
		return tools.NewErrorResult("voice_call: dialer not wired (voice block disabled or misconfigured)"), nil
	}
	target, _ := args["target"].(string)
	if strings.TrimSpace(target) == "" {
		return tools.NewErrorResult("voice_call: target is required (E.164 phone number)"), nil
	}
	requestID, _ := args["request_id"].(string)
	if requestID == "" {
		requestID = id.Generate("voicecall-")
	}

	employeeID := ""
	if t.agentID != nil {
		employeeID = t.agentID(ctx)
	}
	if employeeID == "" {
		return tools.NewErrorResult("voice_call: no employee identity on this request; the dial is attributable only to an employee"), nil
	}

	ccid, err := t.dial(ctx, employeeID, target, requestID)
	if err != nil {
		return tools.NewErrorResultErr(
			fmt.Errorf("voice_call refused: %w", err)), nil
	}
	return tools.NewSuccessResult(VoiceCallResult{
		CallControlID: ccid,
		Target:        target,
		State:         "dialing",
	}), nil
}

// ---- toolprov provider seam (the "voice" group) ----

func init() {
	if err := toolprov.Register("voice", voiceProviderFactory); err != nil {
		panic("builtin: voice provider registration: " + err.Error())
	}
}

// voiceProviderFactoryFunc builds the voice provider from the handle a
// Discover-time caller captures. Declared as a variable so the daemon can
// bind the live dialer before toolprov.RegisterAll constructs the provider.
var voiceProviderFactoryFunc func() toolprov.Provider

// voiceProviderFactory is the thunk handed to toolprov.Register. It defers
// to voiceProviderFactoryFunc; an unbound factory returns nil and
// toolprov.RegisterAll fails closed on the nil Provider (the same posture
// as the scheduler group before its wiring runs).
func voiceProviderFactory() toolprov.Provider {
	if voiceProviderFactoryFunc == nil {
		return nil
	}
	return voiceProviderFactoryFunc()
}

// SetVoiceProvider binds the voice provider's construction input: the
// dialer closure built by the daemon's voice wiring. The broker binding is
// the in-process secrets broker (the voice tool holds no secret material;
// the Telnyx token resolves inside the daemon wiring, never in the tool).
func SetVoiceProvider(dial DialerFunc, agentID AgentIDFunc, broker *secrets.Broker) {
	voiceProviderFactoryFunc = func() toolprov.Provider {
		return &voiceProvider{dial: dial, agentID: agentID, broker: broker}
	}
}

// voiceProvider is the toolprov.Provider for the voice tool group.
type voiceProvider struct {
	dial    DialerFunc
	agentID AgentIDFunc
	broker  *secrets.Broker
}

// Name implements toolprov.Provider.
func (p *voiceProvider) Name() string { return "voice" }

// Discover implements toolprov.Provider.
func (p *voiceProvider) Discover() []tools.Tool {
	return []tools.Tool{
		NewVoiceCallTool(p.dial, p.agentID),
	}
}

// PermissionClass implements toolprov.Provider. Placing a phone call
// mutates the outside world (it rings a human), but the class ladder's
// mutating tier is the honest ceiling: it never spawns processes, so it is
// not privileged. The per-call policy lives in the dialer + BuiltinRules
// risk level, not the provider class.
func (p *voiceProvider) PermissionClass() toolprov.RiskClass {
	return toolprov.RiskMutating
}

// Broker implements toolprov.Provider.
func (p *voiceProvider) Broker() *secrets.Broker {
	return p.broker
}
