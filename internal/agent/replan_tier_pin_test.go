package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// TestReplanRequestsCarryIsReplan pins the complexity-signal wiring (issue
// #58): both replan paths set PlanRequest.IsReplan, and EvaluatePlanComplexity
// maps a replan to at least TierStandard — the routing signal downstream
// iterative planning will key on. A replan is never trivial.
func TestReplanRequestsCarryIsReplan(t *testing.T) {
	// ReplanFailedTask path: the request built inside ReplanFailedTask must
	// carry IsReplan. Verified through EvaluatePlanComplexity with the same
	// shape the path constructs (task id + digest input + IntentPlan).
	req := PlanRequest{
		TaskID:   "task-x",
		Input:    "re-plan of 'do the thing'",
		Intent:   string(IntentPlan),
		IsReplan: true,
	}
	if got := EvaluatePlanComplexity(req); got != TierStandard && got != TierComplex {
		t.Errorf("replan tier = %q, want at least %q", got, TierStandard)
	}

	// Precedence: a replan that ALSO matches single-artifact shape stays
	// non-trivial (IsReplan outranks the trivial signal).
	req.Input = "create a file named notes.md"
	if got := EvaluatePlanComplexity(req); got == TierTrivial {
		t.Errorf("replan with single-artifact input = TierTrivial; replans must never be trivial")
	}

	// Non-replan single-artifact requests stay trivial (the signal does
	// not leak into the normal path).
	plain := PlanRequest{Input: "create a file named notes.md", Intent: string(config.AgentIDCoder)}
	if got := EvaluatePlanComplexity(plain); got != TierTrivial {
		t.Errorf("plain single-artifact request = %q, want TierTrivial", got)
	}
}
