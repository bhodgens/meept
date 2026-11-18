# TUI/GUI Consistency Fixes - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree. Your
> job: (1) dispatch test gates, (2) dispatch implementation agents, (3) review
> their work in-session, (4) re-dispatch if incomplete, (5) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 4 branches, 2 root-level leaves (14 documents total)
- **Scope:** Fix the 10 correctness/consistency issues reported across the
  Flutter GUI and the Bubbletea TUI, and add session search to both.

## Goal

The Flutter session popup is a fixed 800x560 dialog with duplicated, unloaded
tab bodies. The TUI agents list shows only employees, missing bots and
registry agents, with no type or active-count column. Kanban cannot be opened
cleanly from either surface, and the subsystem is off by default. Sessions are
flat lists on both surfaces with no project tree. Session search exists on
both sides but is not reachable from the sessions tree.

This tree delivers a unified agent roster RPC, a full-window popup that reuses
the canonical tabs, agent type/active/cost columns on both surfaces, a
project-grouped session tree with search on both surfaces, a clean kanban
disabled state, and kanban enabled by default with a GUI tools-menu entry.

## Architecture

Three shared contracts anchor the tree. First, a unified roster RPC merges
registry agents, bots, and employees into one row shape with type, active
count, and daily cost. Both UIs consume it. Second, a session-tree grouping
function derives a project bucket from `project_id`, then `project_path`, then
an "unbound" bucket; both UIs render the same grouping. Third, the kanban
disabled state is detected at the RPC error boundary, so an off-by-default
subsystem produces a short guidance message instead of a raw method-not-found
string. Branches are file-disjoint: backend Go, TUI Go, Flutter Dart, and
config/docs/e2e. The config branch owns the kanban default flip and the e2e
pin rewrite; it must land before the TUI/GUI kanban leaves reach final review.

## Interface Contracts

### C1: Unified agent roster row (backend-owned, both UIs consume)

```
type RosterEntry struct {
    ID          string `json:"id"`
    Name        string `json:"name"`
    Type        string `json:"type"`        // "agent" | "bot" | "employee"
    Status      string `json:"status"`      // running | paused | error | stopped | quota_wait
    ActiveCount int    `json:"active_count"`
    DailyCostCents int `json:"daily_cost_cents"`
    Role        string `json:"role,omitempty"`
    Description string `json:"description,omitempty"`
    Enabled     bool   `json:"enabled"`
}
// RPC: "agents.roster" -> {"agents": []RosterEntry}
// Owner: 01-backend/01-roster-rpc.md
// Consumers: 02-tui/02-agents-columns.md, 03-gui/02-popup-tabs.md
```

### C2: Session tree bucket (shared algorithm, implemented twice)

```
bucketFor(session) = session.ProjectID if non-empty,
                     else session.ProjectPath if non-empty,
                     else "unbound"
// Go: internal/tui/models/sessions_tree.go
// Dart: ui/flutter_ui/lib/features/sessions/session_tree.dart
// Owner: 02-tui/03-sessions-tree.md (Go), 03-gui/03-sessions-tree.md (Dart)
// Consumers: their own surfaces only. The bucket NAMES must match.
```

### C3: Kanban disabled guidance string (TUI-owned)

```
const kanbanDisabledHint = "kanban board is not enabled; set [kanban].enabled=true"
// Detect: err string contains "method not found: kanban.board.list"
// Owner: 02-tui/04-kanban-error.md
// Consumers: none (TUI-local)
```

### C4: Search entry points (existing code, reused)

