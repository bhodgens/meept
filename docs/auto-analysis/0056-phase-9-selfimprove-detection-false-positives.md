# Detection returns only TODO comments (220 false positives)

- **Date**: 2026-05-16
- **Phase**: Phase 9 (Self-Improvement System)
- **Severity**: Medium
- **Component**: selfimprove/detector

## Description

Running `meept selfimprove detect` on a 220-issue codebase returns 220 issues, **all** of type `usability`, severity `low`, description `TODO comment`. The detector does not find any actual bugs, errors, or performance issues.

## Reproduction

```bash
# Enable self-improve in ~/.meept/meept.json5: "selfimprove": { "enabled": true, ... }
# Restart daemon
meept selfimprove detect
# Result: 220 issues, all "usability/low/TODO comment"
```

## Root Cause

`internal/selfimprove/detector.go` `ScanCode()` (line 151) uses only 4 hardcoded regex patterns:
1. `//\s*TODO:?\s+(.+)` - type: usability, severity: low
2. `//\s*FIXME:?\s+(.+)` - type: error, severity: medium
3. `//\s*HACK:?\s+(.+)` - type: reliability, severity: medium
4. `panic\([^)]+\)` - type: reliability, severity: high

The codebase has many TODO comments but few FIXME/HACK/panic patterns, so the output is dominated by TODOs.

Meanwhile, the user config (`~/.meept/meept.json5`) specifies richer detection options:
```json5
"detection": {
  "scan_pytest": true,
  "scan_runtime_logs": true,
  "scan_type_check": true,
  "scan_lint": true,
  "log_file": "~/.meept/meept.log",
  "log_lookback_hours": 24,
  "pytest_args": ["-v", "--tb=short"],
  ...
}
```

These config fields are defined in `internal/config/schema.go` `DetectionConfig` (lines 798-808) but are **never wired** to the runtime detector. The daemon in `components.go` line 365 uses `selfimprove.DefaultConfig()` which only has `LogPatterns: ["*.log", "logs/*.log"]` and `ErrorPatterns: ["ERROR", "FATAL", "panic:", "exception:"]`. The `ScanPytest`, `ScanRuntimeLogs`, `ScanTypeCheck`, `ScanLint` detection methods don't exist in the runtime code.

## Impact

- Detection is effectively a TODO-comment scanner, not an issue detector
- 220 issues all at "low" severity means no prioritization is possible
- Analysis phase (LLM-based) is invoked on every TODO comment, wasting LLM budget and time (220 LLM calls for a full-cycle)
- Generated fixes would be TODO-removal patches, which are trivial and low-value

## Proposed Fix

1. **Wire detection config**: Map the user-config `DetectionConfig` fields to the runtime `selfimprove.DetectionConfig`. The runtime config needs fields like `LogFile`, `LogLookbackHours`, `ScanPytest`, etc.

2. **Implement detection methods**: The config declares `scan_pytest`, `scan_runtime_logs`, `scan_type_check`, `scan_lint` but the runtime detector only implements basic regex scanning of TODOs. Add actual implementations:
   - `ScanPytest`: Run pytest, parse failures
   - `ScanRuntimeLogs`: Use `LogFile` + `LogLookbackHours` to scan recent daemon/app logs
   - `ScanTypeCheck`: Run mypy, parse errors
   - `ScanLint`: Run ruff, parse warnings

3. **De-duplicate and prioritize**: Group TODO comments by file and deduplicate. Don't create separate issues for every single TODO - create one issue per file or per distinct TODO cluster.

## Detection Config Field Mismatch

| Config field (schema.go) | Runtime field (config.go) | Mapped? |
|--------------------------|---------------------------|---------|
| `ScanPytest` | (none) | No |
| `ScanRuntimeLogs` | (none) | No |
| `ScanTypeCheck` | (none) | No |
| `ScanLint` | (none) | No |
| `LogFile` | (none) - uses default `*.log` | No |
| `LogLookbackHours` | (none) - uses default `5m` slow query threshold | No |
| `PytestArgs` | (none) | No |
| `MypyArgs` | (none) | No |
| `RuffArgs` | (none) | No |

See also: Bug 0007 in 0014-selfimprove-harness-bugs.md (similar config mapping issue for Safety).
