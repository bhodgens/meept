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

// stripTaskResponseSteps display-strips every step row inside a task RPC
// response payload. Gate C-5 (07-scaffolding-payload-strip): the RPC
// task.* handlers funnel every client-visible response through the single
// handleMessage switch, so this one call is the whole RPC-surface choke
// point - no client (TUI modal, CLI session get, GUI via the HTTP bridge)
// can receive scaffolding or a machine envelope through task.get /
// task.list / task.list_extended / task.steps. Step rows returned by the
// step store are freshly scanned per call, so the in-place strip is
// display-only: the STORE keeps raw descriptions and results for the
// planner and the review pipeline.
//
// Task-level descriptions (Registry.Create) never carry scaffolding today
// (the planner injects onto STEP descriptions only), so only step rows are
// stripped here; a future producer change would extend to the task field
// at the same single site.
func stripTaskResponseSteps(response any) {
	StripTaskResponseSteps(response)
}

// StripTaskResponseSteps is the exported payload-boundary strip shared by
// the RPC surface (internal/task registry, via stripTaskResponseSteps) and
// the HTTP surface (internal/comm/http writeJSON). Gate C-5
// (07-scaffolding-payload-strip): step descriptions go through
// StripPromptScaffolding and step results through
// StripClaimsEvidenceOrProse at ONE choke point per transport. It is
// shape-matched and inert on every non-task payload: only
// []*task.TaskStep / *Task / TaskExtendedResponse shapes (directly or
// nested under the "tasks"/"steps" response keys the task endpoints use)
// are touched; rows come fresh from the store per call, so the strip is
// display-only and never mutates stored data.
func StripTaskResponseSteps(response any) {
	switch v := response.(type) {
	case *Task:
		// The task.get response embeds no step rows (registry.Get reads
		// the task row only); nothing to strip beyond a future task-level
		// field.
		return
	case []TaskExtendedResponse:
		for i := range v {
			for _, st := range v[i].Steps {
				StripStepForDisplay(st)
			}
		}
	case []*TaskStep:
		for _, st := range v {
			StripStepForDisplay(st)
		}
	case *TaskStep:
		StripStepForDisplay(v)
	case map[string]any:
		if tasks, ok := v["tasks"].([]TaskExtendedResponse); ok {
			StripTaskResponseSteps(tasks)
			return
		}
		if tasks, ok := v["tasks"].([]*Task); ok {
			StripTaskResponseSteps(tasks)
			return
		}
		if steps, ok := v["steps"].([]*TaskStep); ok {
			StripTaskResponseSteps(steps)
		}
	}
}
