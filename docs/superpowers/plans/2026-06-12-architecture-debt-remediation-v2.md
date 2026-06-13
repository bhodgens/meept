# Architecture Debt Remediation Implementation Plan (v2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix 6 architecture debt items: context leaks, unsynchronized state, timer leaks, security bypass in reflection, and duplicate Flutter widgets.

**Architecture:** Each fix is independent. Tasks 1-5 fix Go backend issues; Task 6 consolidates duplicate Flutter widgets.

**Tech Stack:** Go 1.22+, Dart/Flutter

---

## Investigation Results

| Issue | Risk | Fix Approach |
|-------|------|-------------|
| Reflection goroutine escapes shutdown | HIGH — unsupervised file writes after shutdown | Use lifecycle context + WaitGroup |
| `applyFix` bypasses security | HIGH — arbitrary file writes from LLM output | Inject FenceChecker into orchestrator |
| `lastClassificationErr` race | MEDIUM — error leaks between requests | Move to per-request local variable |
| RPC proxy timer leak | LOW — timer goroutine leak on busy server | `time.NewTimer` with `defer Stop()` |
| ChatService timer leak | LOW — same pattern as RPC proxy | `time.NewTimer` + `done` channel |
| Duplicate Flutter MetricsPanel | LOW — maintenance confusion | Consolidate to single widget with compact mode |

---

### Task 1: Fix reflection goroutine context leak

**Files:**
- Modify: `internal/agent/orchestrator.go:659-661` (reflection goroutine)
- Modify: `internal/agent/loop.go:1248` (learning pipeline trigger)
- Modify: `internal/agent/loop.go:2134` (shadow training capture)
- Modify: `internal/agent/loop.go:2534` (recordTaskExecution - ADDED in v2)

**Context:** Four goroutines use `context.Background()` instead of the orchestrator/loop lifecycle context. This means they continue running after daemon shutdown for up to 5 minutes, potentially writing files with no supervision.

- [ ] **Step 1: Fix the reflection goroutine in orchestrator.go**

At approximately line 659-661, change from:

```go
go func() {
    reflectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()
    result, err := o.reflectionEngine.RunReflection(reflectCtx, event.EditedFiles)
```

To:

```go
o.wg.Add(1)
go func() {
    defer o.wg.Done()
    reflectCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
    defer cancel()
    result, err := o.reflectionEngine.RunReflection(reflectCtx, event.EditedFiles)
```

