package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/task"
)

// Regression (agent sweep e2e, 2026-09-15): a chat step reported
// "Extraction ran clean on the first pass" — claiming a json_extract tool
// execution — while metrics.db showed ZERO calls routed to the extraction
// endpoint. The step was heuristic-approved because its tool_hint is
// conversational and the artifact-claims guard doesn't apply to
// conversational hints. A conversational agent claiming it RAN a named
// tool with no tool evidence behind it is the same hallucination shape.
func TestHeuristicReview_ToolExecutionClaimWithoutEvidenceRejected(t *testing.T) {
	rm := &ReviewManager{logger: testLogger()}
	step := &task.TaskStep{
		ToolHint: "chat",
		Result: `{"evidence":["job j1 completed by agent chat: Done. Pulled the title and year out of that line."],` +
			`"response":"Extraction ran clean on the first pass — no fuss, no drama.","status":"completed","success":true}`,
	}
	if rm.heuristicReviewPasses(step) {
		t.Error("tool-execution claim with no tool evidence must NOT auto-approve")
	}
}

// A conversational answer that genuinely is the product (no tool-run
// claims) still passes.
func TestHeuristicReview_PlainConversationalAnswerStillPasses(t *testing.T) {
	rm := &ReviewManager{logger: testLogger()}
	step := &task.TaskStep{
		ToolHint: "chat",
		Result:   "17 * 23 = 391, because 17 times 23 is the product of the two numbers.",
	}
	if !rm.heuristicReviewPasses(step) {
		t.Error("plain conversational answer should pass")
	}
}

// claimsToolExecution marker coverage.
func TestClaimsToolExecution(t *testing.T) {
	positives := []string{
		"Extraction ran clean on the first pass",
		"I called the json_extract tool with the schema",
		"json_extract tool returned the record",
		"Ran web_fetch against the docs",
		"extracted via json_extract: title=...",
	}
	for _, p := range positives {
		if !claimsToolExecution(p) {
			t.Errorf("claimsToolExecution(%q) = false, want true", p)
		}
	}
	negatives := []string{
		"17 * 23 = 391",
		"Here is a haiku about version control.",
		"I considered using my tools but none were needed.",
	}
	for _, n := range negatives {
		if claimsToolExecution(n) {
			t.Errorf("claimsToolExecution(%q) = true, want false", n)
		}
	}
}
