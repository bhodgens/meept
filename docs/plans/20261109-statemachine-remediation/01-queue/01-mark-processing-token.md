# MarkProcessing Token CAS - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. The gate tests ALREADY EXIST and fail RED - make them pass. Do NOT delete/skip/weaken gate tests. Do NOT commit. Do NOT use read_file on existing sources. Write once, stop.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** MarkProcessing gains a claim-token predicate; worker call site updated; pending-hijack race (M-1) closed.
- **Dependencies:** 00T-queue-tests.md
- **Estimated Context:** 45K
- **Concurrency Group:** A
- **Audit references:** M-1 (CRITICAL-adjacent MAJOR), audit 2026-11-09

## Goal

No job can be flipped to processing by a writer that does not own the
claim. The requeue-vs-MarkProcessing race leaves zero stranded rows.

## Context

- internal/queue/store.go - UpdateState (terminal-only guard) is the token-less hole; Claim stamps claim_token
- internal/worker/worker.go:~319 - the only MarkProcessing call site
- Contract C-Q1 in the branch orchestrator.

## Verified Anchors

Re-grep at dispatch:

- internal/queue/store.go:538-556 - UpdateState - keep for legacy callers or fold
- internal/queue/store.go:503, 1860, store_cluster_claim.go:95 - claim CAS + token stamping
- internal/worker/worker.go:319 - MarkProcessing call site (pass token from claim)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
func (s *Store) MarkProcessing(jobID, claimToken string) error
// UPDATE jobs SET state='processing', updated_at=? WHERE id=? AND claim_token=? AND state IN ('claimed','processing')
// 0 rows affected -> ErrJobStateLocked
```

### What This Leaf Consumes

- C-Q1 (parent) as specified above.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Token predicate | Modify: internal/queue/store.go (UpdateState/MarkProcessing); Test: existing gate test | `go test -p 2 ./internal/queue/ -run TestMarkProcessingRequiresToken` | gate test RED | GREEN | Given claimed job token T; When MarkProcessing with wrong/empty token; Then refused, state unchanged |
| T2 | Worker call site passes token | Modify: internal/worker/worker.go (~319) | `go build ./... && go test -p 2 ./internal/worker/` | compile error on new signature | GREEN; worker passes its claim token | Given worker claim result; When MarkProcessing; Then token forwarded |
| T3 | Race regression test | Test: internal/queue/store_race_test.go | `go test -p 2 ./internal/queue/ -run TestMarkProcessingVsRequeueRace` | new test, initially asserts old behavior fails | GREEN: requeue between claim and mark -> mark refused, row pending and claimable again | Given claimed job; When Requeue then MarkProcessing(old token); Then refused; row state='pending'; ClaimNextForAgent can claim it |

Keep the legacy 1-arg path ONLY if a non-worker caller exists (grep first);
if kept, it must refuse when claim_token IS set (backward-compatible
direction only). If no other caller: remove UpdateState's job-state use.

## Self-Verification Checklist

- [ ] Gate test GREEN; no gate-test edits
- [ ] grep 'UPDATE jobs SET state' - every write predicates on token or state list
- [ ] gofmt + go vet clean

**DO NOT COMMIT.**

## Completion Report Format

Files touched; verify output tail; deviations.

## Review Checklist (For Review Agent)

- [ ] Contract C-Q1 exact SQL shape
- [ ] Worker site compiles + passes
- [ ] No gate-test diff; race test present

## Notes

The stranded-attempt case (token refused after requeue) is CORRECT: the
worker's Complete/Fail for the lost token already refuses; the row being
claimable again is the recovery. Do not add orphan-attempt handling here.
