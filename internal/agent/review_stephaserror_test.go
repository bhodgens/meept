package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/task"
)

// Regression: sweep e2e (2026-09-15) — a coder step whose result envelope
// carried success=true and PASSING vet/build/test evidence was rejected as
// "step execution error" because stepHasError scanned the model NARRATION
// inside the envelope and hit "failed to link" (describing a PAST failure
// the agent had already recovered from). Narrative mentions of failure are
// not execution failure; the envelope's success flag is authoritative.
func TestStepHasError_EnvelopeNarrativeNotAnError(t *testing.T) {
	rm := &ReviewManager{logger: testLogger()}
	step := &task.TaskStep{
		ToolHint: "coder",
		Result: `{"evidence":["job j1 completed by agent coder: All checks pass. An initial draft failed to link (no main function), so I switched packages."],` +
			`"job_id":"j1","response":"All checks pass. An initial draft failed to link. Verification: VET_OK BUILD_OK PASS.","status":"completed","success":true}`,
	}
	if rm.stepHasError(step) {
		t.Error("successful envelope whose narrative mentions a past failure must NOT be flagged as error")
	}
}

// The envelope path must still fail on structured errors.
func TestStepHasError_EnvelopeWithErrorField(t *testing.T) {
	rm := &ReviewManager{logger: testLogger()}
	step := &task.TaskStep{
		ToolHint: "coder",
		Result:   `{"error":"tool execution failed: boom","status":"failed","success":false}`,
	}
	if !rm.stepHasError(step) {
		t.Error("envelope with error field must be flagged")
	}
}

// Legacy free-text results keep the prose scan.
func TestStepHasError_FreeTextStillScanned(t *testing.T) {
	rm := &ReviewManager{logger: testLogger()}
	step := &task.TaskStep{
		ToolHint: "coder",
		Result:   "failed to compile module",
	}
	if !rm.stepHasError(step) {
		t.Error("free-text failure must still be flagged")
	}
}
