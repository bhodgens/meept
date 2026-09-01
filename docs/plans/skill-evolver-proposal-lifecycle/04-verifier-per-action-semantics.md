# Verifier per-action gating semantics - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Per-action verifier gating: usage-statistics gate for archive
  proposals, existing content rubric for refine/create.
- **Dependencies:** none
- **Estimated Context:** 50K
- **Concurrency Group:** A

## Goal

Every proposal currently passes the 4-dimension content rubric
(internal/skills/lifecycle/verifier.go: min score 0.75 over
grounded_in_evidence / preserves_existing_value / reusable_elsewhere /
safe_to_publish). Archive proposals carry **empty `CandidateContent`**
(evolver.go:528-533) — there is no content to judge, so the rubric
rubber-stamps and gates nothing. Archive proposals must gate on usage
statistics alone (MinEffectiveness threshold, ≥10 injections — already
computed in `passCPrune`, evolver.go:511); refine/create proposals keep the
content rubric. The verdict records which gate produced it.

## Context

Verified facts (2026-08-31, do not re-derive):

- `internal/skills/lifecycle/verifier.go` — the 4-dim rubric, min 0.75.
- `internal/skills/lifecycle/evolver.go:528-533` — archive proposals are
  built with empty `CandidateContent`.
- `internal/skills/lifecycle/evolver.go:511` — `passCPrune` already computes
  the usage signals (injections ≥ 10, effectiveness ≥ MinEffectiveness).

Key files to understand before implementing:

- `internal/skills/lifecycle/verifier.go` — Verify signature(s), verdict
  type, score fields, and its tests (test style to follow).
- `internal/skills/lifecycle/evolver.go` ~:500-540 — passCPrune's threshold
  logic, the archive-proposal construction (:528-533), and where Verify is
  invoked in the proposal pipeline.
- Config: where MinEffectiveness (and the injection minimum) live — config
  struct or constants; keep thresholds single-sourced.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/skills/lifecycle (verifier.go + evolver.go) — gate dispatch by
// proposal action:
//   archive  -> usage gate ONLY: injections >= 10 AND
//               effectiveness >= MinEffectiveness
//               (thresholds already computed in passCPrune, evolver.go:511 —
//               single source of truth; extract shared constants if
//               duplicated)
//   refine /
//   create   -> existing 4-dim content rubric, min 0.75, unchanged
//               (grounded_in_evidence / preserves_existing_value /
//                reusable_elsewhere / safe_to_publish — verifier.go)
// The verdict records WHICH gate produced it (usage|content) for logs and
// tests. Archive proposals with empty CandidateContent (evolver.go:528-533)
// are never scored by the content rubric.
```

### What This Leaf Consumes

```
// internal/skills/lifecycle/verifier.go — rubric + verdict types
// internal/skills/lifecycle/evolver.go:500-540 — passCPrune thresholds,
//   empty-CandidateContent archive construction, Verify invocation point
```

## Tasks

### Task 1: Extract shared usage thresholds

**Objective:** One source of truth for the archive usage thresholds.

**Files:**
- Modify: verifier.go / evolver.go (whichever hosts the constant) + the
  other as consumer
- Test: existing tests keep passing; add an equality assertion if the
  extraction is non-trivial

**Step 1: Write failing test**

If passCPrune's thresholds (injections ≥ 10, effectiveness ≥
MinEffectiveness) are inline literals duplicated from config/constants,
extract named constants and assert passCPrune and the new archive gate use
the SAME values (behavioral test: a skill at exactly the boundary flips the
same way in both paths).

**Step 2: Run test to verify failure**
**Step 3: Write minimal implementation** (extract; no behavior change)
**Step 4: Run test to verify pass**
Run: `go test ./internal/skills/lifecycle/ -run TestPruneThresholds -v`

### Task 2: Usage gate for archive proposals

**Objective:** Archive proposals are gated by usage stats only.

**Files:**
- Modify: verifier.go (or the gate dispatch site in evolver.go — choose the
  seam that reads cleanest and state it)
- Test: verifier/lifecycle test file

**Step 1: Write failing test**

Table-driven over archive proposals with varied usage stats:

- ≥10 injections AND effectiveness ≥ MinEffectiveness → PASS with verdict
  gate "usage".
- 9 injections, effectiveness high → REJECT, gate "usage", reason states
  injection count below minimum.
- Effectiveness below MinEffectiveness → REJECT, gate "usage", reason
  states effectiveness.
- Empty CandidateContent archive proposal → NEVER touches the content
  rubric (no content-dim scores in the verdict).
- Boundary values exactly at both thresholds → PASS.

**Step 2: Run test to verify failure**

**Step 3: Write minimal implementation**

Dispatch by proposal action at the verify entry. Keep the content rubric
code path byte-identical for refine/create.

**Step 4: Run test to verify pass**
Run: `go test ./internal/skills/lifecycle/ -run TestArchiveUsageGate -v`
Expected: PASS

### Task 3: Verdict records the gate

**Objective:** Verdicts carry which gate decided them.

**Files:**
- Modify: verdict type + both gate paths
- Test: same test file

**Step 1: Write failing test**

- Archive verdict → gate == "usage"; refine verdict → gate == "content";
  create verdict → gate == "content".
- Gate string appears in the verifier's log line / proposal record if such
  logging exists (locate; if none, skip — do not build a new logging
  subsystem).

**Step 2/3/4: implement and verify per the TDD loop**
Run: `go test ./internal/skills/lifecycle/ -run TestVerifierGate -v`

### Task 4: Refine/create regression

**Objective:** Content rubric behavior is provably unchanged.

**Step 1: Run the existing verifier tests**

Run: `go test ./internal/skills/lifecycle/ -run TestVerif -v -count=1`
Expected: all pre-existing verifier tests pass UNMODIFIED. If any existing
test asserted archive proposals pass the content rubric, update it to the
new usage-gate expectation and call it out in your report — that is a
deliberate behavior change, not a regression.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep — only what the tasks specify
- [ ] Archive proposals never scored by the content rubric
- [ ] Thresholds single-sourced (no duplicated literals)
- [ ] Verdict gate field populated on both paths

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Interface contracts match (gate dispatch by action; thresholds shared
      with passCPrune)
- [ ] Refine/create rubric code path unchanged (diff shows no rubric edits)
- [ ] Boundary-value tests present (exactly-at-threshold passes, exactly-
      below rejects)
- [ ] Existing verifier tests green (modified only where they asserted the
      old rubber-stamp behavior — flagged in the report)
- [ ] No scope creep beyond specified tasks

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- This leaf changes gate BEHAVIOR deliberately: archive proposals that
  rubber-stamped through the content rubric can now be rejected on usage
  stats. Expect test updates where old tests encoded the rubber-stamp.
- Do not touch the rubric's dimension names, weights, or the 0.75 minimum —
  refine/create semantics are frozen by this plan.
- If the verifier turns out to be pure (no stats input at all today), the
  seam is at the evolver's verify invocation: pass usage stats in for
  archive actions. Keep the verifier's content path signature stable.
