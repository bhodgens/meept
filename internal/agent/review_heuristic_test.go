package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

func TestHeuristicReview_ReasoningOnlyTerminationRejected(t *testing.T) {
	rm := &ReviewManager{logger: testLogger()}
	step := &task.TaskStep{
		ToolHint: "coder",
		Result:   "I stopped after extended thinking without producing output. Here is what I have so far -- please provide more specific guidance if you'd like me to continue.",
		// TokenUsage stays 0: the step-job completion path never populates
		// it (no token_usage key in the job result envelope), so the guard
		// must not arm on it.
		TokenUsage: 0,
		Evidence:   nil, // no tool evidence: the loop never called a tool
	}
	if rm.heuristicReviewPasses(step) {
		t.Error("reasoning-only termination must NOT auto-approve")
	}
}

func TestHeuristicReview_ReasoningOnlyWithZeroEvidenceStructRejected(t *testing.T) {
	// Regression (smoke run task-20260911211112.532862000-0002, 2026-09-11):
	// the step-job result JSON carries `evidence` as []string prose;
	// tactical.go's OnJobCompleted unmarshals that into []models.Evidence
	// and Go leaves the failed element as a ZERO-VALUE struct IN the slice
	// (len==1, all-zero fields). The original len(evidence)==0 check saw
	// that artifact and approved a give-up termination. Structural
	// emptiness is not evidence.
	rm := &ReviewManager{logger: testLogger()}
	step := &task.TaskStep{
		ToolHint: "coder",
		Result:   "I stopped after extended thinking without producing output.",
		Evidence: []models.Evidence{{}}, // decode artifact: one zero struct
	}
	if rm.heuristicReviewPasses(step) {
		t.Error("reasoning-only termination with zero-value evidence artifact must NOT auto-approve")
	}
}

func TestHeuristicReview_LegitNoToolReportStillPasses(t *testing.T) {
	rm := &ReviewManager{logger: testLogger()}
	step := &task.TaskStep{
		ToolHint:   "coder",
		Result:     "Added the fence validation module and verified the path checks pass.",
		TokenUsage: 0, // token usage is NOT set by the step-job completion path (see components.go); the guard must not require it
		Evidence:   nil,
	}
	if !rm.heuristicReviewPasses(step) {
		t.Error("legitimate short report without tool evidence should still pass")
	}
}

func TestHeuristicReview_ConversationalHintUnaffected(t *testing.T) {
	rm := &ReviewManager{logger: testLogger()}
	step := &task.TaskStep{
		ToolHint:   "analyze",
		Result:     "I stopped after extended thinking. Here is my partial analysis.",
		TokenUsage: 9000,
	}
	// conversational hints legitimately produce reports without tools;
	// the canned-termination phrase alone does not reject them here
	// (the reasoning watchdog itself is the guard on that path).
	if !rm.heuristicReviewPasses(step) {
		t.Error("conversational hint should keep legacy heuristic behavior")
	}
}

func TestHeuristicReview_WithEvidencePassesDespitePhrase(t *testing.T) {
	rm := &ReviewManager{logger: testLogger()}
	step := &task.TaskStep{
		ToolHint:   "coder",
		Result:     "Worked through the thinking phase, then ran the migration. stopped after extended thinking is not in this text but evidence exists.",
		TokenUsage: 31000,
		Evidence:   []models.Evidence{{Type: "file_hash", Subject: "migration.sql", Value: "abc123"}},
	}
	if !rm.heuristicReviewPasses(step) {
		t.Error("real tool evidence should pass even if phrase appears")
	}
}
