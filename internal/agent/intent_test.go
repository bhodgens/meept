package agent

import "testing"

func TestIntentCategory(t *testing.T) {
	tests := []struct {
		intent IntentType
		want   IntentCategory
	}{
		{IntentChat, CategoryInline},
		{IntentReport, CategoryInline},
		{IntentRecall, CategoryInline},
		{IntentPlatform, CategoryInline},
		{IntentAnalyze, CategoryInline},
		{IntentSearch, CategoryInline},
		{IntentResearch, CategoryInline},
		{IntentSkill, CategoryInline},
		{IntentCode, CategoryDefer},
		{IntentDebug, CategoryDefer},
		{IntentReview, CategoryDefer},
		{IntentPlan, CategoryDefer},
		{IntentGit, CategoryDefer},
		{IntentSchedule, CategoryDefer},
		{IntentCompound, CategoryDefer},
		{IntentUnknown, CategoryInline},
		{IntentType("nonexistent"), CategoryInline},
	}
	for _, tt := range tests {
		got := tt.intent.Category()
		if got != tt.want {
			t.Errorf("%s.Category() = %s, want %s", tt.intent, got, tt.want)
		}
	}
}

func TestIntentDefaultAgent(t *testing.T) {
	tests := []struct {
		intent IntentType
		want   string
	}{
		{IntentChat, "chat"},
		{IntentReport, "chat"},
		{IntentRecall, "chat"},
		{IntentPlatform, "chat"},
		{IntentCode, "coder"},
		{IntentDebug, "debugger"},
		{IntentReview, "coder"},
		{IntentPlan, "planner"},
		{IntentGit, "committer"},
		{IntentSchedule, "scheduler"},
		{IntentAnalyze, "analyst"},
		{IntentSearch, "analyst"},
		{IntentResearch, "researcher"},
		{IntentSkill, "skill"},
		{IntentCompound, "orchestrator"},
		{IntentUnknown, "chat"},
	}
	for _, tt := range tests {
		got := tt.intent.DefaultAgent()
		if got != tt.want {
			t.Errorf("%s.DefaultAgent() = %s, want %s", tt.intent, got, tt.want)
		}
	}
}

func TestIntentRequiresPlanning(t *testing.T) {
	tests := []struct {
		intent IntentType
		want   bool
	}{
		{IntentCode, true},
		{IntentPlan, true},
		{IntentCompound, true},
		{IntentChat, false},
		{IntentDebug, false},
		{IntentGit, false},
		{IntentSchedule, false},
		{IntentAnalyze, false},
		{IntentSearch, false},
		{IntentReport, false},
		{IntentRecall, false},
		{IntentPlatform, false},
		{IntentSkill, false},
	}
	for _, tt := range tests {
		got := tt.intent.RequiresPlanning()
		if got != tt.want {
			t.Errorf("%s.RequiresPlanning() = %v, want %v", tt.intent, got, tt.want)
		}
	}
}

func TestIntentShouldCreateTask(t *testing.T) {
	tests := []struct {
		intent IntentType
		want   bool
	}{
		{IntentCode, true},
		{IntentDebug, true},
		{IntentPlan, true},
		{IntentSchedule, true},
		{IntentGit, true},
		{IntentCompound, true},
		{IntentChat, false},
		{IntentReport, false},
		{IntentRecall, false},
		{IntentPlatform, false},
		{IntentAnalyze, false},
		{IntentSearch, false},
		{IntentSkill, false},
	}
	for _, tt := range tests {
		got := tt.intent.ShouldCreateTask()
		if got != tt.want {
			t.Errorf("%s.ShouldCreateTask() = %v, want %v", tt.intent, got, tt.want)
		}
	}
}

func TestIntentShouldDispatchAsync(t *testing.T) {
	tests := []struct {
		intent           IntentType
		requiresPlanning bool
		want             bool
	}{
		{IntentCode, false, true},
		{IntentDebug, false, true},
		{IntentPlan, false, true},
		{IntentGit, false, true},
		{IntentCompound, false, true},
		{IntentSchedule, false, false},
		{IntentSchedule, true, true},
		{IntentChat, false, false},
		{IntentReport, false, false},
		{IntentRecall, false, false},
		{IntentPlatform, false, false},
		{IntentAnalyze, false, false},
		{IntentSearch, false, false},
		{IntentSkill, false, false},
	}
	for _, tt := range tests {
		got := tt.intent.ShouldDispatchAsync(tt.requiresPlanning)
		if got != tt.want {
			t.Errorf("%s.ShouldDispatchAsync(%v) = %v, want %v", tt.intent, tt.requiresPlanning, got, tt.want)
		}
	}
}

func TestIsValidIntentType(t *testing.T) {
	valid := []string{
		"chat", "report", "recall", "platform",
		"code", "debug", "review", "plan", "git",
		"schedule", "analyze", "search", "skill", "compound",
	}
	for _, s := range valid {
		if !IsValidIntentType(s) {
			t.Errorf("IsValidIntentType(%q) = false, want true", s)
		}
	}
	invalid := []string{"", "unknown", "foo", "bar", "CODE", "Chat"}
	for _, s := range invalid {
		if IsValidIntentType(s) {
			t.Errorf("IsValidIntentType(%q) = true, want false", s)
		}
	}
}

func TestIntentKeywords(t *testing.T) {
	allTypes := []IntentType{
		IntentChat, IntentReport, IntentRecall, IntentPlatform,
		IntentCode, IntentDebug, IntentReview, IntentPlan, IntentGit,
		IntentSchedule, IntentAnalyze, IntentSearch, IntentSkill,
		IntentCompound,
	}
	for _, it := range allTypes {
		kw := it.Keywords()
		if kw == nil {
			t.Errorf("%s.Keywords() = nil, want non-nil", it)
		}
	}
	if kw := IntentType("nonexistent").Keywords(); kw != nil {
		t.Errorf("nonexistent.Keywords() = %v, want nil", kw)
	}
}

func TestIntentType_SuggestedMode(t *testing.T) {
	cases := []struct {
		in   IntentType
		want string
	}{
		{IntentChat, "direct"},
		{IntentRecall, "direct"},
		{IntentStatus, "direct"},
		{IntentReport, "direct"},
		{IntentPlatform, "direct"},
		{IntentSearch, "direct"},
		{IntentCode, "plan"},
		{IntentDebug, "plan"},
		{IntentGit, "plan"},
		{IntentToolUse, "plan"},
		{IntentSecurity, "plan"},
		{IntentCompound, "spec_pair"},
		{IntentPlan, "spec_plan"},
		{IntentArchitect, "spec_plan"},
		{IntentUnknown, "plan"}, // default
	}
	for _, c := range cases {
		got := c.in.SuggestedMode()
		if got != c.want {
			t.Errorf("%s.SuggestedMode() = %q; want %q", c.in, got, c.want)
		}
	}
}
