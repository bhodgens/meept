# E2E Continuity Assert - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** Tighten the e2e A5 assertion so cross-turn continuity is enforced, and verify the full suite green twice.
- **Dependencies:** 01, 02 (dispatch only after both are committed)
- **Estimated Context:** 25K
- **Concurrency Group:** B
- **Audit references:** A5 (the one FAIL in the chat-dispatch-ux final live run)

## Goal

A5 currently checks "T3 reply references hello.txt" — the one assertion
that failed and stayed failed. With leaves 01+02 landed, T3's model sees
T1's exchange and the task result, so the reference should appear
reliably. This leaf strengthens A5's check, adds a clarification-request
guard, and proves the suite green across two consecutive runs.

## Context

scripts/e2e-naive-user-chat.sh drives T1 (create hello.txt) → T2 (modify)
→ T3 (status: "did the change get made? where is the file?") → T4
(artifact summary). A5 lives in the assertion functions around the T3
handling; the suite prints PASS/FAIL per assertion with a final summary
and honest SKIP semantics. Leaf 10's commit (95aa327a) is the current
state of the script.

Key files to understand before implementing:
- scripts/e2e-naive-user-chat.sh - assertion functions + T3 section
- docs/plans/session-continuity/master.md - contracts

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// scripts/e2e-naive-user-chat.sh — A5 tightened:
//   PASS requires ALL of:
//     a) T3 reply mentions hello.txt (existing check), AND
//     b) T3 reply is NOT a bare clarification request — heuristic: when
//        the reply contains a question mark AND does not mention hello
//        (the artifact), it is a clarification FAIL with a distinct
//        reason string naming continuity.
//   Plus: a run-twice mode — the script gains no new flag; the ORCHESTRATOR
//   runs it twice and both must pass (documented in the summary output:
//   print a final "A5 continuity: PASS" line only when both a and b hold).
// Owner: C. Consumers: Integration Test Plan step 3.
```

### What This Leaf Consumes

```
// leaves 01+02 behavior (task-path recording + DB restore)
```

## Tasks

### Task 1: A5 tightening

**Objective:** Both continuity conditions asserted.

**Files:**
- Modify: `scripts/e2e-naive-user-chat.sh` (A5 assertion block)

**Step 1: implement** the two-part check with distinct FAIL reasons
("A5: T3 reply does not reference hello.txt" vs "A5: T3 is a bare
clarification request — continuity gap").

**Step 2: verify** — `bash -n scripts/e2e-naive-user-chat.sh`; one full
run; report the summary table. A5 must PASS. If it FAILs on clarification,
STOP and report to the orchestrator — that means leaves 01/02 did not
hold; do not weaken the assertion.

### Task 2: double-run verification

**Objective:** Prove reliability (the old A5 was flaky-pass/flaky-fail).

**Files:**
- None (verification only) — record results in the report.

**Step 1:** run the full script twice (fresh scratch daemon each run);
both runs must show A5 PASS and no new FAILs elsewhere (A1-A4, A6 pass;
F6 silent-absent is fine). Provider-unreachable turns → SKIP semantics
unchanged; a SKIP on T3 means A5 is SKIP too (honest, exit 0 per suite
convention) — that does NOT satisfy Task 2; wait for a provider-available
window and re-run.

**Step 2: report** both run summaries verbatim.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented; two clean full runs recorded
- [ ] Interface contracts satisfied exactly
- [ ] No deviations from spec (or documented below)
- [ ] No scope creep
- [ ] No assertion weakened
- [ ] bash -n clean; script lifecycle unchanged

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] A5 = hello.txt reference AND no-bare-clarification
- [ ] Distinct FAIL reason strings
- [ ] Two consecutive green runs evidenced
- [ ] No scope creep

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- If T3 PASSes but T4 still asks "which model?", note it — T4's wording
  collides with the model-noun; not an assertion target today.
- Keep the suite's honest-SKIP semantics intact.
