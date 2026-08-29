# Trajectory Judge - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> Do NOT use read_file on existing source. After writing, do not read back.

## Meta

- **Parent:** ../master.md
- **Scope:** Judge stored trajectories with oracle + first-error step. Evolver/selfimprove consume judged rows only.
- **Dependencies:** 01-eval-core.md, 06-shadow-capture.md
- **Estimated Context:** 55K
- **Concurrency Group:** D

## Goal

Preserving a log is not learning. Only judged trajectories become lessons.

## Context

Leaf 01 oracles. Shadow store holds turns. Selfimprove learning.go stores patterns. Skill evolver reads learned patterns. Insert the judge between store and evolver.

Key files: `internal/eval/oracle.go`, `internal/shadow/store.go`, `internal/selfimprove/learning.go`, `internal/skills/lifecycle/` evolver input.

## Interface Contracts (From Parent)

C7 TrajectoryJudgment.

```go
// internal/eval/judge.go
type TrajectoryJudgment struct {
    TrajectoryID   string `json:"trajectory_id"`
    Passed         bool   `json:"passed"`
    FirstErrorStep int    `json:"first_error_step"` // 0 if passed; 1-based if failed
    Summary        string `json:"summary"`
    OracleName     string `json:"oracle_name"`
}

func Judge(ctx context.Context, steps []Step, oracle Oracle, workdir string) (TrajectoryJudgment, error)
// FirstErrorStep = index of first tool step whose result failed (exit!=0 or error)
// plus oracle.Check. If oracle fails and no tool failed, FirstErrorStep = len(steps)+1 (outcome-only fail).
```

Evolver/selfimprove: skip rows with no judgment or Passed==false unless the caller asks for failure lessons explicitly (default: only Passed==true for promotion).

### What This Leaf Exposes

Judge() + store of judgments next to eval runs. Hook: learning pipeline queries judgments.

### What This Leaf Consumes

C1 Oracle. Shadow steps after leaf 06.

## Tasks

### Task 1: Judge unit tests

**Files:** Create `internal/eval/judge.go`, `judge_test.go`

Cases: all tools ok + oracle pass; first of three tools fails; oracle fail after clean tools.

### Task 2: Persist judgment on eval run

**Files:** Modify eval run path (internal/eval or CLI service) to attach judgment when a trajectory id is present. If CLI has no trajectory, skip.

### Task 3: Gate evolver input

**Files:** Modify `internal/selfimprove/learning.go` or skills lifecycle query — one filter. Test that an unjudged pattern is not promoted.

## Self-Verification Checklist

- [ ] FirstErrorStep 1-based as specified
- [ ] Unjudged != passed
- [ ] Do NOT commit

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** none

## Review Checklist (For Review Agent)

- [ ] LLM-as-judge is NOT required for v1 (oracle + tool exits only)
- [ ] Failed trajectories do not auto-promote skills
