package transport

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestStdioTransport_SecretPlaceholderSubstitution proves that env values of
// the form ${secret:name} are substituted with the MEEPT_SECRET:<name>
// placeholder at launch — NEVER with any real secret value. Non-${secret:}
// values pass through unchanged.
func TestStdioTransport_SecretPlaceholderSubstitution(t *testing.T) {
	// The child script prints its env; we assert on what meept handed it.
	script := `
echo "TOKEN=$TOKEN_VAR"
echo "PLAIN=$PLAIN_VAR"
echo "MIXED=$MIXED_VAR"
`
	transport := NewStdioTransport("/bin/sh", []string{"-c", script}, Config{
		TimeoutMS: 5000,
		Environment: map[string]string{
			"TOKEN_VAR": "${secret:github_api}", // -> MEEPT_SECRET:github_api
			"PLAIN_VAR": "literal-value",        // unchanged
			"MIXED_VAR": "${HOME}/bin",          // non-secret placeholder: passthrough
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := transport.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer transport.Close()

	deadline := time.Now().Add(4 * time.Second)
	var got []string
	for len(got) < 3 && time.Now().Before(deadline) {
		select {
		case line := <-transport.relayCh:
			got = append(got, string(line.data))
		case <-time.After(500 * time.Millisecond):
		}
	}

	if len(got) != 3 {
		t.Fatalf("expected 3 output lines, got %d: %q", len(got), got)
	}

	tokenLine := findLineContaining(t, got, "TOKEN=")
	if !strings.Contains(tokenLine, secretPlaceholderPrefixForTest+"github_api") {
		t.Fatalf("secret env must substitute to placeholder, got %q", tokenLine)
	}

	plainLine := findLineContaining(t, got, "PLAIN=")
	if !strings.Contains(plainLine, "literal-value") {
		t.Fatalf("plain env value must pass through unchanged, got %q", plainLine)
	}

	mixedLine := findLineContaining(t, got, "MIXED=")
	if !strings.Contains(mixedLine, "${HOME}") {
		t.Fatalf("non-secret ${...} values must NOT be touched by secret substitution, got %q", mixedLine)
	}
}

const secretPlaceholderPrefixForTest = "MEEPT_SECRET:"

func findLineContaining(t *testing.T, lines []string, prefix string) string {
	t.Helper()
	for _, l := range lines {
		if strings.Contains(l, prefix) {
			return l
		}
	}
	t.Fatalf("no line containing %q in %q", prefix, lines)
	return ""
}
