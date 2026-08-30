package agent

import (
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/llm"
)

// TurnStatus is the machine-maintained snapshot rendered into the per-turn
// [status] prompt block (harness-eval leaf 13, contract C8). It is populated
// by the loop from state it already holds; it never carries model prose,
// message text, model names, or timestamps.
type TurnStatus struct {
	// TurnIndex is the loop's turn index at prompt-assembly time (the count
	// of turns already completed for this loop). Expected non-negative; the
	// renderer does not clamp.
	TurnIndex int
	// ToolsThisTurn is the count of tool definitions exposed to the model
	// this turn (post skill filtering). Expected non-negative.
	ToolsThisTurn int
	// Isolation is the child-context isolation mode. Empty (or any value
	// that is not a known ContextIsolation constant) renders as "unknown".
	Isolation ContextIsolation
	// Speak is the run's delivery kind. Zero (SpeakSession) is ambiguous
	// with "not set", so it renders as "session" only when SessionAttached
	// corroborates it; otherwise it fails closed to "unknown".
	Speak SpeakKind
	// SessionAttached marks the run as bound to a watching chat session.
	SessionAttached bool
	// IsolatedChild marks the run as an isolated child (C4): it can never
	// reach the user, so the bar always renders speak=parent for it.
	IsolatedChild bool
	// GateState is the roster gate's result on THIS turn: "skipped",
	// "passed", "failed", or "" when the gate did not run this turn
	// (renders "n/a"). Any other value renders "unknown" — the bar never
	// invents success.
	GateState string
}

// StatusBar renders the deterministic agent status block (contract C8):
//
//	[status] turn=3 tools=5 isolation=artifact_only speak=notify gate=passed
//
// Deterministic, code-maintained, and compact: lowercase labels, fixed field
// order, no timestamps, no model names, no message text, no model prose.
// Missing or invalid fields fail closed to "unknown" ("n/a" for a gate that
// did not run); the bar never invents success. Errors cannot occur — the
// function is total over TurnStatus.
func StatusBar(s TurnStatus) string {
	return fmt.Sprintf("[status] turn=%d tools=%d isolation=%s speak=%s gate=%s",
		s.TurnIndex,
		s.ToolsThisTurn,
		isolationLabel(s.Isolation),
		speakLabel(s),
		gateLabel(s.GateState),
	)
}

// isolationLabel renders a known ContextIsolation constant verbatim (they are
// already lowercase) and fails closed to "unknown" for the empty value and
// for any unrecognized value.
func isolationLabel(iso ContextIsolation) string {
	switch iso {
	case IsolationArtifactOnly:
		return string(IsolationArtifactOnly)
	case IsolationSharedTranscript:
		return string(IsolationSharedTranscript)
	case IsolationBusMessage:
		return string(IsolationBusMessage)
	default:
		return "unknown"
	}
}

// speakLabel renders the delivery kind. An isolated child always renders
// "parent" (C4 trumps: fail closed, matching ClassifyRun). An explicit
// non-zero kind renders verbatim; the zero kind (SpeakSession) renders
// "session" only when SessionAttached corroborates it — zero alone is
// indistinguishable from "not set" and fails closed to "unknown". Unknown
// kinds render "unknown".
func speakLabel(s TurnStatus) string {
	if s.IsolatedChild {
		return SpeakParent.String() // C4: isolated children never reach the user
	}
	switch s.Speak {
	case SpeakNotify:
		return SpeakNotify.String()
	case SpeakParent:
		return SpeakParent.String()
	case SpeakSession:
		if s.SessionAttached {
			return SpeakSession.String()
		}
		return "unknown"
	default:
		return "unknown"
	}
}

// gateLabel renders the roster gate state for this turn. "" (the gate did
// not run this turn) renders "n/a". Known states render verbatim. Any other
// value — including wrong-case variants of known states — fails closed to
// "unknown": the bar never invents a passing gate.
func gateLabel(state string) string {
	switch state {
	case "":
		return "n/a"
	case "skipped", "passed", "failed":
		return state
	default:
		return "unknown"
	}
}

// isolationForLoop derives the isolation mode the loop can honestly report.
// The loop holds only the isolatedChild bit, not a ContextIsolation value:
// isolated children are spawned under artifact_only by convention (leaf 10
// call sites in dispatcher/orchestrator/collaboration all use the
// IsolationArtifactOnly default), while attached and detached runs have no
// child-spawn isolation to report. Fail closed: "" renders "unknown".
func isolationForLoop(isolatedChild bool) ContextIsolation {
	if isolatedChild {
		return IsolationArtifactOnly
	}
	return ""
}

// turnStatus assembles this turn's TurnStatus from loop-visible state at
// prompt-assembly time:
//   - TurnIndex is the loop's completed-turn counter (0 on the first build).
//   - ToolsThisTurn is the count of tool definitions exposed to the model
//     this turn (post skill filtering; the skill-filtered registry swap
//     happens before prompt assembly in RunOnce).
//   - Isolation/Speak/attachment bits come from the loop's speak context
//     (leaf 11); C4 makes an isolated child always report speak=parent.
//   - GateState is "" (renders "n/a"): the roster gate Evaluate hook runs
//     POST-turn, so at assembly time the gate has not run this turn — the
//     contract says use "" when the gate did not run this turn.
func (l *AgentLoop) turnStatus() TurnStatus {
	_, attached, isolated := l.speakContextSnapshot()

	l.mu.RLock()
	reg := l.registry
	turns := l.turnCounter
	l.mu.RUnlock()

	var toolCount int
	if reg != nil {
		toolCount = len(reg.List())
	}

	return TurnStatus{
		TurnIndex:       turns,
		ToolsThisTurn:   toolCount,
		Isolation:       isolationForLoop(isolated),
		Speak:           ClassifyRun(attached, isolated),
		SessionAttached: attached,
		IsolatedChild:   isolated,
		GateState:       "", // gate runs post-turn; not run at assembly time
	}
}

// compactToolIndex renders the COMPACT tool index for indexed schema mode:
// one line per tool definition, "- name: description", sorted by name
// (registry.ToLLMDefinitions already sorts). Stubbed definitions carry the
// "use tool_view{name}." marker; no JSON schemas, no per-tool parameter
// dumps — full schemas stay behind the tool_view expansion and never enter
// the prompt prefix. The definitions for a fixed registry and schema mode
// are byte-invariant within a session, so the index is eligible for a
// Stable prompt section (harness-eval leaf 13 Task 3).
func compactToolIndex(defs []llm.ToolDefinition) string {
	var sb strings.Builder
	for i, def := range defs {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("- ")
		sb.WriteString(def.Function.Name)
		sb.WriteString(": ")
		sb.WriteString(def.Function.Description)
	}
	return sb.String()
}
