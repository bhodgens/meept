package agent

import "testing"

// Run 41: the digest summary grabbed the claims/evidence envelope header
// ("- job ... completed by agent coder:") and the continuity answer
// carried no evidence text — the A5 assertion lost its hello.txt. The
// summary must skip envelope headers and carry the prose.
func TestInformativeResultLine(t *testing.T) {
	cases := []struct {
		name   string
		result string
		want   string
	}{
		{
			name:   "envelope header then prose",
			result: "- job job-123 completed by agent coder:\nThe file is at /project/hello.txt",
			want:   "The file is at /project/hello.txt",
		},
		{
			name:   "plain prose only",
			result: "created hello.txt in the project directory",
			want:   "created hello.txt in the project directory",
		},
		{
			name:   "envelope-only falls back to header",
			result: "- job job-123 completed by agent coder:",
			want:   "- job job-123 completed by agent coder:",
		},
		{
			name:   "multi-line prose joined",
			result: "- job job-1 completed by agent coder:\ncreated the file\nat the requested path",
			want:   "created the file at the requested path",
		},
	}
	for _, tc := range cases {
		if got := informativeResultLine(tc.result); got != tc.want {
			t.Errorf("%s: informativeResultLine = %q, want %q", tc.name, got, tc.want)
		}
	}
	if informativeResultLine("") != "" {
		t.Error("empty result should yield empty summary")
	}
}
