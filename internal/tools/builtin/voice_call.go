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
	"errors"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/secrets"
	"github.com/caimlas/meept/internal/toolprov"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/voiceintake"
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

// LocalFunc is the injected local-leg seam (local-leg plan leaf 09): the
// daemon wiring adapts voice.LocalLeg.Open onto it, so the builtin package
// carries NO internal/voice import edge (plain strings, never a voice
// type). The return pair is (call_id, session_id); refusals come back as
// errors and surface through the ToolResult envelope like dial refusals
// (busy, duplicate target, disabled-inside-the-leg all ride this path -
// policy stays entirely in the leg, per the tool's founding rule).
type LocalFunc func(ctx context.Context, employeeID, label, ttsVoice string) (callID, sessionID string, err error)

// LegDefaultFunc resolves the CL4 default leg for the calling employee:
// voice.employee_leg[employee].leg, falling back to voice.default_leg.
// The daemon wiring injects the config lookup; an empty (or nil-func)
// resolution means "phone". Never a policy decision of the tool's own -
// just the CL4 leg chain's last two layers.
type LegDefaultFunc func(ctx context.Context, employeeID string) string

// voice legs a call may name (CL4). The empty string means "not specified"
// and resolves through LegDefaultFunc; anything else refuses.
const (
	toolLegPhone = "phone"
	toolLegLocal = "local"
)

// VoiceCallTool places one outbound call on behalf of the calling employee
// - on the PHONE leg (allowlisted number, carrier) or on the LOCAL leg
// (direct-TTS voice session on a same-machine client, no carrier, no
// cloud). Both legs share the same policy gates behind their seams; the
// tool validates argument shapes and routes.
type VoiceCallTool struct {
	tools.ToolDefaults
	dial    DialerFunc
	agentID AgentIDFunc
	local   LocalFunc
	legDef  LegDefaultFunc
}

// NewVoiceCallTool constructs the tool. A nil dial func fails closed at
// Execute (config error surfaced to the model, never a panic). The local
// seam starts nil (leg disabled); the wiring arms it additively via
// SetVoiceLocalFunc. An empty leg arg with no armed LegDefaultFunc means
// phone - the CL4 config default byte-compatible with the pre-local tool.
func NewVoiceCallTool(dial DialerFunc, agentID AgentIDFunc) *VoiceCallTool {
	return &VoiceCallTool{dial: dial, agentID: agentID}
}

// SetLocalFunc arms the local leg seam. Nil fn is ignored (every Set*
// nil-guarded per repo rule): an unarmed local leg fails closed at Execute
// with the named disabled error.
func (t *VoiceCallTool) SetLocalFunc(fn LocalFunc) {
	if fn == nil {
		return
	}
	t.local = fn
}

// SetLegDefault arms the CL4 leg-default seam. Nil fn is ignored.
func (t *VoiceCallTool) SetLegDefault(fn LegDefaultFunc) {
	if fn == nil {
		return
	}
	t.legDef = fn
}

func (t *VoiceCallTool) Name() string { return "voice_call" }

func (t *VoiceCallTool) Category() string { return "platform" }

func (t *VoiceCallTool) Description() string {
	return "Place a voice call on behalf of the calling employee. Default leg is the phone (allowlisted E.164 number, carrier call). With leg=\"local\" (when the local voice leg is enabled) it instead opens a local voice session for this employee's goal loop on whatever client connects next - on that leg target is an optional session label, not a phone number, and no carrier or cloud provider is involved. Every call is gated by the voice policy stack: the allowlist and effects ledger on the phone leg (a retry of the same request_id never rings twice), the concurrency registry and capacity on both. Pass voice to override the TTS voice for this call. Returns the call_control_id (and session_id on the local leg); the call's lifecycle surfaces via voice.call.* progress events."
}

func (t *VoiceCallTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"target": {
				Type:        "string",
				Description: "Phone leg: the number to dial, E.164 format (+1555...), must be in voice.allowed_numbers. Local leg: optional session label (default: this employee's id).",
			},
			"request_id": {
				Type:        "string",
				Description: "Idempotency key for this dial attempt. Reuse the same value to retry a failed dial without ringing twice. Omit to have one generated.",
			},
			"instructions": {
				Type:        "string",
				Description: "Optional first-turn context for the employee answering the call (why it is calling).",
			},
			"leg": {
				Type:        "string",
				Description: "Which leg to place the call on: \"phone\" (default) or \"local\" (direct-TTS session on a connected client; requires the local voice leg to be enabled). Omit to use the configured employee/config default.",
			},
			"voice": {
				Type:        "string",
				Description: "Optional TTS voice override for this call (highest CL4 precedence). Omit to use the employee/per-leg/provider defaults.",
			},
		},
	}
}

// VoiceCallResult is the structured tool result. SessionID is populated on
// the local leg only (the phone leg has no meept session before answer);
// Target carries the phone number or the resolved local target.
type VoiceCallResult struct {
	CallControlID string `json:"call_control_id"`
	Target        string `json:"target"`
	State         string `json:"state"`                // dialing (terminal outcome via events)
	SessionID     string `json:"session_id,omitempty"` // local leg only
}

// resolveLeg applies the CL4 leg chain: the arg wins; an empty arg falls to
// the injected employee/config default; an unresolved default is phone.
func (t *VoiceCallTool) resolveLeg(ctx context.Context, arg, employeeID string) (string, error) {
	switch arg {
	case toolLegPhone, toolLegLocal:
		return arg, nil
	case "":
		if t.legDef != nil {
			if def := t.legDef(ctx, employeeID); def == toolLegLocal {
				return toolLegLocal, nil
			}
		}
		return toolLegPhone, nil
	default:
		return "", fmt.Errorf("voice_call: leg must be \"phone\" or \"local\" (got %q)", arg)
	}
}

