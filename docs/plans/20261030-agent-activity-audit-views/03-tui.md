# TUI Activity View - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** A new TUI activity view: activity table with three sort modes
  (chronological / by task / by plan), a live-tail toggle, and findings +
  drift panels; palette + keybinding + parity-manifest wiring.
- **Dependencies:** 02-rpc.md (activity.* + agents.review RPC methods)
- **Estimated Context:** 95K
- **Concurrency Group:** C (dispatch after 02 reaches REVIEWED; parallel with 04)

## Goal

An operator in the terminal presses one key and sees what every agent is
doing: a sortable activity table (newest first, grouped by task, grouped by
plan), a live tail fed by activity.recent polling, and panels listing audit
findings with drift scores per employee. All UI text lowercase.

## Context

The TUI (bubbletea) has one ViewType per view. Adding a view = new
ViewType constant + keybinding case + model field + initCurrentView branch.
Copy the TasksModel shape for the new model: an RPC client interface, a
table.Model, filter/view-mode enums, fetch methods. The RPC layer is
request/response - the "live" tail is POLLING (pattern: EventStream bus
polling) on a ticker, not server push. Tables MUST be sized through
internal/tui/tableutil (Size + SetRows; a width-0 viewport renders nothing
and an over-long row panics).

## Verified Anchors

Anchors grep-confirmed 2026-10-30. Re-grep before editing.

- internal/tui/app.go:42-53 - ViewType enum (ViewChat..ViewAgents) - add ViewActivity here.
- internal/tui/app.go:332-338 - model construction in NewApp - add activity model.
- internal/tui/app.go:2395-2425 - keybinding switch setting currentView + initCurrentView - add the case.
- internal/tui/config.go:100-113 - CommandPaletteKeys struct - add ViewActivity key (default "a"; "a" is unused today).
- internal/tui/config.go:168-172 - DefaultClientConfig keybindings defaults - add ViewActivity: "a".
- internal/tui/models/tasks.go:15-61 - TasksModel + TasksRPCClient interface pattern to copy.
- internal/tui/models/tasks.go:344,352 - fetch methods pattern.
- internal/tui/rpc.go:106 - RPCClient.Call(method, params) - call activity.* through thin wrappers near rpc.go:422/795 style.
- internal/tui/tableutil/tableutil.go:20-24,:193-215 - Size + SetRows - MANDATORY for every table.
- internal/tui/testdata/gui_inventory.json - parity manifest: add palette command + shortcut entries.
- internal/tui/parity_test.go:52-58,:112,:186 - parity test loads manifest; deviations map required for any divergence.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/tui/models/activity.go
type ActivityRPCClient interface {
    ActivityQuery(params ActivityQueryParams) ([]ActivityRecordJSON, int, error)
    ActivityRecent(limit int) ([]ActivityRecordJSON, int, int, error) // records, dropped, total, err
    ActivityFindings(params FindingsParams) ([]FindingJSON, int, error)
    AgentsReview(employeeID string) (DriftJSON, error)
}
// ActivityRecordJSON / FindingJSON / DriftJSON decode EXACTLY the C2/C4
// result JSON field names (json tags: kind, at, agent_id, session_id,
// task_id, step_id, plan_id, state, severity, summary / drift_score...).
// internal/tui: ViewActivity ViewType + "a" default key + palette entry
// "activity view" (lowercase) + manifest entries.
```

### What This Leaf Consumes

- C2 activity.* RPC results (leaf 02)
- C4 agents.review result (leaf 02)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | ViewType + keybinding + model skeleton + palette + manifest | Modify: internal/tui/app.go, internal/tui/config.go, internal/tui/app_models wiring, internal/tui/commands.go (palette list), internal/tui/testdata/gui_inventory.json; Create: internal/tui/models/activity.go; Test: internal/tui/models/activity_test.go | `go test -p 2 ./internal/tui/ -run TestActivity` | ViewActivity/model undefined | pressing "a" switches to activity view; palette lists "activity view"; manifest carries the entry | Given app with default keybindings; When ViewActivity key; Then currentView==ViewActivity |
| T2 | Activity table + sort modes (chrono/task/plan) with tableutil sizing | Modify: internal/tui/models/activity.go; Test: internal/tui/models/activity_test.go | `go test -p 2 ./internal/tui/ -run TestActivity` | sort modes absent | tab key cycles sort mode label; each mode issues ActivityQuery with matching sort param; rows sized via tableutil.SetRows (row len == col count) | Given fake RPC returning 10 records; When sort=task; Then groups ordered by TaskID with headers |
| T3 | Live-tail toggle: ticker poll of activity.recent merged into table | Modify: internal/tui/models/activity.go; Test: internal/tui/models/activity_test.go | `go test -p 2 ./internal/tui/ -run TestActivity` | tail absent | toggle key starts/stops polling; new records appear at top; dropped counter shown in status line | Given fake RPC returning growing ring; When tail on and tick; Then table gains the new row without a manual refresh |
| T4 | Findings + drift panel: per-employee drift scores and findings list | Modify: internal/tui/models/activity.go (panel mode); Test: internal/tui/models/activity_test.go | `go test -p 2 ./internal/tui/ -run TestActivity && go test -p 2 ./internal/tui/ -run TestParity` | panel absent | panel mode lists findings (severity-colored, lowercase text) and a drift summary row per employee; parity test green with the new manifest entries | Given fake review {drift_score:0.4}; When panel open; Then employee row shows 0.4 |

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly (JSON tags match C2/C4)
- [ ] All files at exact specified paths
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify
- [ ] ALL table rendering goes through tableutil Size + SetRows
- [ ] All new UI text lowercase

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing
- [ ] Parity manifest updated (palette command + shortcut) and parity_test green
- [ ] Tableutil-only table sizing; SetRows used (no raw SetRows bypass)
- [ ] Lowercase UI text everywhere
- [ ] Polling ticker stopped when view hidden (no background churn)
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- "a" is the chosen default key (unused in CommandPaletteKeys today);
  client.json5 overrides still work through the existing config path.
- Keep the model RPC-client-interfaced like TasksModel so tests inject fakes.
- Do not add server-push/WS code - polling only (RPC layer has no push).
- If the parity test demands a GUI-side counterpart entry that does not exist
  yet, add the deviation note ONLY if leaf 04 has not landed; otherwise the
  real entry must exist (coordinate via orchestrator at integration).
