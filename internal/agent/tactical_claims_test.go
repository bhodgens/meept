package agent

import (
	"testing"
)

// E2E run 7 (2026-09-11, rkl3Th): the step result carried a fabricated
// report ("Created file hello.txt containing 'hello'") with ZERO tool
// executions. claimsFileSideEffects + the claim-vs-evidence gate in
// handleJobComplete mark such steps unverified instead of letting the
// narration read as ground truth.

func TestClaimsFileSideEffects_Run7Claim(t *testing.T) {
	ts := &TacticalScheduler{}
	claims := []string{"Created file hello.txt containing 'hello'"}
	if !ts.claimsFileSideEffects(claims) {
		t.Fatal("run-7 claim not detected as file side-effect claim")
	}
}

func TestClaimsFileSideEffects_Shapes(t *testing.T) {
	ts := &TacticalScheduler{}
	cases := []struct {
		claim string
		want  bool
	}{
		{"Created file hello.txt containing hello", true},
		{"Updated the config with new defaults", true},
		{"wrote report.md summarizing findings", true},
		{"deleted stale cache file", true},
		{"Analyzed the project structure", false},
		{"Searched the web for Go generics docs", false},
		{"Answered the user's question", false},
		{"", false},
	}
	for _, c := range cases {
		got := ts.claimsFileSideEffects([]string{c.claim})
		if got != c.want {
			t.Errorf("claimsFileSideEffects(%q) = %v, want %v", c.claim, got, c.want)
		}
	}
}
