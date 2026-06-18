package http

import (
	"strings"
	"testing"
)

// TestSanitizeErrMessage verifies HTTP error messages are scrubbed of internal
// paths and Go package identifiers before being sent to clients.
//
// This is the D1-3 fix: error responses should not leak filesystem layout
// or internal package structure. See glm52-findings-7.md.
func TestSanitizeErrMessage(t *testing.T) {
	cases := []struct {
		name  string
		input string
		// "mustNotContain" substrings that should NOT appear after sanitization.
		mustNotContain []string
		// "mustContain" substrings that SHOULD survive sanitization.
		mustContain []string
	}{
		{
			name:           "absolute Unix path stripped",
			input:          "open /Users/caimlas/git/meept/config.json: permission denied",
			mustNotContain: []string{"/Users/caimlas/git/meept/config.json"},
			mustContain:    []string{"permission denied"},
		},
		{
			name:           "temp dir path stripped",
			input:          "write /tmp/meept/socket: no such file",
			mustNotContain: []string{"/tmp/meept/socket"},
			mustContain:    []string{"no such file"},
		},
		{
			name:           "windows drive-letter path stripped",
			input:          `open C:\Users\bob\config.json: The system cannot find the file`,
			mustNotContain: []string{`C:\Users\bob\config.json`},
		},
		{
			name:           "go import path stripped",
			input:          "github.com/caimlas/meept/internal/agent: nil pointer",
			mustNotContain: []string{"github.com/caimlas/meept/internal/agent"},
		},
		{
			name:           "file.go line number prefix stripped",
			input:          "server.go:42: something broke",
			mustNotContain: []string{"server.go:42"},
		},
		{
			name:        "sentinel error preserved",
			input:       "job not found: agent-task-123",
			mustContain: []string{"job not found", "agent-task-123"},
		},
		{
			name:        "validation message preserved",
			input:       "invalid session ID: must not be empty",
			mustContain: []string{"invalid session ID", "must not be empty"},
		},
		{
			name:        "plain message unchanged",
			input:       "internal server error",
			mustContain: []string{"internal server error"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeErrMessage(tc.input)
			for _, bad := range tc.mustNotContain {
				if strings.Contains(got, bad) {
					t.Errorf("sanitized %q still contains forbidden %q; got %q", tc.input, bad, got)
				}
			}
			for _, good := range tc.mustContain {
				if !strings.Contains(got, good) {
					t.Errorf("sanitized %q lost required %q; got %q", tc.input, good, got)
				}
			}
		})
	}
}

// TestSanitizeErrMessage_Length verifies very long messages are truncated so
// clients can't be abused as a log-storage vector.
func TestSanitizeErrMessage_Length(t *testing.T) {
	long := strings.Repeat("a", 5000)
	got := sanitizeErrMessage(long)
	if len(got) > 1100 {
		t.Errorf("sanitized message len %d; expected <= 1100 (1024 + truncation suffix)", len(got))
	}
	if !strings.HasSuffix(got, "...(truncated)") {
		t.Errorf("truncated message missing suffix; got %q", got[len(got)-30:])
	}
}

// TestSanitizeErrMessage_Idempotent verifies sanitization is stable: running
// it twice does not produce a different result. This guards against regex
// backreferences or escapes that would double-process.
func TestSanitizeErrMessage_Idempotent(t *testing.T) {
	inputs := []string{
		"open /Users/foo/bar.go: permission denied",
		"github.com/x/y/z/pkg.go:43: error",
		"plain message",
	}
	for _, in := range inputs {
		once := sanitizeErrMessage(in)
		twice := sanitizeErrMessage(once)
		if once != twice {
			t.Errorf("not idempotent for %q:\n  once = %q\n  twice = %q", in, once, twice)
		}
	}
}
