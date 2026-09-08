package config

// Tests for the shared tool-hint routing table. Regression coverage for the
// 2026-09-07 finding: the compiler's legal-hint set and the scheduler's
// router switch were independent copies that drifted, so dialect-legal and
// roster hints (bash, writer, explore, researcher) executed on the chat
// conversationalist instead of executor agents.

import "testing"

// TestToolHintAgent_CoversEveryDialectHint: every hint the plan compiler
// accepts MUST route somewhere. A dialect-legal hint with no route would
// compile cleanly and then silently fall back to the chat agent — the
// original bug.
func TestToolHintAgent_CoversEveryDialectHint(t *testing.T) {
	for _, hint := range DialectToolHints {
		agentID, ok := ToolHintAgent(hint)
		if !ok {
			t.Errorf("dialect-legal hint %q has NO route (would fall back to chat)", hint)
			continue
		}
		if agentID == "" {
			t.Errorf("dialect-legal hint %q routes to empty agent ID", hint)
		}
	}
}

// TestToolHintAgent_ExecutorNotChat: execution-flavored hints must route to
// executor personas, never to chat. Only the literal "chat" hint may route
// to the chat agent.
func TestToolHintAgent_ExecutorNotChat(t *testing.T) {
	executionHints := []string{
		"code", "refactor", "debug", "fix", "analyze", "research",
		"git", "plan", "bash", "shell", "file_write", "file-write",
		"writer", "explore", "researcher", "analyst", "librarian",
		"architect", "skeptic", "coder", "debugger", "committer", "planner",
		"image_gen", "video_gen", "image_id",
	}
	for _, hint := range executionHints {
		agentID, ok := ToolHintAgent(hint)
		if !ok {
			t.Errorf("hint %q has no route", hint)
			continue
		}
		if agentID == AgentIDChat {
			t.Errorf("execution hint %q routes to chat conversationalist", hint)
		}
	}

	if agentID, ok := ToolHintAgent("chat"); !ok || agentID != AgentIDChat {
		t.Errorf("hint %q = (%q, %v), want chat", "chat", agentID, ok)
	}
}

// TestToolHintAgent_RegressionSet: the exact hints observed falling through
// to chat in the 2026-09-07 live runs and DB history.
func TestToolHintAgent_RegressionSet(t *testing.T) {
	cases := map[string]string{
		"bash":       AgentIDCoder,
		"shell":      AgentIDCoder,
		"writer":     AgentIDWriter,
		"explore":    AgentIDExplore,
		"researcher": AgentIDResearcher,
		"analyst":    AgentIDAnalyst,
		"file_write": AgentIDCoder,
		"file-write": AgentIDCoder,
	}
	for hint, want := range cases {
		got, ok := ToolHintAgent(hint)
		if !ok {
			t.Errorf("hint %q has no route (want %q)", hint, want)
			continue
		}
		if got != want {
			t.Errorf("hint %q routes to %q, want %q", hint, got, want)
		}
	}
}

// TestToolHintAgent_CaseAndSpaceInsensitive: hint normalization.
func TestToolHintAgent_CaseAndSpaceInsensitive(t *testing.T) {
	for _, variant := range []string{"Bash", " bash ", "Writer", "CODE"} {
		if _, ok := ToolHintAgent(variant); !ok {
			t.Errorf("hint variant %q has no route", variant)
		}
	}
}

// TestToolHintAgent_UnknownHintNotRouted: unknown hints report not-found so
// the caller's fallback (chat) applies explicitly.
func TestToolHintAgent_UnknownHintNotRouted(t *testing.T) {
	if _, ok := ToolHintAgent("warp_drive"); ok {
		t.Error("unknown hint reported as routed")
	}
	if _, ok := ToolHintAgent(""); ok {
		t.Error("empty hint reported as routed")
	}
}
