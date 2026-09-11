package agent

import (
	"strings"
	"testing"
)

// Pins for e2e run 5 (2026-09-11) A2/t4: the platform-intent fast path
// (Dispatcher.RouteToAgent → handlePlatformIntrospection) returns the
// agent-roster catalog WITHOUT an LLM turn, so the RunOnce
// applyReplyGuard seam never ran and the roster shipped to the user.
// The handleChatRequest choke point now guards the final reply, so a
// catalog-shaped reply arriving through ANY path is sanitized.
//
// These pins exercise the choke point's contract: the roster (with its
// embedded system-prompt fragments) must be replaced by the sanitized
// nudge, while a genuine prose answer about the task passes unchanged.
func TestChatRequestChokePoint_SanitizesRosterReply(t *testing.T) {
	// The exact shape run 5's T4 shipped: roster header + per-agent
	// purpose fragments + baseline tools list.
	roster := "## Platform Capabilities\n\n### Available Agents\n\n" +
		"- **Code Specialist** (`coder`): You are Meept, an autonomous assistant serving your creator.\n" +
		"- **Chat Assistant** (`chat`): You are Meept, an autonomous assistant serving your creator.\n" +
		"\n### Baseline Tools (available to all agents)\n\n" +
		"- memory_store\n- file_write\n"

	got := applyReplyGuard(roster)
	if got == roster {
		t.Fatal("roster reply passed the reply guard un-sanitized; the choke point must cover the platform fast path")
	}
	if strings.Contains(got, "## Available Agents") || strings.Contains(got, "`coder`") {
		t.Fatalf("sanitized reply still leaks roster content: %q", got)
	}
}

// A genuine task answer — the T1 reply shape — must pass through the
// choke point byte-identical.
func TestChatRequestChokePoint_PassesProseReply(t *testing.T) {
	prose := "The absolute path to `hello.txt` is /var/folders/xx/project/hello.txt — the file contains the word hello."
	if got := applyReplyGuard(prose); got != prose {
		t.Fatalf("prose reply mutated by reply guard:\n got: %q\nwant: %q", got, prose)
	}
}
