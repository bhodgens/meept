# e2e-boot-hardening — Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree.
> Dispatch implementation per the single leaf, review in-session with the
> main model, commit with explicit paths only (shared repo). Small
> test-infrastructure tree: the gate is the harness's own behavior, so the
> Test Gate section names the verification directly instead of a 00T leaf.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 1 leaf
- **Scope:** Stop the load-dependent e2e boot-timeout flake class observed 4× on 2026-10-25..27 pre-push gates.
- **Repo:** /Users/caimlas/git/meept (branch main)

## Goal

Observed failure class (4 incidents, all during concurrent sibling-session load, load avg 5-8):

| Incident | Suite | Symptom | Isolated re-run |
|---|---|---|---|
| push #1/#3 | auth-multiuser | "multi-user daemon not healthy within 60s" (60.52s) | GREEN 17-19s |
| push #1 | config-reload | harness health dial refused (1.79s into a 30s budget) | GREEN |
| push #2 | output-filters, tui-flows | health/steer timeouts | GREEN |
| push #9 | state-restart | TestProjectBoundSessionRebindsAfterRestart 30.57s; suite 152s | GREEN 117s |

Root causes:
1. **Hard 60s boot health deadline** — `e2e/harness/daemon.go:715`
   (`deadline := time.Now().Add(60 * time.Second)`, Fatalf at :737). Under
   machine load, daemon boot (SQLite opens, runtime registration, listener
   bind) exceeds 60s; the suite fails before the daemon ever gets sick. The
   daemon-startup path is the tightest wait in the harness.
2. **One-shot suite runs** — `scripts/e2e-affected.sh` runs each affected
   suite once; a single transient boot timeout blocks the push even though
   every failing suite passes isolated (verified 4/4 times).
3. Sibling commit 30eef943 already fixed restart+reload flakes in
   auth-multiuser/config-reload tests; this tree covers the REMAINING
   harness-level class (boot deadline + no retry), not those test-level fixes.

## Architecture

Two minimal changes, both hermetic-preserving:
1. Boot health deadline becomes env-tunable with a raised default:
   `MEEPT_E2E_BOOT_TIMEOUT` (duration string, default 120s). Same loop, same
   process-death fast-fails — only the budget grows. 120s default because the
   observed failures clustered at 60-77s under load while healthy boots take
   2-20s: the raise costs nothing on green runs and absorbs load spikes.
2. `scripts/e2e-affected.sh`: one automatic retry of a FAILED suite, loudly
   labeled (`RETRY(flake-check)`), and the retry result decides. A suite red
   twice stays red — the gate still blocks genuinely broken code; only
   run-once transients pass. No change to which suites are selected.

## Interface Contracts

### Contract 1: boot timeout env

```go
// e2e/harness/daemon.go
// e2eBootTimeout() time.Duration — parses MEEPT_E2E_BOOT_TIMEOUT via
// time.ParseDuration; unset/empty/unparseable → 120s default (log the
// parse failure to the daemon log tail via t.Logf, never Fatal).
// Replaces the literal 60 * time.Second at the boot-wait deadline ONLY.
```

### Contract 2: affected-suite retry

