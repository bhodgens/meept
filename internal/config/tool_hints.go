package config

import "strings"

// Tool-hint routing — the single source of truth shared by the plan
// compiler (hint validation) and the tactical scheduler (hint→agent
// routing). Before this table existed, the dialect's legal-hint set
// (internal/plan/compiler.go) and the router's switch
// (internal/agent/tactical.go selectAgent) were independently maintained
// copies that drifted: dialect-legal hints like "bash" and roster hints
// like "writer"/"explore"/"researcher" fell through selectAgent's default
// and executed on the chat conversationalist, which deflected instead of
// calling tools (2026-09-07 finding; DB history shows writer→chat ×5,
// explore→chat ×5, researcher→chat ×7, bash→chat, shell→chat — specialists
// never routed to even once by hint).

// AgentIDExplore is the read-only codebase search specialist
// (config/agents/explore, discovered via AGENT.md).
const AgentIDExplore = "explore"

// DialectToolHints is the plan-dialect v1 legal hint set, in the order the
// dialect spec (docs/workflows/plan-dialect.md §4/§7) lists them. The
// compiler validates against this slice; the error message stays verbatim
// per the spec-slave rule.
var DialectToolHints = []string{
	"code", "refactor", "debug", "fix", "analyze",
	"research", "git", "plan", "chat", "bash",
}

// dialectToolHintSet is the membership set derived from DialectToolHints.
var dialectToolHintSet = func() map[string]bool {
	m := make(map[string]bool, len(DialectToolHints))
	for _, h := range DialectToolHints {
		m[h] = true
	}
	return m
}()

// IsDialectToolHint reports whether hint is legal in a plan-dialect draft.
func IsDialectToolHint(hint string) bool {
	return dialectToolHintSet[hint]
}

// toolHintAgent routes hints to executor agents. Coverage:
//   - every dialect-legal hint (compiler-validated drafts),
//   - intent-shaped hints (IntentType values used by the legacy LLM-planner
//     and dispatcher paths),
//   - historical aliases observed in the task DB (shell, file_write,
//     file-write) that previously fell through to chat.
//
// Routing principle: hints describe EXECUTION work, so they map to executor
// personas with tool access — never to chat. Chat remains the router's
// terminal default only for hints with no entry here.
var toolHintAgent = map[string]string{
	// Dialect-legal hints.
	"code":     AgentIDCoder,
	"refactor": AgentIDCoder,
	"debug":    AgentIDDebugger,
	"fix":      AgentIDDebugger,
	"analyze":  AgentIDAnalyst,
	"research": AgentIDResearcher,
	"git":      AgentIDCommitter,
	"plan":     AgentIDPlanner,
	"chat":     AgentIDChat,
	"bash":     AgentIDCoder, // shell execution → coder (stateful, shell tools)

	// Roster hints (config/agents/*) seen in the wild.
	"writer":     AgentIDWriter,
	"explore":    AgentIDExplore,
	"architect":  AgentIDArchitect,
	"skeptic":    AgentIDSkeptic,
	"librarian":  AgentIDLibrarian,
	"researcher": AgentIDResearcher,
	"analyst":    AgentIDAnalyst,
	"coder":      AgentIDCoder,
	"debugger":   AgentIDDebugger,
	"planner":    AgentIDPlanner,
	"committer":  AgentIDCommitter,
	"scheduler":  AgentIDScheduler,
	"commit":     AgentIDCommitter, // legacy keyword hint (KeywordCommit)
	"schedule":   AgentIDScheduler, // legacy keyword hint (IntentSchedule)

	// Media hints (media specialists, previously routed by selectAgent's
	// IntentImageGen/VideoGen/ImageID cases).
	"image_gen": AgentIDImageGen,
	"video_gen": AgentIDVideoGen,
	"image_id":  AgentIDImageID,
	"image-gen": AgentIDImageGen,
	"video-gen": AgentIDVideoGen,
	"image-id":  AgentIDImageID,

	// Historical aliases from the legacy planner path.
	"shell":      AgentIDCoder,
	"file_write": AgentIDCoder,
	"file-write": AgentIDCoder,
}

// ToolHintAgent returns the executor agent for a tool hint. Second return
// is false when the hint has no route (caller decides its fallback).
// Lookup is case-insensitive and trims whitespace.
func ToolHintAgent(hint string) (string, bool) {
	agentID, ok := toolHintAgent[normalizeHint(hint)]
	return agentID, ok
}

func normalizeHint(hint string) string {
	out := make([]byte, 0, len(hint))
	for i := 0; i < len(hint); i++ {
		c := hint[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out = append(out, c)
	}
	return strings.TrimSpace(string(out))
}
