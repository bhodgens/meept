# Rehydrate + Resume Interlock - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** The rehydrate.go unit core and its daemon.go wiring: boot-time
  scan of open journal runs, requeue through the EXISTING queue path, and the
  replay interlock with the queue's completion dedup.
- **Dependencies:** 01-journal-schema.md, 02-loop-integration.md
- **Estimated Context:** 85K
- **Concurrency Group:** C
- **Audit references:** master.md Requirements R1, R5, R6, R7

## Goal

Implement `internal/agent/turnjournal/rehydrate.go` (the C3 contract: the
Rehydrate unit over JournalStore + a requeue closure) and wire it into
daemon.go AFTER ResetStaleClaimsAtStartup (daemon.go:2231-2256). The replay
interlock is the heart: a requeued turn resumes through the ordinary pipeline
with its ORIGINAL job ID so the queue's CompleteAttempt/CompletionIsFresh
claim tokens dedup any completion race - this leaf adds no dedup logic of its
own (R6) and no new park path (R5).

Note: the e2e proof of these behaviours is leaf 04. This leaf owns the unit
tests and the daemon wiring tests for the same contracts.

## Context

- Boot order (internal/daemon/daemon.go Run): components.Start (queue +
  workers) -> client transports (RPC + HTTP/WS, deliberately BEFORE the F-D8
  classifier gate so clients can enqueue during slow model loads; boot-window
  submits park on ClassifierBootReadiness, daemon.go:2035-2038) ->
  ResetStaleClaimsAtStartup -> status=running -> gate.
- Rehydrate slots in AFTER the claim reset: reset jobs are pending again, so
  the requeue closure re-enqueues with the same ID and the worker picks them
  up through the ordinary path (where leaf 02's skip seam resumes at the
  next step).
- Best-effort pattern: TurnParker.reArm (parked_turn.go:606-645) - a store
  failure logs and leaves records for the next boot; boot never fails.

## Verified Anchors

<!-- Exact, grep-confirmed at authoring time. No approximate pointers. -->

- internal/daemon/daemon.go:2231-2256 - ResetStaleClaimsAtStartup block;
  the Rehydrate call site goes immediately after it.
- internal/queue/queue.go:728 - PersistentQueue.ResetStaleClaimsAtStartup.
- internal/queue/queue.go:491 CompleteAttempt, :539 CompletionIsFresh -
  the ONLY completion dedup; jobs keep their ID across requeue, which is what
  makes the interlock work.
- internal/agent/parked_turn.go:606-645 - reArm best-effort boot-load pattern.
- internal/agent/parked_turn.go:98-167 - TurnParker: a QuotaResetError during
  a resumed turn parks here (quota_resume path), never short-retries (R4/R5).
- internal/agent/classifier_boot_readiness.go:57 - ClassifierBootReadiness;
  R7: this leaf must not move any transport or gate ordering.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Contract C3 (Resume Hook) + the daemon.go half of C6, exactly as frozen in
../master.md:
internal/agent/turnjournal/rehydrate.go:
  func Rehydrate(ctx context.Context, store JournalStore,
      requeue func(turnID, sessionID string, payload []byte) error,
      logger *slog.Logger) (resumed int, err error)
daemon.go: bounded Rehydrate block after daemon.go:2256, flag- and nil-store
  gated; requeue closure submits through the existing enqueue path with the
  SAME job/turn ID; plus the boot-time PruneClosed retention call.
