package agent

import (
	"strings"
	"testing"
)

// replanDigestMaxChars is the hard ceiling on the replan request payload the
// escalation path hands to the strategic planner. The 2026-09-18 e2e run
// showed a replan loop embedding the FULL prior step result (and, transitively,
// the whole conversation) into each fresh plan request: 27 unbacked-claims
// nudges and context regrowth until llama.cpp returned 500
// "Context size has been exceeded". The planner needs the failure's shape —
// task description, step list, one-line reason — never the transcript.
const replanDigestMaxChars = 2000

// buildReplanDigest renders a compact, bounded replan summary for the
// strategic planner. It carries: the task description, the completed/remaining
// step split, and a FIRST-LINE-ONLY failure reason truncated to ~400 chars.
// It deliberately never includes the failing step's result text or any
// conversation transcript. The output is capped at replanDigestMaxChars, so
// callers can pin the bound without re-measuring.
func buildReplanDigest(taskDesc string, completed, remaining []string, failureReason string) string {
	var sb strings.Builder
	// The literal "RE-PLAN" marker is load-bearing: replan fallback steps
	// carry req.Input verbatim and downstream logic/tests identify replan
	// steps by this marker. Keep it as the digest's first token.
	sb.WriteString("RE-PLAN: ")
	addBounded := func(label, s string, max int) {
		if s == "" {
			return
		}
		sb.WriteString(label)
		sb.WriteString(truncateRunes(s, max, ""))
		sb.WriteByte('\n')
	}

	addBounded("Task: ", taskDesc, 500)
	if len(completed) > 0 {
		sb.WriteString("Completed steps (do not redo):\n")
		for _, d := range completed {
			sb.WriteString("  - ")
			sb.WriteString(truncateRunes(firstLine(d), 150, ""))
			sb.WriteByte('\n')
		}
	}
	if len(remaining) > 0 {
		sb.WriteString("Remaining (uncompleted) steps to retry or finish:\n")
		for _, d := range remaining {
			sb.WriteString("  - ")
			sb.WriteString(truncateRunes(firstLine(d), 150, ""))
			sb.WriteByte('\n')
		}
	}
	sb.WriteString("Failure: ")
	sb.WriteString(truncateRunes(firstLine(failureReason), 400, "…"))
	sb.WriteByte('\n')

	out := sb.String()
	if len(out) <= replanDigestMaxChars {
		return out
	}
	return truncateRunes(out, replanDigestMaxChars-1, "…")
}

// TestBuildReplanDigest_BoundedWithLargeStepResult is the F-B4 pin: with a
// multi-kilobyte step result as the failure reason, the digest must stay
// under replanDigestMaxChars and must not contain the result body.
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
