package agent

import "strings"

// Prompt-scaffolding markers injected onto step descriptions by
// strategic.go (session-context block + "## Your Task" header). The
// executing loop needs them; USER-FACING renders must never show them
// (screenshot evidence 2026-10-22: the task-completed card displayed the
// raw "## Session context ..." instruction block and the machine evidence
// envelope where the answer belonged).
const (
	sessionContextMarker = "## Session context (most recent work in this conversation)"
	yourTaskMarker       = "## Your Task"
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
	if idx := strings.Index(desc, yourTaskMarker); idx >= 0 {
		rest := desc[idx+len(yourTaskMarker):]
		return strings.TrimSpace(rest)
	}
	if idx := strings.Index(desc, sessionContextMarker); idx >= 0 {
		// No Your Task marker: keep any text BEFORE the block (the
		// injection prepends, so this is normally empty) and drop the
		// block itself.
		return strings.TrimSpace(desc[:idx])
	}
	return strings.TrimSpace(desc)
}
