package agent

import (
	"testing"
)

// allIntentTypes is the canonical list of every defined IntentType constant.
// When a new intent is added to intent.go, it must also be added here so the
// completeness test catches omissions from SteeringHeuristicTable.
var allIntentTypes = []IntentType{
	IntentUnknown,
	IntentChat,
	IntentReport,
	IntentRecall,
	IntentPlatform,
	IntentStatus,
	IntentCode,
	IntentDebug,
	IntentReview,
	IntentPlan,
	IntentGit,
	IntentSchedule,
	IntentAnalyze,
	IntentSearch,
	IntentResearch,
	IntentSecurity,
	IntentToolUse,
	IntentSkill,
	IntentCompound,
}

func TestSteeringHeuristicTable_Completeness(t *testing.T) {
	t.Parallel()

	for _, it := range allIntentTypes {
		_, exists := SteeringHeuristicTable[it]
		if !exists {
			t.Errorf("SteeringHeuristicTable missing entry for IntentType %q; add an entry (true for steering, false for follow-up)", it)
		}
	}
}

func TestShouldSteer_ExplicitOverride(t *testing.T) {
	t.Parallel()

	intents := []IntentType{
		IntentCode, IntentDebug, IntentSecurity, IntentToolUse,
		IntentChat, IntentRecall, IntentResearch, IntentReport,
		IntentPlatform, IntentStatus, IntentUnknown,
	}

	for _, it := range intents {
		got := shouldSteer(it, true)
		if !got {
			t.Errorf("shouldSteer(%q, explicitSteerMode=true) = false, want true", it)
		}
	}
}

func TestShouldSteer_SteeringIntents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		intent IntentType
	}{
		{name: "code", intent: IntentCode},
		{name: "debug", intent: IntentDebug},
		{name: "security", intent: IntentSecurity},
		{name: "tool_use", intent: IntentToolUse},
		{name: "git", intent: IntentGit},
		{name: "plan", intent: IntentPlan},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := shouldSteer(tt.intent, false)
			if !got {
				t.Errorf("shouldSteer(%q, false) = false, want true", tt.intent)
			}
		})
	}
}

func TestShouldSteer_FollowUpIntents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		intent IntentType
	}{
		{name: "chat", intent: IntentChat},
		{name: "recall", intent: IntentRecall},
		{name: "research", intent: IntentResearch},
		{name: "report", intent: IntentReport},
		{name: "platform", intent: IntentPlatform},
		{name: "status", intent: IntentStatus},
		{name: "review", intent: IntentReview},
		{name: "schedule", intent: IntentSchedule},
		{name: "analyze", intent: IntentAnalyze},
		{name: "search", intent: IntentSearch},
		{name: "skill", intent: IntentSkill},
		{name: "compound", intent: IntentCompound},
		{name: "unknown", intent: IntentUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := shouldSteer(tt.intent, false)
			if got {
				t.Errorf("shouldSteer(%q, false) = true, want false", tt.intent)
			}
		})
	}
}

func TestShouldSteer_UnknownIntentType(t *testing.T) {
	t.Parallel()

	got := shouldSteer(IntentType("nonexistent_intent"), false)
	if got {
		t.Errorf("shouldSteer(nonexistent, false) = true, want false")
	}
}

func TestSteeringHeuristicTable_HighUrgencyCount(t *testing.T) {
	t.Parallel()

	const wantSteering = 6 // code, debug, security, tool_use, git, plan

	var got int
	for _, v := range SteeringHeuristicTable {
		if v {
			got++
		}
	}

	if got != wantSteering {
		t.Errorf("SteeringHeuristicTable has %d true entries, want %d", got, wantSteering)
	}
}
