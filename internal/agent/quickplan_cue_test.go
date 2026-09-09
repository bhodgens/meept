package agent

import "testing"

// TestQuickPlanCuePattern_Positive pins the cue-positive adjudication
// record rules: these orchestration-phrased messages MUST match.
func TestQuickPlanCuePattern_Positive(t *testing.T) {
	cases := []string{
		"using subagents, fix them",
		"implement tasks 3 and 4",
		"commit, push, then run tests",
		"as you find them",
		"no check-ins needed",
		"finish the remaining waves",
	}
	for _, in := range cases {
		if !QuickPlanCuePattern.MatchString(in) {
			t.Errorf("QuickPlanCuePattern.MatchString(%q) = false, want true", in)
		}
	}
}

// TestQuickPlanCuePattern_Negative pins the cue-negative rules: plain
// code/review/question messages MUST NOT match (quickplan-vs-code/git
// is not decidable from text; absence of the cue is the guard signal).
func TestQuickPlanCuePattern_Negative(t *testing.T) {
	cases := []string{
		"review the json files for completeness",
		"compare the top 3 databases",
		"why is the test failing?",
		"add pagination to the API",
	}
	for _, in := range cases {
		if QuickPlanCuePattern.MatchString(in) {
			t.Errorf("QuickPlanCuePattern.MatchString(%q) = true, want false", in)
		}
	}
}
