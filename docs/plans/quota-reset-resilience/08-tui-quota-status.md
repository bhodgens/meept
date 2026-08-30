# TUI quota status surfaces - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** The TUI agent list and agent detail views render quota_wait and
  blocked states distinctly, with countdown, primary-vs-active model lines,
  and live updates via the WS/bus event path.
- **Dependencies:** 05 (states + events), 07 (WS classification)
- **Estimated Context:** 60K
- **Concurrency Group:** F

## Goal

A user glancing at the TUI sees: which agents are quota-waiting, for how
long, whether a fallback is carrying the work, and which agents are blocked
needing action. Lowercase text per convention.

## Context

The TUI lives in `internal/tui` (bubbletea + bubblezone). Agent lists exist
(components/models — explore for the agents list view and its update loop).
State data reaches the TUI either via bus/WS subscription or periodic
refresh of agent status — VERIFY which path exists and reuse it. The AGENTS.md
parity rule means whatever you build here, leaf 09 mirrors in Flutter.

Key files to understand before implementing:

- `internal/tui/` — the agent list component and its update/refresh path;
  status bar; bubblezone wiring for clickable elements.
- `internal/tui/models` or similar — view-model shapes for agent status.
- Theme tokens (`theme/tokens.json5`) — use existing status colors; do not
  invent new tokens unless none fits (quota_wait = warning-ish, blocked =
  error-ish; reuse existing warning/error tokens).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/tui agent list + detail rendering:
//   state "quota_wait" -> label "quota wait" (lowercase), warning-tone
//     color from existing tokens, with countdown text:
//     "quota resets in 3h 12m" (computed from unblock_at; refresh on tick;
//     past-due -> "resuming…")
//   state "blocked" -> label "blocked", error-tone color, hint text
//     "action required"
//   agent detail (when a fallback is active, from event fallback_model):
//     "primary: <model> (blocked until <time>)"
//     "active: <fallback model>"
//   When no episode data is present, rendering is unchanged (regression
//   safety for agents never quota-hit).
// Live update: consume agent.quota_wait events (agent_progress WS type or
// bus subscription — whichever path the TUI already uses for agent state)
// and refresh the affected agent rows.
```

### What This Leaf Consumes

```
// leaf 05: agent states quota_wait/blocked + agent.quota_wait events
// leaf 07: WS classification (agent_progress) so the TUI event path works
```

## Tasks

### Task 1: Render quota states in the agent list

**Objective:** Distinct labels/colors for quota_wait and blocked.

**Files:**
- Modify: the TUI agent list component file (exact path from exploration)
- Test: `internal/tui/quota_status_test.go`

**Step 1: Write failing test**

View-model with an agent in quota_wait (unblock_at = now+3h12m) renders
label "quota wait" + "quota resets in 3h 12m"; blocked renders "blocked" +
"action required"; running/idle agents render exactly as before (golden-ish
string assertions on the list lines, following existing TUI test style —
find how existing TUI tests assert rendered text).

**Step 2: Run test to verify failure**

Run: `go test ./internal/tui/ -run TestQuotaTUIRender -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Add the two cases to the list renderer using existing theme tokens.

**Step 4: Run test to verify pass**

Run: `go test ./internal/tui/ -run TestQuotaTUIRender -v`
Expected: PASS

### Task 2: Countdown + detail lines

**Objective:** Live countdown and primary/active model display.

**Files:**
- Modify: same component + detail view
- Test: same test file

**Step 1: Write failing test**

(a) Countdown computes from unblock_at correctly for 3h12m, 45m, 90s;
past-due renders "resuming…". (b) Detail view with fallback_model set shows
the two-line primary/active block; without it, no extra lines. (c) Countdown
updates on the TUI's existing tick (use the tick hook the TUI already has;
if none exists for this view, refresh on any message — simplest correct
thing; document choice).

**Step 2: Run test to verify failure**

Run: `go test ./internal/tui/ -run TestQuotaTUICountdown -v`
Expected: FAIL

**Step 3: Write minimal implementation**

**Step 4: Run test to verify pass**

Run: `go test ./internal/tui/ -run TestQuotaTUICountdown -v`
Expected: PASS

### Task 3: Live event handling

**Objective:** quota_wait/blocked/cleared events update rows without restart.

**Files:**
- Modify: the TUI update/message-handling path for agent state
- Test: same test file

**Step 1: Write failing test**

Dispatch the TUI's existing message type for agent-state changes (or the
WS-event message it already handles) carrying quota_wait/blocked/running
transitions: assert the row updates and stale episode data clears on
running. If the TUI currently polls status instead of consuming events,
hook the same refresh and assert the view-model updates from a status
snapshot carrying the new states.

**Step 2: Run test to verify failure**

Run: `go test ./internal/tui/ -run TestQuotaTUIEvents -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Follow the existing state-update path; do not add a second event channel.

**Step 4: Run test to verify pass**

Run: `go test ./internal/tui/ -run 'TestQuotaTUI' -count=1 -v`
Expected: PASS
Also existing TUI suite (scoped): `go test ./internal/tui/ -count=1 -run 'TestAgent'`

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep — only what the tasks specify
- [ ] All new UI text lowercase
- [ ] Agents with no episode render byte-identically to before

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Interface contracts match exactly (labels, countdown format)
- [ ] Existing agent-list rendering unchanged for unaffected states
- [ ] bubblezone click targets unaffected
- [ ] Existing TUI tests green

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- Keep this leaf VIEW-ONLY: no new daemon APIs, no config. If you discover
  the TUI cannot see the new states (status struct lacks the field), STOP
  and report — that is a leaf 05 contract gap for the orchestrator, not
  something to patch silently here.
- Countdown math helper (format "3h 12m") goes in a small exported util in
  internal/tui so leaf 09 can mirror the format exactly (parity).
