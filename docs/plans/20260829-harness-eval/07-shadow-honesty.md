# Shadow Honesty - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> Do NOT use read_file on existing source. After writing, do not read back.

## Meta

- **Parent:** ../master.md
- **Scope:** Real token cost, dedup threshold used, auto-train dead code quarantined, export-only. Scorer must not treat length as correctness.
- **Dependencies:** none
- **Estimated Context:** 50K
- **Concurrency Group:** A

## Goal

Shadow is a data collector, not a trainer. Docs that say closed-loop training are lies until leaf 15. This leaf makes the code match export-only.

## Context

`internal/shadow/teacher.go` cost bug, `exporter.go` hash-only dedup, `config.go` AutoTrain unused, `scorer.go` length heuristics, `trainer.go` / adapters do not train.

Key files: those plus `cmd/meept/shadow.go`.

## Interface Contracts (From Parent)

C7 export path. Training invocation stays out of meept-daemon.

```go
// Cost = inTokens*inPrice + outTokens*outPrice (per million), not a flat /1000.
// Dedup: if DedupSimilarityThreshold > 0, skip near-duplicates (keep hash short-circuit first).
// AutoTrain: NewManager must NOT start a ticker. Config field remains for sidecar docs but is ignored with a startup warn if true.
```

### What This Leaf Exposes

Honest cost. Threshold-honoring dedup. No auto-train goroutine. Scorer correctness must use an oracle hook if present; otherwise leave a conservative 0.5 and do not add points for length.

### What This Leaf Consumes

Token counts on teacher response (already present per implementation-gaps.md).

## Tasks

### Task 1: Cost uses tokens

**Files:** Modify `internal/shadow/teacher.go` + tests.

### Task 2: Dedup threshold

**Files:** Modify `internal/shadow/exporter.go` + tests. Threshold 0.95 default still works; identical first-100 hash still short-circuits.

### Task 3: Quarantine auto-train

**Files:** Modify `internal/shadow/manager.go` — do not start autoTrain. If `trainer.go` only prints instructions, add a comment `// sidecar only` and a test that Manager.Start does not spawn train.

### Task 4: Scorer length trap

**Files:** Modify `internal/shadow/scorer.go` + tests. A long well-formatted wrong answer must not beat a short oracle-pass answer. If oracle result is attached, it dominates.

## Self-Verification Checklist

- [ ] -race internal/shadow
- [ ] AutoTrain=true does not start a goroutine
- [ ] Do NOT commit

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** none

## Review Checklist (For Review Agent)

- [ ] No unsloth/axolotl/trl invocation
- [ ] Cost formula uses both token counts
- [ ] DedupSimilarityThreshold is referenced
