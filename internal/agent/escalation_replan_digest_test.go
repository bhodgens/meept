package agent

import (
	"strings"
	"testing"
)

func TestBuildReplanDigest_BoundedWithLargeStepResult(t *testing.T) {
	huge := strings.Repeat("Evidence from the full step result. Lorem ipsum dolor sit amet. ", 200) // ~13KB
	digest := buildReplanDigest(
		"fix the flaky auth integration test",
		[]string{"Reproduce the failure locally"},
		[]string{"Patch the token refresh handler", "Re-run the test suite"},
		huge+"\nsecond line that must never appear\nthird line",
	)
	if got := len([]rune(digest)); got > replanDigestMaxChars {
		t.Fatalf("digest = %d runes, want <= %d", got, replanDigestMaxChars)
	}
	if strings.Contains(digest, "second line") || strings.Contains(digest, "third line") {
		t.Errorf("digest leaked step-result lines beyond the first:\n%s", digest)
	}
	if !strings.Contains(digest, "fix the flaky auth integration test") {
		t.Errorf("digest lost the task description:\n%s", digest)
	}
	if !strings.Contains(digest, "Patch the token refresh handler") {
		t.Errorf("digest lost the remaining-step list:\n%s", digest)
	}
	// A prefix of the reason survives (bounded), proving the reason was
	// incorporated rather than dropped.
	if !strings.Contains(digest, string([]rune(huge)[:100])) {
		t.Errorf("digest lost the bounded failure reason prefix:\n%.400s", digest)
	}
}

func TestBuildReplanDigest_Empty(t *testing.T) {
	digest := buildReplanDigest("", nil, nil, "")
	if digest == "" {
		t.Fatal("expected a non-empty digest skeleton")
	}
	if !strings.Contains(digest, "Failure:") {
		t.Errorf("expected Failure section, got: %s", digest)
	}
}

// Pin (issue #58 capability 2): the failure block renders the concrete
// per-step failure — description, agent, and bounded error text — under the
// "## Previous attempt failed" header.
func TestBuildFailureBlock_CarriesConcreteFailure(t *testing.T) {
	block := buildFailureBlock([]stepFailure{
		{
			Description: "Create the sprint backlog task via task_create",
			Agent:       "task-manager",
			Error:       "task_create failed: name is missing\nargs were: {\"description\":\"...\"}",
		},
	})
	if !strings.Contains(block, "## Previous attempt failed") {
		t.Errorf("expected failure-block header, got: %s", block)
	}
	if !strings.Contains(block, "Create the sprint backlog task via task_create") {
		t.Errorf("expected step description in block, got: %s", block)
	}
	if !strings.Contains(block, "Agent: task-manager") {
		t.Errorf("expected agent in block, got: %s", block)
	}
	if !strings.Contains(block, "task_create failed: name is missing") {
		t.Errorf("expected concrete error text in block, got: %s", block)
	}
	// Multi-line error text is truncated to ~400 chars, not first-line
	// only: the args shape is exactly what the replanning planner needs.
	if !strings.Contains(block, "args were") {
		t.Errorf("expected error args detail to survive within the bound, got: %s", block)
	}
}

// Pin: empty failure list renders no block (callers attach nothing).
func TestBuildFailureBlock_Empty(t *testing.T) {
	if got := buildFailureBlock(nil); got != "" {
		t.Errorf("expected empty block for nil failures, got: %q", got)
	}
	if got := buildFailureBlock([]stepFailure{}); got != "" {
		t.Errorf("expected empty block for empty failures, got: %q", got)
	}
}

// Pin: per-step error text is bounded at failureStepErrorMaxChars and the
// whole block stays under failureBlockMaxChars even with many failures.
func TestBuildFailureBlock_Bounded(t *testing.T) {
	huge := strings.Repeat("x", 10*failureStepErrorMaxChars)
	failures := make([]stepFailure, 0, 30)
	for i := 0; i < 30; i++ {
		failures = append(failures, stepFailure{
			Description: "step with a giant transcript result",
			Agent:       "coder",
			Error:       huge,
		})
	}
	block := buildFailureBlock(failures)
	if got := len([]rune(block)); got > failureBlockMaxChars {
		t.Fatalf("block = %d runes, want <= %d", got, failureBlockMaxChars)
	}
	// A prefix of the error survives (bounded), proving incorporation.
	if !strings.Contains(block, string([]rune(huge)[:100])) {
		t.Errorf("block lost the bounded error prefix:\\n%.400s", block)
	}
}
