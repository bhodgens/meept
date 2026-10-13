package memory

import "testing"

// TestSanitizeForStore verifies the narrow export used by internal/memory/l0:
// injected <memory-context> spans (with surrounding whitespace) are stripped
// from content before it is persisted, so memory echoed back into a
// conversation cannot be re-recorded (feedback-loop guard).
func TestSanitizeForStore(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"no tags", "plain text", "plain text"},
		{"whole span", "<memory-context>abc</memory-context>", ""},
		{"embedded span", "before <memory-context>x</memory-context> after", "beforeafter"},
		{"multiline span", "a <memory-context>\nline\n</memory-context>\nb", "ab"},
		{"unclosed tag left intact", "<memory-context>no close", "<memory-context>no close"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizeForStore(tc.in); got != tc.want {
				t.Errorf("SanitizeForStore(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
