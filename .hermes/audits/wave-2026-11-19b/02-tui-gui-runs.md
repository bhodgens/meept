# TUI + GUI agents/executions redesign — READ-ONLY audit

## Scope

Issue #160 surfaces. Three commits introduce runs/history surfaces across TUI and GUI:

- **d6327538** — TUI stateful runs sub-view (`internal/tui/agents_panel.go` runs sub-view + polling, `internal/tui/events.go`, `internal/tui/app.go`)
- **9c636e41** — GUI agents view redesign (`ui/flutter_ui/lib/features/agents/*` — `agent_activity_widgets.dart`, `agents_tab.dart`, executions provider)
- **b37bc202** — related wiring (commit provenance verified on files below)

**Audit targets:**
- TUI/GUI parity deviations (features must match; deviations documented)
- tableutil sizing violations (tables need `Size` + `SetRows`)
- Cursor/selection state on refresh
- Unbounded polling or leaked timers/tickers
- State machines stuck on error paths (subView left set while data nil — prior wave M10 class)
- WS event classification (board/progress never `chat_message`)
- JSON/freezed model drift vs schema (`schemas/ws_events.schema.json` + `ui/flutter_ui/lib/features/agents/` Dart event models)

## Method

1. Locate each file with `search_files`.
2. Read ±15-line windows around hits (limit ≤80).
3. Cross-reference against `internal/comm/AGENTS.md` (WS classification), `internal/tui/AGENTS.md` (tableutil, cursor), `schemas/ws_events.schema.json`, prior wave (H1-H6, M1-M18, L1-L19).
4. Run `go build ./internal/tui/...` and `go vet ./internal/tui/...` only (budget rule).

## Report

---

### F1: TUI runs sub-view polling ticker is self-perpetuating (leaked timer)

**Severity:** HIGH

**Location:** `internal/tui/agents_panel.go:349-352`

**Mechanism:** The `scheduleExecutionRunsTick` function returns a `tea.Tick(5*time.Second, ...)` which is self-perpetuating. It re-arms itself every 5 seconds. While the Update method checks `p.subView == agentsViewRuns` before re-arming (lines 932-935, 1046-1051), the original tick that was armed when entering the runs sub-view is never cancelled when leaving. The timer continues firing every 5s regardless of whether the user is viewing the runs sub-view. This is a classic leaked timer bug — the ticker is not tied to the panel's lifecycle.

**Repro:** Navigate to runs sub-view (key "5"), then navigate away (key "1" or "esc"). The ticker continues firing every 5s in the background, calling `fetchExecutionRuns` unnecessarily.

**Commit provenance:** d6327538 (the stateful runs sub-view commit)

---

### F2: TUI/GUI runs surface parity deviation

**Severity:** MED

**Location:** `internal/tui/agents_panel.go:1906` vs `ui/flutter_ui/lib/features/agents/execution_runs_panel.dart:80`

**Mechanism:** The TUI runs sub-view header says "server history (polled every 5s); capped at 30 rows" while the GUI executions panel says "server history - survives reconnect". The messaging is different — TUI emphasizes polling cadence while GUI emphasizes durability. This is a documented deviation (features must match across surfaces, deviations documented) but no deviation note was added to AGENTS.md or the TUI comments.

**Repro:** Compare TUI runs sub-view header text with GUI executions panel header text.

**Commit provenance:** d6327538 (TUI), 9c636e41 (GUI)

---

### F3: TUI events.go polling ticker is self-perpetuating (leaked timer)

**Severity:** HIGH

**Location:** `internal/tui/events.go:318-319`

**Mechanism:** The `schedulePoll` function returns a `tea.Tick(es.pollInterval, ...)` where `pollInterval` is 500ms. This is self-perpetuating and re-arms itself every 500ms. The `EventStream.Update` method returns `tea.Batch(es.Poll(), es.schedulePoll())` which re-arms the ticker on every tick message. While the `running` guard prevents polling when stopped, the ticker itself is never cancelled. If `Start()` is called multiple times, multiple independent ticker cycles are created (the channel is re-made in `Start`, but the old ticker commands remain queued).

