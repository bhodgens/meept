# Components.Start Rollback Coverage Extension (D15 Completion)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extend the existing D15 partial-init rollback in `internal/daemon/components.go` so every stoppable component started in `Components.Start()` is tracked for rollback, eliminating the timing window where started-but-untracked components keep running between a `Start()` failure and daemon-level `shutdown()`.

**Architecture:** Append each successfully-started component to `startedHandlers` immediately after its `Start()` returns nil. Extend the rollback switch statement to handle every case. For background goroutines without a `Stop()` method (`PricingSyncer`), cancel the lifecycle context in the rollback. Preserve stop ordering constraints by iterating `startedHandlers` in reverse (already implemented at line 1716).

**Tech Stack:** Go 1.22+, `internal/daemon/components.go` (4285 LOC, ~24 components).

---

## Current State (after prior D15 fix)

- `startedHandlers []string` is declared at line 1709.
- Deferred rollback at lines 1712-1754 iterates in reverse.
- Only 6 components are appended: `chat`, `status`, `session`, `queue`, `task`, `worker`.
- The `sync` and `syncmgr` switch cases (lines 1744-1751) exist but are **dead code** — their keys are never appended.

## Components to Add to Rollback Tracking (18)

Ordered by their position in `Start()`:

| # | Start Line | Component | Handler Key | Stop Call | Notes |
|---|-----------|-----------|-------------|-----------|-------|
| 1 | 1776 | `MemoryHandler` | `memory` | `c.MemoryHandler.Stop(ctx)` | Error logged-not-returned; add unconditional append after successful start |
| 2 | 1783 | `ResultCache` | `cache` | `c.ResultCache.Stop()` | Sync Start, no error |
| 3 | 1820 | `WorkerPool` | `pool` | `c.WorkerPool.Stop(ctx)` | Error logged-not-returned |
| 4 | 1827 | `Scheduler` | `scheduler` | `c.Scheduler.Stop(ctx)` | Error logged-not-returned |
| 5 | 1834 | `Orchestrator` | `orchestrator` | `c.Orchestrator.Stop(ctx)` | **Ordering**: before scheduler/queue |
| 6 | 1841 | `TeamOrchestrator` | `team` | `c.TeamOrchestrator.Stop(ctx)` | |
| 7 | 1848 | `Watchdog` | `watchdog` | `c.Watchdog.Stop()` | Add nil-guard in Start (currently bare) |
| 8 | 1853 | `SelfImproveSched` | `selfimprove` | `c.SelfImproveSched.Stop()` | Background goroutine |
| 9 | 1859 | `CalendarReminder` | `calendar` | `c.CalendarReminder.Stop()` | Background goroutine |
| 10 | 1874 | `RefreshManager` | `refresh` | `c.RefreshManager.Stop()` | |
| 11 | 1881 | `PricingSyncer` | — | (no Stop method) | Lifecycle ctx cancel only |
| 12 | 1890 | `SyncManager` | `syncmgr` | `c.SyncManager.Stop()` | Rollback case exists (dead code) |
| 13 | 1895 | `SyncHandler` | `sync` | `c.SyncHandler.Stop(ctx)` | Rollback case exists (dead code) |
| 14 | 1903 | `WebServer` | `web` | `c.WebServer.Shutdown(ctx)` | Background goroutine |
| 15 | 1914 | `TelegramBot` | `telegram` | `c.TelegramBot.Stop()` | Background goroutine |
| 16 | 1922 | `BotManager` | `bot` | `c.BotManager.StopAll()` | Error logged-not-returned |
| 17 | 1931 | `ClusterEngine` | `cluster` | `c.ClusterEngine.Stop()` | Conditional on cluster mode |
| 18 | 1950 | `ClusterGitSync` | `clustergit` | `c.ClusterGitSync.Stop()` | Conditional on cluster mode |

## Stop-Ordering Constraints (preserve by reverse-iteration of `startedHandlers`)

| Constraint | Reason |
|-----------|--------|
| `Orchestrator.Stop` before `Scheduler.Stop` and `QueueHandler.Stop` | comment @1997 |
| `SyncHandler.Stop` before `QueueHandler.Stop` | comment @1983 |
| `SyncHandler.Stop` before `SyncManager.Stop` | dependency |
| `WebServer.Shutdown` first | comment @1969 |
| `BotManager.StopAll` before `Scheduler.Stop` | comment @2018 |

