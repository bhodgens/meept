package agent

import (
	"strings"
	"testing"
)

// TestApplyReplyGuardWithFallback_DigestAnswer pins the A5 fix (run 21): when
// the guard replaces a catalog dump and the caller supplies the session
// digest answer, the user gets THAT answer instead of the canned line.
func TestApplyReplyGuardWithFallback_DigestAnswer(t *testing.T) {
	catalog := "## Available Agents\nSpec agents for different task types..."
	fallback := "## Session context (most recent work in this conversation)\n- Prior task: \"create a file named hello.txt\" — status: completed"

	got := applyReplyGuardWithFallback(catalog, nil, replyGuardContext{
		Agent: "chat", Intent: "recall",
		SessionID: "s1", ConversationID: "c1",
	}, fallback)
	if !strings.Contains(got, "hello.txt") {
		t.Errorf("digest fallback not used: %q", got)
	}
}

// TestApplyReplyGuardWithFallback_NoFallbackCanned pins the legacy shape: no
// fallback supplied -> canned line (existing behavior unchanged).
func TestApplyReplyGuardWithFallback_NoFallbackCanned(t *testing.T) {
	got := applyReplyGuardWithFallback("## Available Agents\n...", nil, replyGuardContext{
		Agent: "chat", Intent: "recall",
	}, "")
	if !strings.Contains(got, "ask me to do something specific") && !strings.Contains(got, "looked up the platform") {
		t.Errorf("expected canned line, got %q", got)
	}
}

// TestApplyReplyGuardWithFallback_GenuineProse passes through unchanged even
// with a fallback available.
func TestApplyReplyGuardWithFallback_GenuineProse(t *testing.T) {
	prose := "i created hello.txt at /w/project/hello.txt and verified it contains hello."
	got := applyReplyGuardWithFallback(prose, nil, replyGuardContext{}, "unused")
	if got != prose {
		t.Errorf("genuine prose altered: %q", got)
	}
}