func (t *VoiceCallTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	target, _ := args["target"].(string)
	target = strings.TrimSpace(target)
	legArg, _ := args["leg"].(string)
	ttsVoice, _ := args["voice"].(string)

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

	leg, err := t.resolveLeg(ctx, strings.TrimSpace(legArg), employeeID)
	if err != nil {
		return tools.NewErrorResult(err.Error()), nil
	}

	if leg == toolLegLocal {
		return t.executeLocal(ctx, employeeID, target, ttsVoice)
	}
	return t.executePhone(ctx, employeeID, target, requestID)
}

// executePhone is the pre-existing phone branch, behavior unchanged: the
// target rule (required, E.164) and the DialFunc round-trip stay exactly
// as the leaf-14 tool pinned them.
func (t *VoiceCallTool) executePhone(ctx context.Context, employeeID, target, requestID string) (any, error) {
	if t.dial == nil {
		return tools.NewErrorResult("voice_call: dialer not wired (voice block disabled or misconfigured)"), nil
	}
	if target == "" {
		return tools.NewErrorResult("voice_call: target is required (E.164 phone number)"), nil
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

// executeLocal routes the local leg: the seam must be armed (the wiring
// arms it only when the leg is enabled and constructed - a nil seam IS the
// disabled-leg refusal, mirroring the nil-dial rule); the target arg is an
// OPTIONAL label (empty = the leg's own default); the ttsVoice override
// rides through for the CL4 voice chain.
func (t *VoiceCallTool) executeLocal(ctx context.Context, employeeID, label, ttsVoice string) (any, error) {
	if t.local == nil {
		return tools.NewErrorResultErr(errors.New("voice_call refused: voice local leg disabled")), nil
	}
	callID, sessionID, err := t.local(ctx, employeeID, label, ttsVoice)
	if err != nil {
		return tools.NewErrorResultErr(
			fmt.Errorf("voice_call refused: %w", err)), nil
	}
	return tools.NewSuccessResult(VoiceCallResult{
		CallControlID: callID,
		Target:        label,
		State:         "dialing",
		SessionID:     sessionID,
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

// SetVoiceLocalFunc binds the local-leg seam additively (local-leg plan
// leaf 09 T4): the daemon wiring adapts voice.LocalLeg.Open onto it and
// calls this right after SetVoiceProvider. Nil fn is ignored (repo Set*
// nil-guard rule) so a disabled local leg leaves the previous binding
// intact and every provider-built tool keeps its nil local seam - the
// fail-closed "voice local leg disabled" posture. Kept separate from
// SetVoiceProvider so the pre-existing 3-arg call site (components.go)
// stays source-compatible while the parallel leaf 08 wires it.
func SetVoiceLocalFunc(fn LocalFunc) {
	if fn == nil {
		return
	}
	voiceLocalFunc = fn
}

// voiceLocalFunc is the package-level local-leg binding the factory folds
// into every constructed provider. Nil = leg disabled.
var voiceLocalFunc LocalFunc

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
	tool := NewVoiceCallTool(p.dial, p.agentID)
	tool.SetLocalFunc(voiceLocalFunc)
	// contact rides the same provider group (same policy family: the
	// contact chain drives voice_call + the other channels behind one
	// fallback policy). nil chain = nil seam, the tool fails closed.
	out := []tools.Tool{tool}
	if contactChainFunc != nil {
		out = append(out, NewContactTool(contactChainFunc))
	}
	// Guided intake (leaf 05, secretary-scoped): intake_save joins the
	// voice group ONLY when the ledger seam is armed
	// (SetIntakeSaveSaver, daemon wiring). Unarmed = unregistered =
	// absent from every toolset (the ledgerAppend-off-by-absence
	// posture); the constitution's tools_allowed still gates execution
	// for employees that CAN see it.
	if intakeSaveSaver != nil {
		intake := NewIntakeSaveTool(intakeSaveSaver)
		foldIntakeSeams(intake)
		out = append(out, intake)
	}
	return out
}

// contactChainFunc is the contact tool's chain seam, bound by the daemon
// wiring (SetContactChain) before toolprov.RegisterAll constructs the
// voice provider. nil leaves the contact tool unregistered (the contact
// feature is opt-in; no chain, no tool).
var contactChainFunc ContactChainRunner

// SetContactChain binds the contact chain seam (leaf 02 wiring). Nil fn
// is ignored (repo Set* nil-guard rule).
func SetContactChain(fn ContactChainRunner) {
	if fn == nil {
		return
	}
	contactChainFunc = fn
}

// intakeSaveSaver is the guided-intake ledger seam (leaf 05), bound by
// the daemon wiring (SetIntakeSaveSaver) over the employee ledger store.
// nil = the intake_save tool is not registered at all (guided intake
// off-by-absence).
var intakeSaveSaver voiceintake.IntakeSaver

// SetIntakeSaveSaver binds the guided-intake ledger append seam. nil fn
// is ignored (repo Set* nil-guard rule). Bound ONLY when the daemon's
// guided-intake gate is on (secretary config); the binding closure maps
// onto ledger.Store.Append with kind per record, source=voice,
// source_ref=ccid.
func SetIntakeSaveSaver(fn voiceintake.IntakeSaver) {
	if fn == nil {
		return
	}
	intakeSaveSaver = fn
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
