package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/plan"
)

// M11 pin (2026-09-08 audit): PhaseSpecsFromPlan must decrement PHASE-level
// DependsOn ordinals, same as step-level deps — the compiler writes 1-based
// ordinals in both places, but PlanPhaseSpec.DependsOn is 0-indexed.
func TestPhaseSpecsFromPlan_DecrementsPhaseDependsOn(t *testing.T) {
	in := []plan.PhaseSpec{
		{Name: "one", Steps: []plan.StepSpec{{Description: "s1"}}},
		{
			Name:      "two",
			Steps:     []plan.StepSpec{{Description: "s2"}},
			DependsOn: []int{1, 2}, // 1-based: depends on phases 1 and 2
		},
	}
	out := PhaseSpecsFromPlan(in)
	if len(out) != 2 {
		t.Fatalf("got %d phases, want 2", len(out))
	}
	got := out[1].DependsOn
	if len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Errorf("phase DependsOn = %v, want [0 1] (decremented from [1 2])", got)
	}
	if len(out[0].DependsOn) != 0 {
		t.Errorf("phase 0 DependsOn = %v, want empty", out[0].DependsOn)
	}
}

// Guard behavior: a zero ordinal (invalid in the 1-based dialect) is
// dropped, not shifted to -1.
func TestPhaseSpecsFromPlan_DropsZeroOrdinal(t *testing.T) {
	in := []plan.PhaseSpec{
		{Name: "one", Steps: []plan.StepSpec{{Description: "s1"}}, DependsOn: []int{0, 1}},
	}
	out := PhaseSpecsFromPlan(in)
	got := out[0].DependsOn
	if len(got) != 1 || got[0] != 0 {
		t.Errorf("phase DependsOn = %v, want [0] (zero ordinal dropped)", got)
	}
}