This uses `ctx` (the orchestrator's lifecycle context, cancelled by `o.cancel()` in `Stop()`) and registers with the WaitGroup so `Stop()` waits for completion.

- [ ] **Step 2: Fix shadow training capture in loop.go**

At approximately line 2134, change from:

```go
go l.shadowMgr.CaptureInteraction(context.Background(), ...)
```

To:

```go
go l.shadowMgr.CaptureInteraction(ctx, ...)
```

Where `ctx` is the loop's context from the calling function (verify the variable name at that scope — it may be `loopCtx` or `ctx`).

- [ ] **Step 3: Fix learning pipeline trigger in loop.go**

At approximately line 1248, change from:

```go
go l.triggerLearning(context.Background(), ...)
```

To:

```go
go l.triggerLearning(ctx, ...)
```

Again, verify the context variable name at that scope.

- [ ] **Step 4: Fix recordTaskExecution in loop.go (ADDED in v2)**

At approximately line 2534, change from:

```go
go l.recordTaskExecution(context.Background(), t, response)
```

To:

```go
go l.recordTaskExecution(ctx, t, response)
```

- [ ] **Step 5: Verify build and tests**

Run: `go build ./internal/agent/... && go test ./internal/agent/... -count=1 -timeout 60s`
Expected: all pass

- [ ] **Step 6: Add regression test (NEW in v2)**

Add test `TestOrchestrator_ReflectionGoroutine_StopsOnShutdown` in `internal/agent/orchestrator_test.go`:
- Start orchestrator, trigger reflection goroutine
- Mock reflection engine to block
- Call `Stop()` and verify goroutine exits within 100ms (not 5 minutes)

- [ ] **Step 7: Commit**

```bash
git add internal/agent/orchestrator.go internal/agent/loop.go
git commit -m "fix(agent): use lifecycle context for all background goroutines (reflection, shadow, learning, recordTask)"
```

---

### Task 2: Fix `applyFix` — add security checks before file writes

**Files:**
- Modify: `internal/agent/orchestrator.go:21-36` (Orchestrator struct)
- Modify: `internal/agent/orchestrator.go:756-827` (applyFix method)
- Modify: `internal/daemon/components.go:1410` (wiring — pass FenceChecker)

**Context:** `applyFix()` writes LLM-generated content directly via `os.WriteFile` with no path validation. The `FileEditTool` has `fenceChecker.CheckPath(resolved, "write")` security checks. The fix injects `FenceChecker` into the orchestrator.

- [ ] **Step 1: Add FenceChecker to OrchestratorDeps (CORRECTED in v2)**

In `orchestrator.go`, add to `OrchestratorDeps`:

```go
import intsecurity "github.com/caimlas/meept/internal/security"

type OrchestratorDeps struct {
    // ... existing fields ...
    FenceChecker *intsecurity.FenceChecker // path boundary enforcement (struct pointer, not interface)
}
```

And to the `Orchestrator` struct:

```go
type Orchestrator struct {
    // ... existing fields ...
    fenceChecker *intsecurity.FenceChecker
}
```

In `NewOrchestrator`, assign it:

```go
o.fenceChecker = deps.FenceChecker
```

- [ ] **Step 2: Add path validation to applyFix (CORRECTED in v2)**

In the `applyFix` method, find the existing path resolution (around line 767-775) and add the fence check **after** it:

```go
// Existing path resolution (line 767-775):
resolved, err := filepath.Abs(fixPath)
if err != nil {
    o.logger.Warn("failed to resolve path", "path", fixPath, "error", err)
    continue
}

// NEW: Add fence check after resolution, before os.WriteFile:
if o.fenceChecker != nil {
    if err := o.fenceChecker.CheckPath(resolved, "write"); err != nil {
        o.logger.Warn("reflection fix blocked by path fence", "path", resolved, "error", err)
        continue
    }
}

// Then existing os.WriteFile call continues...
```

**IMPORTANT:** The `if o.fenceChecker != nil` guard is required to prevent typed-nil panic (per CLAUDE.md coding conventions).

- [ ] **Step 3: Wire FenceChecker in components.go (CORRECTED in v2)**

In `internal/daemon/components.go`, find where `OrchestratorDeps` is constructed (around line 1410) and add:

```go
OrchestratorDeps: agent.OrchestratorDeps{
    // ... existing fields ...
    FenceChecker: c.FenceChecker, // c.FenceChecker is created at line 279, available here
}
```

- [ ] **Step 4: Verify build and tests**

Run: `go build ./internal/agent/... ./internal/daemon/... && go test ./internal/agent/... -count=1 -timeout 60s`
Expected: all pass

- [ ] **Step 5: Add regression tests (NEW in v2)**

Add tests in `internal/agent/orchestrator_test.go`:

1. `TestApplyFix_BlocksOutsideRootWrite` — Verify path outside root is blocked
2. `TestApplyFix_AllowsInsideRootWrite` — Verify path inside root succeeds
3. `TestApplyFix_NilFenceChecker_NoPanic` — Verify nil FenceChecker doesn't panic (typed-nil guard)

- [ ] **Step 6: Commit**

```bash
git add internal/agent/orchestrator.go internal/daemon/components.go
git commit -m "fix(agent): add path fencing to reflection applyFix (prevent writes outside project)"
```

---

### Task 3: Fix `lastClassificationErr` race condition

**Files:**
- Modify: `internal/agent/dispatcher.go:184` (field declaration — REMOVE)
- Modify: `internal/agent/dispatcher.go:481-486` (read site — SIMPLIFY)
- Modify: `internal/agent/dispatcher.go:499` (reset site — REMOVE)
- Modify: `internal/agent/dispatcher.go:575` (write site — REMOVE)
- Modify: `internal/agent/dispatcher.go:1886` (public accessor — REMOVE)
- Modify: `internal/agent/handler.go:534` (caller — MIGRATE)

**Context:** `lastClassificationErr` is shared mutable state on the `Dispatcher` with no synchronization. It should be per-request. **CORRECTED in v2:** `ClassificationNotice` already exists on `DispatchResult`, so we just need to populate it correctly and migrate the handler.

- [ ] **Step 1: Remove `lastClassificationErr` field from Dispatcher struct**

Delete the field at line 184.

- [ ] **Step 2: Simplify ClassifyAndRoute to use local error tracking (SIMPLIFIED in v2)**

In `ClassifyAndRoute`, capture the classification error locally:

```go
var classificationErr error

// Inside the LLM classifier path (where classifyIntent is called):
intent, err := d.classifier.IntentFromMessage(ctx, message)
if err != nil {
    classificationErr = err
    // ... existing fallback logic ...
}

// When setting the notice (replace line 481-486):
if classificationErr != nil {
    result.ClassificationNotice = llm.ClassificationUserGuidance(classificationErr)
}
```

Remove the `d.lastClassificationErr = nil` reset at line 499 and the `d.lastClassificationErr` write at line 575.

- [ ] **Step 3: Remove LastClassificationError() method**

Delete the method at line 1886 entirely.

- [ ] **Step 4: Migrate handler.go caller (CORRECTED in v2)**

In `internal/agent/handler.go`, find the caller at line 534 and change from:

```go
if classErr := h.dispatcher.LastClassificationError(); classErr != nil {
    guidance := llm.ClassificationUserGuidance(classErr)
    // ...
}
```

To:

```go
// Use result.ClassificationNotice which is now populated in ClassifyAndRoute
// This check should happen in the success path (after err == nil check)
if result.ClassificationNotice != "" {
    // Append guidance to response
}
```

**IMPORTANT:** The handler currently only calls `LastClassificationError()` in the `dispatchErr != nil` path. The migration should check `result.ClassificationNotice` in the **success path** (line 624-629 already does this — ensure it covers both cases).

- [ ] **Step 5: Verify build and tests**

Run: `go build ./internal/agent/... && go test ./internal/agent/... -count=1 -timeout 60s`
Expected: all pass

- [ ] **Step 6: Add regression test (NEW in v2)**

Add test `TestDispatcher_ConcurrentClassifyNoRace` in `internal/agent/dispatcher_test.go`:
- Two goroutines calling `ClassifyAndRoute()` concurrently
- Verify no race conditions with `-race` detector
- Verify each result has correct `ClassificationNotice`

- [ ] **Step 7: Commit**

```bash
git add internal/agent/dispatcher.go internal/agent/handler.go
git commit -m "fix(agent): eliminate lastClassificationErr race with per-request local variable"
```

---

### Task 4: Fix RPC proxy timer leak

**Files:**
- Modify: `internal/rpc/proxy.go:216-227`

**Context:** `time.After(timeout)` creates a timer that is never stopped. If the response or context cancellation fires first, the timer goroutine leaks until timeout.

- [ ] **Step 1: Read the current select statement in `makeProxy`**

Read `internal/rpc/proxy.go` around lines 210-230 to see the full select.

- [ ] **Step 2: Replace `time.After` with `time.NewTimer`**

Change from:

```go
select {
case resp := <-respChan:
    // ...
case <-time.After(timeout):
    return nil, fmt.Errorf("timeout waiting for response on %s", responseTopic)
case <-ctx.Done():
    return nil, ctx.Err()
}
```

To:

```go
timer := time.NewTimer(timeout)
defer timer.Stop()

select {
case resp := <-respChan:
    var result any
    if err := json.Unmarshal(resp.Payload, &result); err != nil {
        return resp.Payload, nil
    }
    return result, nil
case <-timer.C:
    return nil, fmt.Errorf("timeout waiting for response on %s", responseTopic)
case <-ctx.Done():
    return nil, ctx.Err()
}
```

- [ ] **Step 3: Verify build and tests**

Run: `go build ./internal/rpc/... && go test ./internal/rpc/... -count=1 -timeout 60s`
Expected: all pass

- [ ] **Step 4: Add regression test (NEW in v2)**

Add test `TestMakeProxy_TimesOut_CleanUpTimer` in `internal/rpc/protocol_test.go`:
- Call `makeProxy()` with no responder
- Trigger timeout path
- Verify timer is stopped (run twice to check for goroutine buildup)

- [ ] **Step 5: Commit**

```bash
git add internal/rpc/proxy.go
git commit -m "fix(rpc): replace time.After with time.NewTimer to prevent timer goroutine leaks"
```

---

### Task 5: Fix ChatService timer leak and add done channel

**Files:**
- Modify: `internal/services/chat_service.go:121-164`

**Context:** Same `time.After` leak pattern as RPC proxy. The spawned goroutine also has no explicit cancellation when the function returns via the timer path.

- [ ] **Step 1: Read the current Chat method**

Read `internal/services/chat_service.go` lines 100-170 to understand the full method structure.

- [ ] **Step 2: Add done channel and replace time.After**

Change the goroutine and select block to:

```go
done := make(chan struct{})
defer close(done)

go func() {
    for {
        select {
        case resp, ok := <-sub.Channel:
            if !ok {
                return
            }
            if resp.ReplyTo == msgID {
                select {
                case respChan <- resp:
                default:
                }
                return
            }
        case <-ctx.Done():
            return
        case <-done:
            return
        }
    }
}()

timer := time.NewTimer(2 * time.Minute)
defer timer.Stop()

select {
case resp := <-respChan:
    // ... existing unmarshal logic ...
case <-timer.C:
    return nil, wrapError("chat", "Chat", ErrTimeout)
case <-ctx.Done():
    return nil, ctx.Err()
}
```

The `done` channel ensures the goroutine exits immediately when the function returns via any path. The `time.NewTimer` with `defer timer.Stop()` prevents timer leaks.

- [ ] **Step 3: Verify build and tests**

Run: `go build ./internal/services/... && go test ./internal/services/... -count=1 -timeout 60s`
Expected: all pass

- [ ] **Step 4: Add regression test (NEW in v2)**

Add test `TestChatService_Chat_TimesOut_GoroutineCleanExit` in `internal/services/chat_service_test.go`:
- Publish request with no responder
- Trigger timeout path
- Verify goroutine exits cleanly (signal via channel when goroutine returns)

- [ ] **Step 5: Commit**

```bash
git add internal/services/chat_service.go
git commit -m "fix(services): prevent timer and goroutine leaks in ChatService.Chat"
```

---

### Task 6: Consolidate duplicate Flutter MetricsPanel widgets

**Files:**
- Replace: `ui/flutter_ui/lib/features/metrics/metrics_panel.dart` (already has Riverpod provider + compact param, just needs compact logic wired)
- Delete: `ui/flutter_ui/lib/features/sidebar/metrics_panel.dart` (RAW POLLING VERSION — NOT the target)
- Delete: `ui/flutter_ui/lib/features/drawer/panels/metrics_panel.dart`
- Modify: `ui/flutter_ui/lib/features/drawer/drawer_overlay.dart` (update import)
- Verify: `ui/flutter_ui/lib/features/chat/chat_tab.dart` (already uses features/metrics/metrics_panel.dart)

**Context:** Three `MetricsPanel` classes exist with different implementations. **CORRECTED in v2:** The `features/metrics/metrics_panel.dart` version already uses `MetricsSnapshot` via Riverpod `metricsProvider` and has a `compact` parameter — but the `compact` parameter is never used (dead code). The sidebar and drawer versions both use raw `Map<String,dynamic>` with manual `Timer.periodic` polling.

**Strategy:** Wire up the `compact` parameter in the existing `features/metrics/metrics_panel.dart`. Delete the sidebar and drawer panel versions. Update imports.

- [ ] **Step 1: Read all three MetricsPanel implementations**

Read:
- `ui/flutter_ui/lib/features/metrics/metrics_panel.dart` (KEEP — already uses Riverpod, has compact param)
- `ui/flutter_ui/lib/features/sidebar/metrics_panel.dart` (DELETE — raw polling)
- `ui/flutter_ui/lib/features/drawer/panels/metrics_panel.dart` (DELETE — raw polling)

And their consumers:
- `ui/flutter_ui/lib/features/drawer/drawer_overlay.dart` (imports `panels/metrics_panel.dart`)
- `ui/flutter_ui/lib/features/chat/chat_tab.dart` (already imports `features/metrics/metrics_panel.dart`)

- [ ] **Step 2: Wire up compact mode in unified MetricsPanel (CORRECTED in v2)**

In `ui/flutter_ui/lib/features/metrics/metrics_panel.dart`, modify the `build` method to actually use the `compact` parameter:

```dart
@override
Widget build(BuildContext context, WidgetRef ref) {
  final metrics = ref.watch(metricsProvider);

  return metrics.when(
    data: (snapshot) {
      if (compact) {
        // Horizontal row layout for drawer (show 3-4 key metrics)
        return _buildCompactLayout(snapshot);
      } else {
        // Full GridView layout for chat tab
        return _buildFullGridView(snapshot);
      }
    },
    loading: () => CircularProgressIndicator(),
    error: (error, _) => Text('Error: $error'),
  );
}

Widget _buildCompactLayout(MetricsSnapshot snapshot) {
  return Row(
    children: [
      _CompactMetricTile(label: 'queue', value: snapshot.queueDepth.toString()),
      _CompactMetricTile(label: 'agents', value: snapshot.activeAgents.toString()),
      _CompactMetricTile(label: 'jobs', value: snapshot.runningJobs.toString()),
    ],
  );
}
```

- [ ] **Step 3: Delete sidebar version**

```bash
rm ui/flutter_ui/lib/features/sidebar/metrics_panel.dart
```

- [ ] **Step 4: Delete drawer panels version and update import**

```bash
rm ui/flutter_ui/lib/features/drawer/panels/metrics_panel.dart
```

In `ui/flutter_ui/lib/features/drawer/drawer_overlay.dart`, change the import from:
```dart
import 'panels/metrics_panel.dart';
```
To:
```dart
import '../metrics/metrics_panel.dart';
```

And update the usage to:
```dart
MetricsPanel(compact: true),
```

- [ ] **Step 5: Verify chat_tab.dart still works**

The `chat_tab.dart` should already import `features/metrics/metrics_panel.dart`. Verify it uses the non-compact mode (default, `compact: false`).

- [ ] **Step 6: Add widget tests (NEW in v2)**

Add tests in `ui/flutter_ui/test/features/metrics/metrics_panel_test.dart`:

1. `MetricsPanel compact=false` — Verify all six metric tiles render with GridView
2. `MetricsPanel compact=true` — Verify simplified row layout with 3-4 metrics

- [ ] **Step 7: Verify Flutter analyze**

Run: `cd ui/flutter_ui && flutter analyze`
Expected: no errors

- [ ] **Step 8: Run Flutter tests**

Run: `cd ui/flutter_ui && flutter test`
Expected: all tests pass, including new MetricsPanel tests

- [ ] **Step 9: Commit**

```bash
git add ui/flutter_ui/lib/features/metrics/metrics_panel.dart \
        ui/flutter_ui/lib/features/drawer/drawer_overlay.dart
git add -u ui/flutter_ui/lib/features/sidebar/metrics_panel.dart \
         ui/flutter_ui/lib/features/drawer/panels/metrics_panel.dart
git commit -m "refactor(flutter): consolidate three MetricsPanel widgets into one with compact mode"
```

---

### Task 7: Final verification

- [ ] **Step 1: Run full Go build**

Run: `go build ./...`
Expected: success

- [ ] **Step 2: Run full Go test suite**

Run: `go test ./... -count=1 -timeout 120s`
Expected: all pass

- [ ] **Step 3: Run race detector on affected packages (EXTENDED in v2)**

Run: `go test -race ./internal/agent/... ./internal/rpc/... ./internal/services/... ./internal/daemon/... -timeout 120s`
Expected: no race conditions detected

**Note:** Added `./internal/daemon/...` to catch any typed-nil interface issues with FenceChecker wiring.

- [ ] **Step 4: Run Flutter analyze**

Run: `cd ui/flutter_ui && flutter analyze`
Expected: no errors

- [ ] **Step 5: Run Flutter tests**

Run: `cd ui/flutter_ui && flutter test`
Expected: all pass

- [ ] **Step 6: Run go vet**

Run: `go vet ./...`
Expected: no issues

---

## Parallelization Guide (NEW in v2)

Tasks can be parallelized as follows:

```
Batch A (fully independent — can run in parallel):
├─ Task 4: RPC proxy timer leak (internal/rpc/proxy.go)
├─ Task 5: ChatService timer leak (internal/services/chat_service.go)
└─ Task 6: Flutter MetricsPanel (Dart files only)

Batch B (depends on Batch A completing):
├─ Task 1: Reflection goroutine context leak
├─ Task 2: applyFix security (requires orchestrator.go changes)
└─ Task 3: Classification error race (dispatcher.go)

Batch C (final verification):
└─ Task 7: Full verification suite
```

**Recommendation:** Execute Batch A tasks in parallel first (they touch disjoint files). Then execute Batch B tasks sequentially (they all modify `internal/agent/`). Finally, run Batch C.

---

## Acceptance Criteria Per Task (NEW in v2)

| Task | Acceptance Criteria |
|------|---------------------|
| Task 1 | `go test -race` passes; reflection goroutine exits within 100ms of `Stop()` call |
| Task 2 | Writes outside project root are blocked; nil FenceChecker doesn't panic |
| Task 3 | Concurrent `ClassifyAndRoute` calls show no race; handler uses `ClassificationNotice` |
| Task 4 | Timer goroutine count stable after rapid requests |
| Task 5 | Goroutine exits cleanly on timeout; timer doesn't leak |
| Task 6 | Widget tests pass; both compact modes render correctly |

---

## Changelog (v2 improvements)

| Issue | v1 Problem | v2 Fix |
|-------|------------|--------|
| Task 1 | Missing `recordTaskExecution` context leak | Added Step 4 for line 2534 |
| Task 2 | FenceChecker defined as interface | Changed to `*security.FenceChecker` struct pointer |
| Task 2 | Wiring unspecified | Added explicit `components.go:1410` wiring |
| Task 2 | Missing nil guard | Added `if o.fenceChecker != nil` guard |
| Task 3 | `ClassificationNotice` field said to add | Noted it already exists, simplified approach |
| Task 3 | Handler migration unspecified | Added explicit handler.go:534 migration |
| Task 6 | Wrong file identified as "best" | Corrected: `features/metrics/` already has Riverpod |
| Task 6 | Compact parameter dead code | Added actual compact mode implementation |
| Testing | No regression tests | Added regression test requirements per task |
| Structure | No parallelization guidance | Added Batch A/B/C parallelization guide |
| Structure | No acceptance criteria | Added acceptance criteria table |
