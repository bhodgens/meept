# E2E Suite: Turn-Journal Kill -9 Resume - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** The hermetic e2e suite e2e/suites/turn-journal/ plus the
  e2e/manifest.json path_map entry: kill -9 mid-turn, restart, assert resume
  at the NEXT STEP and no duplicate side effects.
- **Dependencies:** 01-journal-schema.md, 02-loop-integration.md,
  03-rehydrate-interlock.md
- **Estimated Context:** 90K
- **Concurrency Group:** E
- **Audit references:** master.md Requirements R1, R6, R7, R9

## Goal

The tree's acceptance gate, in the hermetic e2e tier (repo policy: ALL new
feature tests live here - e2e/suites/<name>/ with the e2e build tag + an
e2e/manifest.json path_map entry in the same commit). Four scenarios: resume
at next step, no duplicate completion, quota parks on replay, and the
flag-off legacy control. The suite validates the master's Test Gate matrix
(F1-F7).

## Context

- Harness: e2e/harness/ builds scratch binaries once per test process into
  $TMPDIR, spawns the daemon via its Start path, and provides the fake LLM
  (e2e/harness/fakellm.go) for scripted step responses and invocation
  counting. The detached watchdog reaper (e2e/harness/daemon.go:429) removes
  scratch dirs and kills orphaned daemons even after kill -9 of the test
  process - so a deliberate mid-turn SIGKILL of the daemon under test is
  safe. NEVER spawn the daemon with a raw exec.Command.
- Study e2e/suites/state-restart/ and e2e/suites/daemon-lifecycle/ first:
  they hold the restart, submit-poll, and boot-window idioms to copy.
- The suite reads the journal store directly (read-only SQL over
  <DataDir>/turnjournal.db) for row-shape assertions and reads the queue
  store for job-state assertions - both are local SQLite files in the rig's
  data dir.

## Verified Anchors

<!-- Exact, grep-confirmed at authoring time. No approximate pointers. -->

- e2e/manifest.json - path_map maps source dirs to suites; add
  "internal/agent/turnjournal/": ["turn-journal"] (plus the suite-dir
  self-entry) here; the pre-commit e2e gate blocks a new package dir without
  it.
- e2e/harness/daemon.go:429 - watchdog reaper (kill -9 safe); :766 - stop()
  is SIGTERM-then-SIGKILL; use the daemon handle's kill affordance for the
  mid-turn SIGKILL and a fresh Start for restart.
- e2e/harness/fakellm.go - scripted responses + invocation counting: the
  no-re-invocation assertion reads this counter.
- internal/queue/queue.go:491 CompleteAttempt, :539 CompletionIsFresh - the
  dedup semantics the no-duplicate-side-effects scenario asserts.
- internal/daemon/classifier_boot_gate.go - F-D8 gate; R7: the restart
  scenario keeps client transports before the gate (the restarted daemon must
  accept submits during the boot window exactly as the lifecycle suite
  asserts today).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
