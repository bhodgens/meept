package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/llm"
)

// DecisionGateHook: irreversibility gate (decision-model tree leaf 03,
// docs/plans/decision-model/03-irreversibility-gate.md, Contract D in
// master.md). A BeforeToolCallHook that consults the DecisionClient on
// destructive tool calls and, when the decision model's confidence is below
// the escalation threshold, denies the call — the loop converts the deny into
// the same phase-1 ConfirmationResponse shape the tool itself would return,
// so the model re-invokes with confirmed=true and the UI's existing confirm
// flow picks it up.
//
// Semantics (Contract D):
//   - Fires ONLY when: the hook is enabled (decision_gate_enabled), the tool
//     is on the gate allowlist (decision_gate_tools), AND the decision client
//     is Available(). Otherwise zero overhead, zero behavior change.
//   - Above threshold → allow (zero modification). The gate DECIDES whether
//     to ask the user; it never executes or marks the action done.
//   - ANY client error (transport, ErrDecisionUnavailable, ErrDecisionParse)
//     fails OPEN: log Warn, allow. The gate never makes things worse when the
//     decision layer is down.
//
// Deny mechanism: the BeforeToolCallHook interface only carries
// BlockResult{Block, Reason} — the AGENT LOOP (internal/agent/loop.go
// executeToolCalls) turns Block=true into a tool-result-shaped
// ExecutionResult whose Result mirrors ConfirmationResponse
// (requires_confirmation=true, action, summary, confirm_arg:"confirmed").

// DefaultDecisionGateTools is the default decision_gate_tools allowlist.
// Both names verified against the real registry (internal/tools/builtin):
// ShellExecuteTool.Name() = "shell" (schemaJobTypeShell,
// schema_constants.go:70) and WriteFileTool.Name() = "file_write"
// (filesystem.go:393). file_delete is intentionally NOT in the default: the
// allowlist is the operator's blast-radius control and writes already cover
// the destructive-by-content class.
var DefaultDecisionGateTools = []string{"shell", "file_write"}

// decisionAsker is the gate's client seam: the subset of *llm.DecisionClient
// the gate consumes. An interface (not the concrete type) keeps the hook
// unit-testable without resolver scaffolding.
type decisionAsker interface {
	Available() bool
	Ask(ctx context.Context, q llm.DecisionQuestion) (*llm.DecisionResult, error)
}

// DecisionGateHook implements agent.BeforeToolCallHook.
type DecisionGateHook struct {
	client    decisionAsker
	threshold float64
	gateTools map[string]bool
	enabled   bool
	logger    *slog.Logger
}

// compile-time: the hook satisfies the registry's before-tool-call interface.
var _ agent.BeforeToolCallHook = (*DecisionGateHook)(nil)

