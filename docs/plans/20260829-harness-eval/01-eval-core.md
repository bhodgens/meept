# Eval Core - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> Do NOT use read_file on existing source. After writing, do not read back.

## Meta

- **Parent:** ../master.md
- **Scope:** `internal/eval` types, shell oracle, Pass^k, ablation/model-swap helpers, RunRecord JSON.
- **Dependencies:** none
- **Estimated Context:** 55K
- **Concurrency Group:** A

## Goal

A first-class eval package so later leaves can measure harness vs model. Pass^k means K consecutive oracle passes, not one lucky run.

## Context

No `internal/eval` package on disk today. Docs that claim one are stale. Do not put this in `internal/shadow`. Workdir is always an argument; never `os.Getwd()`.

Key files to understand: `pkg/id` for IDs, `internal/employee/gate.go` for shell-run shape (do not import employee from eval — duplicate a tiny exec helper to avoid cycles).

## Interface Contracts (From Parent)

See master C1. This leaf OWNS C1.

### What This Leaf Exposes

```
// internal/eval/record.go, oracle.go, passk.go, harnesshash.go
func NewRun(kind Kind, taskID, modelID string, k int) *RunRecord
func (r *RunRecord) AddAttempt(a Attempt)
func PassK(attempts []Attempt, k int) bool // true iff last k consecutive all Passed; k<=0 invalid
func ShellOracle{}.Check(ctx, workdir) (OracleResult, error)
func HarnessHash(prompt, toolList, gateCommand string) string // sha256 hex
```

### What This Leaf Consumes

none.

## Tasks

### Task 1: RunRecord + PassK

**Files:** Create `internal/eval/record.go`, `internal/eval/passk.go`, `internal/eval/passk_test.go`

Table tests: k=1 single pass; k=3 with fail in middle -> false; k=3 three consecutive pass after a fail -> true only if the LAST three pass; empty attempts -> false; k=0 -> error or false (pick false + document).

### Task 2: ShellOracle

**Files:** Create `internal/eval/oracle.go`, `internal/eval/oracle_test.go`

Command `true` pass; `false` fail; timeout killed; workdir honored (`pwd` matches); reject empty command; never call `os.Getwd()`.

### Task 3: HarnessHash + JSON roundtrip

**Files:** Create `internal/eval/harnesshash.go`, `internal/eval/record_json_test.go`

Same inputs -> same hash. JSON marshal/unmarshal of RunRecord matches C1 field names (snake_case).

## Self-Verification Checklist

- [ ] `go test ./internal/eval/ -race -count=1` pass
- [ ] C1 JSON tags exact
- [ ] No os.Getwd
- [ ] IDs via pkg/id.Generate

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** none

## Review Checklist (For Review Agent)

- [ ] Every Task above is implemented
- [ ] Pass^k is consecutive, not Pass@k
- [ ] Oracle timeout kills the process
- [ ] No import cycle with employee/agent
- [ ] No debug artifacts

Output: APPROVED or gaps.
