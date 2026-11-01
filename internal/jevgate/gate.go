package jevgate

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/llm"
)

// ScoreClient is the gate's scorer seam: the subset of *llm.DecisionClient
// the gate consumes (Contract C1's ScoreCommand). An interface keeps the
// hook testable without resolver scaffolding.
type ScoreClient interface {
	ScoreCommand(ctx context.Context, serializedContext string) (*llm.CommandScoreResult, error)
}

// Recorder is the capture seam for leaf 02 (Contract C4 chokepoints). The
// hook carries it as a nil-tolerant field; leaf 02 attaches an
// implementation without touching gate logic.
type Recorder interface {
	RecordScore(ctx context.Context, command string, serializedContext string, result *llm.CommandScoreResult, escalated bool, reason string)
}

// ScoreGateHook implements agent.BeforeToolCallHook with FAIL-CLOSED
// polarity on shell tool calls (Contract C3, rationale in the package doc).
//
// ALLOW (fall through to the lower security stages) iff ALL of:
//   - the tool call is a shell call,
//   - the scorer returned a result (no error, non-nil),
//   - the result is NOT Degraded,
//   - the parsed SCORE ANSWER cleared the band parse (guaranteed by
//     ScoreCommand returning non-nil + nil error -- the answer bar from the
//     decision_gate.go:150-158 confidence-only bug class),
//   - result.Score >= threshold, AND
//   - result.Confidence >= confidence floor (low confidence VETOES: forces
//     ASK, never allows).
//
// EVERYTHING ELSE escalates: scorer error, nil result, Degraded,
// unparseable answer, below-threshold score, low confidence, scorer
// unavailable. Escalation is agent.BlockResult{Block: true, ...}; the agent
// loop converts the soft deny into the existing confirmation flow (same
// conversion as the builtin decision gate -- decision_gate.go:33-56).
type ScoreGateHook struct {
	scorer     ScoreClient
	config     ScoreGateConfig
	confirm    ConfirmFlow
	logger     *slog.Logger
	recorder   Recorder // nil-tolerant capture seam (leaf 02)
	toolFilter func(llm.ToolCall) bool
}

// ConfirmFlow is the confirmation-flow seam. The deny-to-confirmation
// conversion itself lives in the agent loop (Block=true on a BeforeToolCall
// hook renders the phase-1 ConfirmationResponse shape); the flow field is
// kept for leaf 02's chokepoint recording and for callers that need an
// explicit handle on "this call went to ASK".
type ConfirmFlow interface {
	// Escalated marks a call as escalated to human confirmation.
	Escalated(ctx context.Context, command, reason string)
}

// shellOnly is the default tool filter: shell calls only (the shell tool's
// registry name, shell.go schemaJobTypeShell).
func shellOnly(toolCall llm.ToolCall) bool {
	return toolCall.Function.Name == "shell"
}

// compile-time: the hook satisfies the registry's before-tool-call interface.
var _ agent.BeforeToolCallHook = (*ScoreGateHook)(nil)

// NewScoreGateHook builds the hook (Contract C3). The hook is INERT until
// SetEnabled(true) -- construction alone never alters behavior (opt-in knob,
// zero overhead by default). Nil hook is safe: BeforeToolCall allows
// through.
func NewScoreGateHook(scorer ScoreClient, config ScoreGateConfig, confirm ConfirmFlow, logger *slog.Logger) *ScoreGateHook {
	if logger == nil {
		logger = slog.Default()
	}
	return &ScoreGateHook{
		scorer:     scorer,
		config:     config,
		confirm:    confirm,
		logger:     logger.With("component", "jev-score-gate"),
		toolFilter: shellOnly,
	}
}

// SetEnabled arms the gate. The nil receiver is a no-op (setter convention:
// every Set* is nil-safe, enforced by setters_test.go conventions).
func (h *ScoreGateHook) SetEnabled(enabled bool) {
	if h == nil {
		return
	}
	h.config.Enabled = enabled
}

// SetRecorder attaches the leaf-02 capture seam. Nil clears.
func (h *ScoreGateHook) SetRecorder(r Recorder) {
	if h == nil {
		return
	}
	h.recorder = r
}

// Enabled reports the armed state.
func (h *ScoreGateHook) Enabled() bool {
	return h != nil && h.config.Enabled
}

