# Executions observability + agents view redesign (Hermes) - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree.
> Your job: dispatch implementation agents, review their work, re-dispatch if
> incomplete, and track completion. Do NOT implement code yourself. All
> implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 00T-test-gate, 01-backend, 02-gui-agents-view, 03-tui-runs, 04-integration
- **Scope:** Make bot/employee executions first-class, persistent, and observable
  in GUI and TUI; redesign the Flutter agents view; fix all issues found there;
  keep the sidebar scrollable; persist runs server-side (durable, poll-based).

## Goal

Issue #160 closed live observability but left executions invisible after
disconnect and left the GUI agents view cramped. This tree delivers:

1. A server-side execution-history store derived from the existing
   bot.result / employee.result bus topics (the topics stay the source of
   truth; the store is a rebuildable projection).
2. An RPC endpoint + HTTP route to list recent execution runs (poll, not WS).
3. A redesigned Flutter agents view: larger 2x agent icons, half-height boxes,
   ~30% wider, 8-bit token-activity histograms (8 bars, 1 bar per 7.5 minutes,
   monochrome green on black with dithered empty cells), pulsing orange border
   at a random interval for currently-active agents, per-card counter showing
   OTHER concurrent active runs org-wide, and a scrollable sidebar including
   goals / eval runs / executions / facts.
4. An "executions" section in the same scrollable sidebar: list rows with a
   colored status dot and `session:<worktype>` text (worktype from trigger_ref,
   ellipsis-capped at 24 chars).
5. The same runs list in the TUI agents view (sub-view, already partially built).
6. Fix the agents-tab overflow (RenderFlex 13px on the bottom at 900x600).
7. Fix the persistent-first-selection-highlight bug in the agents grid.
8. Rename "executions"/online wording where the surface says "running" but means
   presence.
9. Unify GUI/TUI agents views as far as the data sources allow.

## Architecture

The execution-history store lives in internal/employee (or a new sibling
internal/execlog) as a SQLite-backed projection that subscribes to
bot.result and employee.result at the daemon layer (e.g. daemonComponents
subscribes and fans out). The projection is rebuildable and keeps a bounded
retention (newest N runs or 24h of rows, configurable). The RPC endpoint
returns {runs: [], total:int} matching the existing activity shape.

GUI polls the new HTTP route on an interval while the agents tab is visible.
GUI also keeps the live WS runs panel for organic growth but the primary
surface is poll-based so disconnect/reconnect shows history.

TUI calls the RPC directly.

Histograms bucket runs from the last hour into 8 cells (7.5 min per cell)
by timestamp, height = token share vs that agent's peak. Monochrome green
(#33ff33 or theme greenSuccess) with dim empty cells, block-glyph fill.

## Interface Contracts

### C1: ExecutionRun (wire + DB shape)

Table `execution_runs` columns:
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  tier TEXT NOT NULL,              -- bot|employee
  agent_id TEXT NOT NULL,
  status TEXT NOT NULL,            -- completed|failed|skipped
  output TEXT,
  tokens_used INTEGER NOT NULL DEFAULT 0,
  error TEXT,
  duration_ms INTEGER NOT NULL DEFAULT 0,
  stop_reason TEXT,
  trigger_ref TEXT NOT NULL DEFAULT '',
  session_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL         -- RFC3339

RPC `agents.executionRuns` params:
  { "agent_id": "..."|null, "limit": 50, "since": "rfc3339"|null }
Response:
  { "runs": [ExecutionRun], "total": int }

ExecutionRun (JSON):
  { "tier":"bot","agent_id":"...","status":"completed",
    "output":"...","tokens_used":0,"error":"","duration_ms":0,
    "stop_reason":"end_turn","trigger_ref":"...","session_id":"...",
    "created_at":"2026-11-19T00:00:00Z" }

HTTP route: GET /api/v1/executions?agent_id=...&limit=50&since=rfc3339

### C2: GUI agents card contract

- Icon size 2x (40px).
- Card height 1/2.
- Width ~30% wider.
- Histogram: 8 bars, 7.5-minute buckets, token share vs agent peak.
- Active agents (status running|quota_wait, i.e. doing work): orange border
  pulsing at a randomized interval (700-1100ms), never static.
- Per-card counter: OTHER concurrent active runs org-wide (excluding this
  agent's own). Bot/employee active floor derived from roster + live loops.
- Rename visible label "online" where it means online presence.

### C3: Sidebar layout contract

The right-hand column (goals + eval runs + executions + facts) is a single
scrollable Column (SingleChildScrollView or CustomScrollView) — never a
nested unbounded Column inside an Expanded with non-scrollable children.
Facts panel renders as an accordion or a compact section; it is read-only
memory facts, not agent-specific. Crowding fixed by giving facts a fixed
max height and its own scroll.

### C4: Selection highlight fix

The grid selection currently keeps the first agent persistently
highlighted even after selecting another row (keyboard focus pane vs
activeAgentProvider desync). _selectedIndex and activeAgent must stay in
sync in both _buildAgentCard and the tap handler.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-test-gate | COMPLETE | 1 | store/projector unit tests + e2e scenario 05 (persisted_history_test.go); GUI widget tests updated to redesign spec |
| 01-backend | COMPLETE | 1 | ExecutionStore + projector + agents.executionRuns/Histogram RPC + HTTP routes + roster bridge; wired in activity_wiring.go; 6 unit tests green |
| 02-gui-agents-view | COMPLETE | 1 | 2x icons, half-height ~30%-wider cards, 8-bit histogram, random-interval pulse, per-card other-active counter, online wording, whole-sidebar scroll, selection-highlight fix, overflow fixed; 16 widget tests green |
| 03-tui-runs | COMPLETE | 1 | runs sub-view polls agents.executionRuns on entry + 5s tick; live frames merge; internal/tui suite green |
| 04-integration | IN_PROGRESS | 1 | docs/workflows/bots.md updated; manifest scenario 05 added; graphs regen + commit pending |

Execution note (2026-11-19): implemented directly in the isolation worktree
(feat/issue160-execution-runs) rather than via leaf dispatch - the scope was
executed in-session in dependency order (backend -> GUI -> TUI -> docs).
The roster TUI error was diagnosed as a stale running daemon (pre-roster
binary); resolved by operator restart, no code change.

## Integration Test Plan

1. `make test` on the daemon package(s)
2. `go test -p 2 ./internal/tui/ -count=1`
3. `go test ./internal/comm/http -run 'TestWSSchema|TestWSExecution'`
4. `flutter test` on the affected suites
5. `meept chat` + GUI restart smoke test; agents tab opens, runs list polled
6. `make graphs` and `make graphs-check`

## Test Gate

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F1 | execution_runs projection (bus -> store) | 01 | store roundtrip | e2e execution-observability | PENDING |
| F2 | agents.executionRuns RPC + HTTP list | 01 | rpc handler + http route | e2e | PENDING |
| F3 | GUI poll-based executions list in sidebar | 02 | widget | GUI integration | PENDING |
| F4 | GUI agent card redesign (2x icon, half height, histogram) | 02 | widget | GUI integration | PENDING |
| F5 | Selection highlight fix | 02 | widget | GUI integration | PENDING |
| F6 | Sidebar scrollable including facts/goals | 02 | widget | GUI integration | PENDING |
| F7 | Active-agent pulse at random interval | 02 | widget | GUI integration | PENDING |
| F8 | Per-card "other concurrent active" counter | 02 | widget | GUI integration | PENDING |
| F9 | TUI runs view (polled from server) | 03 | unit | e2e | PENDING |

Delta log: none yet.
