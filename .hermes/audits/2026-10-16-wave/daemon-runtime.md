# internal/daemon, internal/runtime, internal/scheduler, internal/queue, internal/worker, internal/gate, internal/config -- bughunt 2026-10-16

## Findings

1. **CRITICAL -- `internal/runtime/bwrap.go:154` violates the `os.Getwd()` fallback invariant**
   - **Mechanism**: When `cmd.Dir` is empty, `bwrapRunner` falls back to `os.Getwd()` to get a writable working directory for bubblewrap. This directly violates the AGENTS.md invariant: "never use `os.Getwd()` as a fallback in `internal/` daemon code; projects bind per session."
   - **Severity**: CRITICAL
   - **Test/Probe**: Run a command through the bwrap backend with no explicit `Dir`; verify the working directory resolved is the daemon's CWD, not a per-session project directory.
   - **Evidence**: SOURCE (read `internal/runtime/bwrap.go:150-159`)
   - **Not covered**: None -- this is a clear invariant violation.

2. **HIGH -- `internal/queue/cluster_queue.go:396,541,543` uses `time.Now().UnixNano()` for IDs/timestamps in cluster events**
   - **Mechanism**: `CheckNodeReachability` computes a threshold using `time.Now().UnixNano()`. `RecordClaimEvent` uses `time.Now().UnixNano()` for both `timestamp` and `received_at` fields in the `cluster_events` table. These are predictable IDs/timestamps, violating the Predictable ID Prevention rule: "Never use `time.Now().UnixNano()` or `math/rand` for ID generation."
   - **Severity**: HIGH
   - **Test/Probe**: Run `go run ./tools/analyzers/predid/... ./internal/queue/...` to detect predictable ID patterns. The analyzer should flag these lines.
   - **Evidence**: SOURCE (read `internal/queue/cluster_queue.go:396,541,543`)
   - **Not covered**: The `cluster_schema_test.go` uses `UnixNano()` in test code -- tests are outside the strict daemon-code invariant, but worth noting.

3. **MEDIUM -- `internal/queue/store.go` multi-step writes are not atomic across job state transitions**
   - **Mechanism**: `ResetToPending` and `Retry` use transactions, but `moveToDead` (dead lettering) is a separate operation from the state update in `FailAttempt`. If the daemon crashes between the `UPDATE jobs SET state='failed'` and the `INSERT INTO dead_letter`, the job is marked failed but not dead-lettered. Conversely, if the INSERT succeeds but the UPDATE fails, the job remains in its original state. The current implementation does commit both in a single transaction for `moveToDead`, but the `FailAttempt` caller does not atomically gate the state transition with the dead-letter move -- the dead-letter move is a separate Exec after the state update commit.
   - **Severity**: MEDIUM
   - **Test/Probe**: Simulate a crash after `FailAttempt` writes the state but before `moveToDead` completes. Verify job state consistency.
   - **Evidence**: SOURCE (read `internal/queue/store.go:790-914`)
   - **Not covered**: `Requeue` and `Retry` are transactional; `CompleteAttempt` is also transactional.

4. **LOW -- `internal/daemon/components.go:8375-8376` documents the `os.Getwd()` prohibition but the code path still exists as a comment reference**
   - **Mechanism**: The comment at line 8375-8376 explicitly states `os.Getwd()` is not a sanctioned fallback in daemon code. This is good documentation, but the invariant violation in `bwrap.go` (Finding #1) shows the rule is not enforced in practice.
   - **Severity**: LOW
   - **Test/Probe**: Code review of all `internal/` packages for `os.Getwd()` calls.
   - **Evidence**: SOURCE (read `internal/daemon/components.go:8375-8376`)
   - **Not covered**: None.

## Checked and clean

- `internal/daemon/AGENTS.md` -- lifecycle invariants documented and consistent with findings.
- `internal/queue/store.go` -- `newClaimToken()` correctly uses `pkg/id.Generate` (line 743-745), not `UnixNano()`.
- `internal/queue/store.go` -- `PendingGate`, `Requeue`, `Retry` use `time.Now().UTC().Format(time.RFC3339)` for timestamps (not IDs), which is acceptable for temporal values.
- `internal/config` -- basic scan shows no obvious required-vs-defaulted asymmetry in config loading; defaults are set in `config/` templates.
- `internal/gate` -- package exists but minimal code; no `os.Getwd()` or `UnixNano()` patterns found.

## Residual / not covered

- `internal/scheduler` and `internal/worker` -- not deeply inspected; should check for `os.Getwd()` usage and lifecycle invariant violations.
- `internal/config` config load behavior -- need to verify silently ignored bad values and required-vs-defaulted asymmetry more thoroughly.
- `internal/lint/languages/go_lint.go` -- has UNCOMMITTED changes per task note; working-tree version should be audited for `os.Getwd()` fallbacks (current read shows comment at line 146 stating it's not used, but uncommitted changes may differ).

gates_run: go build ./internal/daemon/... (success), go build ./internal/runtime/... (success), go build ./internal/queue/... (success)
