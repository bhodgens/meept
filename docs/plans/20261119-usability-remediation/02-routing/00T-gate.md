# 02-ROUTING Test Gate - Implementation Leaf (TESTS ONLY)

> **For the implementing agent:** Write tests ONLY for this branch's four
> implementation leaves. No implementation code. Every test fails RED for a
> missing-implementation reason. Do NOT commit. Do NOT git add. Explore
> with search_files/terminal cat; write once, never read back. Worktree:
> .hermes/worktrees/usability-remediation.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Gate suite for F6-F8 (branch Test Gate matrix)
- **Dependencies:** none
- **Estimated Context:** 65K
- **Concurrency Group:** A
- **Audit references:** W8/W9/W12/W13/W22; audit-routing R1-R8; session-steering A1-A9

## Goal

Pin routing correctness, evidence observability, and continuation BEFORE
implementation. Freeze the p1/p6 probe prompts as regression fixtures
(verbatim - never reword to make routing pass).

## Context

- internal/agent/dispatcher.go ~1775-1796 - git-verb veto gated on
  hasLeadingImperativeVerb (the shape gap); recall matchers run FIRST (F41).
- internal/agent/dispatcher.go:231-234 - ClassificationNotice field, zero
  writers; internal/llm/errors.go:591 ClassificationUserGuidance unused.
- internal/metrics/store.go:315-328 - dispatch_log schema (no turn_id);
  dispatcher.go:4678-4780 RecordDispatch; turn_no COUNT(*)+1 racy.
- internal/rpc/dispatch_trace.go:20 - session_id-only lookup.
- internal/agent/dispatcher.go:1449/2592 - new-task creation; no SaveDraft
  branch for parked planning sessions; plan_draft.go:483 seedDraftFromRequest.
- internal/agent/queue_recovery.go:54/82-114 - loadAndClear deletes rows,
  publishes count only.
- internal/agent/tactical.go:513 - ScheduleReadySteps; semaphore-blocked
  steps skipped at Debug, no retry ticker; acquireSlotLease ~870.
- internal/daemon/components.go:11165-11220 - step-job fallback loop, dir
  injection only in GetForTask success branch.
- internal/agent/dispatcher.go:3689-3698 - singleton workingDir mutation.
- Frozen prompts: /tmp/usability-rig/p1_naive.py + p6_control.py messages.

## Verified Anchors

- internal/agent/dispatcher.go:1777 - hasLeadingImperativeVerb in veto condition
- internal/agent/dispatcher.go:231 - ClassificationNotice field
- internal/rpc/dispatch_trace.go:20 - session.dispatch_trace registration
- internal/agent/queue_recovery.go:109 - DELETE in loadAndClear
- internal/agent/tactical.go:513 - ScheduleReadySteps
- internal/daemon/components.go:11182 - dir injection inside success branch

## Interface Contracts (From Parent)

### What This Leaf Exposes
Gate tests per branch matrix; frozen prompt fixtures under
internal/agent/testdata/usability_prompts.json (verbatim from probes).

### What This Leaf Consumes
- C5 (../master.md) dual-key trace + turn_id + input_summary.
- C1 routing_notice key; C3 queue resolution (continuation tests).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Veto shape-independence pins | Create: internal/agent/dispatcher_veto_test.go; testdata/usability_prompts.json | `go test ./internal/agent/ -run TestGitVetoShapeIndependent -count=1` | "Now change hello.txt..." keeps git verdict today | GREEN after 01 | Given frozen edit prompts (imperative-nonleading, question-shaped, declarative) with git verdict + no git verb; When classify; Then vetoed -> coder/tooluse lane; genuine git prompts ("commit my changes") NOT vetoed |
| T2 | Notice + hint validation pins | Create: internal/agent/dispatcher_notice_test.go | `go test ./internal/agent/ -run 'TestNoticePopulated|TestHintCapabilityFit' -count=1` | ClassificationNotice never assigned; research step hint=code accepted | GREEN after 01 | Given classifier failure; Then notice set from ClassificationUserGuidance; research-shaped step with code hint -> warning + researcher hint or explicit mismatch event |
| T3 | Dual-key trace pins | Create: internal/rpc/dispatch_trace_test.go; internal/metrics/dispatch_log_test.go | `go test ./internal/rpc/ ./internal/metrics/ -run 'TestDualKeyTrace|TestTraceTurnID' -count=1` | session_id primary returns 0; no turn_id column | GREEN after 02 | Given dispatch recorded under conv id; When query by primary session id; Then rows returned with turn_id + non-empty input_summary |
| T4 | Continuation pins | Create: internal/agent/dispatcher_continuation_test.go | `go test ./internal/agent/ -run 'TestDraftRefinementRoute|TestThreadResolveOnce' -count=1` | follow-up mints new task; double thread resolve | GREEN after 03 | Given session with StatePlanning task + plan_draft metadata; When non-command follow-up; Then SaveDraft on SAME task, no new task; thread id resolved once per turn |
| T5 | Boot recovery pins | Create: internal/agent/queue_recovery_test.go | `go test ./internal/agent/ -run TestBootFollowupReenqueue -count=1` | loadAndClear deletes content | GREEN after 03 | Given persisted follow-up rows; When boot recovery; Then messages re-enqueued to session inbox, rows deleted only after injection |
| T6 | Scheduler resilience pins | Create: internal/agent/tactical_retry_test.go; internal/daemon/stepjob_dir_test.go | `go test ./internal/agent/ ./internal/daemon/ -run 'TestBlockedStepRetry|TestStepJobDirBinding|TestNoSingletonDirMutation' -count=1` | blocked steps never retry; fallback dir unbound; singleton mutated | GREEN after 04 | Given semaphore-full wave; When ticker fires; Then steps schedule; step-job fallback binds session dir or refuses; GetOrCreateWired failure never mutates shared loop dir |
| T7 | e2e extensions | Modify: e2e/suites/dispatcher/ (add scenarios); Create: e2e/suites/dispatch-trace/; Modify: e2e/manifest.json | `go test -tags e2e ./e2e/suites/dispatch-trace/... -count=1` | suite absent | GREEN after branch | Given harness stack; When frozen prompts dispatched; Then routing+trace assertions hold |

## Self-Verification Checklist

- [ ] Every test RED for documented reason (record output)
- [ ] Frozen prompts VERBATIM from probes (no rewording)
- [ ] No implementation code; no existing tests weakened
- [ ] manifest rows complete; matrix F6-F8 covered

**DO NOT COMMIT.** Orchestrator handles git after review.

## Completion Report Format

1. Files touched. 2. RED output per task. 3. Deviations.

## Review Checklist (For Review Agent)

- [ ] Tests-only; RED reasons = missing implementation/current defect
- [ ] Both veto directions pinned (over-veto guard)
- [ ] Contract shapes exact (C5 keys, C1 notice)
- [ ] No artifacts / corruption

Output: APPROVED or gaps with file+line.

## Notes

- Mutation discipline: T1 must show the CURRENT git verdict surviving on
  the frozen edit prompt before the fix.
