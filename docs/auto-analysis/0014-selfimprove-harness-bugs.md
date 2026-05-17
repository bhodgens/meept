# Bug 0005 (Issue 0014-B1): Self-improve commands fail with "not enabled" -- no discoverable guidance

- **Date**: 2026-05-15
- **Phase**: Phase 9 (Self-Improvement System)
- **Severity**: Medium
- **Component**: selfimprove (CLI + daemon wiring)
- **Status**: FIXED

## Description

All self-improve CLI subcommands (`status`, `detect`, `analyze`) fail immediately with:

```
Error: status query failed: [-32603] self-improve not enabled
```

The `selfimprove` config section defaults to `"enabled": false` in both `config/meept.json5` and the runtime defaults in `internal/config/schema.go`. The RPC handlers are always registered (even when disabled), but the `SelfImproveHandler.controller` is `nil` when the config is disabled, causing every method to return the generic "self-improve not enabled" error.

The error message provides no guidance on how to enable the feature (i.e., set `selfimprove.enabled = true` in `~/.meept/meept.json5`).

## Reproduction

1. Ensure daemon is running with default config (`selfimprove.enabled = false`).
2. Run any self-improve command:
   ```
   ./bin/meept selfimprove status
   ./bin/meept selfimprove detect
   ./bin/meept selfimprove analyze
   ```
3. All return: `Error: ... failed: [-32603] self-improve not enabled`

## Evidence

- `~/.meept/meept.json5` line: `"enabled": false` in `selfimprove` section.
- `internal/config/schema.go` line 1209: `SelfImprove: SelfImproveConfig{Enabled: false, ...}`.
- `internal/rpc/selfimprove.go` line 40-42: `ctrl()` returns `fmt.Errorf("self-improve not enabled")` when `h.controller` is nil.
- `internal/daemon/daemon.go` line 183: `rpc.NewSelfImproveHandler(components.SelfImproveCtrl)` passes nil when disabled.
- Daemon logs confirm handlers registered: `rpc: registered handler method=selfimprove.status` (etc.) but no "Self-improve RPC handlers registered" INFO line (because `components.SelfImproveCtrl` is nil).

## Root Cause

Design: self-improve is disabled by default, which is a reasonable safety choice. However, the error path from the RPC handler is uninformative -- it only says "self-improve not enabled" without telling the user what config key to change.

## Fix Applied

Updated `ctrl()` in `internal/rpc/selfimprove.go` to return:
```
self-improve not enabled: set selfimprove.enabled = true in ~/.meept/meept.json5 and restart the daemon
```

## Classification

**FIXED** -- error message now includes actionable guidance on how to enable the feature.

---

# Bug 0006 (Issue 0014-B2): Self-improve handleAnalyze does not run actual analysis

- **Date**: 2026-05-15
- **Phase**: Phase 9 (Self-Improvement System)
- **Severity**: Medium
- **Component**: selfimprove (RPC handler)
- **Status**: FIXED

## Description

The RPC handler `handleAnalyze` in `internal/rpc/selfimprove.go` re-runs detection but never invokes the actual root-cause analysis. It calls `ctrl.Detect(ctx)` and then returns `ctrl.GetStatus()`, which returns only the controller's cached status (issues count, etc.) -- not analysis results.

## Reproduction

1. Enable self-improve in config.
2. Run `./bin/meept selfimprove analyze`.
3. Observe: the response contains `issues` (from detection) and `analyses` (which is the controller status object, not analysis results).

## Evidence

`internal/rpc/selfimprove.go` lines 63-84:

```go
func (h *SelfImproveHandler) handleAnalyze(ctx context.Context, params json.RawMessage) (any, error) {
    // ...
    issues, err := ctrl.Detect(ctx)          // re-detects
    // ...
    status := ctrl.GetStatus()               // returns status, NOT analysis
    return map[string]any{
        "issues":       issues,
        "analyses":     status,               // BUG: status is not analysis results
        "issues_count": len(issues),
    }, nil
}
```

The comment at line 69-71 acknowledges this is a "lightweight wrapper" but it does not actually invoke `c.analyzer.Analyze()` or `c.analyzer.AnalyzeBatch()`. The `Controller` has no standalone `Analyze()` method -- analysis only happens inside `RunFullCycle`.

## Root Cause

The `Controller` type exposes `Detect()` as a standalone method but does not expose `Analyze()` as a standalone phase. The RPC handler was written as a placeholder that re-runs detection and returns status, rather than running the analysis phase.

## Fix Applied

Added `Analyze(ctx context.Context, issues []Issue) ([]*RootCauseAnalysis, error)` method to `Controller` in `internal/selfimprove/controller.go`. The method clones the analysis loop from `RunFullCycle` (phases 2) but is callable standalone. Updated `handleAnalyze` in `internal/rpc/selfimprove.go` to call `ctrl.Analyze(ctx, issues)` and return actual analysis results.

## Classification

**FIXED** -- `Controller.Analyze()` added as standalone entry point; RPC handler now invokes real root-cause analysis.

---

# Bug 0007 (Issue 0014-B3): Self-improve safety config partially mapped in daemon wiring

- **Date**: 2026-05-15
- **Phase**: Phase 9 (Self-Improvement System)
- **Severity**: Low
- **Component**: selfimprove (daemon components)
- **Status**: FIXED

## Description

When the daemon creates the self-improve controller at `internal/daemon/components.go:364-369`, only one safety field is mapped from the config-level `SafetyConfig` to the runtime `selfimprove.SafetyConfig`:

```go
siCfg.Safety.RequireHumanApproval = cfg.SelfImprove.Safety.RequireHumanApproval
```

The runtime `selfimprove.SafetyConfig` has additional fields that are never populated from the config:
- `AutoApplyLowRisk`
- `MaxConsecutiveFailures`
- `MaxFailuresPerIssue`
- `ProtectedPatterns`
- `RequireTestsPass`
- `RequireBuildSuccess`

Meanwhile, the config-level `SafetyConfig` (in `internal/config/schema.go`) has:
- `MaxFilesPerFix`, `MaxLinesChangedPerFix`, `BlockedPaths`, `AllowedRiskLevels`, `BlockCriticalRisk`, `RequireTestsPass`, `MinConfidenceThreshold`

There is also a structural mismatch: the two `SafetyConfig` types live in different packages (`internal/config` vs `internal/selfimprove`) with different field sets. The daemon wiring only copies `RequireHumanApproval`, leaving all other safety settings at their `DefaultConfig()` values.

## Reproduction

1. Set `require_tests_pass: true` or `block_critical_risk: true` in `~/.meept/meept.json5`.
2. Enable self-improve and run a full cycle.
3. Observe: tests are not required during validation because the runtime config never received the setting.

## Evidence

- `internal/daemon/components.go` line 369: only `RequireHumanApproval` is mapped.
- `internal/selfimprove/config.go` lines 71-86: runtime `SafetyConfig` has fields like `AutoApplyLowRisk`, `MaxConsecutiveFailures`, `RequireTestsPass`, `RequireBuildSuccess`, `ProtectedPatterns`.
- `internal/config/schema.go` lines 786-795: config-level `SafetyConfig` has different fields like `MaxFilesPerFix`, `MaxLinesChangedPerFix`, `BlockedPaths`, `AllowedRiskLevels`.

## Root Cause

Two separate type definitions for `SafetyConfig` evolved independently. The daemon wiring was written with minimal field mapping and never expanded to cover the full config surface.

## Fix Applied

Expanded the field-mapping block in `internal/daemon/components.go` (lines 374-379) to map:
- `RequireTestsPass` from config to runtime config
- `RequireBuildSuccess` set to `true` (sandbox validation enforces this by default)
- `ProtectedPatterns` mapped from config's `BlockedPaths` field
- `MaxConsecutiveFailures` = 5 (defensive circuit breaker default)
- `MaxFailuresPerIssue` = 3 (defensive per-issue cap)

## Classification

**FIXED** -- config-to-runtime wiring now covers all relevant safety fields. Remaining config fields (e.g., `BlockCriticalRisk`, `MinConfidenceThreshold`) are not mapped as they require architectural changes to integrate with the runtime safety engine.

---

# Bug 0008 (Issue 0014-B4): Self-improve handleGenerate and handleValidate are status stubs

- **Date**: 2026-05-15
- **Phase**: Phase 9 (Self-Improvement System)
- **Severity**: Medium
- **Component**: selfimprove (RPC handlers)
- **Status**: FIXED

## Description

The RPC handlers for `handleGenerate` and `handleValidate` in `internal/rpc/selfimprove.go` do not actually run generation or validation. They both return `ctrl.GetStatus()`, which is the controller's cached state -- not the result of running the corresponding phase.

## Reproduction

1. Enable self-improve and run detection + analysis to populate issues.
2. Call `selfimprove.generate` or `selfimprove.validate` via RPC.
3. Observe: response is the current controller status, not generated fixes or validation results.

## Evidence

`internal/rpc/selfimprove.go` lines 87-98 (generate):
```go
func (h *SelfImproveHandler) handleGenerate(ctx context.Context, params json.RawMessage) (any, error) {
    // ...
    status := ctrl.GetStatus()
    return map[string]any{
        RPCKeyStatus:  status,
        "fixes_count": status.FixesCount,
        "pending":     status.PendingApprovals,
    }, nil
}
```

`internal/rpc/selfimprove.go` lines 101-111 (validate):
```go
func (h *SelfImproveHandler) handleValidate(ctx context.Context, params json.RawMessage) (any, error) {
    // ...
    status := ctrl.GetStatus()
    return map[string]any{
        RPCKeyStatus:        status,
        "validations_count": status.ValidationsCount,
    }, nil
}
```

Neither handler calls the corresponding `Controller` phase. The `Controller` only supports running the full cycle via `RunFullCycle()`. There are no standalone `Generate()` or `Validate()` methods on the Controller.

## Root Cause

Same pattern as Bug 0006: the Controller only exposes `Detect()` and `RunFullCycle()`. The intermediate-phase RPC handlers were registered but implemented as status-reporting stubs rather than invoking actual phase logic.

## Fix Applied

Added three methods to `Controller` in `internal/selfimprove/controller.go`:
- `Generate(ctx)` -- runs Phase 3 (generation) standalone, using cached analyses and issues
- `Validate(ctx)` -- runs Phase 4 (validation) standalone, using cached fixes
- `GetCachedFixes()` / `GetCachedValidations()` -- getter helpers for RPC layer

Updated `handleGenerate` and `handleValidate` in `internal/rpc/selfimprove.go` to call `ctrl.Generate(ctx)` and `ctrl.Validate(ctx)` respectively, returning actual fix/validation results instead of status objects.

## Classification

**FIXED** -- standalone `Generate()` and `Validate()` methods added to Controller; RPC handlers now invoke real phase logic instead of returning stub status data.

---
