# Architecture Debt Remediation Implementation Plan

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
| `lastClassificationErr` race | MEDIUM — error leaks between requests | Move to per-request result |
| RPC proxy timer leak | LOW — timer goroutine leak on busy server | `time.NewTimer` with `defer Stop()` |
| ChatService timer leak | LOW — same pattern as RPC proxy | `time.NewTimer` + `done` channel |
| Duplicate Flutter MetricsPanel | LOW — maintenance confusion | Consolidate to single widget |

---

### Task 1: Fix reflection goroutine context leak

**Files:**
- Modify: `internal/agent/orchestrator.go:659-661` (reflection goroutine)
- Modify: `internal/agent/loop.go:2134` (shadow training capture)
- Modify: `internal/agent/loop.go:1248` (learning pipeline trigger)

**Context:** Three goroutines use `context.Background()` instead of the orchestrator/loop lifecycle context. This means they continue running after daemon shutdown for up to 5 minutes, potentially writing files with no supervision.

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

- [ ] **Step 4: Verify build and tests**

Run: `go build ./internal/agent/... && go test ./internal/agent/... -count=1 -timeout 60s`
Expected: all pass

- [ ] **Step 5: Commit**

```bash
git add internal/agent/orchestrator.go internal/agent/loop.go
git commit -m "fix(agent): use lifecycle context instead of context.Background() for background goroutines"
```

---

### Task 2: Fix `applyFix` — add security checks before file writes

**Files:**
- Modify: `internal/agent/orchestrator.go:756-827` (applyFix method)
- Modify: `internal/agent/orchestrator.go:21-36` (Orchestrator struct)
- Modify: `internal/daemon/components.go` (wiring — pass FenceChecker)

**Context:** `applyFix()` writes LLM-generated content directly via `os.WriteFile` with no path validation. The `FileEditTool` has `fenceChecker.CheckPath(resolved, "write")` and `checker.CheckPath(resolved)` security checks. The simplest approach is to inject a `FenceChecker` into the orchestrator.

- [ ] **Step 1: Add FenceChecker to Orchestrator struct and deps**

In `orchestrator.go`, add to `OrchestratorDeps`:

```go
type OrchestratorDeps struct {
    // ... existing fields ...
    FenceChecker security.FenceChecker // path boundary enforcement
}
```

And to the `Orchestrator` struct:

```go
type Orchestrator struct {
    // ... existing fields ...
    fenceChecker security.FenceChecker
}
```

In `NewOrchestrator`, assign it:

```go
o.fenceChecker = deps.FenceChecker
```

- [ ] **Step 2: Add path validation to applyFix**

In the `applyFix` method, before each `os.WriteFile` call, add:

```go
resolved, err := filepath.Abs(fixPath)
if err != nil {
    // ...
    continue
}

if o.fenceChecker != nil {
    if err := o.fenceChecker.CheckPath(resolved, "write"); err != nil {
        o.logger.Warn("reflection fix blocked by path fence", "path", resolved, "error", err)
        continue
    }
}
```

This ensures LLM-generated fixes can only write within project boundaries.

- [ ] **Step 3: Wire FenceChecker in components.go**

In `internal/daemon/components.go`, find where `OrchestratorDeps` is constructed and add the fence checker:

```go
OrchestratorDeps: agent.OrchestratorDeps{
    // ... existing fields ...
    FenceChecker: fenceChecker, // from the security engine setup
}
```