// BeforeToolCall runs the fail-closed gate on a shell tool call.
func (h *ScoreGateHook) BeforeToolCall(ctx context.Context, toolCall llm.ToolCall) agent.BlockResult {
	if h == nil || !h.config.Enabled {
		// Disabled: pure passthrough, zero overhead, zero behavior
		// change (opt-in default).
		return agent.BlockResult{}
	}
	if h.toolFilter == nil || !h.toolFilter(toolCall) {
		return agent.BlockResult{}
	}
	if h.scorer == nil {
		// Scorer unavailable == ASK, never allow (fail-closed).
		return h.escalate(ctx, toolCall, "score gate: scorer unavailable; user confirmation required")
	}

	cmd, _ := extractCommand(toolCall)
	serialized := SerializeCommandContext(cmd, "", extractFacts(toolCall))

	result, err := h.scorer.ScoreCommand(ctx, serialized)
	if err != nil {
		// FAIL-CLOSED: transport error, ErrDecisionUnavailable,
		// ErrDecisionParse -- every scorer failure escalates.
		rec := h.escalate(ctx, toolCall, fmt.Sprintf(
			"score gate: scorer error (%v); user confirmation required", err))
		h.record(ctx, cmd, serialized, nil, true, err.Error())
		return rec
	}
	if result == nil {
		rec := h.escalate(ctx, toolCall, "score gate: scorer returned no result; user confirmation required")
		h.record(ctx, cmd, serialized, nil, true, "nil result")
		return rec
	}
	if result.Degraded {
		// Degraded (argmax-only / parse-fallback) lanes never allow:
		// the score cannot be trusted at the operational threshold.
		rec := h.escalate(ctx, toolCall, fmt.Sprintf(
			"score gate: degraded score answer %q; user confirmation required", result.Answer))
		h.record(ctx, cmd, serialized, result, true, "degraded")
		return rec
	}
	if result.Confidence < h.config.effectiveConfFloor() {
		// Confidence is a VETO signal: low confidence forces ASK.
		rec := h.escalate(ctx, toolCall, fmt.Sprintf(
			"score gate: confidence %.2f below floor %.2f; user confirmation required",
			result.Confidence, h.config.effectiveConfFloor()))
		h.record(ctx, cmd, serialized, result, true, "low confidence")
		return rec
	}
	if result.Score < h.config.effectiveThreshold() {
		rec := h.escalate(ctx, toolCall, fmt.Sprintf(
			"score gate: score %.2f below threshold %.2f; user confirmation required",
			result.Score, h.config.effectiveThreshold()))
		h.record(ctx, cmd, serialized, result, true, "below threshold")
		return rec
	}

	// Clean allow: parsed band answer + not degraded + score and
	// confidence both clear. The gate DECIDES whether to ask; it never
	// executes or marks the action done.
	h.logger.Debug("jev score gate allows",
		"tool", toolCall.Function.Name,
		"score", result.Score,
		"confidence", result.Confidence,
	)
	h.record(ctx, cmd, serialized, result, false, "allowed")
	return agent.BlockResult{}
}

// escalate converts a fail-closed verdict into the soft deny the agent loop
// renders as the confirmation flow. Never a HardError: the human decides.
func (h *ScoreGateHook) escalate(ctx context.Context, toolCall llm.ToolCall, reason string) agent.BlockResult {
	h.logger.Info("jev score gate escalates to confirmation",
		"tool", toolCall.Function.Name,
		"reason", reason,
	)
	if h.confirm != nil {
		h.confirm.Escalated(ctx, toolCall.Function.Arguments, reason)
	}
	return agent.BlockResult{Block: true, Reason: reason}
}

// record emits the capture seam; nil-tolerant (leaf 02 attaches later).
func (h *ScoreGateHook) record(ctx context.Context, command, serialized string, result *llm.CommandScoreResult, escalated bool, reason string) {
	if h.recorder == nil {
		return
	}
	h.recorder.RecordScore(ctx, command, serialized, result, escalated, reason)
}

// extractCommand pulls the shell command text out of the tool call args
// (the shell tool's "command" property). Unparseable or empty args yield ""
// -- the gate still runs and, with an empty command context, the scorer's
// answer decides; any scorer failure still fails closed.
func extractCommand(toolCall llm.ToolCall) (string, error) {
	args, err := toolCall.ParsedArguments()
	if err != nil {
		return "", err
	}
	cmd, _ := args["command"].(string)
	return cmd, nil
}

// extractFacts derives the session facts block for the serialized context
// from the tool call itself. This wave the hook has no session-context
// injection seam, so facts is empty (the byte-stable template renders the
// empty facts block); the wiring wave can extend this without a gate-logic
// change.
func extractFacts(toolCall llm.ToolCall) map[string]string {
	return map[string]string{}
}
