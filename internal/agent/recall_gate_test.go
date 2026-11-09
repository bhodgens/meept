package agent

import "testing"

// Runs 38-40: the same follow-up question classified recall, platform, and
// chat across runs. The recall-continuity gate must therefore be message-
// shaped, not label-shaped.
func TestReferencesPriorWork(t *testing.T) {
	cases := []struct {
		summary string
		want    bool
	}{
		{"did the change get made? where is the file?", true},
		{"what files did you make for me?", true},
		{"is it done yet", true},
		{"where is hello.txt", true},
		{"tell me about the change you made", true},
		{"why is the sky blue?", false},
		{"write a poem about cats", false},
		{"create a file named hello.txt containing hello", true}, // work intent gates this out anyway
	}
	for _, tc := range cases {
		if got := referencesPriorWork(tc.summary); got != tc.want {
			t.Errorf("referencesPriorWork(%q) = %v, want %v", tc.summary, got, tc.want)
		}
	}
}