Search for where `fenceChecker` is created in components.go (it's created for the file tools). Reuse the same instance.

- [ ] **Step 4: Verify build and tests**

Run: `go build ./internal/agent/... ./internal/daemon/... && go test ./internal/agent/... -count=1 -timeout 60s`
Expected: all pass

- [ ] **Step 5: Commit**

```bash
git add internal/agent/orchestrator.go internal/daemon/components.go
git commit -m "fix(agent): add path fencing to reflection applyFix (prevent writes outside project)"
```

---

### Task 3: Fix `lastClassificationErr` race condition

**Files:**
- Modify: `internal/agent/dispatcher.go:184` (field declaration)
- Modify: `internal/agent/dispatcher.go:481-486` (read site)
- Modify: `internal/agent/dispatcher.go:499` (reset site)
- Modify: `internal/agent/dispatcher.go:575` (write site)
- Modify: `internal/agent/dispatcher.go:1886` (public accessor)
- Modify: `internal/agent/dispatcher.go` (DispatchResult type)

**Context:** `lastClassificationErr` is shared mutable state on the `Dispatcher` with no synchronization. It should be per-request.

- [ ] **Step 1: Add classification notice to DispatchResult**

Find the `DispatchResult` struct and add:

```go
type DispatchResult struct {
    // ... existing fields ...
    ClassificationNotice string `json:"classification_notice,omitempty"`
}
```

- [ ] **Step 2: Remove `lastClassificationErr` from Dispatcher struct**

Delete the field at line 184.

- [ ] **Step 3: Update `ClassifyAndRoute` to use local error tracking**

In `ClassifyAndRoute`, instead of reading `d.lastClassificationErr`, capture the error from `classifyIntent` locally:

The `classifyIntent` method should be modified to return the error alongside the intent. Change its signature to return `(intent, llmErr)` where `llmErr` is the classification error (if any). Then in `ClassifyAndRoute`:

```go
intent, classifyErr := d.classifyIntent(ctx, message)
if classifyErr != nil {
    result.ClassificationNotice = fmt.Sprintf("classification fallback: %v", classifyErr)
}
```

Remove the `d.lastClassificationErr = nil` reset at line 499 and the `d.lastClassificationErr` write at line 575.

- [ ] **Step 4: Update or remove `LastClassificationError()` method**

At line 1886, remove or deprecate the `LastClassificationError()` method since the error is now per-request. If it's called from anywhere (grep for callers), update those callers to use `DispatchResult.ClassificationNotice` instead.

- [ ] **Step 5: Verify build and tests**

Run: `go build ./internal/agent/... && go test ./internal/agent/... -count=1 -timeout 60s`
Expected: all pass

- [ ] **Step 6: Commit**

```bash
git add internal/agent/dispatcher.go
git commit -m "fix(agent): move lastClassificationErr to per-request DispatchResult (eliminate race)"
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

- [ ] **Step 4: Commit**

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

- [ ] **Step 4: Commit**

```bash
git add internal/services/chat_service.go
git commit -m "fix(services): prevent timer and goroutine leaks in ChatService.Chat"
```

---

### Task 6: Consolidate duplicate Flutter MetricsPanel widgets

**Files:**
- Modify: `ui/flutter_ui/lib/features/metrics/metrics_panel.dart` (replace with sidebar version)
- Delete: `ui/flutter_ui/lib/features/sidebar/metrics_panel.dart`
- Modify: `ui/flutter_ui/lib/features/drawer/panels/metrics_panel.dart` (update import)
- Modify: `ui/flutter_ui/lib/features/drawer/drawer_overlay.dart` (update import)
- Modify: `ui/flutter_ui/lib/features/chat/chat_tab.dart` (verify import — should already be correct)

**Context:** Three `MetricsPanel` classes exist with different implementations. The sidebar version is the best — uses typed `MetricsSnapshot` via Riverpod `metricsProvider`, has a GridView layout, and is a simpler `ConsumerWidget`. The other two use raw `Map<String,dynamic>` with manual `Timer.periodic` polling.

**Strategy:** Keep the sidebar version's approach. Move it to `features/metrics/metrics_panel.dart` (replacing the simpler version). Add a `compact` parameter. Delete the other two.

- [ ] **Step 1: Read all three MetricsPanel implementations**

Read:
- `ui/flutter_ui/lib/features/metrics/metrics_panel.dart`
- `ui/flutter_ui/lib/features/sidebar/metrics_panel.dart`
- `ui/flutter_ui/lib/features/drawer/panels/metrics_panel.dart`

And their consumers:
- `ui/flutter_ui/lib/features/chat/chat_tab.dart` (find MetricsPanel import)
- `ui/flutter_ui/lib/features/drawer/drawer_overlay.dart` (find MetricsPanel import)

- [ ] **Step 2: Create unified MetricsPanel**

Replace `ui/flutter_ui/lib/features/metrics/metrics_panel.dart` with the sidebar version, adding a `compact` parameter:

```dart
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../providers/metrics_provider.dart';
import '../../models/api_models.dart';

class MetricsPanel extends ConsumerWidget {
  final bool compact;

  const MetricsPanel({super.key, this.compact = false});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    // Use the sidebar version's implementation
    // If compact, use a simplified row layout
    // If not compact, use the full GridView layout
    // ...
  }
}
```

Use the sidebar version's `metricsProvider` and `MetricsSnapshot` pattern. For `compact: true`, render a simplified version suitable for the drawer. For `compact: false`, render the full GridView.

- [ ] **Step 3: Delete sidebar version**

```bash
rm ui/flutter_ui/lib/features/sidebar/metrics_panel.dart
```

- [ ] **Step 4: Update drawer to use unified version with compact mode**

In `ui/flutter_ui/lib/features/drawer/drawer_overlay.dart`, change the import to:
```dart
import '../metrics/metrics_panel.dart';
```

And update the usage to:
```dart
MetricsPanel(compact: true),
```

Delete `ui/flutter_ui/lib/features/drawer/panels/metrics_panel.dart` if it's no longer needed.

- [ ] **Step 5: Verify chat_tab.dart still works**

The `chat_tab.dart` should already import `features/metrics/metrics_panel.dart`. Verify it uses the non-compact mode (or the default).

- [ ] **Step 6: Verify Flutter analyze**

Run: `cd ui/flutter_ui && flutter analyze`
Expected: no errors

- [ ] **Step 7: Commit**

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

- [ ] **Step 3: Run Flutter analyze**

Run: `cd ui/flutter_ui && flutter analyze`
Expected: no errors

- [ ] **Step 4: Run race detector on affected packages**

Run: `go test -race ./internal/agent/... ./internal/rpc/... ./internal/services/... -timeout 120s`
Expected: no race conditions detected
