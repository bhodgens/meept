package agent

import "testing"

// Chat-vs-imperative arbitration (e2e run 3, 2026-09-19): the 8B scored
// "create a file named hello.txt …, then tell me the full path" intent=chat
// @0.9 and the turn deflected with zero work. The arbitration discards the
// chat verdict on an imperative prompt naming a work artifact, letting the
// keyword chain route code.
func TestInputMentionsWorkArtifact(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"create a file named hello.txt in the current directory containing the word hello, then tell me the full path", true},
		{"write a script named backup.sh", true},
		{"hey there", false},
		{"thanks, that helped", false},
		{"what can you do?", false},
	}
	for _, tc := range cases {
		if got := inputMentionsWorkArtifact(tc.input); got != tc.want {
			t.Errorf("inputMentionsWorkArtifact(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestHasLeadingImperativeVerb_T1Prompt(t *testing.T) {
	if !hasLeadingImperativeVerb("create a file named hello.txt in the current directory containing the word hello, then tell me the full path") {
		t.Error("T1 prompt must read as imperative")
	}
}
