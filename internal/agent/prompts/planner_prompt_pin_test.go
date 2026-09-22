package prompts

import (
	"strings"
	"testing"
)

// Issue #53: the 8B planner emitted empty plans and task_create calls with
// empty arguments. PlannerAgentPrompt must carry a concrete GOOD example and
// the explicit empty-args prohibition so the failure mode is anchored shut.
func TestPlannerAgentPrompt_EmptyArgsProhibitionAndGoodExample(t *testing.T) {
	p := PlannerAgentPrompt
	lower := strings.ToLower(p)
	for _, marker := range []string{
		"empty arguments",
		"task_create",
		// The good example must show concrete file actions, not narration.
		"file_write",
	} {
		if !strings.Contains(lower, marker) {
			t.Errorf("PlannerAgentPrompt missing required anchor %q\nprompt:\n%s", marker, p)
		}
	}
}
