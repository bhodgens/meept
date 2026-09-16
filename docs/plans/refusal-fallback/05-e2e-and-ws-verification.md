# E2E Refusal Fallback Verification - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** End-to-end stub-endpoint tests for the three fallback scenarios.
- **Dependencies:** 03-loop-refusal-branch.md, 04-refusal-observability.md
- **Estimated Context:** 50K
- **Concurrency Group:** C

## Goal

Prove the whole chain — refusal detection, fallback re-dispatch, event,
give-up — against fake OpenAI-compatible endpoints, the same technique the
existing e2e tests use. These are the CI-able acceptance tests for the
feature.

## Context

Meept's tests directory holds e2e-style tests that spin httptest servers
speaking provider wire formats. Search tests/ and internal/ for an existing
httptest + openai-format example to mirror (search_files for
"httptest.NewServer" near "chat/completions").

Key files to understand before implementing:
- tests/ - existing e2e test layout and helpers
- internal/llm/errors_refusal.go - detection (leaf 01)
- internal/agent/loop_refusal.go - fallback (leaf 03)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: tests/refusal_fallback_test.go
// Test package name/style: mirror an existing tests/*_test.go file.

// Scenario A (fallback succeeds):
//   stub endpoint: call 1 (model A) => 200, finish_reason "content_filter"
//                  call 2 (expected model = configured refusal model B)
//                  => 200, normal completion "done"
//   config: refusal_model = "stub/model-b"
//   Assert: turn succeeds, result mentions model-b, exactly 2 calls,
//           bus event with reason "refusal_fallback" observed.

// Scenario B (fallback also refuses):
//   both calls refuse. Assert: error returned (errors.As *llm.RefusalError),
//   exactly 2 calls, no third attempt.

// Scenario C (feature off):
//   refusal_model empty. Single refusing call. Assert: error returned,
//   exactly 1 call, no retry, no event.
```

### What This Leaf Consumes

Everything from leaves 01-04. No new production code unless a scenario
exposes a real bug — a bug is a finding, report it, fix at the owning
leaf's site with the orchestrator's approval.

## Tasks

### Task 1: Stub endpoint helper

**Objective:** A reusable fake provider: scripted sequence of responses
(finish_reason refusal vs success), recording each request's model id.

**Files:**
- Create: `tests/refusal_fallback_test.go`

**Step 1: Write the helper + Scenario A test (failing — feature end not
assembled under test yet, or passing if leaves 01-03 are complete; either
way the test is the deliverable).**

**Step 2: Run:** `go test -p 2 ./tests/ -run TestRefusalFallback -v`
Expected: Scenario A documents the full chain.

**Step 3: Fix-forward** any wiring gap the scenario exposes (with approval;
record deviations).

### Task 2: Scenarios B and C

**Objective:** Give-up and feature-off paths.

**Files:**
- Modify: `tests/refusal_fallback_test.go`

**Step 1: Write both tests.**

**Step 2: Run all three** — `go test -p 2 ./tests/ -run TestRefusalFallback -v`

**Step 3: Fix-forward** as above.

## Self-Verification Checklist

- [ ] All three scenarios pass
- [ ] Call-count assertions present in every scenario
- [ ] Model-id assertions present (A: second call uses model-b; B/C: n/a)
- [ ] Event assertion in Scenario A
- [ ] gofmt clean; `go vet ./tests/` passes

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] All three scenarios implemented and passing
- [ ] Assertions match the contract exactly (counts, models, event, error)
- [ ] Stub endpoint uses the real OpenAI wire format (not a simplified one)
- [ ] No flakiness: no sleeps, no real network, deterministic script order

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- If the tests/ package needs a helper that already exists elsewhere
  (server stubs), reuse it instead of duplicating.
- Run the whole suite once at the end: `go test -p 2 ./tests/ -short`
  (-p 2 is mandatory on macOS; unbounded parallelism exhausts ephemeral ports).
