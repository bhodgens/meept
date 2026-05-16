# Phase 9 Self-Improvement System - QA Summary

- **Date**: 2026-05-16
- **Phase**: Phase 9 (Self-Improvement System)
- **Command**: `/Users/caimlas/go/bin/meept selfimprove <subcommand>`

## Test Results

### Test 1: `selfimprove detect`
- **Before enabling feature**: Error `[-32603] self-improve not enabled`
- **After enabling**: Returns 220 issues, ALL are `usability/low/TODO comment`
- **Detection time**: ~0.9 seconds

### Test 2: `selfimprove analyze`
- **Before enabling**: Error `[-32603] self-improve not enabled`
- **After enabling**: Returns 220 issues + status with `analyses_count: 0`
- The handler re-runs detection but does NOT invoke actual root-cause analysis

### Test 3: `selfimprove full-cycle`
- **Before enabling**: Error `[-32603] self-improve not enabled`
- **After enabling**: Error `call failed after 4 attempts: not connected to daemon`
- Daemon reached phase 2 (analyzing 220 issues) then the RPC connection was killed
- Daemon then received SIGTERM (likely killed by user/automation during long run)

### Test 4: `selfimprove status`
- **Before enabling**: Error `[-32603] self-improve not enabled`
- **After enabling**: Works correctly, shows `issues: 220, analyses: 0, fixes: 0, validations: 0, applied: 0, cycles: 0`

## Issues Found/Confirmed

### Bug 1: RPC writeTimeout (30s) kills long-running selfimprove.cycle calls
**Severity**: High
**File**: `/Users/caimlas/git/meept/internal/rpc/server.go` lines 21-22
The RPC server has `writeTimeout = 30 * time.Second`. A `selfimprove.cycle` call that enters the analysis phase (which requires LLM calls for each issue) will exceed this timeout and the RPC connection is dropped. This makes the full-cycle unusable for any non-trivial codebase. The CLI sets `SetTimeout(10 * time.Minute)` and retries, but the server-side write timeout kills the connection before the response can be sent.

See also: [0056-security-test-003-ssh-key-access.md](0056-security-test-003-ssh-key-access.md) pattern for related RPC issues.

### Bug 2: Detection produces only TODO comments (false positives)
**Severity**: Medium
**File**: `/Users/caimlas/git/meept/internal/selfimprove/detector.go`
All 220 detected issues are `type: usability, severity: low, description: "TODO comment"`. The detector only finds 4 pattern types (TODO, FIXME, HACK, panic). It does not use the richer detection config from the user's `meept.json5` (`scan_pytest`, `scan_runtime_logs`, `scan_type_check`, `scan_lint`). The config fields like `LogFile`, `LogLookbackHours`, `PytestArgs` etc. in `config.DetectionConfig` are never wired to the runtime detector.

### Bug 3: Detection config never mapped from user config to runtime
**Severity**: Medium
**File**: `/Users/caimlas/git/meept/internal/daemon/components.go` lines 365-370
The daemon only maps 3 config values: `Enabled`, `DataPath`, `MaxIterationsPerCycle`, `MaxFixesPerCycle`, and `Safety.RequireHumanApproval`. The entire `Detection` sub-config (with `ScanPytest`, `ScanRuntimeLogs`, `LogFile`, etc.) and the config's `AIInfra`, `Sandbox` fields are never wired. The runtime uses `selfimprove.DefaultConfig()` values for everything else.

### Bug 4: Full-cycle connection drops when daemon is killed mid-analysis
**Severity**: Medium
The daemon received SIGTERM during the full-cycle analysis. No graceful handling of long-running cycles - the cycle state is not checkpointed, so if the daemon restarts, the cycle is lost but the partial results (220 detected issues) are persisted in state.json.

### Pre-existing bugs (confirmed still present):
- Bug 0005: "self-improve not enabled" error with no actionable guidance (0014-selfimprove-harness-bugs.md)
- Bug 0006: handleAnalyze does not run actual analysis (0014-selfimprove-harness-bugs.md)
- Bug 0007: Safety config partially mapped (0014-selfimprove-harness-bugs.md)
- Bug 0008: handleGenerate/handleValidate are status stubs (0014-selfimprove-harness-bugs.md)

## Files Examined

- `/Users/caimlas/git/meept/cmd/meept/selfimprove.go` - CLI commands
- `/Users/caimlas/git/meept/internal/rpc/selfimprove.go` - RPC handlers
- `/Users/caimlas/git/meept/internal/selfimprove/controller.go` - Main controller
- `/Users/caimlas/git/meept/internal/selfimprove/detector.go` - Issue detector
- `/Users/caimlas/git/meept/internal/selfimprove/config.go` - Runtime config
- `/Users/caimlas/git/meept/internal/daemon/components.go` - Daemon wiring
- `/Users/caimlas/git/meept/internal/daemon/daemon.go` - Daemon lifecycle
- `/Users/caimlas/git/meept/internal/rpc/server.go` - RPC server (write timeout)
- `/Users/caimlas/git/meept/internal/config/schema.go` - Config schema
- `/Users/caimlas/.meept/meept.json5` - User configuration
- `/Users/caimlas/git/meept/docs/auto-analysis/0014-selfimprove-harness-bugs.md` - Pre-existing bugs

## New Detail Files Created

- [0056-phase-9-selfimforce-detection-false-positives.md](0056-phase-9-selfimprove-detection-false-positives.md) - TODO-only detection issue
- [0056-phase-9-selfimprove-rpc-write-timeout.md](0056-phase-9-selfimprove-rpc-write-timeout.md) - RPC write timeout kills full-cycle