```
Dart: SearchPanel(scope: SearchScope.sessions) in
      ui/flutter_ui/lib/features/search/search_panel.dart
Go:   OpenSearchViewMsg with sessions scope in
      internal/tui/models/search.go
// Owner: 03-gui/04-search-entry.md, 02-tui/05-search-entry.md
// Consumers: session tree headers on both surfaces
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-e2e-tests.md | leaf | none | 28K | Z |
| 01 | 01-backend/orchestrator.md | branch | none | 18K | A |
| 01/00T | 01-backend/00T-roster-tests.md | leaf | none | 26K | A0 |
| 01/01 | 01-backend/01-roster-rpc.md | leaf | 01/00T | 55K | A1 |
| 01/02 | 01-backend/02-roster-active-cost.md | leaf | 01/00T, 01/01 | 45K | A2 |
| 01/03 | 01-backend/03-sidecar-devkey.md | leaf | 04/00T; C5, C7 | 45K | D1b |
| 02 | 02-tui/orchestrator.md | branch | 01/00T (contract) | 18K | B |
| 02/00T | 02-tui/00T-tui-tests.md | leaf | none | 32K | B0 |
| 02/01 | 02-tui/01-agents-data.md | leaf | 02/00T, C1 | 40K | B1 |
| 02/02 | 02-tui/02-agents-columns.md | leaf | 02/00T, 02/01, C1 | 45K | B2 |
| 02/03 | 02-tui/03-sessions-tree.md | leaf | 02/00T, C2 | 55K | B3 |
| 02/04 | 02-tui/04-kanban-error.md | leaf | 02/00T, C3 | 25K | B4 |
| 02/05 | 02-tui/05-search-entry.md | leaf | 02/00T, 02/03, C4 | 30K | B5 |
| 03 | 03-gui/orchestrator.md | branch | none | 18K | C |
| 03/00T | 03-gui/00T-gui-tests.md | leaf | none | 28K | C0 |
| 03/01 | 03-gui/01-popup-shell.md | leaf | 03/00T | 45K | C1 |
| 03/02 | 03-gui/02-popup-tabs.md | leaf | 03/00T, 03/01, C1 | 50K | C2 |
| 03/03 | 03-gui/03-sessions-tree.md | leaf | 03/00T, C2 | 60K | C3 |
| 03/04 | 03-gui/04-search-entry.md | leaf | 03/00T, 03/03, C4 | 30K | C4 |
| 04 | 04-config-docs/orchestrator.md | branch | none | 15K | D |
| 04/00T | 04-config-docs/00T-e2e-pin.md | leaf | none | 25K | D0 |
| 04/01 | 04-config-docs/01-kanban-default.md | leaf | 04/00T | 35K | D1 |
| 04/02 | 04-config-docs/02-docs-ag.md | leaf | 04/00T, 04/01 | 30K | D2 |

Same-letter children have no inter-dependencies and dispatch together
(max 3 per batch per concurrency config). Letters A/B/C/D denote
file-disjoint write ownership and can run in parallel across branches.

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill
`hierarchical-plan-execution`.

### Phase 0: Test Gate (before ANY implementation dispatch)

Dispatch all `00T-` test leaves first (00T root, 01/00T, 02/00T, 03/00T,
04/00T) in parallel. Brief: "Write tests ONLY, no implementation. Tests must
fail RED for missing-implementation reasons. Do NOT commit. Do NOT git add."
Validate in-session (never delegate review) against each branch's Test Gate
matrix. On any delta, the LESSER of plan/tests updates to reflect the GREATER.
When every matrix row is VALIDATED, commit gate tests with `--no-verify`
(RED-by-design; record scoped gates run in the commit body). Set branches to
TESTS_VALIDATED. Then implementation may dispatch.

### Phase 1: Dispatch Concurrency Group Z then A/B/C/D

Dispatch root 00T alone first. Then dispatch branches A, B, C, D in parallel
(their write sets are disjoint). Within each branch, honor the numeric child
dependencies in the child index. Same-number steps in a branch run together.

### Phase 2: Review and Commit Each Child

Review in-session (the main model - never a delegated reviewer). Check leaf
spec, contracts, Review Checklist; run tests; gate-test diff must be empty.
Pass: commit + status REVIEWED. Gaps: re-dispatch with findings, max 3 cycles,
then escalate to parent. Use `git commit -- <exact paths>` to avoid sweeping
sibling work.

### Phase 3: Integration Review

After ALL children REVIEWED: run the full gate set below, verify C1 and C2
parity across surfaces, run the line-number corruption check on every changed
source file, normalize formatting, commit integration changes, set children
COMPLETE. Then: create the isolation worktree before wave 1 per the skill,
dispatch against its absolute path, merge to main only at top-level completion.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts satisfied (signatures, types, paths)
- [ ] All specified files created/modified at exact paths
- [ ] Gate tests pass GREEN; `git diff` shows no gate-test modification
- [ ] Tests written and passing (TDD followed)
- [ ] Code follows project conventions (lowercase UI text, tableutil sizing)
- [ ] No scope creep; no obvious bugs or security issues
- [ ] No debug artifacts (print debugging, TODOs, placeholders)
- [ ] No line-number corruption (`     N|` prefixes) in source files

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Languages:** Go (backend, TUI), Dart (Flutter GUI), JSON5 (config), Markdown (docs).
- **Go:** gofmt; wrap errors with `%w`; no ignored errors; no `time.Now().UnixNano()` IDs (use `pkg/id.Generate()`).
- **Dart:** lowercase UI strings; `dart format` before reporting; no top-level `dart:io` in shared code.
- **UI text:** all lowercase in TUI and GUI.
- **Tables:** size every `bubbles/table` on both axes via `tableutil.Size`; rows via `tableutil.SetRows`.
- **Testing:** new features in hermetic e2e (`e2e/suites/<name>/` + manifest). Unit tests only for bug fixes or gate tests.
- **Commits:** orchestrator only. Leaf agents never run `git add` or `git commit`.
- **Typed-nil:** guard concrete pointers assigned into interface fields.

## Requirement Traceability

| Req | Source (user words, verbatim when possible) | Plan element | Verified by |
|-----|---------------------------------------------|--------------|-------------|
| R1 | "popup dialog should be the full size of the app window" | 03-gui/01-popup-shell.md T1 | widget test fullscreen |
| R2 | "plans and tasks are not populated with anything" | 03-gui/02-popup-tabs.md T1 | widget test loaded tabs |
| R3 | "agents tab should indicate how many are active as well as associated costs similar how to its displayed in TUI columns" | 01-backend/01-roster-rpc.md + 03-gui/02-popup-tabs.md T2 | roster RPC test + widget test |
| R4 | "'plans' should reflect the same data as the TUI plans, as should tasks" | 03-gui/02-popup-tabs.md T1 | canonical tab reuse |
| R5 | "tab-indented (arrow key to expand) trees for projects ... where the search capability button should be placed" | 02-tui/03 + 03-gui/03 + 02-tui/05 + 03-gui/04 | tree tests + search entry tests |
| R6 | "search already implemented somewhere, reuse that code if we can" | C4 + 02-tui/05 + 03-gui/04 | reuse assertion in review |
| R7 | "kanban board ... should have a 'tools' menu option" | 04-config-docs branch + GUI kanban menu (04/01 T3) | menu wiring grep + route test |
| R8 | "agents listing doesn't show all agents, it only shows the two employees" | 01-backend/01 + 02-tui/01 | roster RPC test + TUI view test |
| R9 | "column indicating the type of agent - agent vs bot vs employee" | 02-tui/02 T1 + C1 | TUI column test |
| R10 | "column for the number of active agents of a given type/name" | 01-backend/02 + 02-tui/02 T2 | active count test |
| R11 | "errors with 'method not found kanban.board.list'. This should error cleanly if ... sidecar isn't running" | 02-tui/04 + C3 | TUI error render test |
| R12 | "sessions should also be tab indented and use an expanding tree based on the project" | 02-tui/03 + C2 | TUI tree test |
| R13 | "add session search capability to both TUI and GUI" | 02-tui/05 + 03-gui/04 | search entry tests |
| R14 | "Kanban board should be enabled by default" | 04-config-docs/01-kanban-default.md | default config test + e2e pin rewrite |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-e2e-tests | PENDING | 0 | |
| 01-backend | PENDING | 0 | |
| 01/00T-roster-tests | PENDING | 0 | |
| 01/01-roster-rpc | PENDING | 0 | |
| 01/02-roster-active-cost | PENDING | 0 | |
| 02-tui | PENDING | 0 | |
| 02/00T-tui-tests | PENDING | 0 | |
| 02/01-agents-data | PENDING | 0 | |
| 02/02-agents-columns | PENDING | 0 | |
| 02/03-sessions-tree | PENDING | 0 | |
| 02/04-kanban-error | PENDING | 0 | |
| 02/05-search-entry | PENDING | 0 | |
| 03-gui | PENDING | 0 | |
| 03/00T-gui-tests | PENDING | 0 | |
| 03/01-popup-shell | PENDING | 0 | |
| 03/02-popup-tabs | PENDING | 0 | |
| 03/03-sessions-tree | PENDING | 0 | |
| 03/04-search-entry | PENDING | 0 | |
| 04-config-docs | PENDING | 0 | |
| 04/00T-e2e-pin | PENDING | 0 | |
| 04/01-kanban-default | PENDING | 0 | |
| 04/02-docs-ag | PENDING | 0 | |
| 01/03-sidecar-devkey | PENDING | 0 | |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: implementation children stay PENDING until their branch test-gate
leaves reach TESTS_VALIDATED.

## Integration Test Plan

1. `go build ./...` - all Go packages compile.
2. `make e2e-fast` - hermetic suites, including the rewritten kanban default pin.
3. `flutter analyze lib/ test/` - zero errors under `ui/flutter_ui/lib/`.
4. `flutter test` - full widget suite green.
5. Live: build `./bin/meept`, start `./bin/meept-daemon -f`, drive `agent-tui run ./bin/meept chat`; press agents key, sessions key, kanban key, search key. Capture screenshots and verify type/active/cost columns, project tree, clean kanban message, search entry.
3. Live GUI: open the session popup full window; check plans/tasks populated, agents columns, project tree, search button, hamburger tools menu kanban entry. The board must load with no pairing step on a default install.

## Test Gate

Test leaves: 00T-e2e-tests.md, 01/00T-roster-tests.md, 02/00T-tui-tests.md,
03/00T-gui-tests.md, 04/00T-e2e-pin.md - dispatched in Phase 0, validated by
the control agent (orchestrator, in-session).

| # | Feature (from plan) | Source | Unit tests | E2E tests | Status |
|---|---------------------|--------|-----------|-----------|--------|
| F1 | agents.roster merges 3 sources | 01-backend/01 T1 | TestRoster_MergesSources | e2e roster RPC | PENDING |
| F2 | roster active count + cost | 01-backend/02 T1 | TestRoster_ActiveAndCost | e2e roster fields | PENDING |
| F3 | TUI shows all agent types | 02-tui/01,02 T1 | agents_panel render test | tui-flows | PENDING |
| F4 | TUI type + active columns | 02-tui/02 T2 | agents_panel column test | tui-flows | PENDING |
| F5 | TUI project tree + search | 02-tui/03,05 | sessions tree test | tui-flows | PENDING |
| F6 | TUI clean kanban error | 02-tui/04 | kanban view error test | e2e kanban | PENDING |
| F7 | GUI popup fullscreen | 03-gui/01 T1 | widget test | gui-flows | PENDING |
| F8 | GUI tabs reuse canonical widgets | 03-gui/02 T1 | widget test | gui-flows | PENDING |
| F9 | GUI agent active/cost display | 03-gui/02 T2 | widget test | gui-flows | PENDING |
| F10 | GUI project tree + search | 03-gui/03,04 | widget test | gui-flows | PENDING |
| F11 | kanban default on + clean pin | 04-config-docs/01 | config default test | e2e kanban | PENDING |
| F12 | kanban in GUI tools menu | 04-config-docs/01 T3 | widget test | gui-flows | PENDING |
| F13 | dev key seeded as sidecar board token | 01-backend/03 T1 | TestKanbanDevKeySeed | e2e kanban | PENDING |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED

Delta log: none yet.

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as dated
addenda. Never edit a dispatched leaf document mid-execution.

## Open Questions

1. Q1: Should the unified roster be a new `agents.roster` RPC or an extension
   of `agents.list`? Recommendation: new RPC, so the employees-only contract
   used by existing callers stays byte-identical. Impact: one new method to
   register and document.
2. Q2 (RESOLVED - user chose B): `sidecar_enabled` becomes true alongside
   `enabled`, and `$MEEPT_HOME/dev_key` is seeded into the sidecar token store
   with read/mutate/takeover grants before spawn (01-backend/03-sidecar-devkey.md).
   Rationale: the GUI reaches the board at 127.0.0.1:8790 with the dev key as
   bearer, so flipping `enabled` alone leaves the GUI board unreachable.
   Impact: one extra daemon child; no Dart change; remote access still needs
   the documented TLS proxy and `allowed_origins`.
## Notes

- Two generated files are already modified in the working tree
  (`docs/generated/bus-topology.md`, `docs/generated/rpc-handlers.json`). The
  config/docs branch must not sweep them into a commit.
- Enabling kanban by default is a product decision explicitly requested by the
  user. The e2e pin at `e2e/suites/kanban/kanban_test.go:579-594` must be
  rewritten to use an explicit disabled config, not deleted.
- Two registration paths serve `GET /api/v1/agents`: `internal/comm/http/server.go:1912`
  (employee handler) and `internal/comm/web/agents.go:13-29` (web handler with
  agentListerAdapter wired at `internal/daemon/components.go:5289`). The roster
  RPC leaf must confirm which path owns the socket before extending it.
- Codebase survey already performed in-session before authoring; no separate
  survey subagent needed (Pitfall 30 satisfied).
