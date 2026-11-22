package task

import "strings"

// Prompt-scaffolding markers injected onto step descriptions by the
// strategic planner (internal/agent/strategic.go: session-context block +
// "## Your Task" header). The executing loop needs them; USER-FACING
// renders must never show them (screenshot evidence 2026-10-22: the
// task-completed card displayed the raw "## Session context ..."
// instruction block and the machine evidence envelope where the answer
// belonged).
//
// Lives in internal/task (gate T4b, 04-clutter/01) because the task RPC
// surfaces (task.list_extended) strip step descriptions for client
// consumption, and internal/task must never import internal/agent.
// internal/agent.StripPromptScaffolding delegates here - the canonical
// implementation stays single (no parallel fork).
const (
	SessionContextMarker = "## Session context (most recent work in this conversation)"
	YourTaskMarker       = "## Your Task"
)

// StripPromptScaffolding removes the injected session-context block and
// "## Your Task" header from a step description, returning only the real
// task text. Input without the markers is returned trimmed and unchanged.
//
// The injection shape (strategic.go) is always:
//
//	<session-context block>\n\n## Your Task\n\n<real description>
//
// so when the Your Task marker is present everything after it is the real
// text. When only the session-context marker is present (a partial/legacy
// injection), the block is cut at the marker because no task text follows
// it in that shape.
func StripPromptScaffolding(desc string) string {
	if desc == "" {
		return desc
	}
	if _, rest, ok := strings.Cut(desc, YourTaskMarker); ok {
		return strings.TrimSpace(rest)
	}
	if before, _, ok := strings.Cut(desc, SessionContextMarker); ok {
		// No Your Task marker: keep any text BEFORE the block (the
		// injection prepends, so this is normally empty) and drop the
		// block itself.
		return strings.TrimSpace(before)
	}
	return strings.TrimSpace(desc)
}