```

### What This Leaf Consumes

- [C1 Journal Record and Store] - `OpenRuns(ctx) ([]OpenRun, error)`, `PruneClosed(ctx, olderThan)`, `Close()` - owning leaf 01.
- [C2 JournalWriter] - the run rows rehydrate reads (run_started with TurnPayload, no run_closed) - owning leaf 02.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Rehydrate unit: list OpenRuns, call requeue per run with (turnID, sessionID, payload), count successes; per-run requeue error -> Warn + continue; store error -> return (0, err); already-pending requeue (no-op enqueue return) counts once and does not double-count | Create: internal/agent/turnjournal/rehydrate.go; Test: internal/agent/turnjournal/rehydrate_test.go | `go test -p 2 ./internal/agent/turnjournal/ -run TestRehydrate -v` | function does not exist | table-driven: mixed open/closed runs -> exact requeue set; failing requeue on run 1 of 3 -> resumed=2; failing store -> err + resumed=0 | Given a seeded store; When Rehydrate runs; Then exactly the open runs requeue with their persisted payloads and the returned count equals successful requeues |
| T2 | daemon.go wiring: after the claim-reset block (daemon.go:2231-2256), when flag on AND store non-nil, call Rehydrate with the real queue-enqueue closure (same job ID); log "turn journal: resumed N interrupted turn(s)" at Info; flag off or nil store = block skipped (no log noise beyond leaf 02's flag-on Info line) | Modify: internal/daemon/daemon.go; Test: internal/daemon/daemon_turnjournal_test.go | `go test -p 2 ./internal/daemon/ -run TestBootRehydrate -v` | call site does not exist | rig test: seeded open run + flag on -> enqueue observed with original ID + Info log once; flag off -> no enqueue, no log; ordering assertion: the Rehydrate call sits textually and temporally after ResetStaleClaimsAtStartup in Run | Given flag on and one open run; When daemon boot reaches the rehydrate block; Then the turn requeues via the existing queue path with its original ID and boot proceeds normally |
| T3 | Replay interlock regression: a requeued turn for a job whose completion was already recorded (completed-fresh at claim-token level) is deduped by the queue, not by journal code; pin with a test at the queue/daemon seam and grep-proof no dedup reimplementation | Test: internal/daemon/daemon_turnjournal_test.go (extend) | `go test -p 2 ./internal/daemon/ -run TestReplayDedupInterlock -v` | interlock not pinned | requeue of an already-completed job: queue rejects the stale completion (not-claimable / not-fresh), exactly one completion surfaces; `grep -rn "CompleteAttempt\|CompletionIsFresh" internal/agent/turnjournal/` returns no implementation copies (only none or comments) | Given a journal run for an already-completed job; When boot requeues and the re-run finishes; Then the queue's claim-token guard yields one completion event and no duplicate delivery |
| T4 | Boot-time retention: invoke PruneClosed (leaf 01) in the same daemon block behind a documented retention const in rehydrate.go; prune failure logs Warn, boot continues; prune never runs when the flag is off | Modify: internal/daemon/daemon.go, internal/agent/turnjournal/rehydrate.go; Test: internal/agent/turnjournal/rehydrate_test.go + internal/daemon/daemon_turnjournal_test.go (extend) | `go test -p 2 ./internal/agent/turnjournal/ ./internal/daemon/ -run TestRehydratePrune -v` | prune not wired at boot | old-closed rows pruned at boot, fresh ones kept; failing store -> Warn only; flag off -> no prune call | Given closed runs straddling the retention cutoff; When boot runs; Then only expired rows are deleted and boot timing is unaffected beyond the op timeout |

**Worked example (canonical row expansion for T1):**

1. Write failing test:

```go
func TestRehydrateRequeuesOpenRuns(t *testing.T) {
    store := newSeededStore(t) // 2 open runs, 1 closed
    var got []string
    resumed, err := turnjournal.Rehydrate(ctx, store,
        func(turnID, sessionID string, payload []byte) error {
            got = append(got, turnID)
            return nil
        }, nil)
    if err != nil { t.Fatal(err) }
    if resumed != 2 || len(got) != 2 {
        t.Fatalf("resumed=%d got=%v", resumed, got)
    }
}
```

2. Verify failure: `go test -p 2 ./internal/agent/turnjournal/ -run TestRehydrate -v`
   Expected: FAIL - Rehydrate undefined.
3. Minimal implementation: OpenRuns -> loop -> requeue -> count.
4. Verify pass: same command. Expected: PASS.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly (C3 signature, C6 call site)
- [ ] All files at exact specified paths
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify (e2e proof belongs to
      leaf 04; no skip-seam changes here)
- [ ] No second park path, no agent-side dedup, no transport/gate reorder
- [ ] gofmt clean; pure ASCII; `go vet ./internal/agent/turnjournal/ ./internal/daemon/` clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing
- [ ] Interface contracts match exactly (C3, C6)
- [ ] Boot ordering preserved (transports -> claim reset -> rehydrate ->
      running -> gate; diff moves nothing above it)
- [ ] Requeued jobs retain their original IDs (dedup interlock intact)
- [ ] Best-effort: no boot failure path from journal errors
- [ ] Conventions followed; no bugs; no predictable IDs; no security issues
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- Requeue must reuse the SAME submission surface chat.submit/queue jobs use -
  if the daemon exposes a single enqueue helper, that is the closure body.
  Hand-rolled turn execution here would bypass the skip seam, the watchdog,
  and the park path.
- Coordination with leaf 04: this leaf's T2/T3 unit tests pin the same
  contracts the e2e scenarios prove end-to-end; if e2e exposes a wiring bug,
  the orchestrator re-dispatches THIS leaf (not 04) for the fix.
