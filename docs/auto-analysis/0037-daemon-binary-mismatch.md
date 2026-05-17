# Daemon Binary Mismatch: Running Version Differs From Development
**Date**: 2026-05-15
**Phase**: 2
**Severity**: high
**Component**: `cmd/meept-daemon`, deployment
**Evaluation Dimension**: robustness

## Description
The running daemon process is an older binary installed at `~/go/bin/meept-daemon` (30MB, built at 17:06), while the development binary at `~/git/meept/bin/meept-daemon` (40MB, built at 23:57) is a different and newer version. The CLI at `~/git/meept/bin/meept` communicates with the older daemon, creating version compatibility issues.

## Reproduction
1. Check running process: `ps aux | grep meept-daemon` shows `/Users/caimlas/go/bin/meept-daemon`
2. Check development binary: `ls -la ~/git/meept/bin/meept-daemon` shows different size and timestamp
3. Compare MD5 hashes: completely different

## Evidence
```
Running daemon:
  Path: /Users/caimlas/go/bin/meept-daemon
  Size: 30864402 (30MB)
  Built: May 15 17:06
  MD5: b2a4e98ee8c36214cdb861096f9f906b
  PID: 80722

Development binary:
  Path: /Users/caimlas/git/meept/bin/meept-daemon
  Size: 40664626 (40MB)
  Built: May 15 23:57
  MD5: b1a88478a7ef54da8c798dd3f9060d81
```

The 10MB size difference suggests significant code changes between versions.

## Root Cause
The daemon was started from the installed path (`~/go/bin/`) rather than the development path (`~/git/meept/bin/`). The `make go-daemon` target likely builds to `bin/` and starts from there, but the launchd plist or manual start used the `go install` path.

## Impact
- Budget bug (0034) may only exist in the older binary
- 91 RPC methods registered vs 98 in the development version
- Default model shows "n/a" in status vs "zai/glm-4.7" in development
- Any fixes made to the development source won't take effect until the correct binary is running

## Proposed Fix
1. Ensure the daemon is always started from the development binary when testing: `~/git/meept/bin/meept-daemon -f`
2. Add version info to the RPC status response (commit hash or build timestamp)
3. Add a binary path check to the CLI that warns if it connects to a daemon at a different path
4. Update `make go-daemon` to explicitly stop any existing daemon before starting from the correct path

## Classification
- Deployment/operational issue
- May be the root cause of multiple observed bugs
- Affects all test results in this phase