// NewDecisionGateHook builds the irreversibility gate. tools is the
// decision_gate_tools allowlist (nil/empty → DefaultDecisionGateTools).
// threshold is DecisionEscalationThreshold (confidence >= threshold allows).
// The hook is DISABLED until SetEnabled(true) — construction alone never
// alters behavior (opt-in knob, zero overhead by default). Nil client or nil
// hook is safe: BeforeToolCall allows through.
func NewDecisionGateHook(client decisionAsker, threshold float64, tools []string, logger *slog.Logger) *DecisionGateHook {
	if logger == nil {
		logger = slog.Default()
	}
	names := tools
	if len(names) == 0 {
		names = DefaultDecisionGateTools
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return &DecisionGateHook{
		client:    client,
		threshold: threshold,
		gateTools: set,
		logger:    logger.With("component", "decision-gate"),
	}
}

// SetEnabled arms the gate. The nil receiver is a no-op (setter convention:
// every Set* is nil-safe, enforced by setters_test.go conventions).
func (h *DecisionGateHook) SetEnabled(enabled bool) {
	if h == nil {
		return
	}
	h.enabled = enabled
}

// BeforeToolCall consults the decision model on a gated tool call.
// Deny = BlockResult{Block: true, Reason: ...}; the loop renders the
// phase-1 confirmation shape from it. Everything else allows.
func (h *DecisionGateHook) BeforeToolCall(ctx context.Context, toolCall llm.ToolCall) agent.BlockResult {
	if h == nil || !h.enabled {
		return agent.BlockResult{}
	}
	if h.client == nil || !h.client.Available() {
		// No decision model configured → the gate never fires (zero
		// overhead, Contract D).
		return agent.BlockResult{}
	}
	if !h.gateTools[toolCall.Function.Name] {
		return agent.BlockResult{}
	}

	// Confirm re-invoke bypass: the soft-deny Reason promises the model that
	// re-invoking with confirmed=true picks the call up. Without this check
	// the gate re-asks the decision client on EVERY invocation — a
	// deterministic sub-threshold answer loops ask→deny forever (the args
	// differ by confirmed:true, so the loop's cycle detector never fires)
	// until the no-progress ladder vetoes. The established confirm shape is
	// `confirmed, _ := args["confirmed"].(bool)` (epistemic_actions.go,
	// hermes_agent.go); the loop renders confirm_arg:"confirmed" for soft
	// denies (internal/agent/loop.go executeToolCalls). HardError security
	// denies never reach here — this hook only ever produces soft denies.
	if isConfirmedReinvoke(toolCall) {
		h.logger.Debug("decision gate: confirmed re-invoke, skipping ask",
			"tool", toolCall.Function.Name,
		)
		return agent.BlockResult{}
	}

	result, err := h.client.Ask(ctx, h.question(toolCall))
	if err != nil {
		// Fail OPEN — including ErrDecisionUnavailable and
		// ErrDecisionParse. A down decision layer must degrade to the
		// pre-gate behavior (phase-1 confirmation flow only), never
		// block work.
		h.logger.Warn("decision gate failed open",
			"tool", toolCall.Function.Name,
			"error", err,
		)
		return agent.BlockResult{}
	}

	// TWO bars, mirroring the ACP lane (internal/acp/decision.go): the
	// answer must be the exact affirmative option AND confidence must clear
	// the threshold. The answer bar is load-bearing in production:
	// llm.DecisionClient degrades to argmax-only and ALWAYS returns
	// Confidence 1.0, so a confidence-only gate would allow even when the
	// model answers "no". Any non-affirmative answer (including parse-shape
	// noise) escalates to user confirmation.
	if result != nil &&
		strings.EqualFold(strings.TrimSpace(result.Answer), decisionGateAffirmative) &&
		result.Confidence >= h.threshold {
		h.logger.Debug("decision gate allows",
			"tool", toolCall.Function.Name,
			"answer", result.Answer,
			"confidence", result.Confidence,
		)
		return agent.BlockResult{}
	}

	answer := ""
	confidence := 0.0
	if result != nil {
		answer = result.Answer
		confidence = result.Confidence
	}
	h.logger.Info("decision gate escalates to confirmation",
		"tool", toolCall.Function.Name,
		"answer", answer,
		"confidence", confidence,
		"threshold", h.threshold,
	)
	return agent.BlockResult{
		Block: true,
		Reason: fmt.Sprintf(
			"decision gate: answer %q with confidence %.2f does not clear the %q/%.2f bar; user confirmation required",
			answer, confidence, decisionGateAffirmative, h.threshold),
	}
}

// decisionGateAffirmative is the affirmative option label in the gate's
// question() Options ([]string{"yes", "no"}). The Ask result must match it
// exactly (trim + case-insensitive) for the gate to allow.
const decisionGateAffirmative = "yes"

// isConfirmedReinvoke reports whether the tool-call arguments carry
// confirmed=true — the phase-1 confirm flow's re-invoke marker.
func isConfirmedReinvoke(toolCall llm.ToolCall) bool {
	args, err := toolCall.ParsedArguments()
	if err != nil {
		return false
	}
	confirmed, _ := args["confirmed"].(bool)
	return confirmed
}

// question renders the noul question: tool name + one-line args summary in
// State, the fixed prompt, yes/no options, the configured threshold.
func (h *DecisionGateHook) question(toolCall llm.ToolCall) llm.DecisionQuestion {
	return llm.DecisionQuestion{
		State:     fmt.Sprintf("Tool: %s\nArguments: %s", toolCall.Function.Name, summarizeToolArgs(toolCall)),
		Type:      "noul",
		Prompt:    "May this action run?",
		Options:   []string{"yes", "no"},
		Threshold: h.threshold,
	}
}

// summarizeToolArgs renders a bounded one-line args summary for the question
// state. Unparseable args summarize as their raw string (still bounded).
func summarizeToolArgs(toolCall llm.ToolCall) string {
	const maxLen = 300
	args, err := toolCall.ParsedArguments()
	if err != nil {
		raw := toolCall.Function.Arguments
		if len(raw) > maxLen {
			return raw[:maxLen] + "...(truncated)"
		}
		return raw
	}
	data, err := json.Marshal(args)
	if err != nil {
		return "(failed to serialize)"
	}
	s := string(data)
	if len(s) > maxLen {
		return s[:maxLen] + "...(truncated)"
	}
	if strings.TrimSpace(s) == "" || s == "null" {
		return "{}"
	}
	return s
}