e2e/suites/turn-journal/ - suite package (build tag e2e), scenarios T2-T5;
e2e/manifest.json - path_map entry for internal/agent/turnjournal/.
On pass, this suite is the evidence that marks master Test Gate rows
F1-F7 VALIDATED (F2 via the flag-off control, F4 via the call counter,
F6 via the quota-park scenario, F7 via the dedup scenario).
```

### What This Leaf Consumes

- [C1 Journal Record and Store] - on-disk turnjournal.db row shapes - owning leaf 01.
- [C2 JournalWriter] - expected row sequence per turn (run_started, N x (step_started, step_completed), run_closed) - owning leaf 02.
- [C3 Resume Hook] - boot rehydrate exercised by restart scenarios - owning leaf 03.
- [C4 Skipped-Step Seam] - skip/restore visible as absent fake-LLM invocations - owning leaf 02.
- [C5 Config Flag] - the scenario control variable (flag on/off per rig) - owning leaf 01.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Suite skeleton + manifest registration: suite dir with the e2e build tag, one placeholder smoke test (daemon boots with flag on, submits, completes), manifest path_map entries | Create: e2e/suites/turn-journal/main_test.go; Modify: e2e/manifest.json | `make e2e-fast-area AREA=turn-journal` | suite/entry do not exist | suite builds and passes; `scripts/e2e-affected.sh` (or make e2e-affected with a scratch internal/agent/turnjournal change) selects the suite | Given the manifest entry; When e2e-fast-area runs; Then the suite builds under the e2e tag and the smoke scenario passes |
| T2 | Resume-at-next-step: flag on, 3-step scripted turn, SIGKILL after step 1 completes (poll journal rows), restart over the SAME data dir, assert exactly ONE new fake-LLM invocation (step 2), turn completes, journal ends with 3 step_completed + run_closed | e2e/suites/turn-journal/main_test.go | `make e2e-fast-area AREA=turn-journal` (-run TestKill9ResumeMidTurn) | scenario absent | Given kill -9 between steps 1 and 2; When restart + rehydrate; Then steps 0-1 are not re-invoked (counter), step 2 executes live, terminal result carries all three step results | Given a killed daemon mid-turn; When it restarts; Then the resumed turn executes only the unexecuted step and completes successfully |
| T3 | No duplicate side effects: same rig; force the restart edge so the re-run completes while the pre-kill job state is already completed; assert one job ID end-to-end, exactly one terminal completion event on the bus topic, and the queue store showing the stale attempt rejected | e2e/suites/turn-journal/main_test.go (extend) | `make e2e-fast-area AREA=turn-journal` (-run TestKill9NoDuplicateCompletion) | scenario absent | Given a requeued turn re-completing after restart; When its completion races the pre-kill attempt; Then claim-token dedup yields exactly one completion event and one job ID | Given a job completed pre-kill with its journal run still open; When the daemon restarts and the turn re-runs; Then no duplicate completion or delivery occurs |
| T4 | Quota parks on replay: scripted fake LLM returns a quota-shaped error on the resumed step; assert park via the existing quota path (agent.quota_wait surface), zero short-retries, journal run stays open (no run_closed) | e2e/suites/turn-journal/main_test.go (extend) | `make e2e-fast-area AREA=turn-journal` (-run TestReplayQuotaParks) | scenario absent | Given a resumed turn hitting QuotaResetError; When the error surfaces; Then the turn parks on the existing parker wiring and the journal retains the open run for later resume | Given kill -9 + restart + quota error on the next step; When the turn processes; Then park recorded, no short-retry, run open |
| T5 | Flag-off legacy control: identical rig, flag false; kill -9 mid-turn; restart; assert FULL re-run (fake-LLM counter increments for steps 0-1 again), no turnjournal.db in the data dir, no journal rows | e2e/suites/turn-journal/main_test.go (extend) | `make e2e-fast-area AREA=turn-journal` (-run TestFlagOffLegacyRerun) | scenario absent | Given flag false; When kill -9 + restart; Then the job re-runs from step 0 (legacy) and no journal store exists | Given flag off and a kill mid-turn; When the daemon restarts; Then all steps re-execute and turnjournal.db is absent |

**Worked example (canonical row expansion for T2):**

1. Write the scenario first (it gates the tree):

```go
//go:build e2e

func TestKill9ResumeMidTurn(t *testing.T) {
    // rig: flag on, 3 scripted fake-LLM steps, submit, poll journal until
    // step_completed count == 2 for the turn
    // kill: daemon handle SIGKILL (mid step 2)
    // restart: harness Start over the same data dir
    // assert: fake-LLM invocation counter gained EXACTLY one call
    // assert: turn terminal success; journal: 3 step_completed + run_closed
}
```

2. Verify focus: run the scenario - against the finished tree it must pass;
   any failure names the broken contract (report, do not patch other leaves).
3. Implementation: suite code + manifest entry only.
4. Verify pass: `make e2e-fast-area AREA=turn-journal`. Expected: all PASS.

## Self-Verification Checklist

- [ ] All tasks implemented and scenarios passing
- [ ] Interface contracts satisfied (consumes C1-C5; exposes the gate suite)
- [ ] All files at exact specified paths; manifest entry present
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify (contract fixes in leaves
      01/02/05 are REPORTED to the orchestrator, never patched here)
- [ ] Daemon spawned only via the harness; mid-turn kill is a true SIGKILL on
      the daemon handle; no fixed sleeps for readiness (deadline polling)
- [ ] gofmt clean; pure ASCII

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; all scenarios pass under
      `make e2e-fast-area AREA=turn-journal`
- [ ] Manifest path_map entry present for internal/agent/turnjournal/
- [ ] Assertions behavioural: fake-LLM invocation counts, journal row counts,
      queue store state, bus completion events - no implementation snapshots
- [ ] Kill is a genuine mid-turn SIGKILL; restart reuses the same data dir
- [ ] Conventions followed; no flaky sleeps; no predictable IDs; no security
      issues
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- Scenario T3's "force the restart edge": the practical rig is a job that
  completes pre-kill while its journal run was closed only after the kill -
  i.e. kill between the queue CompleteAttempt and the journal run_closed
  write. Poll the journal for the run still open while the queue store shows
  completed, then restart. If that window is not hittable deterministically,
  assert the equivalent at the seam the harness exposes and DOCUMENT the
  deviation in the completion report.
- Keep per-scenario rigs small: one helper building (flag on|off, script,
  kill-after-step) covers T2-T5.
- CI: the suite joins `make e2e-fast` via the manifest automatically; no
  Makefile change expected.
