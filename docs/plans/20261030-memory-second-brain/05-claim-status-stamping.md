# Claim Status In-Place Stamping - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files. Write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Verify then fix the recorded caveat that MarkResolved/RecordReview versioned status writes leak stale (non-current/rejected) claim rows into Search results.
- **Dependencies:** none (concurrent group A with 03)
- **Estimated Context:** 40K
- **Concurrency Group:** A
- **Audit references:** F6 of master Test Gate; R6

## Goal

Versioned status writes mint a NEW row and leave the OLD row visible to
Search, so rejected/superseded claims resurface in recall. The claim-verdict
path already stamps metadata IN PLACE; this leaf (1) probes whether
MarkResolved and RecordReview still leak via a write-through-read-back
throwaway test, (2) if confirmed, converts them to in-place stamping, (3)
pins with unit tests (this is a BUG FIX - unit tests are the correct tier
per repo policy).

## Context

- Authoritative caveat (references/memory-extension-seams.md): "versioned
  status writes mint a NEW row and leave the old row visible to Search, so
  status flips via them leak non-current/rejected claims into queries -
  claim-status transitions must stamp metadata IN PLACE (verdict path does;
  MarkResolved/RecordReview may still have the leak - verify before reusing)."
- Audit-finding root-cause rule: probe the WRITE side first with a
  write-through-real-store roundtrip before touching the reader.
- Claim lifecycle: confirmed/auto/promoted/rejected; SupersedeClaim exists
  (auto cannot supersede confirmed; rev counter + validity windows).
- When REPLACING a helper, grep the old name repo-wide and delete it in the
  same change or lint (unused) blocks the gate.

## Verified Anchors

- internal/memory/epistemic.go - claim lifecycle (MarkResolved, RecordReview,
  SupersedeClaim, PromoteClaim) - grep exact receivers at implementation
  time.
- internal/memory/epistemic_sqlite_store_test.go + epistemic_test.go -
  existing roundtrip patterns for the probe.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// MarkResolved + RecordReview stamp status IN PLACE (same row id, rev
// policy per SupersedeClaim precedent) such that a subsequent Search for
// the claim's text does NOT return the stale pre-transition row as a live
// claim. Behavior contract, pinned by tests; no signature changes required
// (if signatures must change, the dispatch brief carries the frozen delta).
```

### What This Leaf Consumes

- Existing claim store APIs (internal/memory/epistemic.go).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Probe: write-roundtrip leak test for MarkResolved | Create: internal/memory/claim_status_leak_test.go | `go test -p 2 ./internal/memory/ -run TestClaimStatus` | probe documents current behavior (may be GREEN if no leak: record either way) | verdict recorded: LEAK CONFIRMED or NO LEAK, with row dump evidence | Given claim stored; When MarkResolved; When Search returns matches for its text; Then exactly the expected live rows return (assert stale-row absence if leaking) |
| T2 | Probe: same for RecordReview | Modify: internal/memory/claim_status_leak_test.go | same | same | verdict recorded per T1 | Given claim stored; When RecordReview(reject); Then rejected claim not surfaced as live |
| T3 | Fix if leak confirmed (in-place stamping) | Modify: internal/memory/epistemic.go (+ store as needed) | `go test -p 2 ./internal/memory/` | probe shows leak persists | both transitions in-place; probe GREEN; old versioned-write helper (if orphaned) deleted repo-wide | Given the leak probe; When fix lands; Then stale rows never surface; audit/calibration writes unchanged |
| T4 | Repo-wide sweep for the same class | Modify: none unless found (report) | grep review (orchestrator) | - | every status-transition call site stamps in place OR is documented as deliberately versioned | Given all MarkResolved/RecordReview/SupersedeClaim call sites; When audited; Then no other versioned-transition leak exists |

## Self-Verification Checklist

- [ ] Probe tests use the REAL store (temp dir), not fakes
- [ ] T1/T2 verdicts recorded with evidence BEFORE any fix (T3 conditional)
- [ ] If no leak: report that, change nothing, keep the probes as regression
      pins (bug-fix tier, justified)
- [ ] No orphaned helpers left (repo-wide grep after any replacement)
- [ ] gofmt clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched**
2. **Verify command + output** [probe output + row dumps]
3. **Leak verdict:** [CONFIRMED/NO LEAK per transition]
4. **Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Probe-before-fix order respected (report shows probe results first)
- [ ] In-place stamping keeps rev/validity semantics (no history erasure
      beyond the established in-place pattern)
- [ ] Calibration logging (claim_verdicts.jsonl) unchanged - verdicts still
      logged (starving calibration is a regression)
- [ ] No debug artifacts

Output: APPROVED or specific gaps.

## Notes

- "Fix in the SAME session, no delays" (user standing rule): if the probe
  confirms the leak, fix lands in this leaf, not a follow-up.
- If the leak turns out to be at the STORE persistence layer (canonical-keys
  write drops the field readers need), fix at the producer per the root-cause
  rule; do not filter downstream.
