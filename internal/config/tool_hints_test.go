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
		"writer", "write", "explore", "researcher", "analyst", "librarian",
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
		"write":      AgentIDWriter, // IntentWrite — 05e60e11 fixed this deflection; must never regress to chat
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

// TestToolHintAgent_LegacyIntentSwitchParity: every intent constant that the
// pre-refactor selectAgent switch (internal/agent/tactical.go:1489 at
// caf61fb2) mapped to a NON-chat agent must still route — identically — in
// the shared table. This is the M16 audit: the switch and the map must never
// drift apart again. The table may cover MORE (aliases, dialect hints), but
// it must never cover LESS than the legacy switch, and where both exist the
// target agent must agree.
func TestToolHintAgent_LegacyIntentSwitchParity(t *testing.T) {
	// (intent-hint, legacy target) pairs, transcribed from the caf61fb2
	// switch. Only non-chat cases; default→chat is the caller's fallback.
	legacy := map[string]string{
		"code":       AgentIDCoder,      // IntentCode
		"refactor":   AgentIDCoder,      // KeywordRefactor
		"coder":      AgentIDCoder,      // config.AgentIDCoder roster hint
		"debug":      AgentIDDebugger,   // IntentDebug
		"fix":        AgentIDDebugger,   // KeywordFix
		"debugger":   AgentIDDebugger,   // roster hint
		"analyze":    AgentIDAnalyst,    // IntentAnalyze
		"analyst":    AgentIDAnalyst,    // roster hint
		"research":   AgentIDResearcher, // IntentResearch
		"researcher": AgentIDResearcher, // roster hint
		"git":        AgentIDCommitter,  // IntentGit
		"commit":     AgentIDCommitter,  // KeywordCommit
		"committer":  AgentIDCommitter,  // roster hint
		"schedule":   AgentIDScheduler,  // IntentSchedule
		"scheduler":  AgentIDScheduler,  // roster hint
		"plan":       AgentIDPlanner,    // IntentPlan
		"planner":    AgentIDPlanner,    // roster hint
		"write":      AgentIDWriter,     // IntentWrite
		"writer":     AgentIDWriter,     // roster hint
		"architect":  AgentIDArchitect,  // IntentArchitect
		"skeptic":    AgentIDSkeptic,    // IntentSkeptic
		"librarian":  AgentIDLibrarian,  // IntentLibrarian
		"image_gen":  AgentIDImageGen,   // IntentImageGen
		"image-gen":  AgentIDImageGen,
		"video_gen":  AgentIDVideoGen, // IntentVideoGen
		"video-gen":  AgentIDVideoGen,
		"image_id":   AgentIDImageID, // IntentImageID
		"image-id":   AgentIDImageID,
	}
	for hint, want := range legacy {
		got, ok := ToolHintAgent(hint)
		if !ok {
			t.Errorf("legacy switch case %q→%q has NO route in the shared table", hint, want)
			continue
		}
		if got != want {
			t.Errorf("legacy switch case %q routed to %q, table says %q — drift", hint, want, got)
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
