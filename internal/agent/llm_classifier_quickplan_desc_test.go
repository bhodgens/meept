package agent

import (
	"strings"
	"testing"
)

// Campaign 20260918 phase-2 regression (#52): the 8B scattered 17/20 quickplan
// replay cases across 8 lanes because the classifier prompt described quickplan
// only as an abstract property ("Do the work now, end to end, without
// check-ins") with none of the adjudicated surface forms. 16/20 replay
// quickplan inputs match QuickPlanCuePattern; the description must carry the
// same evidence so the two definitions of "quickplan" cannot drift apart.
func TestClassifierLanes_QuickPlanDescriptionCarriesAdjudicatedCues(t *testing.T) {
	desc := strings.ToLower((&LLMClassifier{}).getIntentDescription(string(IntentQuickPlan)))
	for _, cue := range []string{"subagents", "review", "correct", "implement"} {
		if !strings.Contains(desc, cue) {
			t.Errorf("quickplan description missing adjudicated cue %q: %q", cue, desc)
		}
	}
	// The prompt the model actually sees must carry the same evidence.
	c := &LLMClassifier{}
	prompt := strings.ToLower(c.buildClassificationPrompt("implement the plan using subagents"))
	if !strings.Contains(prompt, "subagents") {
		t.Error("classification prompt does not surface the subagents cue")
	}
}