Reverse iteration of `startedHandlers` (append order = start order) preserves all of these **if** append order matches start order. Verify each task.

---

## Task 1: Add tests covering rollback behavior

**Files:**
- Modify: `internal/daemon/components_test.go`

**Step 1: Write failing test that asserts all started components are stopped on Start() failure**

The test should:
- Construct a `Components` with mocks where Start succeeds for components 1..N, then fails for component N+1 (e.g., QueueHandler.Start returns an error after several others have started).
- Call `Start()`.
- Assert that every prior component's Stop was called.

Use a mock counter or inject a failure-injecting fake. (The existing components.go has nil-guarded Stop calls, so a partial-components instance where later components are nil but earlier ones are real is a viable test pattern.)

**Step 2: Run test, verify it fails**

Run: `go test ./internal/daemon/... -run TestComponentsStart_RollbackCoverage -v`
Expected: FAIL (only 6 of 24 components currently rolled back)

**Step 3: Commit failing test**

```bash
git add internal/daemon/components_test.go
git commit -m "test(daemon): add rollback coverage assertion (currently failing)"
```

---

## Task 2: Append all stoppable components to `startedHandlers`

**Files:**
- Modify: `internal/daemon/components.go:1776-1955`

**Step 1: After each successful `Start()` call, append the handler key**

For each line in the table above, add `startedHandlers = append(startedHandlers, "<key>")` immediately after the `Start()` call succeeds. For components whose `Start()` returns an error that is currently logged-not-returned (e.g., `MemoryHandler.Start` at line 1776), append only on `err == nil`. For components whose `Start()` has no error return (e.g., `ResultCache.Start()` at line 1783), append unconditionally.

For `PricingSyncer` (no Stop method), do **not** append a key; instead, ensure the rollback's deferred function calls `c.cancel()` early so the goroutine's ctx.Done() fires. (The lifecycle ctx is already cancelled by `c.cancel()` at `Stop()` line 1963 — but during a Start() rollback, `c.cancel` may not yet exist. Verify whether the lifecycle context and cancel func are set before `Start()` runs.)

**Step 2: For dead-code rollback cases (`sync`, `syncmgr`)**

Append `startedHandlers = append(startedHandlers, "syncmgr")` after line 1890 and `startedHandlers = append(startedHandlers, "sync")` after line 1895, both on `err == nil`. The existing switch cases at 1744-1751 then become live.

**Step 3: Run the failing test from Task 1**

Run: `go test ./internal/daemon/... -run TestComponentsStart_RollbackCoverage -v`
Expected: now PASSES for tracked components; `PricingSyncer` still leaks (acceptable per design).

**Step 4: Commit**

```bash
git add internal/daemon/components.go internal/daemon/components_test.go
git commit -m "fix(daemon): track all started components for D15 rollback"
```

---

## Task 3: Extend rollback switch statement

**Files:**
- Modify: `internal/daemon/components.go:1712-1754`

**Step 1: Add switch cases for every new key**

Extend the rollback switch to handle each key added in Task 2. Use the same nil-guarded pattern as existing cases:

```go
case "memory":
    if c.MemoryHandler != nil { c.MemoryHandler.Stop(ctx) }
case "cache":
    if c.ResultCache != nil { c.ResultCache.Stop() }
case "pool":
    if c.WorkerPool != nil { c.WorkerPool.Stop(ctx) }
case "scheduler":
    if c.Scheduler != nil { c.Scheduler.Stop(ctx) }
case "orchestrator":
    if c.Orchestrator != nil { c.Orchestrator.Stop(ctx) }
case "team":
    if c.TeamOrchestrator != nil { c.TeamOrchestrator.Stop(ctx) }
case "watchdog":
    if c.Watchdog != nil { c.Watchdog.Stop() }
case "selfimprove":
    if c.SelfImproveSched != nil { c.SelfImproveSched.Stop() }
case "calendar":
    if c.CalendarReminder != nil { c.CalendarReminder.Stop() }
case "refresh":
    if c.RefreshManager != nil { c.RefreshManager.Stop() }
case "web":
    if c.WebServer != nil { _ = c.WebServer.Shutdown(ctx) }
case "telegram":
    if c.TelegramBot != nil { c.TelegramBot.Stop() }
case "bot":
    if c.BotManager != nil { c.BotManager.StopAll() }
case "cluster":
    if c.ClusterEngine != nil { _ = c.ClusterEngine.Stop() }
case "clustergit":
    if c.ClusterGitSync != nil { _ = c.ClusterGitSync.Stop() }
```

