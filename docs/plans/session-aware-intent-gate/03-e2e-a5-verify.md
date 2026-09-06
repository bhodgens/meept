# E2E A5 Verify - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** Verify the full e2e suite with A5 passing on two consecutive provider-available runs; no code changes expected.
- **Dependencies:** 01, 02
- **Estimated Context:** 20K
- **Concurrency Group:** C

## Goal

This is the acceptance leaf. The chain is complete: history recording
(session-continuity 01), DB restore (session-continuity 02), and now
the context-aware gate (leaves 01+02 of this tree). T3's "did the change
get made? where is the file?" must reach the chat agent with full
context and get a real answer referencing hello.txt. Run the suite twice;
both runs green on A5.

## Context

scripts/e2e-naive-user-chat.sh (commit 9bd2c6a4 state) already asserts
A5 strictly: T3 reply must reference hello.txt AND must not be a bare
clarification request ("contains '?' AND does not mention 'hello'" →
FAIL with the continuity-gap reason). No script changes are expected.

Provider reality: agnes free tier 429s intermittently; a T3 SKIP does
not satisfy this leaf — wait 2-5 minutes and re-run until you have two
provider-available runs.

Key files to understand before implementing:
- scripts/e2e-naive-user-chat.sh - the suite; A5 assertion block; --keep flag

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// Verification record only. Expected evidence:
//   - two full-suite outputs, each with:
//       A5 PASS (references hello.txt, not a bare clarification)
//       A1-A4 PASS, A6 PASS
//   - if ANY assertion fails: STOP, report verbatim — the orchestrator
//     decides re-dispatch. Do NOT touch Go code or weaken assertions.
// Owner: 03. Consumers: integration gate / completion report.
```

### What This Leaf Consumes

```
// leaves 01+02 behavior (committed); the e2e script as committed
```

## Tasks

### Task 1: two consecutive green runs

**Objective:** Prove A5 deterministic.

**Files:**
- None modified (verification only).

**Step 1:** `bash scripts/e2e-naive-user-chat.sh --keep` — run 1.
**Step 2:** run 2 (fresh scratch world each time — the script does this).
**Step 3:** if T3 SKIPs (provider), wait 2-5 min, re-run; only
provider-available T3 turns count.
**Step 4:** report both summaries verbatim + the T3 reply text from
each run (from the replies/t3.txt in the kept workdirs).

### Task 2: regression sweep

**Objective:** The fix didn't regress other turns.

**Files:**
- None.

**Step 1:** from the same two runs, confirm A1-A4 and A6 all PASS in
both (T1 must still create the file; T2 must still apply the beep
change or report honestly; T4 artifact summary shape unchanged).

**Step 2:** report any FAIL verbatim with the run's daemon.log path
(--keep preserves it) — STOP, orchestrator decides.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] Two provider-available full runs recorded verbatim
- [ ] A5 PASS in both; no other assertion regressed
- [ ] No files modified (or deviations documented)
- [ ] No scope creep

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Two green runs evidenced with raw summaries
- [ ] T3 replies quoted from both runs
- [ ] No code/assertion changes made
- [ ] No scope creep

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- A4 (file created + path named) is the leaf-03/leaf-04 contract; if IT
  fails while A5 passes, report both — T1's health gates T3's meaning.
- Expected T3 reply shape post-fix: something like "yes — the file was
  created at <path> and the beep change was applied" — the exact wording
  is the model's; the assertions only require the reference and
  non-clarification.
- If the model mentions hello.txt but ALSO asks a clarifying question in
  the same reply, the script's current heuristic FAILs it (contains '?'
  AND mentions hello → the spec's literal reading). If that fires on a
  reasonable answer, STOP and report — the assertion may need a tweak
  the orchestrator should decide on.
