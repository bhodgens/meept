package agent

import (
	"strings"
	"testing"
)

// TestSanitizeCatalogReply covers the catalog-reply guard detectors
// (plan leaf 05-catalog-reply-guard): raw platform_* JSON dumps, tool
// catalogs, and agent rosters get a short user-language fallback; prose,
// empty input, and ordinary markdown are returned byte-identical.
func TestSanitizeCatalogReply(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantSame bool   // true = pass-through expected
		contains string // when !wantSame, fallback must contain this
	}{
		{"prose untouched", "I created water_reminder.py for you. Run it with python3.", true, ""},
		{"empty untouched", "", true, ""},
		{"markdown answer untouched", "## how to run\n\n1. open terminal", true, ""},
		{"status json", "{\n  \"status\": \"running\",\n  \"uptime_seconds\": 1840.8,\n  \"version\": \"1.0\"\n}", false, "platform"},
		{"tool catalog", "### Shell Tools\n\n- **shell**: Execute a shell command...\n\n*Total: 75 tools*", false, "tools"},
		{"agent roster", "## Available Agents\n\n### Coder (`coder`)\n**Role**: executor\n\nYou are Meept, an autonomous assistant serving your creator.\n\n*Total: 28 agents*", false, "agents"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeCatalogReply(tc.in)
			if tc.wantSame {
				if got != tc.in {
					t.Errorf("want pass-through, got %q", got)
				}
				return
			}
			if got == tc.in {
				t.Errorf("catalog reply passed through unguarded")
			}
			if tc.contains != "" && !strings.Contains(strings.ToLower(got), tc.contains) {
				t.Errorf("fallback %q must mention %q", got, tc.contains)
			}
		})
	}
}

// TestApplyReplyGuard verifies the RunOnce seam (plan leaf 05 Task 2): the
// final assistant text is guarded before it becomes ChatResponse.Reply —
// catalog-shaped replies replaced, genuine prose byte-identical.
func TestApplyReplyGuard(t *testing.T) {
	catalog := "### Shell Tools\n\n- **shell**: Execute a shell command...\n\n*Total: 75 tools*"
	got := applyReplyGuard(catalog)
	if got == catalog {
		t.Fatalf("applyReplyGuard let the tool catalog through unchanged")
	}
	if !strings.Contains(got, "tools") {
		t.Errorf("fallback %q must mention %q", got, "tools")
	}

	prose := "I created water_reminder.py for you. Run it with python3."
	if g := applyReplyGuard(prose); g != prose {
		t.Errorf("prose must be byte-identical, got %q", g)
	}
}
