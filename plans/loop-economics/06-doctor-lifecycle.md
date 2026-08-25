# Daemon Lifecycle: doctor / status / shutdown - Implementation Leaf

> Implement ALL tasks via TDD. Do NOT commit. Do NOT read files back.

## Meta
- **Parent:** ../master.md
- **Scope:** meept doctor [--fix], status --json enrichment, graceful shutdown, orphan-child sweep at startup.
- **Deps:** none | **Context:** 60K | **Group:** B

## Goal

Ops surface parity with mature daemons: one command diagnoses a broken install and repairs safe items; status exposes machine-readable vitals; shutdown drains cleanly; children orphaned by a crashed daemon get reaped on next start.

## Context

CLI tree under cmd/meept (follow `meept branch`/`memory` command shapes). Daemon: cmd/meept-daemon + internal/daemon lifecycle. pidfile handling exists (config daemon.pid_file). RPC client exists in cmd/meept for status today — extend, don't fork.

Key files: cmd/meept/*, internal/daemon/*.go startup/shutdown paths, internal/rpc handler registration for new method.

## Interface Contracts (From Parent)

```
RPC additions (registered like existing methods):
  daemon.health -> {ok, checks:[{name,ok,detail}...], version, uptime_s}
     checks: socket-listening, pidfile-fresh, sqlite-integrity(quick_check),
             runtime-processes (llama/MLX procs responding), data-dir-writable,
             config-parse, disk-free (>200MB warn)
  daemon.shutdown {drain_timeout_s?} -> schedules graceful stop; returns {accepted}

meept doctor [--fix]  (client-side where possible; falls back to RPC when daemon up)
  --fix performs ONLY: stale pidfile removal, stale socket file removal,
  orphan-child kill (below). Everything else report-only.

meept status --json   -> includes health block when daemon reachable

Orphan sweep (daemon startup):
  Children spawned by daemon set env MEEPT_DAEMON_CHILD=1 (+parent start-time).
  On boot, scan /proc-style via ps for MEEPT_DAEMON_CHILD processes whose
  recorded parent start-time predates current daemon start AND ppid==1 ->
  SIGTERM, wait 3s, SIGKILL. macOS+Linux via ps parsing; document Windows gap.
```

Shutdown sequence: stop accepting new jobs -> drain running jobs up to drain_timeout (default 30s) -> close listeners/WS -> persist scheduler/journal state hooks if any -> exit 0.

## Tasks
1. Failing tests health-check functions (each check unit-testable w/ injected paths/procs): pass/fail/warn tri-state.
2. Implement checks + RPC handlers + registration.
3. Failing CLI tests where harness exists: doctor output formatting (lowercase), --fix executes only safe repairs (inject fake pidfile).
4. Orphan sweep: factor process-scan behind interface for testability; failing test w/ fake process list incl. edge cases (own children kept, old-tag killed, recent-tag kept).
5. Shutdown drain test: long job + shutdown -> exits within timeout after completion or kills at deadline.
6. Docs: docs/getting-started or workflows ops page section.

## Self-Verification Checklist
- [ ] -race green touched packages
- [ ] doctor never mutates without --fix
- [ ] No kill of non-meept processes (tests prove)
- [ ] Docs updated

## Review Checklist
- [ ] ps parsing robust to locale/format variance (guard rails + skip-on-uncertain)
- [ ] Drain respects context cancellation
- [ ] Conventions per orchestrator

Output: APPROVED or gaps. Notes: keep scope tight — no metrics dashboards here.
