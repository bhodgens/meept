// decision.go is the ACP session decision seam (acp-permission-decision
// leaf 02, master.md Contract B).
//
// The lane is FAIL-CLOSED: when the decision layer is absent, unavailable,
// errors, or answers below the confidence bar, a session/requestPermission
// is cancelled. This is the deliberate OPPOSITE polarity of the tool-call
// decision gate (internal/tools/builtin/decision_gate.go), which fails OPEN
// — a permission request guards an external side effect inside someone
// else's agent session, so a down decider must not silently grant it
// (master.md decision 3).

package acp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// Decisioner is the session's decision-client seam: the subset of
// *llm.DecisionClient the ACP lane consumes. Its shape is identical to
// builtin.decisionAsker (internal/tools/builtin/decision_gate.go:51) —
// mirrored, NOT shared: builtin and acp are sibling consumers and acp must
// not import builtin.
//
// *llm.DecisionClient satisfies Decisioner without an adapter.
type Decisioner interface {
	Available() bool
	Ask(ctx context.Context, q llm.DecisionQuestion) (*llm.DecisionResult, error)
}

// compile-time proof that the real client satisfies the seam — the same
// assertion style builtin uses for its gate; keeps Contract B honest if
// llm's Ask signature ever drifts.
var _ Decisioner = (*llm.DecisionClient)(nil)

// decisionAskTimeout bounds the Ask call: handlePermission runs on the
// Transport Serve goroutine with no caller context, so the decision lane
// derives its own bounded context. A slow decider must never wedge the
// wire loop into an infinite wait.
const decisionAskTimeout = 15 * time.Second

// decisionPrompt is the fixed question text (Contract B).
const decisionPrompt = "May this action run?"

// decisionAnswerAllow / decisionAnswerDeny are the two option labels.
// Exactly "allow" grants; anything else denies.
const (
	decisionAnswerAllow = "allow"
	decisionAnswerDeny  = "deny"
)

// Decision-lane event texts (frozen contract, master.md Contract B +
// drift correction 6): "allow"/"deny" plus one reason token per deny
// cause. All lowercase per repo convention.
const (
	decisionTextAllow         = "allow"
	decisionTextDeny          = "deny"
	decisionTextNoDecisioner  = "no_decisioner"
	decisionTextUnavailable   = "unavailable"
	decisionTextAskError      = "ask_error"
	decisionTextAnswerDeny    = "answer_deny"
	decisionTextLowConfidence = "low_confidence"
)

// decisionOutcome is the lane's verdict: allow the request or cancel it
// with a reason token for the event stream.
type decisionOutcome struct {
	allow bool
	text  string // "allow", "deny", or a reason token
}

// decidePermission runs the Contract B dispatch for one permission request.
// The caller (handlePermission) owns transport/reply concerns; this function
// only consults the seam and renders the question, so it stays unit-testable
// without a wire.
//
// Confidence note (pinned so nobody "fixes" it wrong): today's
// llm.DecisionClient degrades to Confidence=1.0 argmax-only
// (internal/llm/decision_client.go Ask), so the ANSWER is the live signal
// now; the threshold becomes a real second bar only once label logprobs
// land (decision-model leaves 03/04). Both checks stay forward-compatible.
func decidePermission(ctx context.Context, dec Decisioner, threshold float64, state string) decisionOutcome {
	if dec == nil {
		return decisionOutcome{text: decisionTextNoDecisioner}
	}
	if !dec.Available() {
		return decisionOutcome{text: decisionTextUnavailable}
	}
	res, err := dec.Ask(ctx, llm.DecisionQuestion{
		State:     state,
		Type:      "noul",
		Prompt:    decisionPrompt,
		Options:   []string{decisionAnswerAllow, decisionAnswerDeny},
		Threshold: threshold,
	})
	if err != nil {
		// Fail CLOSED on ANY error — transport failure,
		// ErrDecisionUnavailable, ErrDecisionParse. Opposite polarity of
		// the builtin gate by design (master.md decision 3).
		return decisionOutcome{text: decisionTextAskError}
	}
	if res == nil {
		return decisionOutcome{text: decisionTextAskError}
	}
	if res.Answer != decisionAnswerAllow {
		// "deny" is an explicit decision; any other label (parse-shape
		// noise) is still a deny, tagged answer_deny for the stream.
		if res.Answer == decisionAnswerDeny {
			return decisionOutcome{text: decisionTextDeny}
		}
		return decisionOutcome{text: decisionTextAnswerDeny}
	}
	if res.Confidence < threshold {
		return decisionOutcome{text: decisionTextLowConfidence}
	}
	return decisionOutcome{allow: true, text: decisionTextAllow}
}

// decisionStateBudget mirrors builtin.summarizeToolArgs' bound
// (internal/tools/builtin/decision_gate.go, const maxLen = 300): the raw
// permission params enter the question state truncated, never whole.
const decisionStateBudget = 300

// renderDecisionState builds the question State: agent id (with the
// persona suffix "agent/persona" when one is set, acp-employees dim 2B)
// + the raw session/requestPermission params JSON, bounded. v1 parses
// nothing — whatever the agent sent is what the decider sees (truncated).
func renderDecisionState(agentID, persona string, rawParams []byte) string {
	agent := agentID
	if persona != "" {
		agent += "/" + persona
	}
	params := string(rawParams)
	if len([]rune(params)) > decisionStateBudget {
		params = string([]rune(params)[:decisionStateBudget]) + "...(truncated)"
	}
	if strings.TrimSpace(params) == "" {
		params = "{}"
	}
	return fmt.Sprintf("Agent: %s\nPermission request: %s", agent, params)
}
