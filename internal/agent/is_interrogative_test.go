package agent

import "testing"

// Run 42: "did the change get made?" was classified as WORK and created a
// task — the recall branch (gated on !isWorkIntent) never fired. Questions
// about prior work must answer from the stored result regardless of label.
func TestIsInterrogative(t *testing.T) {
	cases := []struct {
		summary string
		want    bool
	}{
		{"did the change get made? where is the file?", true},
		{"what files did you make for me?", true},
		{"is it done", true},
		{"where is hello.txt", true},
		{"change the file to say goodbye", false},
		{"create another file named foo.txt", false},
		{"make it louder", false},
	}
	for _, tc := range cases {
		if got := isInterrogative(tc.summary); got != tc.want {
			t.Errorf("isInterrogative(%q) = %v, want %v", tc.summary, got, tc.want)
		}
	}
}
