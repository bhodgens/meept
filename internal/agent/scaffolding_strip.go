package agent

import "github.com/caimlas/meept/internal/task"

// Prompt-scaffolding markers injected onto step descriptions by
// strategic.go (session-context block + "## Your Task" header). The
// canonical implementation moved to internal/task (gate T4b,
// 04-clutter/01) so the task RPC surfaces can strip step descriptions
// for client consumption without importing internal/agent - one
// implementation, no fork. The former marker-name delegates were dead
// (U1000) and are removed; use task.SessionContextMarker /
// task.YourTaskMarker directly.

// StripPromptScaffolding removes the injected session-context block and
// "## Your Task" header from a step description, returning only the real
// task text. Input without the markers is returned trimmed and unchanged.
// See task.StripPromptScaffolding for the canonical shape contract.
func StripPromptScaffolding(desc string) string {
	return task.StripPromptScaffolding(desc)
}
