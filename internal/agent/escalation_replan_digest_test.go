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
