package agent

import (
	"strings"
	"testing"
)

// TestStripPromptScaffolding pins the user-facing render strip: step
// descriptions carrying the strategic-planner's session-context injection
// (strategic.go: "## Session context ..." + "## Your Task" + real text)
// must render as the real task text only. Live defect 2026-10-22: the
// task-completed chat card displayed the raw instruction block as step 1.
func TestStripPromptScaffolding(t *testing.T) {
	injected := "## Session context (most recent work in this conversation)\n\n" +
		"If the user's message asks about, refers to, or follows up on this work, answer from the context below.\n\n" +
		"- Prior task: \"do x\" — status: completed\n\n" +
		"## Your Task\n\n" +
		"give me a review of this project"

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"full injection", injected, "give me a review of this project"},
		{"no markers", "plain description", "plain description"},
		{"empty", "", ""},
		{"context block only", "## Session context (most recent work in this conversation)\n\nsome instruction\n", ""},
		{"task marker only", "## Your Task\n\nreal text", "real text"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := StripPromptScaffolding(tc.in); got != tc.want {
				t.Fatalf("StripPromptScaffolding(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestFormatTaskCompletedMessageStripsScaffolding pins the end-to-end render:
// the user-facing completion card must not contain prompt scaffolding or the
// machine evidence envelope, and MUST carry the step's full prose answer
// (2026-10-22: a completed project review was reachable nowhere in chat —
// the card showed only an 80-char preview of the raw envelope).
func TestFormatTaskCompletedMessageStripsScaffolding(t *testing.T) {
	h := &ChatHandler{logger: testLogger()}
	answer := "reviewed 42 files. The build fails in slurp.go init(); $HOME is never expanded; log-init branching is inverted."
	steps := []TaskStepSummary{{
		Description: "## Session context (most recent work in this conversation)\n\nIf the user's message asks...\n\n## Your Task\n\ngive me a review of this project",
		State:       "completed",
		Result:      `{"evidence":["job job-1 completed by agent coder: ` + answer + `"]}`,
	}}
	msg := h.formatTaskCompletedMessage("give me a review of this project", steps, "35s", "", 1, 1, 0)
	for _, banned := range []string{"## Session context", "## Your Task", `{"evidence"`} {
		if strings.Contains(msg, banned) {
			t.Fatalf("completion card leaks %q:\n%s", banned, msg)
		}
	}
	if !strings.Contains(msg, "give me a review of this project") {
		t.Fatalf("completion card lost the task text:\n%s", msg)
	}
	// The full answer prose must be present, not an 80-char preview.
	if !strings.Contains(msg, answer) {
		t.Fatalf("completion card lost the full answer prose:\n%s", msg)
	}
}

// TestBestSummaryResult pins the relay-shape answer picker: last
// completed/approved step wins; non-terminal steps are only a fallback.
func TestBestSummaryResult(t *testing.T) {
	steps := []TaskStepSummary{
		{State: "completed", Result: "first answer"},
		{State: "failed", Result: "error text"},
		{State: "approved", Result: "final answer"},
	}
	if got := bestSummaryResult(steps); got != "final answer" {
		t.Fatalf("bestSummaryResult = %q, want %q", got, "final answer")
	}
	only := []TaskStepSummary{{State: "failed", Result: "partial answer"}}
	if got := bestSummaryResult(only); got != "partial answer" {
		t.Fatalf("fallback = %q, want %q", got, "partial answer")
	}
	if got := bestSummaryResult(nil); got != "" {
		t.Fatalf("nil steps = %q, want empty", got)
	}
}