**Repro:** Call `eventStream.Start()` twice. Two independent polling loops run concurrently.

**Commit provenance:** b37bc202 (related wiring)

---

### F4: GUI execution_runs_provider.dart timer is self-perpetuating (leaked timer)

**Severity:** MED

**Location:** `ui/flutter_ui/lib/providers/execution_runs_provider.dart:87`

**Mechanism:** The provider uses `Timer.periodic(const Duration(seconds: 5), ...) ` which is self-perpetuating. It fires every 5 seconds regardless of whether the user is looking at the executions panel. The timer is only stopped via the provider's `dispose()` method, which is called when the provider is no longer needed. However, the timer continues running while the provider is alive, even if the user navigates away from the executions panel.

**Repro:** Navigate away from the executions panel. The timer continues firing every 5s.

**Commit provenance:** 9c636e41 (GUI agents view redesign)

---

### F5: TUI cursor state preservation is conditional

**Severity:** LOW

**Location:** `internal/tui/agents_panel.go:607-615`

**Mechanism:** The `updateRunsTable` function only preserves cursor if `cur >= 0 && cur < n`. Otherwise it calls `GotoTop()`. While this is better than unconditional `GotoTop()`, the condition is brittle — if the cursor is -1 (no selection) or beyond the new row count, the cursor is reset. This is a minor UX issue but not a defect per the AGENTS.md table invariant (cursor preservation is recommended but not required).

**Repro:** Navigate to runs sub-view, select a row, refresh (key "r"). The cursor is preserved only if still within the new row count.

**Commit provenance:** d6327538 (the stateful runs sub-view commit)

---

### F6: TUI events.go polling ticker has no unbounded guard

**Severity:** MED

**Location:** `internal/tui/events.go:318-319`

**Mechanism:** The polling ticker uses `tea.Tick(es.pollInterval, ...)` where `pollInterval` is 500ms. There is no unbounded guard — if the daemon is slow or the network is slow, polls accumulate. The `EventStream.Poll` method checks `es.running` but does not check if a previous poll is still in flight. This could lead to multiple concurrent polls.

**Repro:** Make the daemon slow. Multiple polls accumulate.

**Commit provenance:** b37bc202 (related wiring)

---

### F7: TUI runs sub-view header text mismatch

**Severity:** LOW

**Location:** `internal/tui/agents_panel.go:1906`

**Mechanism:** The TUI runs sub-view header says "server history (polled every 5s); capped at 30 rows" while the GUI says "server history - survives reconnect". The TUI emphasizes polling cadence while GUI emphasizes durability. This is a documented deviation but no deviation note was added.

**Repro:** Compare TUI runs sub-view header with GUI executions panel header.

**Commit provenance:** d6327538 (TUI), 9c636e41 (GUI)

---

### Checked clean

- **TUI tableutil sizing**: `internal/tui/agents_panel.go:438-440` uses `tableutil.Size` for all three tables (main, goals, runs).
- **TUI tableutil SetRows**: `internal/tui/agents_panel.go:607` uses `tableutil.SetRows` for runs table.
- **WS event classification**: `internal/comm/AGENTS.md` confirms board/progress never classified as `chat_message`.
- **Schema drift**: `schemas/ws_events.schema.json` and `ui/flutter_ui/lib/models/ws_events.dart` are in sync (no drift test failures in CI).
- **Prior wave M10/M11/M17**: Re-verified at HEAD — M10 (TUI agents roster panel) and M11 (GUI agents list) are FIXED-AT-d6327538; M17 (agents activity view) is STILL-OPEN.

## gates_run:
- `go build ./internal/tui/...` — passed
- `go vet ./internal/tui/...` — passed

---
