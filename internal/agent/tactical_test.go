package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/task"
)

func TestSelectAgent(t *testing.T) {
	ts := &TacticalScheduler{}

	tests := []struct {
		toolHint  string
		wantAgent string
	}{
		{"code", "coder"},
		{"refactor", "coder"},
		{"debug", "debugger"},
		{"fix", "debugger"},
		{"analyze", "analyst"},
		{"research", "analyst"},
		{"git", "committer"},
		{"commit", "committer"},
		{"schedule", "scheduler"},
		{"plan", "planner"},
		{"chat", "chat"},
		{"", "chat"},
		{"unknown", "chat"},
	}

	for _, tt := range tests {
		step := &task.TaskStep{ToolHint: tt.toolHint}
		got := ts.selectAgent(step)
		if got != tt.wantAgent {
			t.Errorf("selectAgent(%q) = %q, want %q", tt.toolHint, got, tt.wantAgent)
		}
	}
}

func TestTacticalScheduler_IsRateLimitError(t *testing.T) {
	ts := &TacticalScheduler{}

	cases := []struct {
		msg  string
		want bool
	}{
		{"HTTP 429: Too Many Requests", true},
		{"anthropic: rate limit exceeded", true},
		{"quota exceeded for model", true},
		{"rate_limit_error on provider X", true},
		{"", false},
		{"context deadline exceeded", false},
		{"permission denied", false},
	}
	for _, tc := range cases {
		if got := ts.isRateLimitError(tc.msg); got != tc.want {
			t.Errorf("isRateLimitError(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}