```
scripts/e2e-affected.sh — per-suite run becomes:
  run suite; if rc != 0: echo "RETRY(flake-check): <suite> failed (rc=N), re-running once"
  and run it once more; final rc = second run's rc.
Selection logic, -p 2, build-tag handling: UNCHANGED.
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|--------------|--------------|-------------|
| 01 | 01-boot-timeout-retry.md | leaf | none | 40K | A |

## Dispatch Protocol

### Phase 1: Dispatch 01-boot-timeout-retry.md

- Context: full leaf + Contracts 1-2 + "Do NOT commit. Do NOT git add. Shared repo: touch only the 2 named files (+1 new harness test file). No read_file→write_file."
- Review in-session; run the verification block; commit on pass.

## Review Checklist

- [ ] Boot deadline reads MEEPT_E2E_BOOT_TIMEOUT; default 120s; parse failure never fatals
- [ ] Process-death fast-fails in the boot loop UNCHANGED (a dead daemon still fails immediately, not after 120s)
- [ ] e2e-affected.sh retry is exactly one, labeled, and the second rc decides; suite selection untouched
- [ ] shellcheck clean on e2e-affected.sh
- [ ] New harness unit test pins the env parse (default, valid, invalid) — test-infrastructure change, unit test is the appropriate tier (e2e policy exempts harness internals; no new suite needed)
- [ ] `make e2e-fast-area AREA=smoke` GREEN (harness still boots suites)
- [ ] No other harness behavior touched (diff confined to the deadline expression + the new helper + test)

## Coding Conventions

- Go: stdlib only in harness; os.Getenv + time.ParseDuration; helper unexported
- Shell: POSIX-compatible bash matching the existing script style; quote everything
- Test naming mirrors e2e/harness existing test files (check for a *_test.go in harness/; if none exists, create e2e/harness/boot_timeout_test.go with build tag matching the package's convention — VERIFY the harness package's build tags first: if harness has no tag, none; if `e2e`, use it)

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|--------------|
| 01-boot-timeout-retry | PENDING | 0 | |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `go test -count=1 ./e2e/harness/` (or with the package's build tag) GREEN.
2. `make e2e-fast-area AREA=smoke` GREEN.
3. `shellcheck scripts/e2e-affected.sh` clean (or no new findings vs baseline).
4. Dry-run the retry path: temporarily point the script at a suite list including a known-red invocation is NOT required; instead read the diff and confirm rc propagation logic by inspection + one manual `bash -n` syntax check.

## Test Gate

Test-infrastructure tree (the harness IS the test tier): no 00T leaf. The gate
is the new harness unit test (env-parse three-state: default/valid/invalid),
written by the SAME leaf BEFORE the daemon.go change per its task order
(TDD inside the leaf), plus the Integration Test Plan above run by the control
agent. This shape is deliberate and recorded so the N/A is never silent.

| # | Feature (from plan) | Source | Unit tests | E2E tests | Status |
|---|---------------------|--------|-----------|-----------|--------|
| F1 | MEEPT_E2E_BOOT_TIMEOUT parse: default 120s, valid duration honored, invalid → default (no fatal) | 01 leaf Task 1 | e2e/harness boot-timeout test (three-state) | n/a — harness internal | PENDING |
| F2 | Boot wait uses the parsed deadline; process-death fast-fail unchanged | 01 leaf Task 2 | covered by F1 + smoke suite boot | make e2e-fast-area AREA=smoke | PENDING |
| F3 | e2e-affected.sh single labeled retry; second rc decides; selection unchanged | 01 leaf Task 3 | bash -n + shellcheck + diff review | n/a — hook script | PENDING |

Statuses: PENDING | DELTA | VALIDATED

Delta log: (none yet)

## Open Questions

1. Default 120s vs keeping 60s and only adding the retry. Rec: both — the
   observed failures died AT the 60s wall (60.52s), so a retry alone would
   just fail twice at the same wall under sustained load. Impact of 120s:
   genuinely-sick daemons take 60s longer to surface in the harness; acceptable
   because process-death fast-fails still catch real crashes immediately.
2. Retry count 1 vs 2. Rec: 1 — every observed incident passed on the FIRST
   isolated re-run; 2 retries doubles worst-case gate time for no observed
   benefit.

## Notes

- Shared-repo discipline: sibling session actively touches e2e/ (30eef943,
  92b26a72 are recent harness commits) — re-check `git log -- e2e/harness/`
  before dispatch; if the sibling landed an equivalent fix, mark the tree
  SUPERSEDED instead of implementing.
- Do NOT touch .githooks/pre-push-e2e or the MEEPT_E2E_SKIP bypass — the
  gate's strictness is intentional; this tree reduces false reds, not the bar.
