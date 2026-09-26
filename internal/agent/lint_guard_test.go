package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/task"
)

// Runs 33-35: conversational steps (chat/recall/status replies) must never
// pass through the script-lint filter chain — a prose reply inside a ```js
// fence fails node --check and replaces the whole turn with the checker
// error. Code-producing intents stay linted.
func TestLintGuard(t *testing.T) {
	cases := []struct {
		hint string
		want bool
	}{
		{string(IntentCode), true},
		{string(IntentDebug), true},
		{string(IntentReview), true},
		{string(IntentPlan), true},
		{string(IntentQuickPlan), true},
		{string(IntentChat), false},
		{string(IntentRecall), false},
		{string(IntentStatus), false},
		{string(IntentPlatform), false},
		{string(IntentReport), false},
		{string(IntentUnknown), false},
		// Unrecognized hints keep legacy (linted) behavior.
		{"write_file", true},
	}
	for _, tc := range cases {
		step := &task.TaskStep{ToolHint: tc.hint}
		if got := lintGuard(step); got != tc.want {
			t.Errorf("lintGuard(%q) = %v, want %v", tc.hint, got, tc.want)
		}
	}
	if lintGuard(nil) {
		t.Error("lintGuard(nil) = true, want false")
	}
}