**Step 2: Verify stop ordering**

Walk through the reverse-iteration order to confirm constraints. Append order = start order, so reverse gives: clustergit, cluster, bot, telegram, web, refresh, calendar, selfimprove, watchdog, team, orchestrator, scheduler, pool, sync, syncmgr, cache, memory, worker, task, queue, session, status, chat. This means:
- `web` (item 14) is rolled back early enough — before handlers but not absolutely first. Acceptable since `web` is just the HTTP listener.
- `orchestrator` (item 5) rolls back before `scheduler` (item 4) and before queue/task/worker (items 1-3 reverse). ✅ matches constraint.
- `sync` (item 13) rolls back before `syncmgr` (item 12). ✅ matches constraint.

**Step 3: Add a Watchdog nil-guard in Start()**

`Watchdog.Start(ctx)` at line 1848 is the only Start call without a nil check. Add one to prevent latent panic:

```go
if c.Watchdog != nil {
    c.Watchdog.Start(ctx)
}
```

**Step 4: Run tests**

Run: `go test ./internal/daemon/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/daemon/components.go
git commit -m "fix(daemon): handle all component keys in D15 rollback switch"
```

---

## Task 4: Document PricingSyncer lifecycle and add cancel-on-rollback

**Files:**
- Modify: `internal/daemon/components.go` (rollback deferred function)

**Step 1: Verify lifecycle context setup**

Check whether `c.cancel` is assigned before `Start()` is called (in `Daemon.Run` or wherever `Components.Start` is invoked from). If yes, add `if c.cancel != nil { c.cancel() }` at the top of the rollback deferred function so `PricingSyncer`'s goroutine exits.

If `c.cancel` is set inside `Start()` itself, the deferred rollback already has access to it.

**Step 2: Add comment documenting PricingSyncer**

At the PricingSyncer launch site:

```go
// PricingSyncer has no Stop() method; lifecycle is tied to ctx cancellation.
// Rollback relies on c.cancel() being called (see deferred rollback above).
```

**Step 3: Test the cancel-on-rollback path**

Add a unit test that starts components, triggers a Start failure, and asserts that the lifecycle context is cancelled.

**Step 4: Commit**

```bash
git add internal/daemon/components.go internal/daemon/components_test.go
git commit -m "fix(daemon): cancel lifecycle context on Start() rollback for PricingSyncer"
```

---

## Task 5: Verify no regressions

**Step 1: Build and vet**

```bash
go build ./...
go vet ./...
```

**Step 2: Test daemon and dependent packages**

```bash
go test ./internal/daemon/... -v -count=1
go test ./... -count=1
```

**Step 3: Smoke test**

Start the daemon, verify it boots clean, kill it cleanly, verify shutdown ordering is preserved (check logs for expected Stop sequence).

**Step 4: Commit final state**

```bash
git commit --allow-empty -m "chore(daemon): D15 rollback coverage complete"
```

---

## Out of Scope (Noted for Future Work)

- **WaitGroup tracking for background goroutines.** The 5 background goroutines (`SelfImproveSched`, `CalendarReminder`, `PricingSyncer`, `WebServer`, `TelegramBot`) launched via `go func() { ... }()` are not tracked by a WaitGroup. `Stop()` cannot confirm they have exited. This is orthogonal to D15 rollback; recommend a separate plan (`docs/plans/<date>-components-waitgroup.md`) if pursued.
- **Partial-init state recovery.** If a component's `Start()` partially initializes before failing (e.g., opens a port then fails to register), `Stop()` may hit uninitialized sub-fields. Nil-guarding at the component-pointer level is necessary but not sufficient. Separate concern.

---

## Verification

```bash
# After all tasks:
grep -n "startedHandlers = append" internal/daemon/components.go | wc -l
# Expected: 18+ (currently 6)

grep -n "case \"" internal/daemon/components.go | head -25
# Expected: 18+ switch cases in the rollback

go test ./internal/daemon/... -run TestComponentsStart_RollbackCoverage -v
# Expected: PASS
```
