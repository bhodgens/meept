package employee

import (
	"testing"
)

func TestGoal_AutonomyExtension(t *testing.T) {
	g := &Goal{
		MaxRounds:     2,
		RoundsStarted: 0,
	}

	if g.MaxRounds != 2 {
		t.Errorf("MaxRounds = %d, want 2", g.MaxRounds)
	}
	if g.RoundsStarted != 0 {
		t.Errorf("RoundsStarted = %d, want 0", g.RoundsStarted)
	}

	if g.IsArmed() {
		t.Error("New goal should be disarmed by default")
	}
	g.Activate()
	if !g.IsArmed() {
		t.Error("Goal should be armed after Activate()")
	}
	g.Disarm()
	if g.IsArmed() {
		t.Error("Goal should be disarmed after Disarm()")
	}

	if g.IsBlockedAfterNConsecutive() {
		t.Error("Goal should not be blocked yet")
	}
}

func TestGoal_StateTransitions_Blocked(t *testing.T) {
	g := &Goal{
		State: GoalBlocked,
		BlockedReason: &BlockedReason{
			Code:    "round-limit",
			Message: "Goal reached its configured limit of 2 rounds.",
		},
	}

	if g.State != GoalBlocked {
		t.Errorf("State = %v, want GoalBlocked", g.State)
	}

	state, err := ParseGoalState("blocked")
	if err != nil {
		t.Fatalf("ParseGoalState(\"blocked\") failed: %v", err)
	}
	if state != GoalBlocked {
		t.Errorf("ParseGoalState(\"blocked\") = %v, want GoalBlocked", state)
	}

	if g.BlockedReason == nil || g.BlockedReason.Code != "round-limit" {
		t.Error("BlockedReason is incorrect or missing")
	}
}
